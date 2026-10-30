// Package db owns the postgres connection pools and migrations.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open returns the pool every request uses. Each connection switches to the
// app_rw role, which owns nothing and is not a superuser, so row level
// security applies even if a query forgets its tenant predicate.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return open(ctx, dsn, func(conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `set role app_rw`); err != nil {
			return fmt.Errorf("switch to app_rw: %w", err)
		}
		return nil
	})
}

// OpenAdmin returns a pool with the credentials from the dsn untouched. It is
// for migrations only and is closed before the server starts serving.
func OpenAdmin(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return open(ctx, dsn, nil)
}

func open(ctx context.Context, dsn string, afterConnect func(*pgx.Conn) error) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	if afterConnect != nil {
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			return afterConnect(conn)
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
