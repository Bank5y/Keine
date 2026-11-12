// Package testutil provides the real infrastructure the HTTP API tests run against.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	pgOnce sync.Once
	pgURL  string
	pgErr  error
)

// adminURL returns a connection string to a PostgreSQL server shared by the
// whole test binary. KEINE_TEST_DATABASE_URL points at an existing server;
// otherwise a container is started once.
func adminURL(t *testing.T) string {
	t.Helper()
	pgOnce.Do(func() {
		if u := os.Getenv("KEINE_TEST_DATABASE_URL"); u != "" {
			pgURL = u
			return
		}
		ctx := context.Background()
		c, err := tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithDatabase("keine"),
			tcpostgres.WithUsername("keine"),
			tcpostgres.WithPassword("keine"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			pgErr = err
			return
		}
		pgURL, pgErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	if pgErr != nil {
		t.Fatalf("start postgres: %v", pgErr)
	}
	return pgURL
}

// NewDatabase creates an empty database for one test and drops it afterwards.
// It returns the connection string of the new database.
func NewDatabase(t *testing.T) string {
	t.Helper()
	base := adminURL(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "t_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
}
