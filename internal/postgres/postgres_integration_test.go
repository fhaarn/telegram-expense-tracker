//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOpenAndQuery(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var value int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&value); err != nil || value != 1 {
		t.Fatalf("database roundtrip failed: %v", err)
	}
	if pool.Config().MaxConns != 4 {
		t.Fatal("unexpected pool limit")
	}
}
