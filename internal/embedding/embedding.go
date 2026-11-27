// Package embedding turns texts into vectors for the vector path of the
// concept index's recall (#227, ADR 0015): off by default, an
// OpenAI-compatible API (external), or a small model run in this process
// (local) that the admin downloads.
package embedding

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Bank5y/keine/internal/secret"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Provider is where the site's embeddings come from.
type Provider string

const (
	Off      Provider = "off"
	External Provider = "external"
	Local    Provider = "local"
)

var (
	// ErrMasterKeyMissing: an external API's key cannot be stored without
	// a master key.
	ErrMasterKeyMissing = errors.New("no master key to store an embedding key with")
	// ErrNotDownloaded: the local model has not been downloaded.
	ErrNotDownloaded = errors.New("local model not downloaded")
	// ErrUnknownModel: no such local model.
	ErrUnknownModel = errors.New("unknown local model")
	// ErrBadSettings: a setting is missing or malformed.
	ErrBadSettings = errors.New("bad embedding settings")
)

// TestFailed is a setting that could not embed a test sentence; the
// settings stay as they were.
type TestFailed struct{ Cause error }

func (e *TestFailed) Error() string { return "embedding test failed: " + e.Cause.Error() }
func (e *TestFailed) Unwrap() error { return e.Cause }

// Usage is what an embedding call used, in tokens (0 when the provider
// does not say, and for the local model).
type Usage struct{ Tokens int64 }

// Embedder embeds texts, one unit vector each, in order.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, Usage, error)
}

// Current is what embeds now: the embedder (nil when off), the generation
// its vectors belong to, and for usage, the model's name and the
// external API's rate in credits per million tokens.
type Current struct {
	Embedder   Embedder
	Provider   Provider
	Generation int64
	Model      string
	Rate       float64
}

// Settings are the site's embedding settings, as the admin sees them.
type Settings struct {
	Provider   Provider `json:"provider"`
	BaseURL    string   `json:"baseUrl"`
	Model      string   `json:"model"`
	KeyLast4   string   `json:"keyLast4"`
	Rate       float64  `json:"rate"`
	LocalModel string   `json:"localModel"`
	Mirror     string   `json:"mirror"`
	Generation int64    `json:"generation"`
	Dims       int      `json:"dims"`
	// CanStoreKeys: a master key is configured, so an external API's key
	// can be kept.
	CanStoreKeys bool         `json:"canStoreKeys"`
	Models       []LocalModel `json:"models"`
	Downloads    []Download   `json:"downloads"`
}

// Service keeps the site's embedding settings and runs its embedder.
type Service struct {
	db     *pgxpool.Pool
	box    *secret.Box
	dir    string
	client *http.Client

	mu     sync.Mutex
	locals map[string]*localModel
	loads  map[string]*Download

	// Kick asks the server's embedder for a round; the server makes it.
	Kick chan struct{}
}

// New is the embedding service: settings in db, keys sealed with box (nil
// without a master key), local models downloaded under dir.
func New(db *pgxpool.Pool, box *secret.Box, dir string) *Service {
	return &Service{db: db, box: box, dir: dir, client: &http.Client{Timeout: 2 * time.Minute},
		locals: map[string]*localModel{}, loads: map[string]*Download{}}
}

const keyPurpose = "embedding_settings:1"

type row struct {
	provider       Provider
	baseURL, model string
	key            []byte
	rate           float64
	localModel     string
	mirror         string
	generation     int64
	dims           int
}

func (s *Service) read(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, lock bool) (row, error) {
	var r row
	sql := "SELECT provider, base_url, model, api_key, rate, local_model, mirror, generation, dims FROM embedding_settings WHERE id = 1"
	if lock {
		sql += " FOR UPDATE"
	}
	err := q.QueryRow(ctx, sql).Scan(&r.provider, &r.baseURL, &r.model, &r.key, &r.rate, &r.localModel, &r.mirror, &r.generation, &r.dims)
	return r, err
}

// Settings are the site's embedding settings, with the local models and
// how their downloads stand.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	r, err := s.read(ctx, s.db, false)
	if err != nil {
		return Settings{}, err
	}
	out := Settings{Provider: r.provider, BaseURL: r.baseURL, Model: r.model, Rate: r.rate, LocalModel: r.localModel, Mirror: r.mirror,
		Generation: r.generation, Dims: r.dims, CanStoreKeys: s.box != nil, Models: Models, Downloads: s.downloads()}
	if key, err := s.open(r.key); err == nil && len(key) >= 4 {
		out.KeyLast4 = key[len(key)-4:]
	}
	return out, nil
}

func (s *Service) open(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	if s.box == nil {
		return "", ErrMasterKeyMissing
	}
	return s.box.Open(sealed, keyPurpose)
}

