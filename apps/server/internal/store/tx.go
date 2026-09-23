package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// beginner is what both *pgxpool.Pool and pgx.Tx satisfy. On a pool Begin
// opens a transaction; on a transaction it opens a savepoint, so InTx nests.
type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// InTx runs fn against Queries bound to one transaction, committing when fn
// returns nil and rolling back otherwise.
//
// Called on Queries that are already inside a transaction it opens a
// savepoint instead. That is what lets a retry loop survive a unique
// violation inside a larger unit of work: Postgres aborts the whole
// transaction on any error, so each attempt that may collide has to run in a
// savepoint of its own.
func (q *Queries) InTx(ctx context.Context, fn func(*Queries) error) error {
	b, ok := q.db.(beginner)
	if !ok {
		return errors.New("store: connection cannot begin a transaction")
	}
	return pgx.BeginFunc(ctx, b, func(tx pgx.Tx) error {
		return fn(q.WithTx(tx))
	})
}
