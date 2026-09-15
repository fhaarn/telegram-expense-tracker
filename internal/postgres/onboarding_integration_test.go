//go:build integration

package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"telegram-expense-tracker/internal/user"
	"telegram-expense-tracker/migrations"
	"testing"
	"time"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	admin, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("onboarding_%d", time.Now().UnixNano())
	if _, err = admin.Exec(context.Background(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	if err = migrations.Apply(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	if err = migrations.Apply(context.Background(), pool); err != nil {
		t.Fatal("repeat migration:", err)
	}
	return pool
}
func TestOnboardingIsolationAndRestart(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	send := func(id, person int64, text string) {
		t.Helper()
		if err := s.Accept(ctx, user.Message{UpdateID: id, TelegramID: person, ChatID: person, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	send(1, 101, "/start")
	send(2, 202, "/start")
	// Recreate the service to prove pending state is database-owned.
	s = &Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	send(3, 101, "  Farhan  ")
	send(4, 202, "Budi")
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE status='active'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("active users: %d %v", count, err)
	}
	for _, person := range []int64{101, 202} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM categories c JOIN users u ON u.id=c.user_id WHERE u.telegram_user_id=$1", person).Scan(&count); err != nil || count != len(user.StarterCategories) {
			t.Fatalf("categories %d %v", count, err)
		}
	}
	// Concurrent redelivery of one name update must not duplicate anything.
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Accept(ctx, user.Message{UpdateID: 3, TelegramID: 101, ChatID: 101, Text: "Farhan"})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	send(5, 101, "/start")
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notification_outbox").Scan(&count); err != nil || count != 5 {
		t.Fatalf("duplicate replies %d %v", count, err)
	}
	var name string
	if err := pool.QueryRow(ctx, "SELECT display_name FROM users WHERE telegram_user_id=101").Scan(&name); err != nil || name != "Farhan" {
		t.Fatal("name changed")
	}
	var aid, bid int64
	_ = pool.QueryRow(ctx, "SELECT id FROM users WHERE telegram_user_id=101").Scan(&aid)
	_ = pool.QueryRow(ctx, "SELECT id FROM users WHERE telegram_user_id=202").Scan(&bid)
	if _, err := pool.Exec(ctx, "UPDATE categories SET display_name='Vehicle' WHERE user_id=$1 AND normalized_name='transport'", aid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT display_name FROM categories WHERE user_id=$1 AND normalized_name='transport'", bid).Scan(&name); err != nil || name != "Transport" {
		t.Fatal("category crossed user boundary")
	}
}
func TestRegistrationRollbackAndCancel(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	if _, err := pool.Exec(ctx, "ALTER TABLE notification_outbox ADD CONSTRAINT reject_test CHECK (body='never')"); err != nil {
		t.Fatal(err)
	}
	m := user.Message{UpdateID: 10, TelegramID: 303, ChatID: 303, Text: "/start"}
	if err := s.Accept(ctx, m); err == nil {
		t.Fatal("expected failure")
	}
	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&n)
	if n != 0 {
		t.Fatal("user survived rollback")
	}
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM inbound_updates").Scan(&n)
	if n != 0 {
		t.Fatal("update survived rollback")
	}
	_, _ = pool.Exec(ctx, "ALTER TABLE notification_outbox DROP CONSTRAINT reject_test")
	if err := s.Accept(ctx, m); err != nil {
		t.Fatal(err)
	}
	m.UpdateID = 11
	m.Text = "/cancel"
	if err := s.Accept(ctx, m); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM user_interactions").Scan(&n)
	if n != 0 {
		t.Fatal("cancel did not clear interaction")
	}
	m.UpdateID = 12
	m.Text = "Unexpected name"
	if err := s.Accept(ctx, m); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE status='active'").Scan(&n)
	if n != 0 {
		t.Fatal("unsolicited name activated account")
	}
}
func TestOutboxRecovery(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	if err := s.Accept(ctx, user.Message{UpdateID: 20, TelegramID: 404, ChatID: 404, Text: "/start"}); err != nil {
		t.Fatal(err)
	}
	o := Outbox{Pool: pool}
	d, ok, err := o.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim %v", err)
	}
	if _, ok, err = o.Claim(ctx); err != nil || ok {
		t.Fatal("leased twice")
	}
	_, _ = pool.Exec(ctx, "UPDATE notification_outbox SET lease_until=now()-interval '1 second' WHERE id=$1", d.ID)
	recovered, ok, err := (Outbox{Pool: pool}).Claim(ctx)
	if err != nil || !ok || recovered.ID != d.ID {
		t.Fatal("did not recover lease")
	}
	if err = o.Finish(ctx, d, nil); err != nil {
		t.Fatal(err)
	} // Stale attempts cannot finish a newer lease.
	var status string
	_ = pool.QueryRow(ctx, "SELECT status FROM notification_outbox WHERE id=$1", d.ID).Scan(&status)
	if status != "sending" {
		t.Fatal("stale worker completed delivery")
	}
	if err = o.Finish(ctx, recovered, nil); err != nil {
		t.Fatal(err)
	}
	if _, pending, err := o.Next(ctx); err != nil || pending {
		t.Fatalf("outbox should be idle: %v", err)
	}
}

