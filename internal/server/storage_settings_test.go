package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Bank5y/keine/internal/filestore"
	"github.com/Bank5y/keine/internal/server"
	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/testutil"
)

// routedApp runs on a routed store whose local side the test can look at;
// the bucket it may be pointed at is cfg.
type routedApp struct {
	*app
	local *storage.Local
	cfg   storage.S3Config
	opts  []appOption
}

func newRoutedApp(t *testing.T, defaults func(*filestore.Settings, storage.S3Config), opts ...appOption) *routedApp {
	t.Helper()
	cfg := testutil.NewS3Config(t)
	local := storage.NewLocal(t.TempDir())
	st := filestore.Settings{Enabled: true}
	if defaults != nil {
		defaults(&st, cfg)
	}
	opts = append([]appOption{func(o *server.Options) {
		o.StorageDefaults, o.StorageSecret, o.StorageAttempts = st, cfg.SecretKey, 3
	}}, opts...)
	return &routedApp{app: startApp(t, testutil.NewDatabase(t), storage.NewRouted(local), opts...), local: local, cfg: cfg, opts: opts}
}

// bucketSettings is the console's form for the app's bucket.
func (a *routedApp) bucketSettings(extra map[string]any) map[string]any {
	m := map[string]any{
		"enabled": true, "endpoint": a.cfg.Endpoint, "region": a.cfg.Region, "bucket": a.cfg.Bucket,
		"accessKey": a.cfg.AccessKey, "insecure": true, "pathStyle": true, "createBucket": true,
		"media": "video", "delivery": "proxy",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// bucket opens the app's bucket directly.
func (a *routedApp) bucket() *storage.S3 {
	a.t.Helper()
	cfg := a.cfg
	cfg.CreateBucket = true
	s, err := storage.NewS3(context.Background(), cfg)
	if err != nil {
		a.t.Fatal(err)
	}
	return s
}

func inStore(s storage.Store, key string) bool {
	rc, err := s.Get(context.Background(), key)
	if err != nil {
		return false
	}
	rc.Close()
	return true
}

func mediaCourse(t *testing.T, c *client) int {
	t.Helper()
	m := map[string]any{
		"version": 1, "title": "媒体课", "type": "video",
		"chapters": []any{map[string]any{"id": "c1", "title": "第一章", "lessons": []any{
			map[string]any{"id": "v", "title": "视频", "video": map[string]any{"file": "videos/1.mp4"}, "file": "v.md"},
		}}},
	}
	return uploadCourse(t, c, map[string]string{"course.json": manifestJSON(m), "videos/1.mp4": "0123456789", "v.md": "# 稿\n\n![](images/a.png)\n", "images/a.png": "png-bytes"})
}

// courseKeys are the stored keys of the course's files ending in suffix.
func courseKey(t *testing.T, s storage.Store, id int, suffix string) string {
	t.Helper()
	keys, _ := s.List(context.Background(), fmt.Sprintf("courses/%d/", id))
	for _, k := range keys {
		if strings.HasSuffix(k, suffix) {
			return k
		}
	}
	return ""
}

func TestStorageSettingsStartFromTheEnvironmentWithoutABucket(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	got := admin.mustOK(admin.do("GET", "/api/admin/storage", nil)).json()
	if got["enabled"] != true || got["media"] != "video" || got["delivery"] != "proxy" {
		t.Errorf("defaults %v", got)
	}
	if st := got["status"].(map[string]any); st["connected"] != false || st["active"] != false {
		t.Errorf("status %v", st)
	}
	if _, ok := got["sealedSecret"]; ok {
		t.Error("the sealed secret is shown")
	}
	// with no bucket, media stays local
	id := mediaCourse(t, a.user("alice"))
	if courseKey(t, a.local, id, "videos/1.mp4") == "" {
		t.Error("the video is not on local disk")
	}
	expectStatus(t, a.user("bob").do("GET", "/api/admin/storage", nil), http.StatusForbidden)
}

func TestStorageConnectsToTheEnvironmentsBucketAtStart(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, func(st *filestore.Settings, cfg storage.S3Config) {
		st.Endpoint, st.Bucket, st.AccessKey, st.Region = cfg.Endpoint, cfg.Bucket, cfg.AccessKey, cfg.Region
		st.Insecure, st.PathStyle, st.CreateBucket = true, true, true
	})
	admin := a.admin()
	got := admin.mustOK(admin.do("GET", "/api/admin/storage", nil)).json()
	if st := got["status"].(map[string]any); st["active"] != true {
		t.Fatalf("status %v", st)
	}
	if got["secretSet"] != true || got["secretFromEnv"] != true {
		t.Errorf("secret %v %v", got["secretSet"], got["secretFromEnv"])
	}
	id := mediaCourse(t, a.user("alice"))
	bucket := a.bucket()
	if k := courseKey(t, bucket, id, "videos/1.mp4"); k == "" || inStore(a.local, k) {
		t.Errorf("the video is not (only) in the bucket: %q", k)
	}
	if k := courseKey(t, a.local, id, "images/a.png"); k == "" {
		t.Error("an image left local disk under 仅视频")
	}
}

func TestStorageConnectionTestWritesReadsAndDeletes(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	res := admin.mustOK(admin.do("POST", "/api/admin/storage/test", a.bucketSettings(nil))).json()
	steps, _ := res["steps"].([]any)
	if res["ok"] != true || len(steps) != 4 {
		t.Fatalf("test %v", res)
	}
	bad := admin.mustOK(admin.do("POST", "/api/admin/storage/test", a.bucketSettings(map[string]any{"accessKey": "wrong"}))).json()
	if bad["ok"] == true {
		t.Errorf("a wrong key passes: %v", bad)
	}
	// testing saves nothing
	got := admin.mustOK(admin.do("GET", "/api/admin/storage", nil)).json()
	if got["endpoint"] != "" {
		t.Errorf("the test saved %v", got["endpoint"])
	}
}

func TestStorageSettingsSaveOnlyWhatWorks(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	expectError(t, admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"media": "everything"})), http.StatusBadRequest, "invalid")
	expectError(t, admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"delivery": "cdn", "cdnBase": "cdn.example"})), http.StatusBadRequest, "invalid")
	expectError(t, admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"accessKey": "wrong"})), http.StatusBadGateway, "unreachable")
	// a new secret key is sealed with the master key, which this app lacks
	expectError(t, admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"secretKey": "s"})), http.StatusConflict, "master_key_missing")

	got := admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"media": "media"}))).json()
	if st := got["status"].(map[string]any); st["active"] != true {
		t.Fatalf("status after saving %v", st)
	}
	id := mediaCourse(t, a.user("alice"))
	if courseKey(t, a.bucket(), id, "images/a.png") == "" {
		t.Error("an image is not in the bucket under 视频与图片")
	}

	// it outlives a restart
	// (the same environment: its secret key)
	b := &routedApp{app: a.restart(a.opts...), local: a.local, cfg: a.cfg}
	got = b.admin().mustOK(b.admin().do("GET", "/api/admin/storage", nil)).json()
	if got["media"] != "media" || got["status"].(map[string]any)["active"] != true {
		t.Errorf("after a restart %v", got)
	}
}

