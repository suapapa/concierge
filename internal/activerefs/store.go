// Package activerefs persists per-key download reference counts on disk with file locking.
package activerefs

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/goccy/go-yaml"
)

// Store is a file-backed map of luggage keys to active reference counts.
type Store struct {
	file     string
	lockFile string
}

// NewStore returns a Store that reads and writes refsFile under an advisory lock on lockFile.
func NewStore(refsFile, lockFile string) *Store {
	return &Store{file: refsFile, lockFile: lockFile}
}

func (s *Store) withFileLock(fn func() error) error {
	lockF, err := os.OpenFile(s.lockFile, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockF.Close()

	if err := syscall.Flock(int(lockF.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire file lock: %w", err)
	}
	defer func() {
		if uerr := syscall.Flock(int(lockF.Fd()), syscall.LOCK_UN); uerr != nil {
			fmt.Fprintf(os.Stderr, "activerefs: unlock: %v\n", uerr)
		}
	}()

	return fn()
}

// Read returns a copy of all reference counts. A missing file yields an empty map.
func (s *Store) Read(ctx context.Context) (map[string]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var refs map[string]int
	err := s.withFileLock(func() error {
		refs = make(map[string]int)
		data, rerr := os.ReadFile(s.file)
		if rerr != nil {
			if os.IsNotExist(rerr) {
				return nil
			}
			return fmt.Errorf("read active refs: %w", rerr)
		}
		if len(data) == 0 {
			return nil
		}
		if uerr := yaml.Unmarshal(data, &refs); uerr != nil {
			return fmt.Errorf("unmarshal active refs: %w", uerr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// Count returns the active reference count for key.
func (s *Store) Count(ctx context.Context, key string) (int, error) {
	refs, err := s.Read(ctx)
	if err != nil {
		return 0, err
	}
	return refs[key], nil
}

// Increment adds one active reference for key.
func (s *Store) Increment(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withFileLock(func() error {
		refs := make(map[string]int)
		data, err := os.ReadFile(s.file)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read active refs: %w", err)
		}
		if len(data) > 0 {
			if uerr := yaml.Unmarshal(data, &refs); uerr != nil {
				return fmt.Errorf("unmarshal active refs: %w", uerr)
			}
		}
		refs[key]++
		return s.writeRefsLocked(refs)
	})
}

// Decrement removes one active reference for key.
func (s *Store) Decrement(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withFileLock(func() error {
		refs := make(map[string]int)
		data, err := os.ReadFile(s.file)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read active refs: %w", err)
		}
		if len(data) > 0 {
			if uerr := yaml.Unmarshal(data, &refs); uerr != nil {
				return fmt.Errorf("unmarshal active refs: %w", uerr)
			}
		}
		if count, ok := refs[key]; ok {
			if count <= 1 {
				delete(refs, key)
			} else {
				refs[key] = count - 1
			}
		}
		return s.writeRefsLocked(refs)
	})
}

// DeleteKey removes key from the store regardless of count.
func (s *Store) DeleteKey(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withFileLock(func() error {
		refs := make(map[string]int)
		data, err := os.ReadFile(s.file)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read active refs: %w", err)
		}
		if len(data) > 0 {
			if uerr := yaml.Unmarshal(data, &refs); uerr != nil {
				return fmt.Errorf("unmarshal active refs: %w", uerr)
			}
		}
		delete(refs, key)
		return s.writeRefsLocked(refs)
	})
}

func (s *Store) writeRefsLocked(refs map[string]int) error {
	data, err := yaml.Marshal(refs)
	if err != nil {
		return fmt.Errorf("marshal active refs: %w", err)
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write active refs temp: %w", err)
	}
	if err := os.Rename(tmp, s.file); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename active refs: %w", err)
	}
	return nil
}
