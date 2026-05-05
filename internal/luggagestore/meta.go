package luggagestore

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/suapapa/concierge/internal/luggage"
	"github.com/suapapa/concierge/internal/store"
)

// Meta implements luggage.MetaStore using PostgreSQL.
type Meta struct {
	Store  *store.Store
	TmpDir string
}

// Put persists metadata for an object whose payload already exists on disk.
func (m *Meta) Put(ctx context.Context, key string, info luggage.FileInfo, payloadSize int64) error {
	if m == nil || m.Store == nil {
		return fmt.Errorf("luggagestore.Meta: nil store")
	}
	exp, err := time.Parse(time.RFC3339Nano, info.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expiresAt: %w", err)
	}
	return m.Store.InsertLuggage(ctx, key, info.OwnerUserID, info.Filename, info.MimeType, payloadSize, exp.UTC())
}

// Get loads metadata for a key.
func (m *Meta) Get(ctx context.Context, key string) (luggage.FileInfo, error) {
	var zero luggage.FileInfo
	if m == nil || m.Store == nil {
		return zero, fmt.Errorf("luggagestore.Meta: nil store")
	}
	row, err := m.Store.GetLuggageByKey(ctx, key)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, luggage.ErrNotFound
	}
	return luggage.FileInfo{
		MimeType:    row.MimeType,
		Filename:    row.Filename,
		OwnerUserID: row.OwnerUserID,
		ExpiresAt:   row.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// Delete removes the metadata row for a key.
func (m *Meta) Delete(ctx context.Context, key string) error {
	if m == nil || m.Store == nil {
		return fmt.Errorf("luggagestore.Meta: nil store")
	}
	return m.Store.DeleteLuggageByKey(ctx, key)
}

// List returns KeyStat rows (without active ref counts) and total payload bytes.
func (m *Meta) List(ctx context.Context, filterOwnerID *int64) ([]luggage.KeyStat, int64, error) {
	if m == nil || m.Store == nil {
		return nil, 0, fmt.Errorf("luggagestore.Meta: nil store")
	}
	rows, err := m.Store.ListLuggage(ctx, filterOwnerID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]luggage.KeyStat, 0, len(rows))
	var total int64
	for _, r := range rows {
		keyDir := filepath.Join(m.TmpDir, r.Key)
		out = append(out, luggage.KeyStat{
			Key:         r.Key,
			Filename:    r.Filename,
			MimeType:    r.MimeType,
			OwnerUserID: r.OwnerUserID,
			FileSize:    r.FileSizeBytes,
			Directory:   keyDir,
			ExpiresAt:   r.ExpiresAt.UTC().Format(time.RFC3339Nano),
		})
		total += r.FileSizeBytes
	}
	return out, total, nil
}

// ListExpiredKeys returns keys with expires_at strictly before before.
func (m *Meta) ListExpiredKeys(ctx context.Context, before time.Time, limit int) ([]string, error) {
	if m == nil || m.Store == nil {
		return nil, fmt.Errorf("luggagestore.Meta: nil store")
	}
	return m.Store.ListLuggageKeysExpiredBefore(ctx, before, limit)
}

var _ luggage.MetaStore = (*Meta)(nil)
