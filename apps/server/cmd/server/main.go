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

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
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
	// A purpose-bound subkey, never the master secret: a leaked WOPI token
	// must not be a session, and rotating one class must not rotate the other.
	wopiSvc := wopi.NewService(queries, cfg.DeriveKey(wopi.KeyPurpose))
	// Only the pure-Go renderers run in-process. PDF and video need poppler
	// and ffmpeg, which live in cmd/worker; jobs for those kinds stay pending
	// until such a worker exists, and are never claimed-and-failed here.
	previewWorker := preview.NewWorker(queries, blobs,
		preview.ImageRenderer{}, preview.SVGRenderer{}, preview.TextRenderer{})
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

	// Sweep expired WOPI locks, dead sessions and aged security events.
	//
	// The first two are pure housekeeping — an expired lock is ignored and an
	// expired session is refused on lookup — but the third is a retention
	// policy, and a default of "forever" is not one: the table takes a row on
	// every sign-in and every failed attempt, so an unbounded log is a slow
	// leak that also makes the console useless to read.
	auditSvc := audit.New(queries)
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
				if n, err := authSvc.PurgeExpiredSessions(ctx); err != nil {
					slog.Warn("session sweep", "err", err)
				} else if n > 0 {
					slog.Info("expired sessions purged", "count", n)
				}
				cutoff := pgtype.Timestamptz{Time: time.Now().Add(-cfg.AuditRetention), Valid: true}
				if n, err := auditSvc.Purge(ctx, cutoff); err != nil {
					slog.Warn("security event sweep", "err", err)
				} else if n > 0 {
					slog.Info("aged security events purged", "count", n)
				}
			}
		}
	}()
	usersSvc := users.NewService(queries)
	api := httpapi.NewServer(httpapi.Deps{
		Auth:           authSvc,
		Spaces:         spacesSvc,
		FS:             fsSvc,
		Share:          shareSvc,
		Wopi:           wopiSvc,
		Preview:        previewWorker,
		Users:          usersSvc,
		Store:          queries,
		ExternalURL:    cfg.ExternalURL,
		OnlyOfficeURL:  cfg.OnlyOfficeURL,
		WopiServerURL:  cfg.WopiServerURL,
		MaxUploadBytes: cfg.MaxUploadBytes,
		TrustedProxies: cfg.TrustedProxies,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Router(),
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

// waitForOIDC retries discovery until the IdP is up (a cold Logto or Keycloak
// takes a while to migrate its own schema).
//
// It logs the first failure and then every tenth attempt. Silence here used to
// mean a container that is "running", never healthy, and says nothing for two
// minutes — and the cause is almost always one of two mundane things the error
// names precisely: the issuer is unreachable from inside the container, or the
// `issuer` in its discovery document does not match PP_OIDC_ISSUER (what
// PP_OIDC_ISSUER_EXTERNAL exists to resolve).
func waitForOIDC(ctx context.Context, cfg *config.Config, q *store.Queries) (*auth.Service, error) {
	const maxAttempts = 60
	for attempt := 1; ; attempt++ {
		svc, err := auth.NewService(ctx, cfg, q)
		if err == nil {
			if attempt > 1 {
				slog.Info("oidc discovery succeeded", "attempts", attempt)
			}
			return svc, nil
		} else if attempt >= maxAttempts {
			return nil, fmt.Errorf("oidc not ready after %d attempts: %w", attempt, err)
		}
		if attempt == 1 || attempt%10 == 0 {
			slog.Warn("oidc discovery failed, retrying",
				"attempt", attempt, "of", maxAttempts,
				"issuer", cfg.OIDCIssuerExt, "err", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
