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

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Bank5y/keine/internal/storage"
)

// MinIO no longer publishes container images, so the tests run against
// RustFS, another S3-compatible server. Any S3 server can be used instead by
// setting KEINE_TEST_S3_ENDPOINT, KEINE_TEST_S3_ACCESS_KEY and
// KEINE_TEST_S3_SECRET_KEY.
const s3Image = "rustfs/rustfs:1.0.4"

var (
	s3Once sync.Once
	s3Cfg  storage.S3Config
	s3Err  error
)

func s3Server(t *testing.T) storage.S3Config {
	t.Helper()
	s3Once.Do(func() {
		if ep := os.Getenv("KEINE_TEST_S3_ENDPOINT"); ep != "" {
			s3Cfg = storage.S3Config{Endpoint: ep, AccessKey: os.Getenv("KEINE_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("KEINE_TEST_S3_SECRET_KEY"), Insecure: true, PathStyle: true}
			return
		}
		ctx := context.Background()
		const user, pass = "keine-test", "keine-test-secret"
		c, err := testcontainers.Run(ctx, s3Image,
			testcontainers.WithExposedPorts("9000/tcp"),
			testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": user, "RUSTFS_SECRET_KEY": pass}),
			testcontainers.WithWaitStrategy(wait.ForListeningPort("9000/tcp").WithStartupTimeout(90*time.Second)),
		)
		if err != nil {
			s3Err = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			s3Err = err
			return
		}
		port, err := c.MappedPort(ctx, "9000/tcp")
		if err != nil {
			s3Err = err
			return
		}
		s3Cfg = storage.S3Config{Endpoint: fmt.Sprintf("%s:%s", host, port.Port()), AccessKey: user, SecretKey: pass, Insecure: true, PathStyle: true, Region: "us-east-1"}
	})
	if s3Err != nil {
		t.Fatalf("start s3 server: %v", s3Err)
	}
	return s3Cfg
}

// NewS3Store returns a store on a fresh bucket of the shared S3 server.
func NewS3Store(t *testing.T) *storage.S3 {
	t.Helper()
	cfg := s3Server(t)
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	cfg.Bucket = "t-" + hex.EncodeToString(b)
	cfg.CreateBucket = true
	var s *storage.S3
	var err error
	// the server accepts connections a moment before it serves requests
	for i := 0; i < 30; i++ {
		if s, err = storage.NewS3(context.Background(), cfg); err == nil {
			return s
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("open s3 store: %v", err)
	return nil
}
