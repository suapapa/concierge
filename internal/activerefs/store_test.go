package activerefs

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStore_incrementDecrement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "refs.yaml"), filepath.Join(dir, "refs.lock"))
	ctx := context.Background()

	if err := store.Increment(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Increment(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	n, err := store.Count(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d; want 2", n)
	}
	if err := store.Decrement(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	n, err = store.Count(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d; want 1", n)
	}
	if err := store.Decrement(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	n, err = store.Count(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("count = %d; want 0", n)
	}
}

func TestStore_Read_missingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "missing.yaml"), filepath.Join(dir, "refs.lock"))
	m, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("map = %#v; want empty", m)
	}
}
