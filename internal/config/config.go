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
	DataDir       string
}

func FromEnv() (Config, error) {
	c := Config{
		Addr:          env("KEINE_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("KEINE_DATABASE_URL"),
		AdminUsername: os.Getenv("KEINE_ADMIN_USERNAME"),
		AdminPassword: os.Getenv("KEINE_ADMIN_PASSWORD"),
		DataDir:       env("KEINE_DATA_DIR", "./data"),
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
