package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const apiKeyPrefix = "concierge_"

// APIKeyMeta is persisted key metadata (secret is never stored or listed).
type APIKeyMeta struct {
	ID         int64      `json:"id"`
	Prefix     string     `json:"prefix"`
	Label      string     `json:"label,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// CreateAPIKey generates a new secret, stores its hash, and returns metadata plus the plaintext secret once.
func (s *Store) CreateAPIKey(ctx context.Context, userID int64, label string) (*APIKeyMeta, string, error) {
	if s == nil || s.pool == nil {
		return nil, "", pgx.ErrNoRows
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	secret := apiKeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	prefix := secret
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}
	label = strings.TrimSpace(label)
	sum := sha256.Sum256([]byte(secret))

	var m APIKeyMeta
	err := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, token_hash, prefix, label)
		VALUES ($1, $2, $3, $4)
		RETURNING id, prefix, COALESCE(label,''), created_at, last_used_at`,
		userID, sum[:], prefix, nullIfEmpty(label),
	).Scan(&m.ID, &m.Prefix, &m.Label, &m.CreatedAt, &m.LastUsedAt)
	if err != nil {
		return nil, "", err
	}
	return &m, secret, nil
}

// ListAPIKeys returns metadata for keys owned by the user.
func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]APIKeyMeta, error) {
	if s == nil || s.pool == nil {
		return nil, pgx.ErrNoRows
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, prefix, COALESCE(label,''), created_at, last_used_at
		FROM api_keys WHERE user_id = $1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeyMeta
	for rows.Next() {
		var m APIKeyMeta
		if err := rows.Scan(&m.ID, &m.Prefix, &m.Label, &m.CreatedAt, &m.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteAPIKey removes a key row if it belongs to the user.
func (s *Store) DeleteAPIKey(ctx context.Context, userID, keyID int64) error {
	if s == nil || s.pool == nil {
		return pgx.ErrNoRows
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// LookupAPIKey validates a plaintext secret, updates last_used_at, and returns the owning user id and role.
func (s *Store) LookupAPIKey(ctx context.Context, rawSecret string) (userID int64, role string, err error) {
	if s == nil || s.pool == nil {
		return 0, "", pgx.ErrNoRows
	}
	rawSecret = strings.TrimSpace(rawSecret)
	if rawSecret == "" || !strings.HasPrefix(rawSecret, apiKeyPrefix) {
		return 0, "", pgx.ErrNoRows
	}
	sum := sha256.Sum256([]byte(rawSecret))
	err = s.pool.QueryRow(ctx, `
		UPDATE api_keys k
		SET last_used_at = now()
		FROM users u
		WHERE k.token_hash = $1 AND u.id = k.user_id
		RETURNING u.id, u.role`, sum[:],
	).Scan(&userID, &role)
	return userID, role, err
}
