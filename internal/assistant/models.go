package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/secret"
)

// DefaultContextWindow applies to a model whose context window is not set.
const DefaultContextWindow = 128_000

// FormatOpenAI is the only interface format of the first version.
const FormatOpenAI = "openai"

// TurnKind is what a run is for; each kind is served by one site model.
type TurnKind string

const (
	TurnAnalysis TurnKind = "analysis"
	TurnFollowup TurnKind = "followup"
	TurnSync     TurnKind = "sync"
)

var TurnKinds = []TurnKind{TurnAnalysis, TurnFollowup, TurnSync}

func ValidTurnKind(k string) bool {
	for _, t := range TurnKinds {
		if string(t) == k {
			return true
		}
	}
	return false
}

// Rates are credits per million tokens of each kind, and per search.
type Rates struct {
	Input      float64 `json:"input"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Output     float64 `json:"output"`
	Search     float64 `json:"search"`
}

// Model is a site model as the admin page shows it: never the key, only
// its last four characters ("" when there is none).
type Model struct {
	ID          int64  `json:"id"`
	Format      string `json:"format"`
	BaseURL     string `json:"baseUrl"`
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	// ContextWindow is nil when unset (DefaultContextWindow applies).
	ContextWindow *int      `json:"contextWindow"`
	Rates         *Rates    `json:"rates"`
	KeyLast4      string    `json:"keyLast4"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// ModelInput is a model as an admin submits it. An empty Key keeps the
// stored one when editing.
type ModelInput struct {
	Format        string     `json:"format"`
	BaseURL       string     `json:"baseUrl"`
	Key           string     `json:"key"`
	Model         string     `json:"model"`
	DisplayName   string     `json:"displayName"`
	ContextWindow *int       `json:"contextWindow"`
	Rates         *rateInput `json:"rates"`
}

// rateInput tells a missing rate from a zero one.
type rateInput struct {
	Input      *float64 `json:"input"`
	CacheRead  *float64 `json:"cacheRead"`
	CacheWrite *float64 `json:"cacheWrite"`
	Output     *float64 `json:"output"`
	Search     *float64 `json:"search"`
}

// TurnModel is the model serving one turn kind.
type TurnModel struct {
	ModelID  int64 `json:"modelId"`
	Thinking bool  `json:"thinking"`
}

var (
	ErrMasterKeyMissing = errors.New("no master key to store a model key with")
	ErrModelNotFound    = errors.New("site model not found")
	ErrModelUnrated     = errors.New("a model without rates cannot serve a turn kind")
	ErrModelInUse       = errors.New("site model serves a turn kind")
	// ErrKeyUnreadable: the stored key does not open with the current
	// master key, so it has to be entered again.
	ErrKeyUnreadable = errors.New("stored model key cannot be decrypted")
	// ErrModelChanged: the stored key changed while an edit that keeps it
	// was being tested, so the saved model would pair untested settings.
	ErrModelChanged = errors.New("site model changed during the connection test")
)

// InvalidModelError lists what is wrong with a submitted model.
type InvalidModelError struct{ Problems []string }

func (e *InvalidModelError) Error() string { return "invalid model: " + strings.Join(e.Problems, "; ") }

// TestFailedError is a connection test that did not pass. Only Category
// goes back to the admin; Cause is for the server log.
type TestFailedError struct {
	Category Category
	Cause    error
}

func (e *TestFailedError) Error() string { return fmt.Sprintf("connection test failed: %v", e.Cause) }

// AdapterFor builds the adapter for a model's format.
type AdapterFor func(format, baseURL, key string) Adapter

// Models keeps the site models and the turn kind mapping.
type Models struct {
	db *pgxpool.Pool
	// box is nil when the deployment has no master key.
	box        *secret.Box
	adapterFor AdapterFor
}

func NewModels(db *pgxpool.Pool, box *secret.Box, adapterFor AdapterFor) *Models {
	return &Models{db: db, box: box, adapterFor: adapterFor}
}

// CanStoreKeys tells whether a master key is configured.
func (s *Models) CanStoreKeys() bool { return s.box != nil }

const modelColumns = `id, format, base_url, key_last4, model, display_name, context_window,
	rate_input, rate_cache_read, rate_cache_write, rate_output, rate_search, updated_at`

func scanModel(row pgx.Row) (Model, error) {
	var (
		m  Model
		r  [5]*float64
		cw *int32
	)
	if err := row.Scan(&m.ID, &m.Format, &m.BaseURL, &m.KeyLast4, &m.Model, &m.DisplayName, &cw,
		&r[0], &r[1], &r[2], &r[3], &r[4], &m.UpdatedAt); err != nil {
		return m, err
	}
	if cw != nil {
		v := int(*cw)
		m.ContextWindow = &v
	}
	if r[0] != nil {
		m.Rates = &Rates{Input: *r[0], CacheRead: *r[1], CacheWrite: *r[2], Output: *r[3], Search: *r[4]}
	}
	return m, nil
}

func (s *Models) List(ctx context.Context) ([]Model, error) {
	rows, err := s.db.Query(ctx, "SELECT "+modelColumns+" FROM site_models ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Model{}
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Turns returns the mapping; an unmapped kind is absent.
func (s *Models) Turns(ctx context.Context) (map[TurnKind]TurnModel, error) {
	rows, err := s.db.Query(ctx, "SELECT kind, model_id, thinking FROM turn_models")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[TurnKind]TurnModel{}
	for rows.Next() {
		var k string
		var t TurnModel
		if err := rows.Scan(&k, &t.ModelID, &t.Thinking); err != nil {
			return nil, err
		}
		out[TurnKind(k)] = t
	}
	return out, rows.Err()
}

// Available tells whether every turn kind has a model, which is when the
// assistant shows up for readers.
func (s *Models) Available(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRow(ctx, "SELECT count(*) FROM turn_models").Scan(&n)
	return n == len(TurnKinds), err
}

// validated is a ModelInput that passed validation.
type validated struct {
	in    ModelInput
	rates *Rates
}

func validate(in ModelInput) (validated, error) {
	var problems []string
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Model = strings.TrimSpace(in.Model)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Key = strings.TrimSpace(in.Key)
	if in.Format != FormatOpenAI {
		problems = append(problems, "接口格式只支持 OpenAI 兼容")
	}
	if u, err := url.Parse(in.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		problems = append(problems, "地址要以 http:// 或 https:// 开头")
	}
	if in.Model == "" {
		problems = append(problems, "模型名不能为空")
	}
	if in.DisplayName == "" {
		problems = append(problems, "显示名称不能为空")
	}
	if in.ContextWindow != nil && *in.ContextWindow <= 0 {
		problems = append(problems, "上下文窗口要是正整数")
	}
	var rates *Rates
	if r := in.Rates; r != nil {
		vals := []*float64{r.Input, r.CacheRead, r.CacheWrite, r.Output, r.Search}
		ok := true
		for _, v := range vals {
			if v == nil || *v < 0 {
				ok = false
			}
		}
		if ok {
			rates = &Rates{Input: *r.Input, CacheRead: *r.CacheRead, CacheWrite: *r.CacheWrite, Output: *r.Output, Search: *r.Search}
		} else {
			problems = append(problems, "费率五项都要填不小于 0 的数, 或者都留空")
		}
	}
	if len(problems) > 0 {
		return validated{}, &InvalidModelError{Problems: problems}
	}
	return validated{in: in, rates: rates}, nil
}

// Create validates the model, tests the connection and stores it.
func (s *Models) Create(ctx context.Context, in ModelInput) (Model, error) {
	v, err := validate(in)
	if err != nil {
		return Model{}, err
	}
	if v.in.Key != "" && s.box == nil {
		return Model{}, ErrMasterKeyMissing
	}
	if err := s.test(ctx, v.in, v.in.Key); err != nil {
		return Model{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Model{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, "SELECT nextval('site_models_id_seq')").Scan(&id); err != nil {
		return Model{}, err
	}
	sealed, last4 := s.seal(id, v.in.Key)
	r := rateArgs(v.rates)
	m, err := scanModel(tx.QueryRow(ctx, `
		INSERT INTO site_models (id, format, base_url, api_key, key_last4, model, display_name, context_window,
			rate_input, rate_cache_read, rate_cache_write, rate_output, rate_search)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+modelColumns,
		id, v.in.Format, v.in.BaseURL, sealed, last4, v.in.Model, v.in.DisplayName, v.in.ContextWindow, r[0], r[1], r[2], r[3], r[4]))
	if err != nil {
		return Model{}, err
	}
	return m, tx.Commit(ctx)
}

// Update validates the edit, tests the connection with the new or the
// stored key and saves it. A model serving a turn kind keeps its rates.
func (s *Models) Update(ctx context.Context, id int64, in ModelInput) (Model, error) {
	v, err := validate(in)
	if err != nil {
		return Model{}, err
	}
	if v.in.Key != "" && s.box == nil {
		return Model{}, ErrMasterKeyMissing
	}
	sealedBefore, mapped, err := s.current(ctx, id)
	if err != nil {
		return Model{}, err
	}
	key := ""
	if v.in.Key == "" && len(sealedBefore) > 0 {
		if key, err = s.open(id, sealedBefore); err != nil {
			return Model{}, err
		}
	}
	if mapped && v.rates == nil {
		return Model{}, ErrModelUnrated
	}
	if v.in.Key != "" {
		key = v.in.Key
	}
	if err := s.test(ctx, v.in, key); err != nil {
		return Model{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Model{}, err
	}
	defer tx.Rollback(ctx)
	// lock the row and re-check: a turn kind may have been mapped to it,
	// or its key replaced, while the test ran
	sealedNow, mapped, err := lockedModel(ctx, tx, id)
	if err != nil {
		return Model{}, err
	}
	if mapped && v.rates == nil {
		return Model{}, ErrModelUnrated
	}
	if v.in.Key == "" && !bytes.Equal(sealedNow, sealedBefore) {
		return Model{}, ErrModelChanged
	}
	r := rateArgs(v.rates)
	args := []any{id, v.in.Format, v.in.BaseURL, v.in.Model, v.in.DisplayName, v.in.ContextWindow, r[0], r[1], r[2], r[3], r[4]}
	keySet := ""
	if v.in.Key != "" {
		sealed, last4 := s.seal(id, v.in.Key)
		args = append(args, sealed, last4)
		keySet = ", api_key = $12, key_last4 = $13"
	}
	m, err := scanModel(tx.QueryRow(ctx, `
		UPDATE site_models SET format = $2, base_url = $3, model = $4, display_name = $5, context_window = $6,
			rate_input = $7, rate_cache_read = $8, rate_cache_write = $9, rate_output = $10, rate_search = $11,
			updated_at = now()`+keySet+`
		WHERE id = $1 RETURNING `+modelColumns, args...))
	if err != nil {
		return Model{}, err
	}
	return m, tx.Commit(ctx)
}

const modelStateQuery = `SELECT api_key, EXISTS (SELECT 1 FROM turn_models WHERE model_id = $1)
	FROM site_models WHERE id = $1`

// current returns a model's sealed key and whether it serves a turn kind.
func (s *Models) current(ctx context.Context, id int64) (sealed []byte, mapped bool, err error) {
	return scanModelState(s.db.QueryRow(ctx, modelStateQuery, id))
}

// lockedModel is current, under a row lock in tx.
func lockedModel(ctx context.Context, tx pgx.Tx, id int64) (sealed []byte, mapped bool, err error) {
	return scanModelState(tx.QueryRow(ctx, modelStateQuery+" FOR UPDATE", id))
}

func scanModelState(row pgx.Row) (sealed []byte, mapped bool, err error) {
	err = row.Scan(&sealed, &mapped)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrModelNotFound
	}
	return sealed, mapped, err
}

// open decrypts a stored key.
func (s *Models) open(id int64, sealed []byte) (string, error) {
	if s.box == nil {
		return "", ErrMasterKeyMissing
	}
	key, err := s.box.Open(sealed, keyPurpose(id))
	if err != nil {
		return "", ErrKeyUnreadable
	}
	return key, nil
}

func (s *Models) Delete(ctx context.Context, id int64) error {
	_, mapped, err := s.current(ctx, id)
	if err != nil {
		return err
	}
	if mapped {
		return ErrModelInUse
	}
	tag, err := s.db.Exec(ctx, "DELETE FROM site_models WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM turn_models WHERE model_id = $1)", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrModelInUse
	}
	return nil
}

// SetTurn maps a turn kind to a rated model, or unmaps it when modelID is
// nil.
func (s *Models) SetTurn(ctx context.Context, kind TurnKind, modelID *int64, thinking bool) error {
	if modelID == nil {
		_, err := s.db.Exec(ctx, "DELETE FROM turn_models WHERE kind = $1", kind)
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var rated bool
	err = tx.QueryRow(ctx, "SELECT rate_input IS NOT NULL FROM site_models WHERE id = $1 FOR UPDATE", *modelID).Scan(&rated)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrModelNotFound
	}
	if err != nil {
		return err
	}
	if !rated {
		return ErrModelUnrated
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO turn_models (kind, model_id, thinking) VALUES ($1, $2, $3)
		ON CONFLICT (kind) DO UPDATE SET model_id = EXCLUDED.model_id, thinking = EXCLUDED.thinking`,
		kind, *modelID, thinking); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func keyPurpose(id int64) string { return fmt.Sprintf("site_model:%d", id) }

// seal encrypts a key for the model's row; an empty key stays empty.
func (s *Models) seal(id int64, key string) ([]byte, string) {
	if key == "" {
		return nil, ""
	}
	return s.box.Seal(key, keyPurpose(id)), lastFour(key)
}

// lastFour is what the admin page shows of a key: its last four
// characters, or nothing when that would give away half the key or more.
func lastFour(key string) string {
	r := []rune(key)
	if len(r) < 8 {
		return ""
	}
	return string(r[len(r)-4:])
}

func rateArgs(r *Rates) [5]*float64 {
	if r == nil {
		return [5]*float64{}
	}
	return [5]*float64{&r.Input, &r.CacheRead, &r.CacheWrite, &r.Output, &r.Search}
}

// testTimeout bounds a connection test.
const testTimeout = 30 * time.Second

// test makes one streaming call that declares a tool, which is what every
// turn kind needs from the endpoint: auth, streaming, and tools accepted.
func (s *Models) test(ctx context.Context, in ModelInput, key string) error {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	_, err := s.adapterFor(in.Format, in.BaseURL, key).Stream(ctx, Request{
		Model: in.Model,
		Messages: []Message{
			{Role: RoleSystem, Content: "This is a connection test. Do not call any tool."},
			{Role: RoleUser, Content: "Reply with OK."},
		},
		Tools: []Tool{{
			Name:        "connection_check",
			Description: "Declared only to check that tools are accepted; never call it.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		ToolChoice: "auto",
		MaxTokens:  64,
	}, func(Event) error { return nil })
	if err == nil {
		return nil
	}
	// the provider's answer may echo the key; keep it out of the log
	cause := err
	if key != "" {
		cause = errors.New(strings.ReplaceAll(err.Error(), key, "[key]"))
	}
	if c := CategoryOf(err); c != "" {
		return &TestFailedError{Category: c, Cause: cause}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &TestFailedError{Category: ErrTimeout, Cause: cause}
	}
	return err
}
