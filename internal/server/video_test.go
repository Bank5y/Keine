package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

func videoCourse(t *testing.T, c *client) int {
	t.Helper()
	m := map[string]any{
		"version": 1, "title": "视频课", "type": "video",
		"chapters": []any{map[string]any{"id": "c1", "title": "第一章", "lessons": []any{
			map[string]any{"id": "file", "title": "课程包里的视频", "video": map[string]any{"file": "videos/1.mp4"}},
			map[string]any{"id": "mp4", "title": "直链 mp4", "video": map[string]any{"url": "https://cdn.example.com/a/1.mp4?sig=x"}},
			map[string]any{"id": "hls", "title": "m3u8", "video": map[string]any{"url": "https://cdn.example.com/live/index.m3u8"}},
			map[string]any{"id": "bili", "title": "B站", "video": map[string]any{"url": "https://www.bilibili.com/video/BV1xx411c7mD/?p=2"}},
			map[string]any{"id": "yt", "title": "YouTube", "video": map[string]any{"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}},
			map[string]any{"id": "ytshort", "title": "youtu.be", "video": map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ"}},
			map[string]any{"id": "page", "title": "别的网页", "video": map[string]any{"url": "https://example.com/talks/42"}, "file": "page.md"},
		}}},
	}
	return uploadCourse(t, c, map[string]string{"course.json": manifestJSON(m), "videos/1.mp4": "0123456789", "page.md": "# 文字稿"})
}

func lessonVideo(t *testing.T, c *client, courseID, lessonID int) map[string]any {
	t.Helper()
	l := c.mustOK(c.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", courseID, lessonID), nil)).json()
	v, _ := l["video"].(map[string]any)
	return v
}

func TestVideoSourcesAreClassified(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := videoCourse(t, alice)
	ids := lessonIDs(t, alice, id)

	cases := map[string]map[string]any{
		"file":    {"mode": "player", "src": fmt.Sprintf("/api/courses/%d/files/videos/1.mp4", id)},
		"mp4":     {"mode": "player", "src": "https://cdn.example.com/a/1.mp4?sig=x"},
		"hls":     {"mode": "player", "src": "https://cdn.example.com/live/index.m3u8"},
		"bili":    {"mode": "embed", "src": "https://player.bilibili.com/player.html?autoplay=0&bvid=BV1xx411c7mD&p=2"},
		"yt":      {"mode": "embed", "src": "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		"ytshort": {"mode": "embed", "src": "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		"page":    {"mode": "link", "src": "https://example.com/talks/42"},
	}
	for key, want := range cases {
		got := lessonVideo(t, alice, id, ids[key])
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", key, k, got[k], v)
			}
		}
	}
}

func TestTextLessonsHaveNoVideo(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := uploadCourse(t, alice, columnFiles(columnManifest()))
	ids := lessonIDs(t, alice, id)

	if v := lessonVideo(t, alice, id, ids["l01"]); v != nil {
		t.Fatalf("a text lesson has no video, got %v", v)
	}
}

func TestPackagedVideosSupportRanges(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := videoCourse(t, alice)

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/courses/%d/files/videos/1.mp4", a.srv.URL, id), nil)
	req.Header.Set("Range", "bytes=2-5")
	r := alice.send(req)

	expectStatus(t, r, http.StatusPartialContent)
	if string(r.body) != "2345" || r.header.Get("Content-Range") != "bytes 2-5/10" || r.header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("unexpected range response: %q %q %q", r.body, r.header.Get("Content-Range"), r.header.Get("Content-Type"))
	}
}

func TestVideoPositionIsSecondsAndMayExceedOne(t *testing.T) {
	a := newApp(t)
	alice := a.user("alice")
	id := videoCourse(t, alice)
	ids := lessonIDs(t, alice, id)

	alice.mustOK(setProgress(alice, id, ids["file"], map[string]any{"position": 754.5}))

	l := alice.mustOK(alice.do("GET", fmt.Sprintf("/api/courses/%d/lessons/%d", id, ids["file"]), nil)).json()
	if l["position"] != 754.5 {
		t.Fatalf("expected 754.5 seconds, got %v", l["position"])
	}
}
