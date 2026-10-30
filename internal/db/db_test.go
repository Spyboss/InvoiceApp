package db

import (
	"context"
	"os"
	"testing"
	"time"
)

// The connection test only runs where TEST_DATABASE_URL is set, which CI and a
// developer machine do. Everything else stays hermetic.
func TestOpenAdmin(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := OpenAdmin(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenAdmin: %v", err)
	}
	defer pool.Close()

	var n int
	if err := pool.QueryRow(ctx, "select 1").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("query = %d, want 1", n)
	}
}

func TestOpenRejectsBadDSN(t *testing.T) {
	if _, err := OpenAdmin(context.Background(), "not a url"); err == nil {
		t.Fatal("OpenAdmin: want error, got nil")
	}
}

func TestMigrationNames(t *testing.T) {
	version, name, err := parseMigrationName("migrations/0007_add_vehicle_stock.sql")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if version != 7 || name != "0007_add_vehicle_stock" {
		t.Fatalf("got %d %q", version, name)
	}

	for _, bad := range []string{"migrations/init.sql", "migrations/zero_init.sql", "migrations/0002_.sql"} {
		if _, _, err := parseMigrationName(bad); err == nil {
			t.Errorf("parseMigrationName(%q): want error", bad)
		}
	}
}