func TestStorageSavesASecretKeyWithTheMasterKey(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil, withMasterKey(strings.Repeat("k", 32)), func(o *server.Options) { o.StorageSecret = "" })
	admin := a.admin()
	got := admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"secretKey": a.cfg.SecretKey}))).json()
	if got["secretSet"] != true || got["secretFromEnv"] != false || got["status"].(map[string]any)["active"] != true {
		t.Errorf("saved %v", got)
	}
	if _, ok := got["secretKey"]; ok {
		t.Error("the secret key is shown")
	}
}

// noRedirect is c's browser, but stops at redirects.
func noRedirect(c *client, path string) *http.Response {
	c.a.t.Helper()
	h := *c.http
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := h.Get(c.a.srv.URL + path)
	if err != nil {
		c.a.t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestStorageServesMediaFromPresignedLinks(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"delivery": "presign", "presignSeconds": 900})))
	alice := a.user("alice")
	id := mediaCourse(t, alice)

	res := noRedirect(alice, fmt.Sprintf("/api/courses/%d/files/videos/1.mp4", id))
	loc := res.Header.Get("Location")
	if res.StatusCode != http.StatusFound || !strings.Contains(loc, "X-Amz-Signature") {
		t.Fatalf("video answers %d %q", res.StatusCode, loc)
	}
	got, err := http.Get(loc)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if string(body) != "0123456789" {
		t.Errorf("presigned video reads %q", body)
	}
	// an image is local under 仅视频, and served here
	if res := noRedirect(alice, fmt.Sprintf("/api/courses/%d/files/images/a.png", id)); res.StatusCode != http.StatusOK {
		t.Errorf("image answers %d", res.StatusCode)
	}
	// someone who may not see the course gets no link
	if res := noRedirect(a.user("mallory"), fmt.Sprintf("/api/courses/%d/files/videos/1.mp4", id)); res.StatusCode == http.StatusFound {
		t.Error("a stranger got a link")
	}
}

