// Package bootstrap builds the process-wide dependencies every entrypoint
// needs: config, a Postgres pool, sqlc queries and the blob store.
//
// It exists because cmd/server and cmd/worker are separate binaries over the
// same data. Constructing these by hand in two places is how the two drift —
// a new dependency gets wired into one main and forgotten in the other, and
// the symptom shows up as a nil pointer in the process nobody tested. Both
// entrypoints call New, so there is one wiring to keep correct.
//
// See docs/architecture/preview-pipeline.md.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/config"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Deps are the shared dependencies of any pp-hub process.
type Deps struct {
	Cfg     *config.Config
	Pool    *pgxpool.Pool
	Queries *store.Queries
	Blobs   blob.Store
}

// Options controls how New brings the process up.
type Options struct {
	// Migrate makes this process the one that applies schema migrations.
	// Exactly one entrypoint may set it — cmd/server. Everything else waits
	// for the schema via store.AwaitSchema; see the note there on why two
	// migrators race.
	Migrate bool
}

// New connects to Postgres, readies the schema and opens the blob store.
// The returned func closes the pool and must be called by the caller.
func New(ctx context.Context, opt Options) (*Deps, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres: %w", err)
	}
	closeFn := pool.Close

	if err := waitForPostgres(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	if opt.Migrate {
		if err := store.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("migrate: %w", err)
		}
	} else if err := store.AwaitSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("await schema: %w", err)
	}

	queries := store.New(pool)
	blobs, err := openBlobStore(ctx, cfg, queries.BlobClaims())
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("blob store: %w", err)
	}

	return &Deps{Cfg: cfg, Pool: pool, Queries: queries, Blobs: blobs}, closeFn, nil
}

// openBlobStore picks the backend named by PP_BLOB_BACKEND. Config validated
// the combination already, so an unknown value here is a programming error.
// Both backends record every write through claims before installing bytes.
func openBlobStore(ctx context.Context, cfg *config.Config, claims blob.Claims) (blob.Store, error) {
	switch cfg.BlobBackend {
	case "s3":
		slog.InfoContext(ctx, "blob store: s3", "bucket", cfg.S3.Bucket, "endpoint", cfg.S3.Endpoint)
		return blob.NewS3Store(ctx, blob.S3Config{
			Bucket:    cfg.S3.Bucket,
			Region:    cfg.S3.Region,
			Endpoint:  cfg.S3.Endpoint,
			AccessKey: cfg.S3.AccessKey,
			SecretKey: cfg.S3.SecretKey,
			PathStyle: cfg.S3.PathStyle,
		}, claims)
	default:
		return blob.NewLocalStore(cfg.BlobRoot, claims)
	}
}

// waitForPostgres retries until Postgres accepts pings (compose startup order).
func waitForPostgres(ctx context.Context, pool *pgxpool.Pool) error {
	for attempt := 1; ; attempt++ {
		err := pool.Ping(ctx)
		if err == nil {
			return nil
		}
		if attempt >= 30 {
			return fmt.Errorf("postgres not ready after %d attempts: %w", attempt, err)
		}
		if attempt == 1 {
			slog.InfoContext(ctx, "waiting for postgres")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
