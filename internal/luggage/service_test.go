package luggage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/suapapa/concierge/internal/activerefs"
)

func TestService_Upload_stat_roundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	svc := NewService(ctx, dir, 1024, store)

	resp, err := svc.Upload(context.Background(), UploadParams{
		Reader:   bytes.NewReader([]byte("hello")),
		Filename: "a.txt",
		Size:     5,
		MIMEType: "text/plain",
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Key == "" {
		t.Fatal("empty key")
	}

	stat, err := svc.Stat(context.Background(), StatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stat.TotalKeys != 1 {
		t.Fatalf("totalKeys=%d", stat.TotalKeys)
	}
	if stat.TotalSize != 5 {
		t.Fatalf("totalSize=%d", stat.TotalSize)
	}
}

func TestService_Upload_rejectsOversizedBody(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	svc := NewService(ctx, dir, 4, store)

	_, err := svc.Upload(context.Background(), UploadParams{
		Reader:   bytes.NewReader([]byte("12345")),
		Filename: "big.txt",
		Size:     0,
		TTL:      time.Minute,
	})
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v; want ErrPayloadTooLarge", err)
	}
}

func TestService_OpenGet_notFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	svc := NewService(ctx, dir, 1024, store)

	_, err := svc.OpenGet(context.Background(), "deadbeefdeadbeefdeadbeefdeadbeef")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v; want ErrNotFound", err)
	}
}

func TestService_tryExpire_removesDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	svc := NewService(ctx, dir, 1024, store)

	resp, err := svc.Upload(context.Background(), UploadParams{
		Reader:   bytes.NewReader([]byte("x")),
		Filename: "f.bin",
		Size:     1,
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, resp.Key)
	if _, err := os.Stat(keyDir); err != nil {
		t.Fatal(err)
	}
	if !svc.tryExpire(context.Background(), resp.Key, keyDir) {
		t.Fatal("tryExpire returned false")
	}
	if _, err := os.Stat(keyDir); !os.IsNotExist(err) {
		t.Fatalf("dir still exists: %v", err)
	}
}

func TestService_Delete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	svc := NewService(ctx, dir, 1024, store)

	resp, err := svc.Upload(context.Background(), UploadParams{
		Reader:        bytes.NewReader([]byte("x")),
		Filename:      "f.bin",
		Size:          1,
		TTL:           time.Hour,
		OwnerUserID:   42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), resp.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadFileInfo(context.Background(), resp.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadFileInfo err = %v; want ErrNotFound", err)
	}
}

func TestService_Stat_filterByOwner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	store := activerefs.NewStore(filepath.Join(dir, "active_refs.yaml"), filepath.Join(dir, "active_refs.lock"))
	svc := NewService(ctx, dir, 1024, store)

	_, err := svc.Upload(context.Background(), UploadParams{
		Reader:        bytes.NewReader([]byte("aa")),
		Filename:      "a.txt",
		Size:          2,
		TTL:           time.Hour,
		OwnerUserID:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Upload(context.Background(), UploadParams{
		Reader:        bytes.NewReader([]byte("b")),
		Filename:      "b.txt",
		Size:          1,
		TTL:           time.Hour,
		OwnerUserID:   2,
	})
	if err != nil {
		t.Fatal(err)
	}

	filter := int64(2)
	stat, err := svc.Stat(context.Background(), StatOptions{FilterUserID: &filter})
	if err != nil {
		t.Fatal(err)
	}
	if stat.TotalKeys != 1 {
		t.Fatalf("TotalKeys=%d want 1", stat.TotalKeys)
	}
	if stat.TotalSize != 1 {
		t.Fatalf("TotalSize=%d want 1", stat.TotalSize)
	}
}
