package db

import (
	"context"
	"os"
	"testing"
	"time"
)

// The connection test only runs where TEST_DATABASE_URL is set, which CI and a
// developer machine do. Everything else stays hermetic.
func TestOpen(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
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
	if _, err := Open(context.Background(), "not a url"); err == nil {
		t.Fatal("Open: want error, got nil")
	}
}
