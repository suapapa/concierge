package luggage

import "context"

// ActiveRefKeeper tracks in-flight download reference counts per luggage key.
// Production uses PostgreSQL; unit tests use the file-backed implementation in internal/activerefs.
type ActiveRefKeeper interface {
	Read(ctx context.Context) (map[string]int, error)
	Count(ctx context.Context, key string) (int, error)
	Increment(ctx context.Context, key string) error
	Decrement(ctx context.Context, key string) error
	DeleteKey(ctx context.Context, key string) error
}
