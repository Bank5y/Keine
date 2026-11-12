// Command keine runs the Keine learning space.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bank5y/keine/internal/config"
	"github.com/Bank5y/keine/internal/server"
	"github.com/Bank5y/keine/internal/storage"
	"github.com/Bank5y/keine/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("keine stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	webFS := web.FS()
	if webFS == nil {
		slog.Warn("front end not built; serving the API only")
	}
	files, err := openStorage(ctx, cfg)
	if err != nil {
		return err
	}
	s, err := server.New(ctx, server.Options{
		DatabaseURL:    cfg.DatabaseURL,
		Storage:        files,
		AdminUsername:  cfg.AdminUsername,
		AdminPassword:  cfg.AdminPassword,
		SecureCookies:  cfg.SecureCookies,
		MaxUploadBytes: cfg.MaxUploadMB << 20,
		Web:            webFS,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	srv := &http.Server{Addr: cfg.Addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("keine listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func openStorage(ctx context.Context, cfg config.Config) (storage.Store, error) {
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
