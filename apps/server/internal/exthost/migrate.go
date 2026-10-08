package exthost

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
)

// migrate applies each extension's schema. The extension's schema is created
// first (its id is validated, so interpolating it is safe), and each file runs
// once, in a transaction whose search_path is ext_<id>, public — so a
// migration writes `CREATE TABLE cards` and may reference `nodes(id)`.
func (r *Registry) migrate(ctx context.Context) error {
	for _, e := range r.exts {
		if e.m.Migrations == nil {
			continue
		}
		schema := e.m.Schema()
		if _, err := r.cfg.Pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
			return fmt.Errorf("exthost: schema %s: %w", schema, err)
		}
		names, err := fs.Glob(e.m.Migrations, "*.sql")
		if err != nil {
			return err
		}
		sort.Strings(names)
		for _, name := range names {
			var done bool
			if err := r.cfg.Pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM ext_migrations WHERE ext_id = $1 AND version = $2)`,
				e.m.ID, name).Scan(&done); err != nil {
				return err
			}
			if done {
				continue
			}
			body, err := fs.ReadFile(e.m.Migrations, name)
			if err != nil {
				return err
			}
			tx, err := r.cfg.Pool.Begin(ctx)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "SET LOCAL search_path = "+schema+", public"); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("exthost: apply %s/%s: %w", e.m.ID, name, err)
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO ext_migrations (ext_id, version) VALUES ($1, $2)`, e.m.ID, name); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
