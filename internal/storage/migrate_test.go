package storage_test

import (
	"context"
	"testing"

	"github.com/Bank5y/keine/internal/storage"
)

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*storage.Local, *storage.Local) {
		local, cloud := storage.NewLocal(t.TempDir()), storage.NewLocal(t.TempDir())
		put(t, local, "courses/1/v1/a.mp4", "video a")
		put(t, local, "courses/1/v1/b.png", "image b")
		put(t, local, "courses/1/v1/l.md", "lesson")
		put(t, cloud, "courses/2/v1/c.mp4", "video c")
		return local, cloud
	}
	want := storage.MediaVideoImage.Covers

	t.Run("push moves the media to the cloud", func(t *testing.T) {
		local, cloud := setup(t)
		p, err := storage.Migrate(ctx, local, cloud, storage.Push, want, nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 2 || p.Copied != 2 || p.Failed != 0 {
			t.Errorf("progress %+v", p)
		}
		if has(t, local, "courses/1/v1/a.mp4") || read(t, cloud, "courses/1/v1/a.mp4") != "video a" || read(t, cloud, "courses/1/v1/b.png") != "image b" {
			t.Error("the media did not move")
		}
		if !has(t, local, "courses/1/v1/l.md") || has(t, cloud, "courses/1/v1/l.md") {
			t.Error("the lesson moved")
		}
	})

	t.Run("sync copies and keeps the local files", func(t *testing.T) {
		local, cloud := setup(t)
		if _, err := storage.Migrate(ctx, local, cloud, storage.Sync, want, nil); err != nil {
			t.Fatal(err)
		}
		if !has(t, local, "courses/1/v1/a.mp4") || read(t, cloud, "courses/1/v1/a.mp4") != "video a" {
			t.Error("sync did not copy, or removed the local file")
		}
		// a second sync finds everything there already
		p, err := storage.Migrate(ctx, local, cloud, storage.Sync, want, nil)
		if err != nil || p.Copied != 0 || p.Skipped != 2 {
			t.Errorf("second sync %+v, %v", p, err)
		}
	})

	t.Run("push does not trust a same-sized object in the cloud", func(t *testing.T) {
		local, cloud := setup(t)
		put(t, cloud, "courses/1/v1/a.mp4", "VIDEO A") // stale, same size
		if _, err := storage.Migrate(ctx, local, cloud, storage.Push, want, nil); err != nil {
			t.Fatal(err)
		}
		if read(t, cloud, "courses/1/v1/a.mp4") != "video a" {
			t.Error("push deleted the only current copy")
		}
	})

	t.Run("pull copies the cloud back and keeps it", func(t *testing.T) {
		local, cloud := setup(t)
		p, err := storage.Migrate(ctx, local, cloud, storage.Pull, want, nil)
		if err != nil || p.Copied != 1 {
			t.Fatalf("pull %+v, %v", p, err)
		}
		if read(t, local, "courses/2/v1/c.mp4") != "video c" || !has(t, cloud, "courses/2/v1/c.mp4") {
			t.Error("pull did not copy, or removed the cloud object")
		}
	})

	t.Run("progress is reported as it goes", func(t *testing.T) {
		local, cloud := setup(t)
		var seen []int
		_, _ = storage.Migrate(ctx, local, cloud, storage.Sync, want, func(p storage.Progress) { seen = append(seen, p.Done) })
		if len(seen) < 2 || seen[len(seen)-1] != 2 {
			t.Errorf("reports %v", seen)
		}
	})

	t.Run("a cancelled migration stops", func(t *testing.T) {
		local, cloud := setup(t)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := storage.Migrate(cctx, local, cloud, storage.Push, want, nil); err == nil {
			t.Error("no error")
		}
		if !has(t, local, "courses/1/v1/a.mp4") {
			t.Error("a cancelled push removed a file")
		}
	})
}

func TestProbe(t *testing.T) {
	s := storage.NewLocal(t.TempDir())
	res := storage.Probe(context.Background(), s)
	if !res.OK || len(res.Steps) != 3 {
		t.Fatalf("probe %+v", res)
	}
	keys, _ := s.List(context.Background(), ".keine-probe/")
	if len(keys) != 0 {
		t.Errorf("probe left %v", keys)
	}
}
