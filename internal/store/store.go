// Package store provides PostgreSQL persistence for users and sessions.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store wraps a connection pool and user/session operations.
type Store struct {
	pool *pgxpool.Pool
}

// Connect opens a pool and returns a Store. Caller must Close on shutdown.
func Connect(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Pool exposes the underlying pool for transactions (optional).
func (s *Store) Pool() *pgxpool.Pool {
	if s == nil {
		return nil
	}
	return s.pool
}

// Migrate applies embedded SQL migrations in lexical order.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := s.pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
	}
	return nil
}

// User is a persisted account row.
type User struct {
	ID                   int64  `json:"id"`
	GoogleSub            string `json:"googleSub"`
	Email                string `json:"email"`
	DisplayName          string `json:"displayName,omitempty"`
	PictureURL           string `json:"pictureUrl,omitempty"`
	Role                 string `json:"role"` // "admin" or "guest"
	MaxPoolBytes         int64  `json:"maxPoolBytes"`
	MaxSingleFileBytes   int64  `json:"maxSingleFileBytes"`
	DailyMaxUploads      int    `json:"dailyMaxUploads"`
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store: nil")
	}
	return s.pool.Ping(ctx)
}

// UpsertGoogleUser inserts or updates a user keyed by google_sub and returns the row.
func (s *Store) UpsertGoogleUser(ctx context.Context, sub, email, displayName, pictureURL string, adminEmails map[string]struct{}) (*User, error) {
	email = strings.TrimSpace(email)
	normalized := strings.ToLower(email)

	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, google_sub, email, COALESCE(display_name,''), COALESCE(picture_url,''), role,
			quota_max_pool_bytes, quota_max_single_file_bytes, quota_daily_max_uploads
		FROM users WHERE google_sub = $1`, sub,
	).Scan(&u.ID, &u.GoogleSub, &u.Email, &u.DisplayName, &u.PictureURL, &u.Role,
		&u.MaxPoolBytes, &u.MaxSingleFileBytes, &u.DailyMaxUploads)
	if err == nil {
		_, err = s.pool.Exec(ctx, `
			UPDATE users SET email = $2, display_name = $3, picture_url = $4,
				last_login_at = now(), updated_at = now()
			WHERE google_sub = $1`,
			sub, email, nullIfEmpty(displayName), nullIfEmpty(pictureURL))
		if err != nil {
			return nil, err
		}
		u.Email = email
		u.DisplayName = displayName
		u.PictureURL = pictureURL
		return s.UserByID(ctx, u.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	role := "guest"
	if _, ok := adminEmails[normalized]; ok {
		role = "admin"
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (google_sub, email, display_name, picture_url, role, last_login_at)
		VALUES ($1, $2, $3, $4, $5, now())
		RETURNING id, google_sub, email, COALESCE(display_name,''), COALESCE(picture_url,''), role,
			quota_max_pool_bytes, quota_max_single_file_bytes, quota_daily_max_uploads`,
		sub, email, nullIfEmpty(displayName), nullIfEmpty(pictureURL), role,
	).Scan(&u.ID, &u.GoogleSub, &u.Email, &u.DisplayName, &u.PictureURL, &u.Role,
		&u.MaxPoolBytes, &u.MaxSingleFileBytes, &u.DailyMaxUploads)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// CreateSession inserts a session and returns the raw token bytes (caller puts in cookie).
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (rawToken []byte, err error) {
	rawToken = make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(rawToken)
	expires := time.Now().UTC().Add(ttl)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`,
		userID, sum[:], expires)
	if err != nil {
		return nil, err
	}
	return rawToken, nil
}

// LookupSession returns user id and role if the session is valid.
func (s *Store) LookupSession(ctx context.Context, rawToken []byte) (userID int64, role string, err error) {
	if len(rawToken) == 0 {
		return 0, "", pgx.ErrNoRows
	}
	sum := sha256.Sum256(rawToken)
	err = s.pool.QueryRow(ctx, `
		SELECT u.id, u.role
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, sum[:],
	).Scan(&userID, &role)
	return userID, role, err
}

// DeleteSession removes a session by raw token.
func (s *Store) DeleteSession(ctx context.Context, rawToken []byte) error {
	if len(rawToken) == 0 {
		return nil
	}
	sum := sha256.Sum256(rawToken)
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, sum[:])
	return err
}

// ListUsers returns all users (admin UI).
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, google_sub, email, COALESCE(display_name,''), COALESCE(picture_url,''), role,
			quota_max_pool_bytes, quota_max_single_file_bytes, quota_daily_max_uploads
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.GoogleSub, &u.Email, &u.DisplayName, &u.PictureURL, &u.Role,
			&u.MaxPoolBytes, &u.MaxSingleFileBytes, &u.DailyMaxUploads); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserRole updates role by user id.
func (s *Store) SetUserRole(ctx context.Context, userID int64, role string) error {
	if role != "admin" && role != "guest" {
		return fmt.Errorf("invalid role")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET role = $2, updated_at = now() WHERE id = $1`, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// CountAdmins returns how many users have admin role.
func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n, err
}

// UserByID loads a user by primary key.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, google_sub, email, COALESCE(display_name,''), COALESCE(picture_url,''), role,
			quota_max_pool_bytes, quota_max_single_file_bytes, quota_daily_max_uploads
		FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.GoogleSub, &u.Email, &u.DisplayName, &u.PictureURL, &u.Role,
		&u.MaxPoolBytes, &u.MaxSingleFileBytes, &u.DailyMaxUploads)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
