//go:build integration

package telegram

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"telegram-expense-tracker/internal/postgres"
	"telegram-expense-tracker/migrations"
	"testing"
	"time"
)

type delivered struct {
	chat int64
	text string
}
type captureSender struct{ messages chan delivered }

func (s captureSender) Send(ctx context.Context, id int64, text string) error {
	select {
	case s.messages <- delivered{id, text}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestWebhookToDatabaseToReply(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("webhook_%d", time.Now().UnixNano())
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
	wake := make(chan struct{}, 1)
	h := Webhook("secret", &postgres.Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}, func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	post := func(update, person int64, text string) {
		t.Helper()
		body := fmt.Sprintf(`{"update_id":%d,"message":{"from":{"id":%d},"chat":{"id":%d,"type":"private"},"text":%q}}`, update, person, person, text)
		req := httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("webhook status %d", w.Code)
		}
	}
	post(1, 111, "/start")
	post(2, 222, "/start")
	post(3, 111, "Farhan")
	post(4, 222, "Budi")
	post(4, 222, "Budi")
	// Start delivery only after all webhooks have committed, simulating restart recovery.
	sender := captureSender{messages: make(chan delivered, 8)}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunDelivery(workerCtx, postgres.Outbox{Pool: pool}, sender, wake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() { cancel(); <-done }()
	counts := map[int64]int{}
	for i := 0; i < 4; i++ {
		select {
		case m := <-sender.messages:
			counts[m.chat]++
			if m.chat == 111 && strings.Contains(m.text, "Budi") || m.chat == 222 && strings.Contains(m.text, "Farhan") {
				t.Fatal("cross-user reply")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reply not delivered")
		}
	}
	if counts[111] != 2 || counts[222] != 2 {
		t.Fatal("incorrect recipient counts")
	}
	var n int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE status='active'").Scan(&n); err != nil || n != 2 {
		t.Fatal("registration failed")
	}
}