func TestOutboxBoundedRetry(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	for i, text := range []string{"/start", "Retry User"} {
		if err := s.Accept(ctx, user.Message{UpdateID: int64(30 + i), TelegramID: 505, ChatID: 505, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	o := Outbox{Pool: pool}
	for attempt := 1; attempt <= 5; attempt++ {
		d, ok, err := o.Claim(ctx)
		if err != nil || !ok || d.Attempts != attempt {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if err = o.Finish(ctx, d, fmt.Errorf("temporary failure")); err != nil {
			t.Fatal(err)
		}
		if attempt < 5 {
			delay, pending, err := o.Next(ctx)
			if err != nil || !pending || delay < time.Second {
				t.Fatal("missing retry delay")
			}
			if _, ok, err = o.Claim(ctx); err != nil || ok {
				t.Fatal("later reply overtook failed reply")
			}
			if _, err = pool.Exec(ctx, "UPDATE notification_outbox SET available_at=now()-interval '1 second' WHERE id=$1", d.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notification_outbox WHERE status='failed' AND body=''").Scan(&count); err != nil || count != 1 {
		t.Fatal("not terminal after retry limit")
	}
	if _, ok, err := o.Claim(ctx); err != nil || !ok {
		t.Fatal("next reply blocked after terminal failure")
	}
}

func TestPerUserBurstBoundAndRecovery(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	for i := int64(1); i <= 63; i++ {
		if err := s.Accept(ctx, user.Message{UpdateID: i, TelegramID: 606, ChatID: 606, Text: "/help"}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notification_outbox").Scan(&n); err != nil || n != 61 {
		t.Fatalf("bound %d %v", n, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET request_window=now()-interval '2 minutes'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Accept(ctx, user.Message{UpdateID: 64, TelegramID: 606, ChatID: 606, Text: "/help"}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT request_count FROM users").Scan(&n); err != nil || n != 1 {
		t.Fatal("window did not recover")
	}
}

func TestCancelDiscardsOnboardingInvite(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	for i, text := range []string{"/start compare_0123456789abcdef0123456789abcdef", "/cancel", "/start", "Name"} {
		if err := s.Accept(ctx, user.Message{UpdateID: int64(i + 1), TelegramID: 707, ChatID: 707, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	var hash *string
	if err := pool.QueryRow(ctx, "SELECT pending_comparison_hash FROM users").Scan(&hash); err != nil || hash != nil {
		t.Fatal("cancelled invite survived")
	}
	var body string
	if err := pool.QueryRow(ctx, "SELECT body FROM notification_outbox WHERE update_id=4").Scan(&body); err != nil || !strings.Contains(body, "Nice to meet") {
		t.Fatalf("unexpected onboarding reply %q %v", body, err)
	}
}
