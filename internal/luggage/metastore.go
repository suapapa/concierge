package luggage

import (
	"context"
	"time"
)

// MetaStore persists object metadata when the process uses PostgreSQL.
// When nil on Service, metadata is read/written as info.yaml next to the payload (tests).
type MetaStore interface {
	Put(ctx context.Context, key string, info FileInfo, payloadSize int64) error
	Get(ctx context.Context, key string) (FileInfo, error)
	Delete(ctx context.Context, key string) error
	// List returns KeyStat entries without ActiveRefs filled; caller merges ref counts.
	List(ctx context.Context, filterUserID *int64) ([]KeyStat, int64, error)
	// ListExpiredKeys returns keys with expires_at strictly before before, ordered by expires_at, capped at limit.
	ListExpiredKeys(ctx context.Context, before time.Time, limit int) ([]string, error)
}
