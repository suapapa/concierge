package luggage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/suapapa/concierge/internal/activerefs"
)

// fakeMeta is an in-memory MetaStore for DB-mode code paths without PostgreSQL.
type fakeMeta struct {
	mu      sync.Mutex
	byKey   map[string]FileInfo
	sizes   map[string]int64
	deleted []string
}

func newFakeMeta() *fakeMeta {
	return &fakeMeta{
		byKey: make(map[string]FileInfo),
		sizes: make(map[string]int64),
	}
}

func (f *fakeMeta) Put(ctx context.Context, key string, info FileInfo, payloadSize int64) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byKey[key] = info
	f.sizes[key] = payloadSize
	return nil
}

func (f *fakeMeta) Get(ctx context.Context, key string) (FileInfo, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.byKey[key]
	if !ok {
		return FileInfo{}, ErrNotFound
	}
	return info, nil
}

func (f *fakeMeta) Delete(ctx context.Context, key string) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byKey, key)
	delete(f.sizes, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeMeta) List(ctx context.Context, filterUserID *int64) ([]KeyStat, int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []KeyStat
	var total int64
	for key, info := range f.byKey {
		if filterUserID != nil && info.OwnerUserID != *filterUserID {
			continue
		}
		sz := f.sizes[key]
		out = append(out, KeyStat{
			Key:         key,
			Filename:    info.Filename,
			MimeType:    info.MimeType,
			OwnerUserID: info.OwnerUserID,
			FileSize:    sz,
			Directory:   "/tmp/" + key,
			ExpiresAt:   info.ExpiresAt,
		})
		total += sz
	}
	return out, total, nil
}

func (f *fakeMeta) ListExpiredKeys(ctx context.Context, before time.Time, limit int) ([]string, error) {
	_ = ctx
	if limit <= 0 {
		return nil, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	type pair struct {
		key string
		exp time.Time
	}
	var pairs []pair
	for key, info := range f.byKey {
		tm, err := time.Parse(time.RFC3339Nano, info.ExpiresAt)
		if err != nil {
			continue
		}
		if tm.UTC().Before(before.UTC()) {
			pairs = append(pairs, pair{key: key, exp: tm.UTC()})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if !pairs[i].exp.Equal(pairs[j].exp) {
			return pairs[i].exp.Before(pairs[j].exp)
		}
		return pairs[i].key < pairs[j].key
	})
	out := make([]string, 0, limit)
	for i := 0; i < len(pairs) && len(out) < limit; i++ {
		out = append(out, pairs[i].key)
	}
	return out, nil
}

func TestService_MetaStore_upload_openget_delete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	meta := newFakeMeta()
	svc := NewService(ctx, dir, 1024, store, WithMetaStore(meta))

	resp, err := svc.Upload(ctx, UploadParams{
		Reader:      bytes.NewReader([]byte("hello")),
		Filename:    "a.txt",
		Size:        5,
		MIMEType:    "text/plain",
		TTL:         time.Hour,
		OwnerUserID: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	infoPath := filepath.Join(dir, resp.Key, "info.yaml")
	if _, err := os.Stat(infoPath); err == nil {
		t.Fatal("expected no info.yaml when using MetaStore")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat info.yaml: %v", err)
	}

	lease, err := svc.OpenGet(ctx, resp.Key)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()

	if err := svc.Delete(ctx, resp.Key); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range meta.deleted {
		if k == resp.Key {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("meta.Delete not recorded for key %q", resp.Key)
	}
}

func TestService_MetaStore_tryExpire_deletesMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	refs := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	meta := newFakeMeta()
	svc := NewService(ctx, dir, 1024, refs, WithMetaStore(meta))

	resp, err := svc.Upload(ctx, UploadParams{
		Reader:   bytes.NewReader([]byte("x")),
		Filename: "f.bin",
		Size:     1,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, resp.Key)
	if !svc.tryExpire(ctx, resp.Key, keyDir) {
		t.Fatal("tryExpire returned false")
	}
	if _, err := os.Stat(keyDir); !os.IsNotExist(err) {
		t.Fatalf("dir: %v", err)
	}
	found := false
	for _, k := range meta.deleted {
		if k == resp.Key {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected meta row deleted on expire")
	}
}

func TestService_MetaStore_SweepExpired_skipsActiveRefs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	refs := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	meta := newFakeMeta()
	svc := NewService(ctx, dir, 1024, refs, WithMetaStore(meta))

	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if err := meta.Put(ctx, "fixedkey000000000000000000000001", FileInfo{
		MimeType:    "text/plain",
		Filename:    "a.txt",
		OwnerUserID: 1,
		ExpiresAt:   past,
	}, 3); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, "fixedkey000000000000000000000001")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "a.txt"), []byte("hi!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := refs.Increment(ctx, "fixedkey000000000000000000000001"); err != nil {
		t.Fatal(err)
	}
	examined, removed, err := svc.SweepExpired(ctx, time.Now().UTC(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 1 || removed != 0 {
		t.Fatalf("examined=%d removed=%d want 1,0", examined, removed)
	}
	if _, err := os.Stat(keyDir); err != nil {
		t.Fatalf("dir should remain: %v", err)
	}
	if err := refs.Decrement(ctx, "fixedkey000000000000000000000001"); err != nil {
		t.Fatal(err)
	}
	examined, removed, err = svc.SweepExpired(ctx, time.Now().UTC(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 1 || removed != 1 {
		t.Fatalf("examined=%d removed=%d want 1,1", examined, removed)
	}
	if _, err := os.Stat(keyDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dir should be gone: %v", err)
	}
}
