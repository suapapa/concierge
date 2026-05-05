package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActiveRefDB stores per-key in-flight download counts in PostgreSQL for multi-instance consistency.
type ActiveRefDB struct {
	pool *pgxpool.Pool
}

// ActiveRefDB returns a DB-backed active reference counter. Caller must not use after Store.Close.
func (s *Store) ActiveRefDB() *ActiveRefDB {
	if s == nil || s.pool == nil {
		return nil
	}
	return &ActiveRefDB{pool: s.pool}
}

// Read returns a copy of all keys with a positive reference count.
func (a *ActiveRefDB) Read(ctx context.Context) (map[string]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.pool == nil {
		return nil, fmt.Errorf("store: nil ActiveRefDB")
	}
	rows, err := a.pool.Query(ctx, `SELECT key, ref_count FROM luggage_active_refs WHERE ref_count > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}

// Count returns the active reference count for key (0 if absent).
func (a *ActiveRefDB) Count(ctx context.Context, key string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if a == nil || a.pool == nil {
		return 0, fmt.Errorf("store: nil ActiveRefDB")
	}
	var n int
	err := a.pool.QueryRow(ctx, `SELECT ref_count FROM luggage_active_refs WHERE key = $1`, key).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Increment adds one active reference for key (requires a luggage_objects row for key).
func (a *ActiveRefDB) Increment(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.pool == nil {
		return fmt.Errorf("store: nil ActiveRefDB")
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO luggage_active_refs (key, ref_count) VALUES ($1, 1)
		ON CONFLICT (key) DO UPDATE SET ref_count = luggage_active_refs.ref_count + 1`,
		key)
	return err
}

// Decrement removes one active reference for key (no-op if key is absent).
func (a *ActiveRefDB) Decrement(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.pool == nil {
		return fmt.Errorf("store: nil ActiveRefDB")
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var n int
	err = tx.QueryRow(ctx, `
		UPDATE luggage_active_refs SET ref_count = ref_count - 1
		WHERE key = $1 RETURNING ref_count`, key).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if n <= 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM luggage_active_refs WHERE key = $1`, key); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteKey removes bookkeeping for key regardless of count.
func (a *ActiveRefDB) DeleteKey(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.pool == nil {
		return fmt.Errorf("store: nil ActiveRefDB")
	}
	_, err := a.pool.Exec(ctx, `DELETE FROM luggage_active_refs WHERE key = $1`, key)
	return err
}
