package storage

import (
	"context"
	"errors"
	"io"
	"path"
	"sort"
	"strings"
	"sync/atomic"
)

// Media says which course files go to the cloud; the rest (lesson text,
// manifests, attachments) always stay on local disk, where the reading
// paths are fast.
type Media string

const (
	MediaVideo      Media = "video" // videos only
	MediaVideoImage Media = "media" // videos and images
)

var videoExts = map[string]bool{".mp4": true, ".m4v": true, ".webm": true, ".ogv": true, ".mov": true, ".mkv": true, ".m3u8": true, ".ts": true, ".m4s": true}
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".svg": true, ".bmp": true, ".ico": true}

// Covers tells whether the key's file goes to the cloud under m.
func (m Media) Covers(key string) bool {
	ext := strings.ToLower(path.Ext(key))
	switch m {
	case MediaVideo:
		return videoExts[ext]
	case MediaVideoImage:
		return videoExts[ext] || imageExts[ext]
	}
	return false
}

func (m Media) Valid() bool { return m == MediaVideo || m == MediaVideoImage }

// Route is where a Routed store sends what Media covers.
type Route struct {
	// Cloud is nil while no object storage is on: everything is local.
	Cloud Store
	Media Media
	// Link gives an address the browser can fetch a cloud object from
	// itself (a presigned or CDN URL), or "" to serve it through the
	// application. Nil always serves it through the application.
	Link func(ctx context.Context, key string) (string, error)
}

// Routed keeps media in the cloud and everything else on local disk. An
// object may be on the other side than its route says (uploaded before the
// cloud was on, kept by a sync, or written by an older deployment that put
// every file in its bucket), so reads look on both sides. The route can be
// changed while the store is in use.
type Routed struct {
	local Store
	route atomic.Pointer[Route]
}

func NewRouted(local Store) *Routed {
	r := &Routed{local: local}
	r.route.Store(&Route{})
	return r
}

func (r *Routed) SetRoute(rt Route) { r.route.Store(&rt) }
func (r *Routed) Route() Route      { return *r.route.Load() }
func (r *Routed) Local() Store      { return r.local }

// sides is where key belongs, then the other side (nil without a cloud).
func (r *Routed) sides(key string) (Store, Store) {
	rt := r.route.Load()
	if rt.Cloud == nil {
		return r.local, nil
	}
	if rt.Media.Covers(key) {
		return rt.Cloud, r.local
	}
	return r.local, rt.Cloud
}

// Put writes to the key's side and drops a copy the other side may hold, so
// a later change of route cannot bring back stale content.
func (r *Routed) Put(ctx context.Context, key string, rd io.Reader, size int64) error {
	to, other := r.sides(key)
	if err := to.Put(ctx, key, rd, size); err != nil {
		return err
	}
	if other != nil {
		_ = other.Delete(ctx, key)
	}
	return nil
}

func (r *Routed) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	first, other := r.sides(key)
	rc, err := first.Get(ctx, key)
	if errors.Is(err, ErrNotFound) && other != nil {
		return other.Get(ctx, key)
	}
	return rc, err
}

func (r *Routed) Delete(ctx context.Context, key string) error {
	first, other := r.sides(key)
	if err := first.Delete(ctx, key); err != nil {
		return err
	}
	if other != nil {
		return other.Delete(ctx, key)
	}
	return nil
}

func (r *Routed) DeletePrefix(ctx context.Context, prefix string) error {
	if err := r.local.DeletePrefix(ctx, prefix); err != nil {
		return err
	}
	if c := r.route.Load().Cloud; c != nil {
		return c.DeletePrefix(ctx, prefix)
	}
	return nil
}

// Copy copies on the side the source is on, so no data crosses sides.
func (r *Routed) Copy(ctx context.Context, src, dst string) error {
	first, other := r.sides(src)
	err := first.Copy(ctx, src, dst)
	if errors.Is(err, ErrNotFound) && other != nil {
		err = other.Copy(ctx, src, dst)
	}
	return err
}

func (r *Routed) List(ctx context.Context, prefix string) ([]string, error) {
	keys, err := r.local.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	c := r.route.Load().Cloud
	if c == nil {
		return keys, nil
	}
	more, err := c.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		seen[k] = true
	}
	for _, k := range more {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Link is the address a browser can fetch key from directly, or "" when
// the application must serve it.
func (r *Routed) Link(ctx context.Context, key string) (string, error) {
	rt := r.route.Load()
	if rt.Cloud == nil || rt.Link == nil || !rt.Media.Covers(key) {
		return "", nil
	}
	return rt.Link(ctx, key)
}
