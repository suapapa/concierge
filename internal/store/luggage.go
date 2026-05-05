package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LuggageObject is one row in luggage_objects (metadata only; bytes live on disk).
type LuggageObject struct {
	Key             string
	OwnerUserID     int64
	Filename        string
	MimeType        string
	FileSizeBytes   int64
	ExpiresAt       time.Time
	CreatedAt       time.Time
}

// InsertLuggage inserts a new luggage metadata row. Duplicate key returns an error from PostgreSQL.
func (s *Store) InsertLuggage(ctx context.Context, key string, ownerUserID int64, filename, mimeType string, fileSizeBytes int64, expiresAt time.Time) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store: nil")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO luggage_objects (key, owner_user_id, filename, mime_type, file_size_bytes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		key, ownerUserID, filename, mimeType, fileSizeBytes, expiresAt)
	return err
}

// UpsertLuggage inserts or replaces a row (used by yaml→DB backfill).
func (s *Store) UpsertLuggage(ctx context.Context, key string, ownerUserID int64, filename, mimeType string, fileSizeBytes int64, expiresAt time.Time) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store: nil")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO luggage_objects (key, owner_user_id, filename, mime_type, file_size_bytes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (key) DO UPDATE SET
			owner_user_id = EXCLUDED.owner_user_id,
			filename = EXCLUDED.filename,
			mime_type = EXCLUDED.mime_type,
			file_size_bytes = EXCLUDED.file_size_bytes,
			expires_at = EXCLUDED.expires_at`,
		key, ownerUserID, filename, mimeType, fileSizeBytes, expiresAt)
	return err
}

// DeleteLuggageByKey removes the metadata row for a key.
func (s *Store) DeleteLuggageByKey(ctx context.Context, key string) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("store: nil")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM luggage_objects WHERE key = $1`, key)
	return err
}

// GetLuggageByKey loads metadata for a key. Returns (nil, nil) when not found.
func (s *Store) GetLuggageByKey(ctx context.Context, key string) (*LuggageObject, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("store: nil")
	}
	var o LuggageObject
	err := s.pool.QueryRow(ctx, `
		SELECT key, owner_user_id, filename, mime_type, file_size_bytes, expires_at, created_at
		FROM luggage_objects WHERE key = $1`, key,
	).Scan(&o.Key, &o.OwnerUserID, &o.Filename, &o.MimeType, &o.FileSizeBytes, &o.ExpiresAt, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// SumLuggageBytesByOwner returns the sum of file_size_bytes for objects owned by the user.
func (s *Store) SumLuggageBytesByOwner(ctx context.Context, ownerUserID int64) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, fmt.Errorf("store: nil")
	}
	var sum int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(file_size_bytes), 0) FROM luggage_objects WHERE owner_user_id = $1`,
		ownerUserID,
	).Scan(&sum)
	return sum, err
}

// ListLuggageKeysExpiredBefore returns keys with expires_at strictly before before, oldest first, at most limit rows.
func (s *Store) ListLuggageKeysExpiredBefore(ctx context.Context, before time.Time, limit int) ([]string, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("store: nil")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("store: limit must be positive")
	}
	pgxRows, err := s.pool.Query(ctx, `
		SELECT key FROM luggage_objects
		WHERE expires_at < $1
		ORDER BY expires_at ASC
		LIMIT $2`, before.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer pgxRows.Close()
	var keys []string
	for pgxRows.Next() {
		var k string
		if err := pgxRows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, pgxRows.Err()
}

// ListLuggage returns all objects, optionally filtered by owner_user_id.
func (s *Store) ListLuggage(ctx context.Context, filterOwnerID *int64) ([]LuggageObject, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("store: nil")
	}
	var pgxRows pgx.Rows
	var err error
	if filterOwnerID != nil {
		pgxRows, err = s.pool.Query(ctx, `
			SELECT key, owner_user_id, filename, mime_type, file_size_bytes, expires_at, created_at
			FROM luggage_objects WHERE owner_user_id = $1 ORDER BY key`, *filterOwnerID)
	} else {
		pgxRows, err = s.pool.Query(ctx, `
			SELECT key, owner_user_id, filename, mime_type, file_size_bytes, expires_at, created_at
			FROM luggage_objects ORDER BY key`)
	}
	if err != nil {
		return nil, err
	}
	defer pgxRows.Close()
	out := make([]LuggageObject, 0)
	for pgxRows.Next() {
		var o LuggageObject
		if err := pgxRows.Scan(&o.Key, &o.OwnerUserID, &o.Filename, &o.MimeType, &o.FileSizeBytes, &o.ExpiresAt, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, pgxRows.Err()
}
