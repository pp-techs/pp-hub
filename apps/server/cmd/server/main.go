package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/alert"
	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/bootstrap"
	"github.com/nhatbui/pp-hub/apps/server/internal/collab"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/gc"
	"github.com/nhatbui/pp-hub/apps/server/internal/httpapi"
	"github.com/nhatbui/pp-hub/apps/server/internal/mail"
	"github.com/nhatbui/pp-hub/apps/server/internal/notifications"
	"github.com/nhatbui/pp-hub/apps/server/internal/notify"
	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
	"github.com/nhatbui/pp-hub/apps/server/internal/orgsync"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
	"github.com/nhatbui/pp-hub/apps/server/internal/share"
	"github.com/nhatbui/pp-hub/apps/server/internal/spaces"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
	"github.com/nhatbui/pp-hub/apps/server/internal/uploads"
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
	obs.InitLogging()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The server owns migrations; cmd/worker waits for them.
	deps, closeDeps, err := bootstrap.New(ctx, bootstrap.Options{Migrate: true})
	if err != nil {
		return err
	}
	defer closeDeps()
	cfg, queries, blobs := deps.Cfg, deps.Queries, deps.Blobs
	if err := cfg.Logto.Validate(); err != nil {
		return err
	}

	// Up before OIDC discovery so a server stuck waiting for its IdP is still
	// scrapeable. A bind failure is logged, not fatal: losing metrics must
	// not stop file serving.
	metrics := obs.NewMetrics(deps.Pool, queries, cfg.BlobRoot)
	if cfg.MetricsAddr != "" {
		go func() {
			slog.InfoContext(ctx, "metrics listening", "addr", cfg.MetricsAddr)
			if err := metrics.Serve(ctx, cfg.MetricsAddr); err != nil {
				slog.ErrorContext(ctx, "metrics listener", "err", err)
			}
		}()
	}

	usersSvc := users.NewService(queries, blobs)
	authSvc, err := waitForOIDC(ctx, cfg, queries, usersSvc)
	if err != nil {
		return err
	}

	fsSvc := fs.NewService(queries, blobs)
	spacesSvc := spaces.NewService(queries, blobs)
	shareSvc := share.NewService(queries)
	// A purpose-bound subkey, never the master secret: a leaked WOPI token
	// must not be a session, and rotating one class must not rotate the other.
	wopiSvc := wopi.NewService(queries, cfg.DeriveKey(wopi.KeyPurpose))
	// Only the pure-Go renderers run in-process. PDF and video need poppler
	// and ffmpeg, which live in cmd/worker; jobs for those kinds stay pending
	// until such a worker exists, and are never claimed-and-failed here.
	previewWorker := preview.NewWorker(queries, blobs,
		preview.ImageRenderer{}, preview.SVGRenderer{}, preview.TextRenderer{}).
		WithExtractors(preview.TextExtractor{})
	// Content search (M27) indexes a node's current version: a restored
	// older version commits nothing, so it is re-extracted here instead.
	fsSvc.SetRestoreHook(func(ctx context.Context, ver store.NodeVersion) {
		if err := previewWorker.Reindex(ctx, ver); err != nil {
			slog.WarnContext(ctx, "enqueue text extraction", "err", err)
		}
	})
	// Files stored before content search existed get indexed without an
	// operator step; the scan skips everything already indexed or queued.
	go func() {
		if n, err := preview.BackfillText(ctx, queries); err != nil {
			slog.WarnContext(ctx, "text extraction backfill", "enqueued", n, "err", err)
		} else if n > 0 {
			slog.InfoContext(ctx, "text extraction backfill", "enqueued", n)
		}
	}()
	// In-app notifications (M29). One service per process: its broker holds
	// the open streams, and the mention job below delivers through it, so
	// the job runs here and never in cmd/worker.
	notificationsSvc := notifications.NewService(queries, blobs, shareSvc)
	fsSvc.SetVersionHook(func(ctx context.Context, ver store.NodeVersion, name string) {
		if err := previewWorker.Enqueue(ctx, ver, name); err != nil {
			slog.WarnContext(ctx, "enqueue preview", "err", err)
		}
		if err := notificationsSvc.EnqueueMentions(ctx, ver); err != nil {
			slog.WarnContext(ctx, "enqueue mention notifications", "err", err)
		}
	})
	go previewWorker.Run(ctx)
	go notificationsSvc.Run(ctx)

	// Reclaims disk from blobs nothing references any more (without it,
	// every permanent delete leaks its bytes forever), re-hashes stored
	// blobs to find damage, and reconciles the store with the blobs table.
	go gc.NewWorker(queries, blobs, cfg.BlobScrubBytesPerDay).Run(ctx)

	// Purges tombstones past their space's retention window. Retention
	// defaults to 0 (never), so this is inert until a space opts in.
	go gc.NewTrashWorker(fsSvc).Run(ctx)

	// Sweep expired WOPI locks, dead sessions, aged security events and
	// read notifications past notifications.ReadRetention.
	//
	// The first two are pure housekeeping — an expired lock is ignored and an
	// expired session is refused on lookup — but the third is a retention
	// policy, and a default of "forever" is not one: the table takes a row on
	// every sign-in and every failed attempt, so an unbounded log is a slow
	// leak that also makes the console useless to read. File access events
	// are the bulk of it and have their own, shorter, retention.
	syscfg := sysconfig.NewService(queries, cfg)
	if err := syscfg.Reload(ctx, "default"); err != nil {
		slog.WarnContext(ctx, "sysconfig initial load", "err", err)
	}
	fsSvc.SetVersionRetention(func() int { return syscfg.VersionRetentionCount("default") })

	// Resumable uploads stage under the blob root; the sweep drops those
	// idle past uploads.TTL, with their bytes.
	uploadsSvc, err := uploads.NewService(queries, cfg.UploadDir())
	if err != nil {
		return err
	}
	go uploadsSvc.Run(ctx, 15*time.Minute)
	mailer := mail.New(syscfg.SMTP)
	notifier := notify.New(queries, mailer, syscfg, cfg.ExternalURL)
	go notifier.Run(ctx)

	auditSvc := audit.New(queries)
	// Raises security alerts from the log: a row, a metric and admin email.
	go alert.NewWatcher(queries, auditSvc, syscfg, notifier, metrics.Alerts).Run(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := wopiSvc.Sweep(ctx); err != nil {
					slog.WarnContext(ctx, "lock sweep", "err", err)
				}
				if n, err := authSvc.PurgeExpiredSessions(ctx); err != nil {
					slog.WarnContext(ctx, "session sweep", "err", err)
				} else if n > 0 {
					slog.InfoContext(ctx, "expired sessions purged", "count", n)
				}
				if n, err := authSvc.PurgeExpiredAgentTokens(ctx); err != nil {
					slog.WarnContext(ctx, "agent token sweep", "err", err)
				} else if n > 0 {
					slog.InfoContext(ctx, "expired agent tokens purged", "count", n)
				}
				_ = syscfg.Reload(ctx, "default")
				cutoff := pgtype.Timestamptz{Time: time.Now().Add(-syscfg.AuditRetention("default")), Valid: true}
				readCutoff := pgtype.Timestamptz{Time: time.Now().Add(-syscfg.ReadAuditRetention("default")), Valid: true}
				if n, err := auditSvc.Purge(ctx, cutoff, readCutoff); err != nil {
					slog.WarnContext(ctx, "security event sweep", "err", err)
				} else if n > 0 {
					slog.InfoContext(ctx, "aged security events purged", "count", n)
				}
				if n, err := notificationsSvc.Purge(ctx, time.Now().Add(-notifications.ReadRetention)); err != nil {
					slog.WarnContext(ctx, "notification sweep", "err", err)
				} else if n > 0 {
					slog.InfoContext(ctx, "read notifications purged", "count", n)
				}
			}
		}
	}()
	// Membership is a projection of Logto organizations. The sign-in path
	// needs this before the first callback lands, and the worker drains the
	// reconcile jobs the webhook enqueues.
	orgSvc := orgsync.NewService(queries, spacesSvc, usersSvc,
		orgsync.NewClient(cfg.Logto), cfg.OIDCIssuerExt, cfg.ExternalURL)
	authSvc.SetReconciler(orgSvc)
	go orgSvc.Run(ctx)

	// The block editor's rooms (M39). Started below, once the listener is
	// bound: starting clears the locks a previous process's rooms left.
	collabHub := collab.New(fsSvc, shareSvc, queries, metrics.Collab,
		collab.Config{Origin: httpapi.OriginOf(cfg.ExternalURL)})
	api := httpapi.NewServer(httpapi.Deps{
		Auth:            authSvc,
		Spaces:          spacesSvc,
		FS:              fsSvc,
		Share:           shareSvc,
		Wopi:            wopiSvc,
		Preview:         previewWorker,
		Users:           usersSvc,
		Store:           queries,
		Pool:            deps.Pool,
		Blobs:           blobs,
		OrgSync:         orgSvc,
		Uploads:         uploadsSvc,
		Collab:          collabHub,
		LogtoWebhookKey: cfg.Logto.WebhookKey,
		ExternalURL:     cfg.ExternalURL,
		OnlyOfficeURL:   cfg.OnlyOfficeURL,
		WopiServerURL:   cfg.WopiServerURL,
		MaxUploadBytes:  cfg.MaxUploadBytes,
		TrustedProxies:  cfg.TrustedProxies,
		SysConfig:       syscfg,
		Mail:            mailer,
		Notifier:        notifier,
		Notifications:   notificationsSvc,
		Metrics:         metrics.HTTP,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The listener first, the hub after it. Starting the hub releases every
	// room lock in the database, which is only right when no other process
	// is hosting rooms — and a process still serving on this address is
	// exactly what a failed bind reveals (a second copy started by mistake,
	// or a restart racing the old one's drain). Cleared before the bind, its
	// rooms would lose their locks to a process that then exits.
	ln, err := listenThenStart(cfg.Addr, func() error { return collabHub.Start(ctx) })
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "listening", "addr", ln.Addr().String(), "external", cfg.ExternalURL)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// The rooms close once the listener has: a socket accepted while
		// they flush could open one nothing waits for. Their sockets are
		// hijacked connections, which Shutdown does not wait for, so the hub
		// closes them and writes every room back — alongside the drain of
		// ordinary requests rather than after it, so a slow download cannot
		// spend the grace period the rooms' unsaved edits need, and before
		// the database goes away with this function.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		roomsClosed := make(chan error, 1)
		srv.RegisterOnShutdown(func() { roomsClosed <- collabHub.Shutdown(shutdownCtx) })
		// Notification streams never end on their own; without this,
		// Shutdown would wait out its whole grace period for them.
		srv.RegisterOnShutdown(notificationsSvc.Shutdown)
		err := srv.Shutdown(shutdownCtx)
		return errors.Join(err, <-roomsClosed)
	}
}

