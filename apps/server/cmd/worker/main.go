// Command worker renders previews that the API server cannot.
//
// It is optional. The server registers only the pure-Go renderers; PDF and
// video need poppler and ffmpeg, which would otherwise have to be installed
// next to the API — both parse hostile bytes in memory-unsafe code, and the
// API process holds the master secret and the connection pool. Running them
// here instead keeps that blast radius inside a container with no listener.
//
// An install that never deploys this binary is fully supported: the jobs it
// would have claimed simply stay pending, because ClaimJobs filters on the
// kinds a process registered renderers for. Start the worker months later and
// the backlog drains on the next tick.
//
// See docs/architecture/preview-pipeline.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/bootstrap"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/obs"
	"github.com/nhatbui/pp-hub/apps/server/internal/preview"
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
	backfill := flag.Bool("backfill", false,
		"enqueue preview jobs for existing files that have none, then exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The server owns migrations; this waits for them instead. Two processes
	// racing DDL on a cold start can both apply the same file.
	deps, closeDeps, err := bootstrap.New(ctx, bootstrap.Options{Migrate: false})
	if err != nil {
		return err
	}
	defer closeDeps()

	if *backfill {
		n, err := preview.Backfill(ctx, deps.Queries)
		slog.InfoContext(ctx, "backfill finished", "enqueued", n)
		return err
	}

	// Each constructor returns nil when its binary is missing, so this process
	// advertises exactly what it can actually run. A worker image built
	// without ffmpeg claims no render:video job rather than failing it five
	// times and parking it in 'failed' forever.
	renderers := nonNil(
		preview.NewPDFRenderer(),
		preview.NewVideoRenderer(),
		preview.NewOfficeRenderer(preview.OfficeConfig{
			ConvertURL:    deps.Cfg.OnlyOfficeIntURL,
			SourceBaseURL: deps.Cfg.WopiServerURL,
			WopiKey:       officeSigningKey(deps.Cfg),
			JWTSecret:     deps.Cfg.OnlyOfficeJWTSecret,
		}),
	)
	if len(renderers) == 0 {
		return errNoRenderers
	}

	w := preview.NewWorker(deps.Queries, deps.Blobs, renderers...)
	go healthBeacon(ctx, deps.Pool, deps.Blobs)
	slog.InfoContext(ctx, "worker starting", "kinds", w.Kinds())
	w.Run(ctx)
	return nil
}

// errNoRenderers fails loudly rather than idling: a worker container with no
// usable binary is a broken image, and silently claiming nothing would look
// exactly like a healthy deployment.
var errNoRenderers = errNoRenderersType{}

type errNoRenderersType struct{}

func (errNoRenderersType) Error() string {
	return "no renderers available: this image is missing pdftoppm (poppler-utils) and ffmpeg"
}

// officeSigningKey returns the key to sign conversion source URLs with, or nil
// to disable office rendering entirely.
//
// The token the worker mints is verified by the *API server*, so the two
// processes must derive the same key. When PP_SESSION_KEY is unset each one
// generates its own at startup, and every conversion would fail with an opaque
// "invalid or expired WOPI token" after five retries. Refusing to register the
// renderer leaves those jobs pending instead — recoverable by setting the key
// and restarting, which is exactly what the operator needs to do.
func officeSigningKey(cfg *config.Config) []byte {
	if !cfg.SecretKeyShared {
		slog.Warn("office thumbnails disabled: PP_SESSION_KEY must be set to the same value as the API server",
			"effect", "render:office jobs stay pending until it is")
		return nil
	}
	return cfg.DeriveKey(wopi.KeyPurpose)
}

func nonNil(rs ...preview.Renderer) []preview.Renderer {
	out := make([]preview.Renderer, 0, len(rs))
	for _, r := range rs {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// workerHealthFile is the freshness signal the compose and image
// healthchecks read; see the HEALTHCHECK in apps/server/Dockerfile's worker
// stage and the worker service in compose.prod.yml.
const workerHealthFile = "/tmp/worker-healthy"

// healthBeacon touches workerHealthFile while Postgres and the blob store are
// both reachable and removes it when they are not; the container healthcheck
// reads its freshness. This container holds ffmpeg and poppler and exposes no
// port by design, which is why the signal is a file and not an endpoint.
//
// It proves the dependencies, not the claim loop: a wedged drain would still
// touch the file. Stated rather than hidden.
func healthBeacon(ctx context.Context, pool *pgxpool.Pool, blobs blob.Store) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if err := func() error {
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := pool.Ping(c); err != nil {
				return err
			}
			return blobs.Check(c)
		}(); err != nil {
			slog.WarnContext(ctx, "worker dependency check failed", "err", err)
			_ = os.Remove(workerHealthFile)
		} else if err := os.WriteFile(workerHealthFile, []byte("ok\n"), 0o644); err != nil {
			slog.WarnContext(ctx, "worker health file not written", "err", err)
		}
		select {
		case <-ctx.Done():
			_ = os.Remove(workerHealthFile)
			return
		case <-t.C:
		}
	}
}
