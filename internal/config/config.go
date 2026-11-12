// Package config reads the deployment's settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Addr          string
	DatabaseURL   string
	AdminUsername string
	AdminPassword string
	SecureCookies bool
	MaxUploadMB   int64

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
	var err error
	if c.SecureCookies, err = boolEnv("KEINE_SECURE_COOKIES", false); err != nil {
		return c, err
	}
	if c.MaxUploadMB, err = intEnv("KEINE_MAX_UPLOAD_MB", 4096); err != nil {
		return c, err
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