func TestStorageServesMediaFromACDN(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"delivery": "cdn", "cdnBase": "https://cdn.example/k"})))
	alice := a.user("alice")
	id := mediaCourse(t, alice)
	key := courseKey(t, a.bucket(), id, "videos/1.mp4")
	res := noRedirect(alice, fmt.Sprintf("/api/courses/%d/files/videos/1.mp4", id))
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "https://cdn.example/k/"+key {
		t.Errorf("video answers %d %q", res.StatusCode, res.Header.Get("Location"))
	}
}

func waitMigration(t *testing.T, admin *client) map[string]any {
	t.Helper()
	for i := 0; i < 100; i++ {
		m, _ := admin.mustOK(admin.do("GET", "/api/admin/storage", nil)).json()["migration"].(map[string]any)
		if m != nil && m["state"] != "running" {
			return m
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the migration did not finish")
	return nil
}

func TestStorageMigrations(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	alice := a.user("alice")
	// uploaded while there is no bucket: all local
	id := mediaCourse(t, alice)
	expectError(t, admin.do("POST", "/api/admin/storage/migration", map[string]any{"direction": "push"}), http.StatusConflict, "not_connected")
	admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"media": "media"})))
	video := courseKey(t, a.local, id, "videos/1.mp4")
	bucket := a.bucket()

	expectError(t, admin.do("POST", "/api/admin/storage/migration", map[string]any{"direction": "sideways"}), http.StatusBadRequest, "invalid")

	expectStatus(t, admin.do("POST", "/api/admin/storage/migration", map[string]any{"direction": "sync"}), http.StatusAccepted)
	if m := waitMigration(t, admin); m["state"] != "done" || m["copied"] != float64(2) {
		t.Fatalf("sync %v", m)
	}
	if !inStore(a.local, video) || !inStore(bucket, video) {
		t.Error("sync did not keep both copies")
	}

	expectStatus(t, admin.do("POST", "/api/admin/storage/migration", map[string]any{"direction": "push"}), http.StatusAccepted)
	// a push copies again what a sync left: it deletes the source, so a
	// same-sized object is not proof enough
	if m := waitMigration(t, admin); m["state"] != "done" || m["copied"] != float64(2) {
		t.Fatalf("push %v", m)
	}
	if inStore(a.local, video) || courseKey(t, a.local, id, "v.md") == "" {
		t.Error("push left the video, or took the lesson")
	}
	// still readable, now from the bucket
	if r := alice.do("GET", fmt.Sprintf("/api/courses/%d/files/videos/1.mp4", id), nil); string(r.body) != "0123456789" {
		t.Errorf("video after push reads %q", r.body)
	}

	expectStatus(t, admin.do("POST", "/api/admin/storage/migration", map[string]any{"direction": "pull"}), http.StatusAccepted)
	if m := waitMigration(t, admin); m["state"] != "done" {
		t.Fatalf("pull %v", m)
	}
	if !inStore(a.local, video) || !inStore(bucket, video) {
		t.Error("pull did not copy back, or emptied the bucket")
	}

	// turned off, a push would strand files
	off := a.bucketSettings(map[string]any{"enabled": false})
	admin.mustOK(admin.do("PUT", "/api/admin/storage", off))
	expectError(t, admin.do("POST", "/api/admin/storage/migration", map[string]any{"direction": "push"}), http.StatusBadRequest, "invalid")
}

func TestStorageTurnedOffStillReadsTheBucket(t *testing.T) {
	t.Parallel()
	a := newRoutedApp(t, nil)
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(nil)))
	alice := a.user("alice")
	id := mediaCourse(t, alice)
	video := courseKey(t, a.bucket(), id, "videos/1.mp4")
	admin.mustOK(admin.do("PUT", "/api/admin/storage", a.bucketSettings(map[string]any{"enabled": false})))

	if r := alice.do("GET", fmt.Sprintf("/api/courses/%d/files/videos/1.mp4", id), nil); string(r.body) != "0123456789" {
		t.Errorf("a video in the bucket reads %d %q once storage is off", r.status, r.body)
	}
	// a new one goes local
	id2 := mediaCourse(t, alice)
	if courseKey(t, a.local, id2, "videos/1.mp4") == "" {
		t.Error("a new video went to the bucket while storage is off")
	}
	// deleting the course still clears the bucket
	alice.mustOK(alice.do("DELETE", fmt.Sprintf("/api/courses/%d", id), nil))
	if inStore(a.bucket(), video) {
		t.Error("the deleted course's video stayed in the bucket")
	}
}
