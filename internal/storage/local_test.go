package storage_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/storage/storagetest"
)

func TestLocalStore(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Store { return storage.NewLocal(t.TempDir()) })
}

func TestLocalCopyHardLinksOnTheSameFilesystem(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := storage.NewLocal(root)
	if err := s.Put(ctx, "courses/1/v1/a.mp4", strings.NewReader("video"), 5); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy(ctx, "courses/1/v1/a.mp4", "courses/1/v2/a.mp4"); err != nil {
		t.Fatal(err)
	}
	src, err := os.Stat(filepath.Join(root, "courses/1/v1/a.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.Stat(filepath.Join(root, "courses/1/v2/a.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(src, dst) {
		t.Fatal("expected the copy to be a hard link to the source")
	}
}

func TestLocalCopyFallsBackToCopyingAcrossFilesystems(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	other, err := os.MkdirTemp("/dev/shm", "keine-storage-")
	if err != nil {
		t.Skipf("no second filesystem: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	if device(t, root) == device(t, other) {
		t.Skip("/dev/shm is on the same filesystem as the temp dir")
	}
	// courses/2 lives on another filesystem, so a hard link cannot reach it
	if err := os.MkdirAll(filepath.Join(root, "courses"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "courses/2")); err != nil {
		t.Fatal(err)
	}
	s := storage.NewLocal(root)
	if err := s.Put(ctx, "courses/1/a.md", strings.NewReader("正文"), 6); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy(ctx, "courses/1/a.md", "courses/2/a.md"); err != nil {
		t.Fatalf("copy across filesystems: %v", err)
	}
	rc, err := s.Get(ctx, "courses/2/a.md")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if b, _ := io.ReadAll(rc); string(b) != "正文" {
		t.Fatalf("copied content mismatch: %q", b)
	}
	entries, err := os.ReadDir(other)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected only the copy in the target directory, got %v %v", entries, err)
	}
}

func device(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return uint64(st.Sys().(*syscall.Stat_t).Dev)
}
