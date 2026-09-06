package main

import (
	"context"
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
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		// Telemetry may have redirected slog to the collector by now. A fatal
		// startup error has to reach the container log, where an operator reading
		// "docker compose logs" will actually see it.
		slog.Error("application stopped", "error", err)
		fmt.Fprintf(os.Stderr, "\napplication stopped: %v\n", err)
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
	// One loader for the installed packages: it parses each manifest, validates
	// its identity, and re-hashes the package so a handler edited without a
	// rebuild is refused here rather than discovered mid-run.
	packages, err := plugin.Load(cfg.String("plugins.directory"))
	if err != nil {
		return err
	}
	installed := map[string]bool{}
	for _, item := range packages {
		if err = store.Register(ctx, item.Manifest); err != nil {
			return err
		}
		installed[item.Manifest.Key()] = true
	}
	catalog, err := store.Catalog(ctx)
	if err != nil {
		return err
	}
	for key, manifest := range catalog {
		if !installed[key] {
			if err = store.Uninstall(ctx, manifest.Name, manifest.Version); err != nil {
				return err
			}
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
	// The platform and the worker load plugins from separate filesystems. Confirm
	// they resolved the same artifacts, and take the concurrency limit from the
	// side that owns it, before accepting any work.
	report, err := worker.Catalog(ctx)
	if err != nil {
		return fmt.Errorf("plugin worker is unreachable: %w", err)
	}
	if err = execution.Agree(packages, report); err != nil {
		return err
	}
	worker.Capacity = report.Capacity
	worker.Start()
	slog.Info("plugin catalog verified", "plugins", len(packages), "worker.capacity", report.Capacity)
	manager := brun.New()
	manager.Register(api, worker)
	if err = manager.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
