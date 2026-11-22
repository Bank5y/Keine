package assistant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/netguard"
	"github.com/Bank5y/keine/internal/secret"
)

// OwnContextWindow is a reader's own model's context window when they
// leave it unset: a conservative one (ADR 0010).
const OwnContextWindow = 32_000

var (
	// ErrOwnModelsClosed: the deployer has not opened own models.
	ErrOwnModelsClosed = errors.New("own models are not open")
	// ErrOwnModelNotFound: the reader has no own model.
	ErrOwnModelNotFound = errors.New("no own model")
	// ErrOwnTavilyKeyMissing: Tavily search was chosen without a key.
	ErrOwnTavilyKeyMissing = errors.New("tavily search needs a key")
	// ErrOwnKeyUnreadable: the reader's stored keys do not open with the
	// current master key, so they have to enter them again.
	ErrOwnKeyUnreadable = errors.New("own model key cannot be decrypted")
)

// OwnModel is a reader's own model as they see it: the keys only by
// their last four.
type OwnModel struct {
	Format        string      `json:"format"`
	Provider      Provider    `json:"provider"`
	BaseURL       string      `json:"baseUrl"`
	Model         string      `json:"model"`
	ContextWindow int         `json:"contextWindow"`
	Search        SearchRoute `json:"search"`
	KeyLast4      string      `json:"keyLast4"`
	TavilyLast4   string      `json:"tavilyLast4"`
	InUse         bool        `json:"inUse"`
	UpdatedAt     time.Time   `json:"updatedAt"`
}

// OwnModelInput is a reader's own model as they submit it. An empty Key
// or TavilyKey keeps the stored one.
type OwnModelInput struct {
	Format        string      `json:"format"`
	Provider      Provider    `json:"provider"`
	BaseURL       string      `json:"baseUrl"`
	Key           string      `json:"key"`
	Model         string      `json:"model"`
	ContextWindow *int        `json:"contextWindow"`
	Search        SearchRoute `json:"search"`
	TavilyKey     string      `json:"tavilyKey"`
}

// OwnModels keeps the readers' own models (ADR 0010): at most one each,
// and each reader's switch to run on it. The deployer opens them; their
// endpoints are reached through the SSRF guard.
type OwnModels struct {
	db         *pgxpool.Pool
	box        *secret.Box
	adapterFor AdapterFor
	searchFor  SearchFor
	client     *http.Client
	open       bool
}

// NewOwnModels keeps own models; open is the deployer's switch, which
// needs a box to seal the keys with.
func NewOwnModels(db *pgxpool.Pool, box *secret.Box, adapterFor AdapterFor, searchFor SearchFor, guard netguard.Policy, open bool) *OwnModels {
	return &OwnModels{db: db, box: box, adapterFor: adapterFor, searchFor: searchFor, client: guard.Client(), open: open && box != nil}
}

// Open tells whether the deployer opened own models.
func (s *OwnModels) Open() bool { return s.open }

const ownColumns = `format, provider, base_url, model, context_window, search, key_last4, tavily_last4, in_use, updated_at`

func scanOwn(row pgx.Row) (OwnModel, error) {
	var m OwnModel
	err := row.Scan(&m.Format, &m.Provider, &m.BaseURL, &m.Model, &m.ContextWindow, &m.Search, &m.KeyLast4, &m.TavilyLast4, &m.InUse, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrOwnModelNotFound
	}
	return m, err
}

// Get is the reader's own model, ErrOwnModelNotFound when they have none.
func (s *OwnModels) Get(ctx context.Context, user int64) (OwnModel, error) {
	if !s.open {
		return OwnModel{}, ErrOwnModelsClosed
	}
	return scanOwn(s.db.QueryRow(ctx, "SELECT "+ownColumns+" FROM own_models WHERE user_id = $1", user))
}

// validateOwn checks a submitted own model and fills in its defaults:
// the provider's or the format's address, the conservative window, and no
// web.
func validateOwn(in OwnModelInput) (OwnModelInput, error) {
	var problems []string
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Model = strings.TrimSpace(in.Model)
	in.Key = strings.TrimSpace(in.Key)
	in.TavilyKey = strings.TrimSpace(in.TavilyKey)
	if in.Provider != ProviderNone {
		p, ok := preset(in.Provider)
		switch {
		case !ok:
			problems = append(problems, "没有这个服务商配置")
		case in.Format != p.Format:
			problems = append(problems, p.Name+" 只能用 OpenAI 兼容格式")
		case in.BaseURL == "":
			in.BaseURL = p.BaseURL
		}
	}
	if f, ok := formatOf(in.Format); !ok {
		problems = append(problems, "没有这种接口格式")
	} else if in.BaseURL == "" {
		in.BaseURL = f.BaseURL
	}
	if u, err := url.Parse(in.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		problems = append(problems, "地址要以 https:// 开头（部署者允许的内网地址可以用 http://）")
	}
	if in.Model == "" {
		problems = append(problems, "模型名不能为空")
	}
	if in.ContextWindow == nil {
		w := OwnContextWindow
		in.ContextWindow = &w
	} else if *in.ContextWindow <= 0 {
		problems = append(problems, "上下文长度要是正整数")
	}
	if in.Search == RouteAuto {
		in.Search = RouteOff
	}
	if in.Search != RouteTavily {
		// a Tavily key is only tested, and kept, for Tavily search
		in.TavilyKey = ""
	}
	switch in.Search {
	case RouteOff, RouteTavily:
	case RouteProvider:
		if !providerWeb(in.Format) {
			problems = append(problems, "服务商自带搜索只能用于 Anthropic 格式")
		}
	case RouteDeepSeek:
		if in.Provider != ProviderDeepSeek {
			problems = append(problems, "DeepSeek 搜索只能用于 DeepSeek")
		}
	default:
		problems = append(problems, "没有这种联网搜索方式")
	}
	if len(problems) > 0 {
		return in, &InvalidModelError{Problems: problems}
	}
	return in, nil
}

