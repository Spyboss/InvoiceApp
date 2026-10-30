package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockKey serialises migration runs when more than one instance
// boots at once. Any fixed number works as long as it is never reused.
const migrationLockKey = 7_213_004_991

// Migrate applies every embedded migration that has not run yet. Migrations
// are forward only, one transaction each, and never edited after they run.
//
// This is deliberately a small runner instead of a library: there are no down
// migrations and no out-of-order runs to reconcile, and the whole contract is
// the three statements below. If either of those needs changes, replace this
// with goose rather than growing it.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return 0, fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return 0, fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, migrationLockKey)
	}()

	if _, err := conn.Exec(ctx, `
		create table if not exists schema_migrations (
			version    bigint primary key,
			name       text not null,
			applied_at timestamptz not null default now()
		)`); err != nil {
		return 0, fmt.Errorf("create migration ledger: %w", err)
	}

	applied, highest, err := appliedVersions(ctx, conn)
	if err != nil {
		return 0, err
	}

	appliedCount := 0
	for _, file := range files {
		version, name, err := parseMigrationName(file)
		if err != nil {
			return appliedCount, err
		}
		if applied[version] {
			continue
		}
		if version < highest {
			return appliedCount, fmt.Errorf("migration %s is older than applied version %d; migrations are append only", name, highest)
		}

		body, err := fs.ReadFile(migrationFiles, file)
		if err != nil {
			return appliedCount, fmt.Errorf("read migration %s: %w", name, err)
		}

		if err := applyMigration(ctx, conn, version, name, string(body)); err != nil {
			return appliedCount, err
		}
		appliedCount++
		highest = version
	}
	return appliedCount, nil
}

func appliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[int64]bool, int64, error) {
	rows, err := conn.Query(ctx, `select version from schema_migrations`)
	if err != nil {
		return nil, 0, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]bool)
	var highest int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, 0, fmt.Errorf("scan migration version: %w", err)
		}
		applied[version] = true
		if version > highest {
			highest = version
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read migration ledger: %w", err)
	}
	return applied, highest, nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, version int64, name, body string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Simple protocol because a migration file is many statements.
	if _, err := tx.Exec(ctx, body, pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (version, name) values ($1, $2)`, version, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

// parseMigrationName turns "migrations/0001_init.sql" into 1, "0001_init".
func parseMigrationName(file string) (int64, string, error) {
	base := strings.TrimSuffix(path.Base(file), ".sql")
	prefix, rest, ok := strings.Cut(base, "_")
	if !ok {
		return 0, "", fmt.Errorf("migration %q: want NNNN_name.sql", base)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version <= 0 {
		return 0, "", fmt.Errorf("migration %q: version must be a positive integer", base)
	}
	if rest == "" {
		return 0, "", fmt.Errorf("migration %q: missing name", base)
	}
	return version, base, nil
}
