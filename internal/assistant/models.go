package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/secret"
)

// DefaultContextWindow applies to a model whose context window is not set.
const DefaultContextWindow = 128_000

// The interface formats a site model can speak: OpenAI-compatible Chat
// Completions, and Anthropic Messages.
const (
	FormatOpenAI    = "openai"
	FormatAnthropic = "anthropic"
)

// TurnKind is what a run is for; each kind is served by one site model.
type TurnKind string

const (
	TurnAnalysis     TurnKind = "analysis"
	TurnFollowup     TurnKind = "followup"
	TurnReport       TurnKind = "report"
	TurnConceptIndex TurnKind = "concept_index"
)

var TurnKinds = []TurnKind{TurnAnalysis, TurnFollowup, TurnReport, TurnConceptIndex}

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
	ID          int64    `json:"id"`
	Format      string   `json:"format"`
	Provider    Provider `json:"provider"`
	BaseURL     string   `json:"baseUrl"`
	Model       string   `json:"model"`
	DisplayName string   `json:"displayName"`
	// ContextWindow is nil when unset (DefaultContextWindow applies).
	ContextWindow *int   `json:"contextWindow"`
	Rates         *Rates `json:"rates"`
	KeyLast4      string `json:"keyLast4"`
	// SearchRoute is the admin's choice; Search is what it resolves to now,
	// and SearchReady false when it resolves to nothing (ADR 0008).
	SearchRoute SearchRoute `json:"searchRoute"`
	Search      SearchRoute `json:"search"`
	SearchReady bool        `json:"searchReady"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// ModelInput is a model as an admin submits it. An empty Key keeps the
// stored one when editing.
type ModelInput struct {
	Format string `json:"format"`
	// Provider fills in an empty address or display name with its own.
	Provider      Provider    `json:"provider"`
	BaseURL       string      `json:"baseUrl"`
	Key           string      `json:"key"`
	Model         string      `json:"model"`
	DisplayName   string      `json:"displayName"`
	ContextWindow *int        `json:"contextWindow"`
	Rates         *rateInput  `json:"rates"`
	SearchRoute   SearchRoute `json:"searchRoute"`
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
	// Tavily: the test was the search that saves the site's Tavily key
	Tavily bool
}

func (e *TestFailedError) Error() string { return fmt.Sprintf("connection test failed: %v", e.Cause) }

// AdapterFor builds the adapter for a model's endpoint.
type AdapterFor func(Endpoint) Adapter

// Models keeps the site models and the turn kind mapping.
type Models struct {
	db *pgxpool.Pool
	// box is nil when the deployment has no master key.
	box        *secret.Box
	adapterFor AdapterFor
	searchFor  SearchFor
}

func NewModels(db *pgxpool.Pool, box *secret.Box, adapterFor AdapterFor, searchFor SearchFor) *Models {
	return &Models{db: db, box: box, adapterFor: adapterFor, searchFor: searchFor}
}

// CanStoreKeys tells whether a master key is configured.
func (s *Models) CanStoreKeys() bool { return s.box != nil }

const modelColumns = `id, format, provider, base_url, key_last4, model, display_name, context_window,
	rate_input, rate_cache_read, rate_cache_write, rate_output, rate_search, search_route, updated_at`

func scanModel(row pgx.Row) (Model, error) {
	var (
		m  Model
		r  [5]*float64
		cw *int32
	)
	if err := row.Scan(&m.ID, &m.Format, &m.Provider, &m.BaseURL, &m.KeyLast4, &m.Model, &m.DisplayName, &cw,
		&r[0], &r[1], &r[2], &r[3], &r[4], &m.SearchRoute, &m.UpdatedAt); err != nil {
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
	tv, err := s.Tavily(ctx)
	if err != nil {
		return nil, err
	}
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
		out = append(out, m.resolve(tv.Set))
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

// Problem is why the assistant is unavailable: some turn kinds have no
// model (Turns), or some mapped models' search routes resolve to no
// search service (Models, by display name).
type Problem struct {
	Reason ProblemReason `json:"reason"`
	Turns  []TurnKind    `json:"turns,omitempty"`
	Models []string      `json:"models,omitempty"`
}

type ProblemReason string

const (
	ProblemTurnsUnmapped  ProblemReason = "turns_unmapped"
	ProblemSearchUnrouted ProblemReason = "search_unrouted"
)

// Problem says why the assistant is unavailable, nil when every turn kind
// has a model whose search route resolves (ADR 0008).
func (s *Models) Problem(ctx context.Context) (*Problem, error) {
	tv, err := s.Tavily(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, "SELECT t.kind, m.display_name, m.search_route, m.format, m.provider FROM turn_models t JOIN site_models m ON m.id = t.model_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mapped := map[TurnKind]bool{}
	var unrouted []string
	for rows.Next() {
		var (
			kind   TurnKind
			name   string
			route  SearchRoute
			format string
			p      Provider
		)
		if err := rows.Scan(&kind, &name, &route, &format, &p); err != nil {
			return nil, err
		}
		mapped[kind] = true
		if _, ok := Resolve(route, format, p, tv.Set); !ok && !slices.Contains(unrouted, name) {
			unrouted = append(unrouted, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing []TurnKind
	for _, k := range TurnKinds {
		if !mapped[k] {
			missing = append(missing, k)
		}
	}
	switch {
	case len(missing) > 0:
		return &Problem{Reason: ProblemTurnsUnmapped, Turns: missing}, nil
	case len(unrouted) > 0:
		slices.Sort(unrouted)
		return &Problem{Reason: ProblemSearchUnrouted, Models: unrouted}, nil
	}
	return nil, nil
}

// Available tells whether every turn kind has a model and every such
// model a search route (or the deployer's choice of none), which is when
// the assistant shows up for readers.
func (s *Models) Available(ctx context.Context) (bool, error) {
	p, err := s.Problem(ctx)
	return p == nil && err == nil, err
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
	if in.Provider != ProviderNone {
		p, ok := preset(in.Provider)
		switch {
		case !ok:
			problems = append(problems, "没有这个服务商配置")
		case in.Format != p.Format:
			problems = append(problems, p.Name+" 只能用 OpenAI 兼容格式")
		default:
			if in.BaseURL == "" {
				in.BaseURL = p.BaseURL
			}
			if in.DisplayName == "" {
				in.DisplayName = p.DisplayName
			}
		}
	}
	if f, ok := formatOf(in.Format); !ok {
		problems = append(problems, "没有这种接口格式")
	} else if in.BaseURL == "" {
		in.BaseURL = f.BaseURL
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
	switch {
	case !validRoute(in.SearchRoute):
		problems = append(problems, "没有这种联网搜索路线")
	case in.SearchRoute == RouteDeepSeek && in.Provider != ProviderDeepSeek:
		problems = append(problems, "DeepSeek 搜索只能用于 DeepSeek 服务商配置的模型")
	case in.SearchRoute == RouteProvider && !providerWeb(in.Format):
		problems = append(problems, "服务商自带搜索只能用于 Anthropic Messages 格式的模型")
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
			rate_input, rate_cache_read, rate_cache_write, rate_output, rate_search, provider, search_route)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+modelColumns,
		id, v.in.Format, v.in.BaseURL, sealed, last4, v.in.Model, v.in.DisplayName, v.in.ContextWindow, r[0], r[1], r[2], r[3], r[4], v.in.Provider, v.in.SearchRoute))
	if err != nil {
		return Model{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, err
	}
	return s.resolved(ctx, m)
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
	args := []any{id, v.in.Format, v.in.BaseURL, v.in.Model, v.in.DisplayName, v.in.ContextWindow, r[0], r[1], r[2], r[3], r[4], v.in.Provider, v.in.SearchRoute}
	keySet := ""
	if v.in.Key != "" {
		sealed, last4 := s.seal(id, v.in.Key)
		args = append(args, sealed, last4)
		keySet = ", api_key = $14, key_last4 = $15"
	}
	m, err := scanModel(tx.QueryRow(ctx, `
		UPDATE site_models SET format = $2, base_url = $3, model = $4, display_name = $5, context_window = $6,
			rate_input = $7, rate_cache_read = $8, rate_cache_write = $9, rate_output = $10, rate_search = $11,
			provider = $12, search_route = $13, updated_at = now()`+keySet+`
		WHERE id = $1 RETURNING `+modelColumns, args...))
	if err != nil {
		return Model{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Model{}, err
	}
	return s.resolved(ctx, m)
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

// resolved fills in what a model's search route resolves to now.
func (s *Models) resolved(ctx context.Context, m Model) (Model, error) {
	tv, err := s.Tavily(ctx)
	if err != nil {
		return Model{}, err
	}
	return m.resolve(tv.Set), nil
}

// resolve fills in what the model's search route resolves to, given
// whether the site has a Tavily key.
func (m Model) resolve(tavilyKey bool) Model {
	m.Search, m.SearchReady = Resolve(m.SearchRoute, m.Format, m.Provider, tavilyKey)
	return m
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
// turn kind needs from the endpoint: auth, streaming, and tools accepted;
// and on the provider route its search, which the model has to support.
func (s *Models) test(ctx context.Context, in ModelInput, key string) error {
	route, _ := Resolve(in.SearchRoute, in.Format, in.Provider, false)
	adapter := s.adapterFor(Endpoint{Format: in.Format, Provider: in.Provider, BaseURL: in.BaseURL, Key: key})
	return testModel(ctx, adapter, in.Model, route == RouteProvider, key)
}

// testModel is the connection test of a model (a site's or a reader's
// own): one streaming call declaring a tool, with the provider's own
// search when the model searches with it. A failure is a
// *TestFailedError whose cause has the key taken out.
func testModel(ctx context.Context, adapter Adapter, model string, providerWeb bool, key string) error {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	_, err := adapter.Stream(ctx, Request{
		Model: model,
		Messages: []Message{
			{Role: RoleSystem, Content: "This is a connection test. Do not call any tool."},
			{Role: RoleUser, Content: "Reply with OK."},
		},
		Tools: []ToolSpec{{
			Name:        "connection_check",
			Description: "Declared only to check that tools are accepted; never call it.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		ToolChoice:  "auto",
		MaxTokens:   64,
		ProviderWeb: providerWeb,
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

// ErrAssistantUnavailable: no usable site model serves the turn kind (none
// is mapped, or its key cannot be opened), so readers cannot run it.
var ErrAssistantUnavailable = errors.New("assistant unavailable")

// Turn is what a run calls: the adapter of the model serving a turn kind,
// with the settings read when the run starts.
type Turn struct {
	// Own: the reader's own model (ADR 0010), which is not charged and
	// whose failures are the reader's model's, never the site's.
	Own      bool
	Adapter  Adapter
	Model    string
	Thinking bool
	// DisplayName is the model's name as the admin gave it; Rates are its
	// rates now, which a run is charged at (ADR 0010).
	DisplayName string
	Rates       Rates
	// ContextWindow is the model's, in tokens (DefaultContextWindow when
	// the admin left it unset).
	ContextWindow int
	// Web is the run's way to the web on the Keine tool route; nil when the
	// deployer chose none, or on the provider route.
	Web *WebRoute
	// ProviderWeb: the run searches with the provider's own tools (the
	// provider route), which its calls declare.
	ProviderWeb bool
}

// ForTurn resolves the model serving a turn kind.
func (s *Models) ForTurn(ctx context.Context, kind TurnKind) (Turn, error) {
	var (
		id              int64
		t               Turn
		format, baseURL string
		provider        Provider
		route           SearchRoute
		sealed          []byte
		window          *int
	)
	err := s.db.QueryRow(ctx, `
		SELECT m.id, m.format, m.provider, m.base_url, m.api_key, m.model, t.thinking, m.search_route, m.context_window,
			m.display_name, m.rate_input, m.rate_cache_read, m.rate_cache_write, m.rate_output, m.rate_search
		FROM turn_models t JOIN site_models m ON m.id = t.model_id WHERE t.kind = $1`, kind).
		Scan(&id, &format, &provider, &baseURL, &sealed, &t.Model, &t.Thinking, &route, &window,
			&t.DisplayName, &t.Rates.Input, &t.Rates.CacheRead, &t.Rates.CacheWrite, &t.Rates.Output, &t.Rates.Search)
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, fmt.Errorf("%w: %s is not mapped", ErrAssistantUnavailable, kind)
	}
	if err != nil {
		return Turn{}, err
	}
	t.ContextWindow = DefaultContextWindow
	if window != nil {
		t.ContextWindow = *window
	}
	key := ""
	if len(sealed) > 0 {
		if key, err = s.open(id, sealed); err != nil {
			return Turn{}, fmt.Errorf("%w: %w", ErrAssistantUnavailable, err)
		}
	}
	tavily, err := s.tavilyKey(ctx, s.db)
	if err != nil {
		return Turn{}, fmt.Errorf("%w: tavily key: %w", ErrAssistantUnavailable, err)
	}
	resolved, ok := Resolve(route, format, provider, tavily != "")
	if !ok {
		return Turn{}, fmt.Errorf("%w: %s has no search route", ErrAssistantUnavailable, kind)
	}
	t.Adapter = s.adapterFor(Endpoint{Format: format, Provider: provider, BaseURL: baseURL, Key: key})
	t.Web = s.routeOf(resolved, key, tavily)
	t.ProviderWeb = resolved == RouteProvider
	return t, nil
}