func ownKeyPurpose(user int64) string    { return fmt.Sprintf("own_model:%d", user) }
func ownTavilyPurpose(user int64) string { return fmt.Sprintf("own_tavily:%d", user) }

// sealedKeys are a stored own model's keys as stored.
type sealedKeys struct{ model, tavily []byte }

func (s *OwnModels) sealed(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, user int64, lock bool) (sealedKeys, bool, error) {
	var k sealedKeys
	query := "SELECT api_key, tavily_key FROM own_models WHERE user_id = $1"
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRow(ctx, query, user).Scan(&k.model, &k.tavily)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, false, nil
	}
	return k, err == nil, err
}

func (s *OwnModels) openKey(sealed []byte, purpose string) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	key, err := s.box.Open(sealed, purpose)
	if err != nil {
		return "", ErrKeyUnreadable
	}
	return key, nil
}

func (s *OwnModels) endpoint(format string, provider Provider, baseURL, key string) Endpoint {
	return Endpoint{Format: format, Provider: provider, BaseURL: baseURL, Key: key, Client: s.client, Conservative: provider == ProviderNone}
}

// Save tests the model with a streaming call that declares a tool, and a
// new Tavily key with one real search, then stores it; the reader's switch
// stays as it was.
func (s *OwnModels) Save(ctx context.Context, user int64, in OwnModelInput) (OwnModel, error) {
	if !s.open {
		return OwnModel{}, ErrOwnModelsClosed
	}
	in, err := validateOwn(in)
	if err != nil {
		return OwnModel{}, err
	}
	before, _, err := s.sealed(ctx, s.db, user, false)
	if err != nil {
		return OwnModel{}, err
	}
	key := in.Key
	if key == "" {
		if key, err = s.openKey(before.model, ownKeyPurpose(user)); err != nil {
			return OwnModel{}, err
		}
	}
	tavily := in.TavilyKey
	if in.Search == RouteTavily && tavily == "" {
		if tavily, err = s.openKey(before.tavily, ownTavilyPurpose(user)); err != nil {
			return OwnModel{}, err
		}
		if tavily == "" {
			return OwnModel{}, ErrOwnTavilyKeyMissing
		}
	}
	adapter := s.adapterFor(s.endpoint(in.Format, in.Provider, in.BaseURL, key))
	if err := testModel(ctx, adapter, in.Model, in.Search == RouteProvider, key); err != nil {
		return OwnModel{}, err
	}
	if in.Search == RouteTavily && in.TavilyKey != "" {
		testCtx, cancel := context.WithTimeout(ctx, testTimeout)
		_, _, err := s.searchFor(RouteTavily, tavily).Search.Search(testCtx, testQuery, 1)
		cancel()
		if err != nil {
			return OwnModel{}, searchTestFailure(err, tavily)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OwnModel{}, err
	}
	defer tx.Rollback(ctx)
	now, _, err := s.sealed(ctx, tx, user, true)
	if err != nil {
		return OwnModel{}, err
	}
	// a key kept is the key tested only if no other save replaced it since
	if (in.Key == "" && !bytes.Equal(now.model, before.model)) || (in.Search == RouteTavily && in.TavilyKey == "" && !bytes.Equal(now.tavily, before.tavily)) {
		return OwnModel{}, ErrModelChanged
	}
	modelKey, modelLast4 := now.model, ""
	if in.Key != "" {
		modelKey, modelLast4 = s.box.Seal(in.Key, ownKeyPurpose(user)), lastFour(in.Key)
	}
	tavilyKey, tavilyLast4 := now.tavily, ""
	switch {
	case in.Search != RouteTavily:
		// a key no longer used is not kept
		tavilyKey = nil
	case in.TavilyKey != "":
		tavilyKey, tavilyLast4 = s.box.Seal(in.TavilyKey, ownTavilyPurpose(user)), lastFour(in.TavilyKey)
	}
	m, err := scanOwn(tx.QueryRow(ctx, `
		INSERT INTO own_models (user_id, format, provider, base_url, api_key, key_last4, model, context_window, search, tavily_key, tavily_last4)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (user_id) DO UPDATE SET format = EXCLUDED.format, provider = EXCLUDED.provider,
			base_url = EXCLUDED.base_url, model = EXCLUDED.model, context_window = EXCLUDED.context_window,
			search = EXCLUDED.search, updated_at = now(),
			api_key = CASE WHEN $12 THEN EXCLUDED.api_key ELSE own_models.api_key END,
			key_last4 = CASE WHEN $12 THEN EXCLUDED.key_last4 ELSE own_models.key_last4 END,
			tavily_key = CASE WHEN $13 THEN EXCLUDED.tavily_key ELSE own_models.tavily_key END,
			tavily_last4 = CASE WHEN $13 THEN EXCLUDED.tavily_last4 ELSE own_models.tavily_last4 END
		RETURNING `+ownColumns,
		user, in.Format, in.Provider, in.BaseURL, modelKey, modelLast4, in.Model, *in.ContextWindow, in.Search, tavilyKey, tavilyLast4,
		in.Key != "", in.TavilyKey != "" || in.Search != RouteTavily))
	if err != nil {
		return OwnModel{}, err
	}
	return m, tx.Commit(ctx)
}

// SearchName is where the model's searches go, by name; "" for none.
func (m OwnModel) SearchName() string {
	if m.Search == RouteProvider {
		return m.Model
	}
	return searchNames[m.Search]
}

// Delete removes the reader's own model, and with it their switch.
func (s *OwnModels) Delete(ctx context.Context, user int64) error {
	if !s.open {
		return ErrOwnModelsClosed
	}
	tag, err := s.db.Exec(ctx, "DELETE FROM own_models WHERE user_id = $1", user)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrOwnModelNotFound
	}
	return err
}

