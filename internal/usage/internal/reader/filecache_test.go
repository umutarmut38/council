package reader

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCachedParseHitAndInvalidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	parse := func() (string, error) {
		calls++
		b, err := os.ReadFile(path)
		return string(b), err
	}
	for i := 0; i < 2; i++ {
		v, err := cachedParse(path, path, parse)
		if err != nil || v != "one" {
			t.Fatalf("read %d: %q, %v", i, v, err)
		}
	}
	if calls != 1 {
		t.Fatalf("parse ran %d times for unchanged file, want 1", calls)
	}
	if err := os.WriteFile(path, []byte("one+two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, err := cachedParse(path, path, parse); err != nil || v != "one+two" {
		t.Fatalf("after change: %q, %v", v, err)
	}
	if calls != 2 {
		t.Fatalf("parse ran %d times after size change, want 2", calls)
	}

	// Errors surface and are never cached: the next call re-parses.
	boom := errors.New("boom")
	fails := 0
	failing := func() (string, error) { fails++; return "", boom }
	other := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := cachedParse(other, other, failing); !errors.Is(err, boom) {
			t.Fatalf("want boom, got %v", err)
		}
	}
	if fails != 2 {
		t.Fatalf("failing parse ran %d times, want 2 (errors not cached)", fails)
	}

	// Missing file: stat error surfaces.
	if _, err := cachedParse("gone", filepath.Join(t.TempDir(), "gone"), parse); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}
