package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationNames lists the embedded migrations in apply order.
func migrationNames() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// AwaitSchema blocks until the newest embedded migration has been applied by
// whoever owns migrations (the API server).
//
// Only one process may run DDL: two racing Migrate calls on a cold start can
// both read schema_migrations as empty and then apply the same file, and a
// migration that is not idempotent fails the second caller. Processes that
// merely need the schema — cmd/worker — wait here instead.
func AwaitSchema(ctx context.Context, pool *pgxpool.Pool) error {
	names, err := migrationNames()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	latest := names[len(names)-1]
	for attempt := 1; ; attempt++ {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, latest).
			Scan(&exists)
		// The table itself may not exist yet — that is just an early start.
		if err == nil && exists {
			return nil
		}
		if attempt >= 60 {
			return fmt.Errorf("schema %s not applied after %d attempts (is the server running?)", latest, attempt)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Migrate applies embedded migrations in filename order. Simple, idempotent
// (tracks applied versions in schema_migrations), sufficient for M1.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name).
			Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