// listenThenStart binds addr and only then runs start, closing the listener
// if start fails. A bind that fails runs nothing.
func listenThenStart(addr string, start func() error) (net.Listener, error) {
	if addr == "" {
		addr = ":http" // as http.Server.ListenAndServe
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := start(); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// waitForOIDC retries discovery until Logto is up (a cold one takes a while to
// migrate its own schema).
//
// It logs the first failure and then every tenth attempt. Silence here used to
// mean a container that is "running", never healthy, and says nothing for two
// minutes — and the cause is almost always one of two mundane things the error
// names precisely: the issuer is unreachable from inside the container, or the
// `issuer` in its discovery document does not match PP_OIDC_ISSUER (what
// PP_OIDC_ISSUER_EXTERNAL exists to resolve).
func waitForOIDC(ctx context.Context, cfg *config.Config, q *store.Queries, profiles *users.Service) (*auth.Service, error) {
	const maxAttempts = 60
	for attempt := 1; ; attempt++ {
		svc, err := auth.NewService(ctx, cfg, q, profiles)
		if err == nil {
			if attempt > 1 {
				slog.InfoContext(ctx, "oidc discovery succeeded", "attempts", attempt)
			}
			return svc, nil
		} else if attempt >= maxAttempts {
			return nil, fmt.Errorf("oidc not ready after %d attempts: %w", attempt, err)
		}
		if attempt == 1 || attempt%10 == 0 {
			slog.WarnContext(ctx, "oidc discovery failed, retrying",
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
