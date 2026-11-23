// Package config reads the deployment's settings from the environment.
package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Bank5y/keine/internal/secret"
	"github.com/Bank5y/keine/internal/storage"
)

type Config struct {
	Addr          string
	DatabaseURL   string
	AdminUsername string
	AdminPassword string
	SecureCookies bool
	MaxUploadMB   int64
	// MasterKey encrypts the model keys stored in the database.
	MasterKey string
	// RunDeadlineSeconds ends an assistant run; at three quarters of it
	// the run stops looking things up and answers.
	RunDeadlineSeconds int64
	// MaxRuns is how many assistant runs go at once; one more is turned
	// away as busy, not queued.
	MaxRuns int64
	// OwnModels opens readers' own models; it needs MasterKey.
	OwnModels bool
	// OwnModelNetworks are the CIDRs an own model may be at besides the
	// public internet, over plain http too.
	OwnModelNetworks []string

	// Storage is "local" (files under DataDir) or "s3".
	Storage string
	DataDir string
	S3      S3
}

type S3 struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey, Prefix string
	Insecure, PathStyle, CreateBucket                      bool
}

func FromEnv() (Config, error) {
	c := Config{
		Addr:          env("KEINE_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("KEINE_DATABASE_URL"),
		AdminUsername: os.Getenv("KEINE_ADMIN_USERNAME"),
		AdminPassword: os.Getenv("KEINE_ADMIN_PASSWORD"),
		MasterKey:     os.Getenv("KEINE_MASTER_KEY"),
		DataDir:       env("KEINE_DATA_DIR", "./data"),
		Storage:       env("KEINE_STORAGE", "local"),
		S3: S3{
			Endpoint:  os.Getenv("KEINE_S3_ENDPOINT"),
			Region:    os.Getenv("KEINE_S3_REGION"),
			Bucket:    os.Getenv("KEINE_S3_BUCKET"),
			AccessKey: os.Getenv("KEINE_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("KEINE_S3_SECRET_KEY"),
			Prefix:    os.Getenv("KEINE_S3_PREFIX"),
		},
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("KEINE_DATABASE_URL is required")
	}
	if (c.AdminUsername == "") != (c.AdminPassword == "") {
		return c, fmt.Errorf("set both KEINE_ADMIN_USERNAME and KEINE_ADMIN_PASSWORD, or neither")
	}
	if c.MasterKey != "" && len(c.MasterKey) < secret.MinMasterKeyLen {
		return c, fmt.Errorf("KEINE_MASTER_KEY must be at least %d characters", secret.MinMasterKeyLen)
	}
	var err error
	if c.SecureCookies, err = boolEnv("KEINE_SECURE_COOKIES", false); err != nil {
		return c, err
	}
	if c.MaxUploadMB, err = intEnv("KEINE_MAX_UPLOAD_MB", 4096); err != nil {
		return c, err
	}
	if c.RunDeadlineSeconds, err = intEnv("KEINE_RUN_DEADLINE_SECONDS", 120); err != nil {
		return c, err
	}
	if c.MaxRuns, err = intEnv("KEINE_MAX_RUNS", 16); err != nil {
		return c, err
	}
	if c.OwnModels, err = boolEnv("KEINE_OWN_MODELS", false); err != nil {
		return c, err
	}
	if c.OwnModels && c.MasterKey == "" {
		return c, fmt.Errorf("KEINE_OWN_MODELS needs KEINE_MASTER_KEY to seal readers' keys with")
	}
	if v := os.Getenv("KEINE_OWN_MODEL_NETWORKS"); v != "" {
		c.OwnModelNetworks = strings.Split(v, ",")
	}
	switch c.Storage {
	case "local":
	case "s3":
		if c.S3.Endpoint == "" || c.S3.Bucket == "" {
			return c, fmt.Errorf("KEINE_STORAGE=s3 needs KEINE_S3_ENDPOINT and KEINE_S3_BUCKET")
		}
		for key, dst := range map[string]*bool{"KEINE_S3_INSECURE": &c.S3.Insecure, "KEINE_S3_PATH_STYLE": &c.S3.PathStyle, "KEINE_S3_CREATE_BUCKET": &c.S3.CreateBucket} {
			if *dst, err = boolEnv(key, false); err != nil {
				return c, err
			}
		}
	default:
		return c, fmt.Errorf("KEINE_STORAGE must be local or s3, got %q", c.Storage)
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolEnv(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func intEnv(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}

// OpenStore opens the course files where the deployment keeps them: on
// local disk, or in an S3 bucket, waiting a while for one started
// alongside.
func (cfg Config) OpenStore(ctx context.Context) (storage.Store, error) {
	if cfg.Storage != "s3" {
		slog.Info("course files on local disk", "dir", cfg.DataDir)
		return storage.NewLocal(cfg.DataDir), nil
	}
	slog.Info("course files in s3", "endpoint", cfg.S3.Endpoint, "bucket", cfg.S3.Bucket)
	s3cfg := storage.S3Config{
		Endpoint:     cfg.S3.Endpoint,
		Region:       cfg.S3.Region,
		Bucket:       cfg.S3.Bucket,
		AccessKey:    cfg.S3.AccessKey,
		SecretKey:    cfg.S3.SecretKey,
		Prefix:       cfg.S3.Prefix,
		Insecure:     cfg.S3.Insecure,
		PathStyle:    cfg.S3.PathStyle,
		CreateBucket: cfg.S3.CreateBucket,
	}
	// an object store started alongside us may need a moment to answer
	var err error
	for attempt := 1; ; attempt++ {
		var s *storage.S3
		if s, err = storage.NewS3(ctx, s3cfg); err == nil || attempt == 15 {
			return s, err
		}
		slog.Warn("object storage not ready, retrying", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
