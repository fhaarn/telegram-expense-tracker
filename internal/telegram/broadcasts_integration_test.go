//go:build integration

package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telegram-expense-tracker/internal/httpserver"
	"telegram-expense-tracker/internal/postgres"
	"telegram-expense-tracker/migrations"
)

func TestBroadcastAPIToWorker(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("broadcast_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,display_name,status) VALUES(111,111,'Farhan','active')`); err != nil {
		t.Fatal(err)
	}
	wake := make(chan struct{}, 1)
	sender := captureSender{messages: make(chan delivered, 2)}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunBroadcasts(workerCtx, postgres.BroadcastQueue{Pool: pool}, sender, wake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() { cancel(); <-done }()
	handler := httpserver.AdminHandler("existing-key", postgres.Admin{Pool: pool}, func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	req := httptest.NewRequest("POST", "/admin/broadcasts", strings.NewReader(`{"message":"📣 Try bulk expenses!","dry_run":false}`))
	req.Header.Set("X-API-Key", "existing-key")
	req.Header.Set("Idempotency-Key", "test-announcement")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case got := <-sender.messages:
		if got.chat != 111 || got.text != "📣 Try bulk expenses!" {
			t.Fatal(got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker was not woken")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM admin_broadcast_recipients WHERE status='sent'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("delivery status not persisted")
}