// Use switches the reader's runs to their own model, or back to the site's.
func (s *OwnModels) Use(ctx context.Context, user int64, on bool) (OwnModel, error) {
	if !s.open {
		return OwnModel{}, ErrOwnModelsClosed
	}
	return scanOwn(s.db.QueryRow(ctx, "UPDATE own_models SET in_use = $2 WHERE user_id = $1 RETURNING "+ownColumns, user, on))
}

// InUse is the reader's own model when they run on it: ok is false when
// own models are closed, or the reader has none or has it switched off.
func (s *OwnModels) InUse(ctx context.Context, user int64) (OwnModel, bool, error) {
	if !s.open {
		return OwnModel{}, false, nil
	}
	m, err := scanOwn(s.db.QueryRow(ctx, "SELECT "+ownColumns+" FROM own_models WHERE user_id = $1 AND in_use", user))
	if errors.Is(err, ErrOwnModelNotFound) {
		return OwnModel{}, false, nil
	}
	return m, err == nil, err
}

// ForReader is the turn a reader's run takes on their own model, read as
// the run starts (ADR 0010); ok is false when they do not run on one. It
// is not charged, thinks with the provider's default, and reaches the web
// only the way the reader chose, on their own keys.
func (s *OwnModels) ForReader(ctx context.Context, user int64) (Turn, bool, error) {
	if !s.open {
		return Turn{}, false, nil
	}
	// the settings and the keys in one read, so a save cannot pair them
	var (
		m    OwnModel
		keys sealedKeys
	)
	err := s.db.QueryRow(ctx, "SELECT "+ownColumns+", api_key, tavily_key FROM own_models WHERE user_id = $1 AND in_use", user).
		Scan(&m.Format, &m.Provider, &m.BaseURL, &m.Model, &m.ContextWindow, &m.Search, &m.KeyLast4, &m.TavilyLast4, &m.InUse, &m.UpdatedAt, &keys.model, &keys.tavily)
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, false, nil
	}
	if err != nil {
		return Turn{}, false, err
	}
	key, err := s.openKey(keys.model, ownKeyPurpose(user))
	if err != nil {
		return Turn{}, false, ErrOwnKeyUnreadable
	}
	t := Turn{
		Own:           true,
		Adapter:       s.adapterFor(s.endpoint(m.Format, m.Provider, m.BaseURL, key)),
		Model:         m.Model,
		DisplayName:   m.Model,
		ContextWindow: m.ContextWindow,
		ProviderWeb:   m.Search == RouteProvider,
	}
	switch m.Search {
	case RouteDeepSeek:
		w := s.searchFor(RouteDeepSeek, key)
		w.Own = true
		t.Web = &w
	case RouteTavily:
		tavily, err := s.openKey(keys.tavily, ownTavilyPurpose(user))
		if err != nil {
			return Turn{}, false, ErrOwnKeyUnreadable
		}
		w := s.searchFor(RouteTavily, tavily)
		w.Own = true
		t.Web = &w
	}
	return t, true, nil
}
