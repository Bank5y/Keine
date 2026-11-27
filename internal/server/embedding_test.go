package server_test

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeEmbeddings is an OpenAI-compatible embeddings API: each text's
// vector counts its character pairs, hashed into dims slots, so texts that
// share words point the same way. It keeps what it was asked.
type fakeEmbeddings struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls int
	texts []string
	down  bool
}

func newFakeEmbeddings(t *testing.T) *fakeEmbeddings {
	t.Helper()
	f := &fakeEmbeddings{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		down := f.down
		f.mu.Unlock()
		if down || r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer emb-key-0123" {
			http.Error(w, `{"error":{"message":"no"}}`, http.StatusUnauthorized)
			return
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.calls++
		f.texts = append(f.texts, req.Input...)
		f.mu.Unlock()
		var data []map[string]any
		for i, text := range req.Input {
			data = append(data, map[string]any{"index": i, "embedding": pairVector(req.Model+text, 64)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"prompt_tokens": 10 * len(req.Input)}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// pairVector counts a text's character pairs into dims slots.
func pairVector(text string, dims int) []float32 {
	r := []rune(text)
	v := make([]float32, dims)
	for i := 0; i+1 < len(r); i++ {
		h := fnv.New32a()
		h.Write([]byte(string(r[i : i+2])))
		v[h.Sum32()%uint32(dims)]++
	}
	return v
}

func (f *fakeEmbeddings) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeEmbeddings) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

func (f *fakeEmbeddings) settings(model string) map[string]any {
	return map[string]any{"baseUrl": f.srv.URL + "/v1", "key": "emb-key-0123", "model": model, "rate": 2}
}

// embedded waits until every passage of the course is embedded.
func embedded(t *testing.T, admin *client, id int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, x := range admin.mustOK(admin.do("GET", "/api/admin/embedding", nil)).list("courses") {
			c := x.(map[string]any)
			if c["courseId"] == float64(id) && c["total"].(float64) > 0 && c["done"] == c["total"] {
				return c
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the course is not embedded")
	return nil
}

func TestWithAnExternalAPIThePassagesAreEmbeddedAndRecalledByVector(t *testing.T) {
	t.Parallel()
	f := newFakeEmbeddings(t)
	a := aiApp(t, newScriptedModel())
	admin := a.admin()
	id := uploadCourse(t, a.user("alice"), linkedFiles())
	admin.mustOK(admin.do("POST", "/api/admin/courses/"+strconv.Itoa(id)+"/publish", nil))

	got := admin.mustOK(admin.do("PUT", "/api/admin/embedding/external", f.settings("emb-1"))).json()
	if st := got["settings"].(map[string]any); st["provider"] != "external" || st["dims"] != 64.0 || st["keyLast4"] != "0123" {
		t.Fatalf("settings: %v", st)
	}

	embedded(t, admin, id)
	trace := recallTest(t, admin, id, "意图识别分几层", "")
	hits := recallPathHits(t, trace, "vector")
	if len(hits) == 0 || sectionTitle(hits[0]) != "三层意图" {
		t.Fatalf("the vector path finds the passage most like the text: %v", hits)
	}
	c := trace["candidates"].([]any)[0].(map[string]any)
	if s := c["sources"].([]any); len(s) < 2 || s[len(s)-1] != "vector" {
		t.Fatalf("the vector path joins the fusion: %v", c)
	}
	site := admin.mustOK(admin.do("GET", "/api/admin/usage", nil)).json()["total"].(map[string]any)
	if site["runs"].(float64) < 2 || site["credits"].(float64) <= 0 {
		t.Fatalf("the API's use is charged to the site: %v", site)
	}
}

func TestAnExternalAPIThatFailsIsNotSaved(t *testing.T) {
	t.Parallel()
	f := newFakeEmbeddings(t)
	a := aiApp(t, newScriptedModel())
	admin := a.admin()
	admin.mustOK(admin.do("PUT", "/api/admin/embedding/external", f.settings("emb-1")))

	f.setDown(true)
	expectError(t, admin.do("PUT", "/api/admin/embedding/external", f.settings("emb-2")), http.StatusBadRequest, "embedding_test_failed")

	st := admin.mustOK(admin.do("GET", "/api/admin/embedding", nil)).json()["settings"].(map[string]any)
	if st["model"] != "emb-1" || st["provider"] != "external" {
		t.Fatalf("the settings stay as they were: %v", st)
	}
}

func TestAnotherModelMakesTheVectorsStaleUntilEmbeddedAgain(t *testing.T) {
	t.Parallel()
	f := newFakeEmbeddings(t)
	a := aiApp(t, newScriptedModel())
	admin := a.admin()
	id := uploadCourse(t, a.user("alice"), linkedFiles())
	admin.mustOK(admin.do("POST", "/api/admin/courses/"+strconv.Itoa(id)+"/publish", nil))
	admin.mustOK(admin.do("PUT", "/api/admin/embedding/external", f.settings("emb-1")))
	total := embedded(t, admin, id)["total"]
	before := admin.mustOK(admin.do("GET", "/api/admin/embedding", nil)).json()["settings"].(map[string]any)["generation"].(float64)

	admin.mustOK(admin.do("PUT", "/api/admin/embedding/external", f.settings("emb-2")))

	st := admin.mustOK(admin.do("GET", "/api/admin/embedding", nil)).json()["settings"].(map[string]any)
	if st["generation"].(float64) != before+1 {
		t.Fatalf("another model, another generation: %v", st)
	}
	if c := embedded(t, admin, id); c["total"] != total {
		t.Fatalf("every passage embedded again: %v", c)
	}
	if hits := recallPathHits(t, recallTest(t, admin, id, "意图识别分几层", ""), "vector"); len(hits) == 0 {
		t.Fatal("recall by vector is back")
	}
}

func TestWithEmbeddingOffRecallHasTwoPathsAndCallsNoAPI(t *testing.T) {
	t.Parallel()
	f := newFakeEmbeddings(t)
	a := aiApp(t, newScriptedModel())
	admin := a.admin()
	id := uploadCourse(t, a.user("alice"), linkedFiles())
	admin.mustOK(admin.do("POST", "/api/admin/courses/"+strconv.Itoa(id)+"/publish", nil))
	admin.mustOK(admin.do("PUT", "/api/admin/embedding/external", f.settings("emb-1")))
	embedded(t, admin, id)

	admin.mustOK(admin.do("PUT", "/api/admin/embedding/off", nil))
	calls := f.callCount()
	trace := recallTest(t, admin, id, "意图识别分几层", "")

	if paths := trace["paths"].([]any); len(paths) != 2 {
		t.Fatalf("two paths: %v", paths)
	}
	if f.callCount() != calls {
		t.Fatal("the API was called with embedding off")
	}
}

func TestOnlyAnAdminSetsEmbeddingUp(t *testing.T) {
	t.Parallel()
	f := newFakeEmbeddings(t)
	a := aiApp(t, newScriptedModel())
	bob := a.user("bob")

	expectStatus(t, bob.do("GET", "/api/admin/embedding", nil), http.StatusForbidden)
	expectStatus(t, bob.do("PUT", "/api/admin/embedding/external", f.settings("emb-1")), http.StatusForbidden)
	expectError(t, a.admin().do("PUT", "/api/admin/embedding/local", map[string]any{"model": "bge-small-zh-v1.5"}), http.StatusConflict, "model_not_downloaded")
}
