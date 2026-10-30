// Package dbtest holds the one helper the integration test binaries share.
// Both packages truncate the same database, so without this they would wipe
// each other's rows mid test.
package dbtest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// lockKey is any constant, as long as every test binary uses the same one.
const lockKey int64 = 7213004992

// Lock waits for the other test binaries to finish and returns the release
// function. It holds a session level advisory lock on a connection of its own,
// so the lock survives the pool churn inside the tests.
func Lock(ctx context.Context, dsn string) (func(), error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect for test lock: %w", err)
	}
	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, lockKey); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("test lock: %w", err)
	}

	return func() {
		_, _ = conn.Exec(context.Background(), `select pg_advisory_unlock($1)`, lockKey)
		_ = conn.Close(context.Background())
	}, nil
}
