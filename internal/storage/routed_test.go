package storage_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/storage/storagetest"
)

// newRouted is a routed store whose cloud is a second local directory, so
// the routing can be watched without an S3 server.
func newRouted(t *testing.T, media storage.Media) (*storage.Routed, *storage.Local, *storage.Local) {
	local, cloud := storage.NewLocal(t.TempDir()), storage.NewLocal(t.TempDir())
	r := storage.NewRouted(local)
	r.SetRoute(storage.Route{Cloud: cloud, Media: media})
	return r, local, cloud
}

func TestRoutedStore(t *testing.T) {
	for _, m := range []storage.Media{storage.MediaVideo, storage.MediaVideoImage} {
		t.Run(string(m), func(t *testing.T) {
			storagetest.Run(t, func(t *testing.T) storage.Store { r, _, _ := newRouted(t, m); return r })
		})
	}
	t.Run("without a cloud", func(t *testing.T) {
		storagetest.Run(t, func(t *testing.T) storage.Store { return storage.NewRouted(storage.NewLocal(t.TempDir())) })
	})
}

func put(t *testing.T, s storage.Store, key, body string) {
	t.Helper()
	if err := s.Put(context.Background(), key, strings.NewReader(body), int64(len(body))); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

func has(t *testing.T, s storage.Store, key string) bool {
	t.Helper()
	rc, err := s.Get(context.Background(), key)
	if err != nil {
		return false
	}
	rc.Close()
	return true
}

func read(t *testing.T, s storage.Store, key string) string {
	t.Helper()
	rc, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b)
}

func TestRoutedPutsMediaInTheCloudAndTextLocally(t *testing.T) {
	cases := []struct {
		media                storage.Media
		video, image, lesson bool // in the cloud
	}{
		{storage.MediaVideo, true, false, false},
		{storage.MediaVideoImage, true, true, false},
	}
	for _, c := range cases {
		t.Run(string(c.media), func(t *testing.T) {
			r, local, cloud := newRouted(t, c.media)
			for key, want := range map[string]bool{"c/1/v.MP4": c.video, "c/1/a.png": c.image, "c/1/l.md": c.lesson} {
				put(t, r, key, "x")
				if has(t, cloud, key) != want || has(t, local, key) == want {
					t.Errorf("%s: in the cloud %v, want %v", key, has(t, cloud, key), want)
				}
			}
		})
	}
}

func TestRoutedReadsAnObjectFromWhereverItIs(t *testing.T) {
	r, local, cloud := newRouted(t, storage.MediaVideo)
	// a video uploaded before the cloud was on, and a lesson an older
	// deployment kept in its bucket
	put(t, local, "c/1/v.mp4", "old video")
	put(t, cloud, "c/1/l.md", "old lesson")
	if got := read(t, r, "c/1/v.mp4"); got != "old video" {
		t.Errorf("video reads %q", got)
	}
	if got := read(t, r, "c/1/l.md"); got != "old lesson" {
		t.Errorf("lesson reads %q", got)
	}
	keys, _ := r.List(context.Background(), "c/")
	if strings.Join(keys, ",") != "c/1/l.md,c/1/v.mp4" {
		t.Errorf("list %v", keys)
	}
	if err := r.Copy(context.Background(), "c/1/v.mp4", "c/2/v.mp4"); err != nil || read(t, r, "c/2/v.mp4") != "old video" {
		t.Errorf("copy of a local video: %v", err)
	}
	if err := r.DeletePrefix(context.Background(), "c/1/"); err != nil {
		t.Fatal(err)
	}
	if has(t, local, "c/1/v.mp4") || has(t, cloud, "c/1/l.md") {
		t.Error("delete prefix leaves objects on one side")
	}
}

func TestRoutedPutRemovesTheStaleCopyOnTheOtherSide(t *testing.T) {
	r, local, cloud := newRouted(t, storage.MediaVideo)
	put(t, local, "c/1/v.mp4", "kept by a sync")
	put(t, r, "c/1/v.mp4", "new")
	if has(t, local, "c/1/v.mp4") || read(t, cloud, "c/1/v.mp4") != "new" {
		t.Error("the local copy outlived a new upload")
	}
}

func TestRoutedLinksOnlyCloudMedia(t *testing.T) {
	r, _, cloud := newRouted(t, storage.MediaVideo)
	r.SetRoute(storage.Route{Cloud: cloud, Media: storage.MediaVideo, Link: func(_ context.Context, key string) (string, error) {
		return "https://cdn.example/" + key, nil
	}})
	ctx := context.Background()
	if u, _ := r.Link(ctx, "c/1/v.mp4"); u != "https://cdn.example/c/1/v.mp4" {
		t.Errorf("video link %q", u)
	}
	if u, _ := r.Link(ctx, "c/1/a.png"); u != "" {
		t.Errorf("an image outside the policy links to %q", u)
	}
	r.SetRoute(storage.Route{})
	if u, _ := r.Link(ctx, "c/1/v.mp4"); u != "" {
		t.Errorf("without a cloud a video links to %q", u)
	}
}
