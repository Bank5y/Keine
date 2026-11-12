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
	s, err := server.New(ctx, server.Options{
		DatabaseURL:   cfg.DatabaseURL,
		Storage:       storage.NewLocal(cfg.DataDir),
		AdminUsername: cfg.AdminUsername,
		AdminPassword: cfg.AdminPassword,
		SecureCookies: cfg.SecureCookies,
		Web:           webFS,
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