// Current is what embeds now; its Embedder is nil when embedding is off,
// or the local model is gone.
func (s *Service) Current(ctx context.Context) (Current, error) {
	r, err := s.read(ctx, s.db, false)
	if err != nil {
		return Current{}, err
	}
	cur := Current{Provider: r.provider, Generation: r.generation, Rate: r.rate}
	switch r.provider {
	case External:
		key, err := s.open(r.key)
		if err != nil {
			return cur, err
		}
		cur.Embedder, cur.Model = &external{client: s.client, baseURL: r.baseURL, key: key, model: r.model}, r.model
	case Local:
		m, err := s.local(r.localModel)
		if err != nil {
			return cur, err
		}
		cur.Embedder, cur.Model = m, r.localModel
	}
	return cur, nil
}

// ExternalInput is an external API's settings as the admin enters them;
// an empty Key keeps the one stored.
type ExternalInput struct {
	BaseURL string  `json:"baseUrl"`
	Key     string  `json:"key"`
	Model   string  `json:"model"`
	Rate    float64 `json:"rate"`
}

// testSentence is what a setting has to embed before it is kept.
const testSentence = "概念索引用嵌入找出意思相近的段落。"

// SaveExternal switches the site to an external API, once it embeds a test
// sentence; when it does not, the settings stay as they were.
func (s *Service) SaveExternal(ctx context.Context, in ExternalInput) error {
	in.BaseURL, in.Model, in.Key = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"), strings.TrimSpace(in.Model), strings.TrimSpace(in.Key)
	if in.BaseURL == "" || in.Model == "" || in.Rate < 0 || math.IsNaN(in.Rate) {
		return ErrBadSettings
	}
	if s.box == nil {
		return ErrMasterKeyMissing
	}
	was, err := s.read(ctx, s.db, false)
	if err != nil {
		return err
	}
	key := in.Key
	if key == "" {
		if key, err = s.open(was.key); err != nil {
			return err
		}
	}
	e := &external{client: s.client, baseURL: in.BaseURL, key: key, model: in.Model}
	v, _, err := e.Embed(ctx, []string{testSentence})
	if err != nil {
		return &TestFailed{Cause: err}
	}
	return s.switchTo(ctx, func(cur row) row {
		cur.provider, cur.baseURL, cur.model, cur.rate = External, in.BaseURL, in.Model, in.Rate
		cur.key = s.box.Seal(key, keyPurpose)
		cur.dims = len(v[0])
		return cur
	})
}

// UseLocal switches the site to a local model the admin downloaded, once
// it embeds a test sentence.
func (s *Service) UseLocal(ctx context.Context, model string) error {
	m, err := s.local(model)
	if err != nil {
		return err
	}
	v, _, err := m.Embed(ctx, []string{testSentence})
	if err != nil {
		return &TestFailed{Cause: err}
	}
	return s.switchTo(ctx, func(cur row) row {
		cur.provider, cur.localModel, cur.dims = Local, model, len(v[0])
		return cur
	})
}

// TurnOff turns embedding off: recall goes back to its two paths.
func (s *Service) TurnOff(ctx context.Context) error {
	return s.switchTo(ctx, func(cur row) row {
		cur.provider = Off
		return cur
	})
}

// switchTo changes the settings as change says; when what embeds changed
// (another provider, API or model), the generation goes up and every
// vector so far is stale.
func (s *Service) switchTo(ctx context.Context, change func(row) row) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	was, err := s.read(ctx, tx, true)
	if err != nil {
		return err
	}
	now := change(was)
	if now.provider != Off && (now.provider != was.provider || now.baseURL != was.baseURL || now.model != was.model || now.localModel != was.localModel) {
		now.generation = was.generation + 1
	}
	if _, err := tx.Exec(ctx, `UPDATE embedding_settings SET provider = $1, base_url = $2, model = $3, api_key = $4, rate = $5, local_model = $6,
		generation = $7, dims = $8, updated_at = now() WHERE id = 1`,
		now.provider, now.baseURL, now.model, now.key, now.rate, now.localModel, now.generation, now.dims); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetMirror is where local models are downloaded from: Hugging Face or a
// mirror of it.
func (s *Service) SetMirror(ctx context.Context, mirror string) error {
	mirror = strings.TrimRight(strings.TrimSpace(mirror), "/")
	if !strings.HasPrefix(mirror, "https://") && !strings.HasPrefix(mirror, "http://") {
		return ErrBadSettings
	}
	_, err := s.db.Exec(ctx, "UPDATE embedding_settings SET mirror = $1, updated_at = now() WHERE id = 1", mirror)
	return err
}

// normalize scales v to unit length; a zero vector stays as it is.
func normalize(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

// Cosine is the cosine similarity of two unit vectors (their dot product);
// 0 for vectors of different lengths.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var d float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
	}
	return d
}

func errorf(format string, args ...any) error { return fmt.Errorf(format, args...) }
