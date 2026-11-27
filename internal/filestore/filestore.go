// Package filestore keeps the deployment's object storage settings, which an
// admin edits in the console, and points the routed course file store at
// the bucket they describe. The environment (KEINE_STORAGE, KEINE_S3_*)
// only gives the settings a deployment starts with.
package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Bank5y/keine/internal/secret"
	"github.com/Bank5y/keine/internal/storage"
)

// Settings say where media files are kept and how browsers get them.
type Settings struct {
	// Enabled sends media to the bucket; off, every new file goes to local
	// disk (files already in the bucket are still read from it).
	Enabled      bool   `json:"enabled"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	AccessKey    string `json:"accessKey"`
	Prefix       string `json:"prefix"`
	Insecure     bool   `json:"insecure"`
	PathStyle    bool   `json:"pathStyle"`
	CreateBucket bool   `json:"createBucket"`

	Media          storage.Media    `json:"media"`
	Delivery       storage.Delivery `json:"delivery"`
	PresignSeconds int              `json:"presignSeconds"`
	CDNBase        string           `json:"cdnBase"`

	// SealedSecret is the secret key an admin saved, sealed with the master
	// key; without one the environment's secret key is used.
	SealedSecret []byte `json:"sealedSecret,omitempty"`
	SecretLast4  string `json:"secretLast4,omitempty"`
}

const (
	settingsKey    = "storage"
	secretPurpose  = "storage:s3"
	defaultPresign = 3600
	// a presigned link outlasts the five minutes a browser may keep the
	// redirect to it
	minPresign = 600
	// a presigned link lasts at most 7 days, the S3 limit
	maxPresign = 7 * 24 * 3600
)

var (
	ErrMasterKeyMissing = errors.New("no master key to store the secret key with")
	ErrNotConnected     = errors.New("object storage is not connected")
	ErrBusy             = errors.New("a migration is running")
	// ErrConnect is a bucket that did not answer to settings being saved.
	ErrConnect = errors.New("连接对象存储失败")
)

// Invalid is a setting an admin got wrong.
type Invalid struct{ Message string }

func (e Invalid) Error() string { return e.Message }

// Configured tells whether the settings name a bucket.
func (st Settings) Configured() bool { return st.Endpoint != "" && st.Bucket != "" }

// normalize fills what was left out and rejects what cannot work.
func (st *Settings) normalize() error {
	st.Endpoint = strings.TrimSpace(st.Endpoint)
	st.Bucket = strings.TrimSpace(st.Bucket)
	st.CDNBase = strings.TrimSpace(st.CDNBase)
	if st.Media == "" {
		st.Media = storage.MediaVideo
	}
	if st.Delivery == "" {
		st.Delivery = storage.DeliverProxy
	}
	if st.PresignSeconds == 0 {
		st.PresignSeconds = defaultPresign
	}
	switch {
	case !st.Media.Valid():
		return Invalid{"上传范围只能是仅视频或视频与图片"}
	case !st.Delivery.Valid():
		return Invalid{"访问方式只能是代理、Presign 或 CDN"}
	case st.PresignSeconds < minPresign || st.PresignSeconds > maxPresign:
		return Invalid{"Presign 有效期须在 10 分钟到 7 天之间"}
	case st.Delivery == storage.DeliverCDN && !strings.HasPrefix(st.CDNBase, "https://") && !strings.HasPrefix(st.CDNBase, "http://"):
		return Invalid{"CDN 地址须以 http:// 或 https:// 开头"}
	case st.Enabled && !st.Configured():
		return Invalid{"启用对象存储需要填写 Endpoint 和 Bucket"}
	case st.Prefix != "" && (!strings.HasSuffix(st.Prefix, "/") || storage.CheckKey(strings.TrimSuffix(st.Prefix, "/")) != nil):
		return Invalid{`前缀须形如 "a/b/"`}
	}
	return nil
}

// Status is the state of the bucket connection.
type Status struct {
	// Connected is true while a bucket answers to the saved settings.
	Connected bool `json:"connected"`
	// Active is true while media goes to the bucket.
	Active bool   `json:"active"`
	Error  string `json:"error,omitempty"`
}

// Service owns the settings and the connection.
type Service struct {
	db       *pgxpool.Pool
	box      *secret.Box
	defaults Settings
	// envSecret is the secret key from the environment.
	envSecret string
	files     *storage.Routed

	mu     sync.Mutex
	cloud  *storage.S3
	status Status

	jobs jobs
}

// New makes the service; defaults are the settings until an admin saves
// some. Start connects.
func New(db *pgxpool.Pool, box *secret.Box, files *storage.Routed, defaults Settings, envSecret string) *Service {
	return &Service{db: db, box: box, files: files, defaults: defaults, envSecret: envSecret}
}

// Load reads the saved settings, or the defaults.
func (s *Service) Load(ctx context.Context) (Settings, error) {
	st := s.defaults
	var raw []byte
	err := s.db.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", settingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = st.normalize()
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st = Settings{}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, err
	}
	_ = st.normalize()
	return st, nil
}

// HasMasterKey tells whether a secret key can be saved.
func (s *Service) HasMasterKey() bool { return s.box != nil }

// SecretFromEnv tells whether the settings use the environment's secret key.
func (s *Service) SecretFromEnv(st Settings) bool {
	return len(st.SealedSecret) == 0 && s.envSecret != ""
}

func (s *Service) secretOf(st Settings) (string, error) {
	if len(st.SealedSecret) == 0 {
		return s.envSecret, nil
	}
	if s.box == nil {
		return "", ErrMasterKeyMissing
	}
	return s.box.Open(st.SealedSecret, secretPurpose)
}

func (s *Service) connect(ctx context.Context, st Settings) (*storage.S3, error) {
	secretKey, err := s.secretOf(st)
	if err != nil {
		return nil, err
	}
	return storage.NewS3(ctx, storage.S3Config{
		Endpoint: st.Endpoint, Region: st.Region, Bucket: st.Bucket, AccessKey: st.AccessKey, SecretKey: secretKey,
		Insecure: st.Insecure, PathStyle: st.PathStyle, Prefix: st.Prefix, CreateBucket: st.CreateBucket,
	})
}

// apply points the store at cloud (nil: everything local) under st.
func (s *Service) apply(st Settings, cloud *storage.S3, connErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cloud = cloud
	s.status = Status{Connected: cloud != nil}
	if connErr != nil {
		s.status.Error = connErr.Error()
	}
	if cloud == nil {
		s.files.SetRoute(storage.Route{})
		return
	}
	if !st.Enabled {
		// connected but off: new files go local (no Media covers them),
		// while what is in the bucket is still read, copied and deleted
		s.files.SetRoute(storage.Route{Cloud: cloud})
		return
	}
	s.status.Active = true
	s.files.SetRoute(storage.Route{
		Cloud: cloud,
		Media: st.Media,
		Link:  cloud.Linker(st.Delivery, time.Duration(st.PresignSeconds)*time.Second, st.CDNBase),
	})
}

// Start connects to the saved bucket, trying for a while for one started
// alongside. A bucket that does not answer leaves every file local, with
// the error in Status, rather than stopping the deployment: an admin can
// fix the settings in the console.
func (s *Service) Start(ctx context.Context, attempts int, wait time.Duration) error {
	st, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if !st.Configured() {
		s.apply(st, nil, nil)
		slog.Info("course files on local disk; no object storage configured")
		return nil
	}
	var cloud *storage.S3
	for attempt := 1; ; attempt++ {
		if cloud, err = s.connect(ctx, st); err == nil || attempt >= attempts {
			break
		}
		slog.Warn("object storage not ready, retrying", "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	if err != nil {
		slog.Error("object storage unreachable; new files go to local disk", "endpoint", st.Endpoint, "err", err)
	} else {
		slog.Info("object storage connected", "endpoint", st.Endpoint, "bucket", st.Bucket, "enabled", st.Enabled, "media", st.Media)
	}
	s.apply(st, cloud, err)
	return nil
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Cloud is the connected bucket, or nil.
func (s *Service) Cloud() *storage.S3 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cloud
}

// Update is what an admin sends: the settings, and a new secret key, or ""
// to keep the one in use.
type Update struct {
	Settings
	SecretKey string `json:"secretKey"`
}

// merged is u over the saved settings.
func (s *Service) merged(ctx context.Context, u Update) (Settings, error) {
	cur, err := s.Load(ctx)
	if err != nil {
		return Settings{}, err
	}
	st := u.Settings
	st.SealedSecret, st.SecretLast4 = cur.SealedSecret, cur.SecretLast4
	if u.SecretKey != "" {
		if s.box == nil {
			return st, ErrMasterKeyMissing
		}
		st.SealedSecret = s.box.Seal(u.SecretKey, secretPurpose)
		st.SecretLast4 = last4(u.SecretKey)
	}
	return st, st.normalize()
}

func last4(v string) string {
	if len(v) <= 4 {
		return ""
	}
	return v[len(v)-4:]
}

// Save stores the settings once their bucket answers (or with none named)
// and switches the store over to them.
func (s *Service) Save(ctx context.Context, u Update) (Settings, error) {
	// a running migration keeps writing to the bucket it started with
	if s.migrating() {
		return Settings{}, ErrBusy
	}
	st, err := s.merged(ctx, u)
	if err != nil {
		return st, err
	}
	var cloud *storage.S3
	if st.Configured() {
		if cloud, err = s.connect(ctx, st); err != nil {
			return st, fmt.Errorf("%w: %v", ErrConnect, err)
		}
	}
	raw, _ := json.Marshal(st)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, settingsKey, raw); err != nil {
		return st, err
	}
	s.apply(st, cloud, nil)
	return st, nil
}

// Test connects with u (unsaved) and writes, reads and deletes an object.
func (s *Service) Test(ctx context.Context, u Update) (storage.ProbeResult, error) {
	st, err := s.merged(ctx, u)
	if err != nil {
		return storage.ProbeResult{}, err
	}
	if !st.Configured() {
		return storage.ProbeResult{}, Invalid{"需要填写 Endpoint 和 Bucket"}
	}
	start := time.Now()
	cloud, err := s.connect(ctx, st)
	conn := storage.ProbeStep{Name: "connect", OK: err == nil, Ms: time.Since(start).Milliseconds()}
	if err != nil {
		conn.Error = err.Error()
		return storage.ProbeResult{Steps: []storage.ProbeStep{conn}}, nil
	}
	res := storage.Probe(ctx, cloud)
	res.Steps = append([]storage.ProbeStep{conn}, res.Steps...)
	return res, nil
}
