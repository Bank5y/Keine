// Package storagetest is the contract every storage.Store implementation
// must satisfy. Each backend runs the same cases, so swapping backends does
// not change behaviour (ADR-0001).
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/storage"
)

// Run runs the contract against stores made by newStore; each case gets a
// fresh, empty store.
func Run(t *testing.T, newStore func(t *testing.T) storage.Store) {
	ctx := context.Background()

	put := func(t *testing.T, s storage.Store, key, body string) {
		t.Helper()
		if err := s.Put(ctx, key, strings.NewReader(body), int64(len(body))); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	read := func(t *testing.T, s storage.Store, key string) (string, error) {
		t.Helper()
		rc, err := s.Get(ctx, key)
		if err != nil {
			return "", err
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		return string(b), err
	}
	expectMissing := func(t *testing.T, s storage.Store, key string) {
		t.Helper()
		if _, err := read(t, s, key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("expected %s to be missing, got %v", key, err)
		}
	}

	t.Run("stored objects read back unchanged", func(t *testing.T) {
		s := newStore(t)
		body := strings.Repeat("课程内容 0123456789\n", 1000)
		put(t, s, "courses/1/讲/01.md", body)
		got, err := read(t, s, "courses/1/讲/01.md")
		if err != nil || got != body {
			t.Fatalf("read back mismatch: err=%v len=%d want %d", err, len(got), len(body))
		}
	})

	t.Run("binary objects read back unchanged", func(t *testing.T) {
		s := newStore(t)
		body := []byte{0, 1, 2, 255, 254, 0, 10, 13}
		if err := s.Put(ctx, "a/b.bin", bytes.NewReader(body), int64(len(body))); err != nil {
			t.Fatal(err)
		}
		got, err := read(t, s, "a/b.bin")
		if err != nil || got != string(body) {
			t.Fatalf("binary mismatch: %v %q", err, got)
		}
	})

	t.Run("missing objects are not found", func(t *testing.T) {
		expectMissing(t, newStore(t), "nope/nothing.md")
	})

	t.Run("putting again replaces the object", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "k/v", "first")
		put(t, s, "k/v", "second")
		if got, _ := read(t, s, "k/v"); got != "second" {
			t.Fatalf("expected replaced object, got %q", got)
		}
	})

	t.Run("delete removes the object and tolerates missing ones", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "k/v", "x")
		if err := s.Delete(ctx, "k/v"); err != nil {
			t.Fatal(err)
		}
		expectMissing(t, s, "k/v")
		if err := s.Delete(ctx, "k/v"); err != nil {
			t.Fatalf("deleting a missing object: %v", err)
		}
	})

	t.Run("delete prefix removes only that prefix", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "courses/1/a.md", "x")
		put(t, s, "courses/1/images/b.png", "x")
		put(t, s, "courses/10/c.md", "keep")
		put(t, s, "courses/2/d.md", "keep")
		if err := s.DeletePrefix(ctx, "courses/1/"); err != nil {
			t.Fatal(err)
		}
		expectMissing(t, s, "courses/1/a.md")
		expectMissing(t, s, "courses/1/images/b.png")
		for _, k := range []string{"courses/10/c.md", "courses/2/d.md"} {
			if got, err := read(t, s, k); err != nil || got != "keep" {
				t.Fatalf("%s should survive: %v %q", k, err, got)
			}
		}
		if err := s.DeletePrefix(ctx, "courses/404/"); err != nil {
			t.Fatalf("deleting an empty prefix: %v", err)
		}
	})

	t.Run("list returns the keys under a prefix in order", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "courses/1/b.md", "x")
		put(t, s, "courses/1/a/z.png", "x")
		put(t, s, "courses/10/c.md", "x")
		got, err := s.List(ctx, "courses/1/")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"courses/1/a/z.png", "courses/1/b.md"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("expected %v, got %v", want, got)
		}
		if got, err := s.List(ctx, "courses/404/"); err != nil || len(got) != 0 {
			t.Fatalf("expected an empty list, got %v %v", got, err)
		}
	})

	t.Run("copy makes the target read back like the source", func(t *testing.T) {
		s := newStore(t)
		body := strings.Repeat("视频片段 0123456789\n", 1000)
		put(t, s, "courses/1/v1/a.md", body)
		if err := s.Copy(ctx, "courses/1/v1/a.md", "courses/1/v2/a.md"); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"courses/1/v1/a.md", "courses/1/v2/a.md"} {
			if got, err := read(t, s, k); err != nil || got != body {
				t.Fatalf("%s mismatch after copy: err=%v len=%d want %d", k, err, len(got), len(body))
			}
		}
	})

	t.Run("copying a missing object is not found", func(t *testing.T) {
		s := newStore(t)
		if err := s.Copy(ctx, "courses/1/v1/nope.md", "courses/1/v2/nope.md"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		expectMissing(t, s, "courses/1/v2/nope.md")
	})

	t.Run("copy replaces an existing target", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "courses/1/v1/a.md", "new")
		put(t, s, "courses/1/v2/a.md", "old")
		if err := s.Copy(ctx, "courses/1/v1/a.md", "courses/1/v2/a.md"); err != nil {
			t.Fatal(err)
		}
		if got, err := read(t, s, "courses/1/v2/a.md"); err != nil || got != "new" {
			t.Fatalf("expected the target replaced, got %q %v", got, err)
		}
	})

	t.Run("the copy outlives changes to the source", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "courses/1/v1/a.md", "kept")
		put(t, s, "courses/1/v1/b.md", "kept")
		for _, k := range []string{"a.md", "b.md"} {
			if err := s.Copy(ctx, "courses/1/v1/"+k, "courses/1/v2/"+k); err != nil {
				t.Fatal(err)
			}
		}
		put(t, s, "courses/1/v1/a.md", "changed")
		if err := s.DeletePrefix(ctx, "courses/1/v1/"); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"courses/1/v2/a.md", "courses/1/v2/b.md"} {
			if got, err := read(t, s, k); err != nil || got != "kept" {
				t.Fatalf("%s should be unaffected, got %q %v", k, got, err)
			}
		}
		got, err := s.List(ctx, "courses/1/")
		if err != nil || strings.Join(got, ",") != "courses/1/v2/a.md,courses/1/v2/b.md" {
			t.Fatalf("expected only the copies left, got %v %v", got, err)
		}
	})

	t.Run("copy rejects unsafe keys", func(t *testing.T) {
		s := newStore(t)
		put(t, s, "k/v", "x")
		for _, k := range []string{"", "/abs", "a/../b", `a\b`, "a/"} {
			if err := s.Copy(ctx, k, "k/w"); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("copy from %q: expected ErrInvalidKey, got %v", k, err)
			}
			if err := s.Copy(ctx, "k/v", k); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("copy to %q: expected ErrInvalidKey, got %v", k, err)
			}
		}
	})

	t.Run("unsafe keys are rejected", func(t *testing.T) {
		s := newStore(t)
		for _, k := range []string{"", "/abs", "a/../b", "../x", "a//b", "a/./b", `a\b`, "a/"} {
			if err := s.Put(ctx, k, strings.NewReader("x"), 1); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("put %q: expected ErrInvalidKey, got %v", k, err)
			}
			if _, err := s.Get(ctx, k); !errors.Is(err, storage.ErrInvalidKey) {
				t.Errorf("get %q: expected ErrInvalidKey, got %v", k, err)
			}
		}
	})
}
