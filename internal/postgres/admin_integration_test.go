//go:build integration

package postgres

import (
	"context"
	"fmt"
	"sync"
	"telegram-expense-tracker/internal/user"
	"telegram-expense-tracker/migrations"
	"testing"
)

func TestAdminAccessLifecycle(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR", BotUsername: "test_bot"}
	a := Admin{Pool: p}
	send := func(update, person int64, text, cb string) {
		t.Helper()
		if err := s.Accept(ctx, user.Message{UpdateID: update, TelegramID: person, ChatID: person, Text: text, Callback: cb}); err != nil {
			t.Fatal(err)
		}
	}
	send(1, 101, "/start", "")
	send(2, 101, "Alice", "")
	send(3, 202, "/start", "")
	send(4, 202, "Bob", "")
	users, err := a.List(ctx, 0, 10, "all")
	if err != nil || len(users) != 2 {
		t.Fatalf("%v %v", users, err)
	}
	uid, partner := users[0].ID, users[1].ID
	var pair int64
	if err = p.QueryRow(ctx, `INSERT INTO comparison_pairs DEFAULT VALUES RETURNING id`).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO active_pair_members(user_id,pair_id,slot) VALUES($1,$3,1),($2,$3,2)`, uid, partner, pair); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO comparison_invites(creator_id,token_hash) VALUES($1,'testhash')`, uid); err != nil {
		t.Fatal(err)
	}
	if err = a.SetAccess(ctx, uid, "blocked", "test"); err != nil {
		t.Fatal(err)
	}
	if err = a.SetAccess(ctx, uid, "blocked", "repeat"); err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"/start", "/today", "/recent", "/compare", "secret 20k"} {
		send(int64(10+i), 101, text, "")
	}
	send(20, 101, "", "profile:smoker:yes")
	var n int
	for _, q := range []string{`SELECT count(*) FROM expense_drafts WHERE user_id=$1`, `SELECT count(*) FROM active_pair_members WHERE user_id=$1`, `SELECT count(*) FROM comparison_invites WHERE creator_id=$1 AND state='pending'`, `SELECT count(*) FROM notification_outbox WHERE user_id=$1 AND status IN ('pending','sending')`} {
		if err = p.QueryRow(ctx, q, uid).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", q, n, err)
		}
	}
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_access_events WHERE user_id=$1`, uid).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	found, err := a.List(ctx, 0, 10, "blocked")
	if err != nil || len(found) != 1 || found[0].ID != uid {
		t.Fatal(found, err)
	}
	found, err = a.List(ctx, uid, 10, "all")
	if err != nil || len(found) != 1 || found[0].ID != partner {
		t.Fatal(found, err)
	}
	if err = a.SetAccess(ctx, uid, "allowed", ""); err != nil {
		t.Fatal(err)
	}
	send(21, 101, "hello 20k", "")
	if err = p.QueryRow(ctx, `SELECT count(*) FROM expense_drafts WHERE user_id=$1`, uid).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// Concurrent requests share the intake lock and leave no deliverable blocked replies.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs <- a.SetAccess(ctx, uid, "blocked", "") }()
	go func() {
		defer wg.Done()
		errs <- s.Accept(ctx, user.Message{UpdateID: 30, TelegramID: 101, ChatID: 101, Text: "/help"})
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = p.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE user_id=$1 AND status IN ('pending','sending')`, uid).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	for {
		d, ok, err := (Outbox{Pool: p}).Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if d.ChatID == 101 {
			t.Fatal("blocked delivery")
		}
		if err = (Outbox{Pool: p}).Finish(ctx, d, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBlacklistRacesWithSaveAndInvite(t *testing.T) {
	for _, flow := range []string{"save", "invite"} {
		t.Run(flow, func(t *testing.T) {
			p := testPool(t)
			ctx := context.Background()
			s := Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR", BotUsername: "test_bot"}
			a := Admin{Pool: p}
			send := func(n, person int64, text, cb string) {
				t.Helper()
				if err := s.Accept(ctx, user.Message{UpdateID: n, TelegramID: person, ChatID: person, Text: text, Callback: cb}); err != nil {
					t.Fatal(err)
				}
			}
			send(1, 100, "/start", "")
			send(2, 100, "Alice", "")
			send(3, 200, "/start", "")
			send(4, 200, "Bob", "")
			var uid, other int64
			p.QueryRow(ctx, `SELECT id FROM users WHERE telegram_user_id=100`).Scan(&uid)
			p.QueryRow(ctx, `SELECT id FROM users WHERE telegram_user_id=200`).Scan(&other)
			var cb string
			person := int64(100)
			if flow == "save" {
				send(5, 100, "food 20k", "")
				var draft, version, cat int64
				if err := p.QueryRow(ctx, `SELECT d.id,d.version,c.id FROM expense_drafts d JOIN categories c ON c.user_id=d.user_id AND c.normalized_name='food & drinks' WHERE d.user_id=$1`, uid).Scan(&draft, &version, &cat); err != nil {
					t.Fatal(err)
				}
				send(6, 100, "", fmt.Sprintf("e:cat:%d:%d:%d", draft, version, cat))
				if err := p.QueryRow(ctx, `SELECT version FROM expense_drafts WHERE id=$1`, draft).Scan(&version); err != nil {
					t.Fatal(err)
				}
				cb = fmt.Sprintf("e:save:%d:%d:", draft, version)
			} else {
				send(5, 100, "/compare", "")
				var hash string
				var invite int64
				if err := p.QueryRow(ctx, `SELECT id,token_hash FROM comparison_invites WHERE creator_id=$1`, uid).Scan(&invite, &hash); err != nil {
					t.Fatal(err)
				}
				if _, err := p.Exec(ctx, `UPDATE users SET pending_comparison_hash=$2 WHERE id=$1`, other, hash); err != nil {
					t.Fatal(err)
				}
				cb = fmt.Sprintf("c:accept:%d", invite)
				person = 200
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			wg.Add(2)
			go func() { defer wg.Done(); errs <- a.SetAccess(ctx, uid, "blocked", "") }()
			go func() {
				defer wg.Done()
				errs <- s.Accept(ctx, user.Message{UpdateID: 7, TelegramID: person, ChatID: person, Callback: cb})
			}()
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var before, after int
			if err := p.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE user_id=$1`, uid).Scan(&before); err != nil {
				t.Fatal(err)
			}
			send(8, person, "", cb)
			if err := p.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE user_id=$1`, uid).Scan(&after); err != nil || before != after {
				t.Fatal("post-block save", err)
			}
			var pairs int
			if err := p.QueryRow(ctx, `SELECT count(*) FROM active_pair_members`).Scan(&pairs); err != nil || pairs != 0 {
				t.Fatal("pair survived block", err)
			}
			if err := a.SetAccess(ctx, uid, "allowed", ""); err != nil {
				t.Fatal(err)
			}
			if err := p.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE user_id=$1`, uid).Scan(&after); err != nil || before != after {
				t.Fatal("history changed", err)
			}
		})
	}
}

func TestAdminMigrationBackfill(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	// Recreate the pre-007 shape inside this test's isolated schema.
	if _, err := p.Exec(ctx, `DROP TABLE admin_access_events; ALTER TABLE users DROP COLUMN access_status, DROP COLUMN access_updated_at; DELETE FROM schema_migrations WHERE name='007_admin_access.sql'; INSERT INTO users(telegram_user_id,telegram_chat_id) VALUES(100,100)`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := p.QueryRow(ctx, `SELECT access_status FROM users WHERE telegram_user_id=100`).Scan(&status); err != nil || status != "allowed" {
		t.Fatal(status, err)
	}
	if err := migrations.Apply(ctx, p); err != nil {
		t.Fatal("migration not repeatable", err)
	}
}
