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

	"github.com/Bank5y/keine/internal/db"
)

var (
	pgOnce sync.Once
	pgURL  string
	pgErr  error

	tmplOnce sync.Once
	tmplName string
	tmplErr  error
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
			// a throwaway server: durability buys nothing, and parallel
			// tests each hold a pool of connections
			testcontainers.WithCmdArgs("-c", "synchronous_commit=off", "-c", "full_page_writes=off", "-c", "max_connections=1000"),
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

// template is a database the migrations have been applied to, made once
// per schema version: a test's database is a copy of it, so the migrations
// run once and not once a test. It is named by the schema version, so a
// server shared between runs (KEINE_TEST_DATABASE_URL) keeps one per
// version, and a test binary of another version makes its own.
func template(t *testing.T) string {
	t.Helper()
	base := adminURL(t)
	tmplOnce.Do(func() { tmplName, tmplErr = makeTemplate(base) })
	if tmplErr != nil {
		t.Fatalf("make template database: %v", tmplErr)
	}
	return tmplName
}

func makeTemplate(base string) (string, error) {
	ctx := context.Background()
	name := "keine_tmpl_" + db.SchemaVersion()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	// test binaries of several packages may share a server
	const lockID = 7_314_160
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return "", err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockID) //nolint:errcheck
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return name, nil
	}
	// migrated under another name and renamed when done, so a run cut
	// short leaves no half-migrated template behind under the real name
	building := name + "_building"
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+building+" WITH (FORCE)"); err != nil {
		return "", err
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+building); err != nil {
		return "", err
	}
	pool, err := db.Open(ctx, databaseURL(base, building))
	if err != nil {
		return "", err
	}
	pool.Close()
	if _, err := conn.Exec(ctx, "ALTER DATABASE "+building+" RENAME TO "+name); err != nil {
		return "", err
	}
	return name, nil
}

// NewDatabase creates a database for one test, migrated as the server
// would migrate an empty one, and drops it afterwards. It returns the
// connection string of the new database.
func NewDatabase(t *testing.T) string {
	t.Helper()
	base := adminURL(t)
	tmpl := template(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "t_" + hex.EncodeToString(b)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+tmpl); err != nil {
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
	return databaseURL(base, name)
}

// databaseURL is base's connection string for the database name.
func databaseURL(base, name string) string {
	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		panic(fmt.Sprintf("parse url: %v", err))
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
}
