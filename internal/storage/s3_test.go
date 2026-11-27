package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

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

func TestS3Links(t *testing.T) {
	s := testutil.NewS3Store(t)
	ctx := context.Background()
	data := []byte("not really a video")
	if err := s.Put(ctx, "c/1/v.mp4", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}

	presign := s.Linker(storage.DeliverPresign, time.Minute, "")
	u, err := presign(ctx, "c/1/v.mp4")
	if err != nil || u == "" {
		t.Fatalf("presign: %q, %v", u, err)
	}
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(got, data) || res.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("presigned fetch: %d %q %q", res.StatusCode, res.Header.Get("Content-Type"), got)
	}
	if u, err := presign(ctx, "c/1/missing.mp4"); u != "" || err != nil {
		t.Errorf("an object not in the bucket links to %q, %v", u, err)
	}
	if u, _ := presign(ctx, "c/1/list.m3u8"); u != "" {
		t.Errorf("a playlist is presigned: %q", u)
	}

	if u, _ := presign(ctx, "c/1/a.svg"); u != "" {
		t.Errorf("an svg is presigned: %q", u)
	}

	cdn := s.Linker(storage.DeliverCDN, 0, "https://cdn.example/media/")
	if u, _ := cdn(ctx, "c/1/v.mp4"); u != "https://cdn.example/media/c/1/v.mp4" {
		t.Errorf("cdn link %q", u)
	}
	if err := s.Copy(ctx, "c/1/v.mp4", "c/2/v.mp4"); err != nil {
		t.Fatal(err)
	}
	// a CDN serves the stored type, so a copy keeps one
	if info, err := storage.ContentTypeOf(ctx, s, "c/2/v.mp4"); err != nil || info != "video/mp4" {
		t.Errorf("a copy's type is %q, %v", info, err)
	}
	if s.Linker(storage.DeliverProxy, 0, "") != nil {
		t.Error("proxy delivery has a linker")
	}
	if n, err := s.Size(ctx, "c/1/v.mp4"); n != int64(len(data)) || err != nil {
		t.Errorf("size %d, %v", n, err)
	}
}

func TestRoutedStoreOnS3(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Store {
		r := storage.NewRouted(storage.NewLocal(t.TempDir()))
		r.SetRoute(storage.Route{Cloud: testutil.NewS3Store(t), Media: storage.MediaVideoImage})
		return r
	})
}
