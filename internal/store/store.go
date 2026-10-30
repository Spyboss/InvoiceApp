// Package store is the only place that talks SQL. Every method either runs on
// the pool (cross tenant lookups such as resolving a hostname) or inside
// InTenant, where row level security is set to one tenant.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned whenever a lookup matched no row.
var ErrNotFound = errors.New("not found")

// DBTX is satisfied by both the pool and a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store holds the connection and, when inside a transaction, the transaction.
type Store struct {
	pool *pgxpool.Pool
	db   DBTX
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, db: pool}
}

// InTx runs fn in a transaction. The Store passed to fn is bound to the
// transaction, so anything fn does commits or rolls back as one unit.
func (s *Store) InTx(ctx context.Context, fn func(*Store) error) error {
	if s.pool == nil {
		return errors.New("store: InTx on a transaction bound store")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := fn(&Store{pool: s.pool, db: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// InTenant runs fn in a transaction scoped to one tenant. The setting is
// transaction local, so it cannot leak to the next request that borrows the
// connection.
func (s *Store) InTenant(ctx context.Context, tenantID uuid.UUID, fn func(*Store) error) error {
	if tenantID == uuid.Nil {
		return errors.New("store: InTenant needs a tenant id")
	}
	return s.InTx(ctx, func(q *Store) error {
		if _, err := q.db.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
			return fmt.Errorf("set tenant: %w", err)
		}
		return fn(q)
	})
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
