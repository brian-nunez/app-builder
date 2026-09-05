package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brian-nunez/app-builder/internal/config"
	"github.com/brian-nunez/app-builder/internal/execution"
	"github.com/brian-nunez/app-builder/internal/platform"
	"github.com/brian-nunez/app-builder/internal/server"
	"github.com/brian-nunez/app-builder/internal/telemetry"
	"github.com/brian-nunez/app-builder/sdk/go/plugin"
	"github.com/brian-nunez/bhttp/pkg/brun"
	"github.com/brian-nunez/bhttp/pkg/bsuite"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	cipher, err := platform.NewCipher(cfg.String("security.encryption_key"))
	if err != nil {
		return err
	}
	service, err := bsuite.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = service.Shutdown(c)
	}()
	if cfg.Bool("telemetry.enabled") {
		shutdown, err := telemetry.Logs(ctx, cfg)
		if err != nil {
			return err
		}
		defer func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(c)
		}()
	}
	if service.DB() == nil {
		return fmt.Errorf("platform PostgreSQL database is required")
	}
	store := &platform.Store{DB: service.DB(), Cipher: cipher}
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(cfg.String("plugins.directory"), "*", "manifest.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var manifest plugin.Manifest
		if err = json.Unmarshal(raw, &manifest); err != nil {
			return err
		}
		if err = store.Register(ctx, manifest); err != nil {
			return err
		}
	}
	api, err := server.New(ctx, service, store)
	if err != nil {
		return err
	}
	worker := &execution.Worker{
		Store:    store,
		Endpoint: cfg.String("worker.endpoint"),
		Token:    cfg.String("worker.token"),
		Client: &http.Client{
			Timeout: 135 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	manager := brun.New()
	manager.Register(api, worker)
	if err = manager.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
