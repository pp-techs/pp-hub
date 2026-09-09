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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/httpapi"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
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

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := waitForPostgres(ctx, pool); err != nil {
		return err
	}
	if err := store.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	queries := store.New(pool)
	authSvc, err := waitForOIDC(ctx, cfg, queries)
	if err != nil {
		return err
	}

	blobs, err := blob.NewLocalStore(cfg.BlobRoot)
	if err != nil {
		return fmt.Errorf("blob store: %w", err)
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
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewServer(authSvc, spacesSvc, fsSvc, shareSvc, wopiSvc, previewWorker, queries, cfg.ExternalURL, cfg.OnlyOfficeURL, cfg.WopiServerURL).Router(),
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

// waitForPostgres retries until Postgres accepts pings (compose startup order).
func waitForPostgres(ctx context.Context, pool *pgxpool.Pool) error {
	for attempt := 1; ; attempt++ {
		if err := pool.Ping(ctx); err == nil {
			return nil
		} else if attempt >= 30 {
			return fmt.Errorf("postgres not ready after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
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
