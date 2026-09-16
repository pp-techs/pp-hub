package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/bootstrap"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/gc"
	"github.com/nhatbui/pp-hub/apps/server/internal/httpapi"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/users"
	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The server owns migrations; cmd/worker waits for them.
	deps, closeDeps, err := bootstrap.New(ctx, bootstrap.Options{Migrate: true})
	if err != nil {
		return err
	}
	defer closeDeps()
	cfg, queries, blobs := deps.Cfg, deps.Queries, deps.Blobs

	authSvc, err := waitForOIDC(ctx, cfg, queries)
	if err != nil {
		return err
	}

	fsSvc := fs.NewService(queries, blobs)
	spacesSvc := spaces.NewService(queries)
	shareSvc := share.NewService(queries)
	wopiSvc := wopi.NewService(queries, cfg.SessionKey)
	previewWorker := preview.NewWorker(queries, blobs)
	fsSvc.SetVersionHook(func(ctx context.Context, ver store.NodeVersion, name string) {
		if err := previewWorker.Enqueue(ctx, ver, name); err != nil {
			slog.Warn("enqueue preview", "err", err)
		}
	})
	go previewWorker.Run(ctx)

	// Reclaims disk from blobs nothing references any more. Without it, every
	// permanent delete leaks its bytes forever.
	go gc.NewWorker(queries, blobs).Run(ctx)

	// Purges tombstones past their space's retention window. Retention
	// defaults to 0 (never), so this is inert until a space opts in.
	go gc.NewTrashWorker(fsSvc).Run(ctx)

	// Sweep expired WOPI locks periodically.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := wopiSvc.Sweep(ctx); err != nil {
					slog.Warn("lock sweep", "err", err)
				}
			}
		}
	}()
	usersSvc := users.NewService(queries)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewServer(authSvc, spacesSvc, fsSvc, shareSvc, wopiSvc, previewWorker, usersSvc, queries, cfg.ExternalURL, cfg.OnlyOfficeURL, cfg.WopiServerURL).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "external", cfg.ExternalURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// waitForOIDC retries discovery until the IdP is up (Keycloak import takes a while).
func waitForOIDC(ctx context.Context, cfg *config.Config, q *store.Queries) (*auth.Service, error) {
	for attempt := 1; ; attempt++ {
		svc, err := auth.NewService(ctx, cfg, q)
		if err == nil {
			return svc, nil
		} else if attempt >= 60 {
			return nil, fmt.Errorf("oidc not ready after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
