package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/storage/storagetest"
	"github.com/Bank5y/keine/internal/testutil"
)

func TestS3Store(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Store { return testutil.NewS3Store(t) })
}

func TestS3CopiesAnObjectLargerThanOnePartInParts(t *testing.T) {
	s := testutil.NewS3Store(t)
	storage.SetCopyPartSize(s, 6<<20) // 10 MiB splits evenly into two 5 MiB parts, the S3 minimum
	ctx := context.Background()
	data := bytes.Repeat([]byte("keine-01"), (10<<20)/8)
	if err := s.Put(ctx, "big/video.mp4", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}

	if err := s.Copy(ctx, "big/video.mp4", "copy/video.mp4"); err != nil {
		t.Fatalf("copy in parts: %v", err)
	}

	rc, err := s.Get(ctx, "copy/video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Fatalf("the copy differs: %d bytes, want %d", len(got), len(data))
	}
	if err := s.Copy(ctx, "big/missing.mp4", "copy/missing.mp4"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("a missing source is ErrNotFound, got %v", err)
	}
}
