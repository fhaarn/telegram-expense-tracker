//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	report "telegram-expense-tracker/internal/export"
	"telegram-expense-tracker/internal/user"
	"testing"
)

func TestExportSnapshotAndIsolation(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR"}
	send := func(n, person int64, text string) {
		t.Helper()
		if err := s.Accept(ctx, user.Message{UpdateID: n, TelegramID: person, ChatID: person, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	send(1, 100, "/start")
	send(2, 100, "Alice")
	send(3, 200, "/start")
	send(4, 200, "Bob")
	var uid, other, cat int64
	if err := p.QueryRow(ctx, `SELECT id FROM users WHERE telegram_user_id=100`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `SELECT id FROM users WHERE telegram_user_id=200`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	add := func(owner int64, date string, deleted bool) int64 {
		t.Helper()
		var draft, id int64
		if err := p.QueryRow(ctx, `SELECT id FROM categories WHERE user_id=$1 ORDER BY id LIMIT 1`, owner).Scan(&cat); err != nil {
			t.Fatal(err)
		}
		if err := p.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) VALUES($1,'Lunch','lunch',2000000,$2,$3) RETURNING id`, owner, cat, date).Scan(&draft); err != nil {
			t.Fatal(err)
		}
		if err := p.QueryRow(ctx, `INSERT INTO expenses(user_id,originating_draft_id,description,amount_minor,category_id,expense_date,deleted_at) VALUES($1,$2,'Lunch',2000000,$3,$4,CASE WHEN $5 THEN now() END) RETURNING id`, owner, draft, cat, date, deleted).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := add(uid, "2026-09-01", false)
	add(uid, "2026-09-30", false)
	add(uid, "2026-10-01", false)
	add(uid, "2026-09-10", true)
	add(other, "2026-09-15", false)
	if _, err := p.Exec(ctx, `UPDATE expenses SET amount_minor=3000000 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	send(5, 100, "/export 2026-09-01 2026-09-30")
	send(5, 100, "/export 2026-09-01 2026-09-30")
	var payload []byte
	if err := p.QueryRow(ctx, `SELECT export_payload FROM notification_outbox WHERE update_id=5`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(payload, &r); err != nil || len(r.Rows) != 2 || r.Rows[0].Amount != 30000 {
		t.Fatalf("snapshot: %+v %v", r, err)
	}
	if _, err := p.Exec(ctx, `UPDATE expenses SET amount_minor=4000000 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	send(6, 100, "/export 2026-09-01 2026-09-30")
	var n int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE user_id=$1 AND export_payload IS NOT NULL`, uid).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	// A fresh worker can claim the durable snapshot; successful delivery clears it.
	o := Outbox{Pool: p}
	found := false
	for {
		d, ok, err := o.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if len(d.ExportPayload) > 0 {
			found = true
			var snap report.Report
			if err = json.Unmarshal(d.ExportPayload, &snap); err != nil || snap.Rows[0].Amount != 30000 {
				t.Fatal("snapshot changed", err)
			}
			if _, err = report.Build(snap); err != nil {
				t.Fatal(err)
			}
		}
		if err = o.Finish(ctx, d, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal("report not delivered")
	}
	if err := p.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE export_payload IS NOT NULL`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := p.Exec(ctx, `UPDATE users SET last_export_at=NULL WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	send(7, 100, "/export 2026-09-01 2026-09-30")
	if err := (Admin{Pool: p}).SetAccess(ctx, uid, "blocked", ""); err != nil {
		t.Fatal(err)
	}
	send(8, 100, "/export")
	if err := p.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE user_id=$1 AND export_payload IS NOT NULL`, uid).Scan(&n); err != nil || n != 0 {
		t.Fatal("blocked snapshot retained", n, err)
	}
}

func TestExportRowCap(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR"}
	for i, text := range []string{"/start", "Test"} {
		if err := s.Accept(ctx, user.Message{UpdateID: int64(i + 1), TelegramID: 100, ChatID: 100, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := p.Exec(ctx, `WITH owner AS (SELECT u.id uid,c.id cid FROM users u JOIN categories c ON c.user_id=u.id WHERE u.telegram_user_id=100 ORDER BY c.id LIMIT 1), drafts AS (INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) SELECT uid,'Lunch','lunch',100,cid,'2026-09-01' FROM owner,generate_series(1,2001) RETURNING *) INSERT INTO expenses(user_id,originating_draft_id,description,amount_minor,category_id,expense_date) SELECT user_id,id,description,amount_minor,category_id,expense_date FROM drafts`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Accept(ctx, user.Message{UpdateID: 3, TelegramID: 100, ChatID: 100, Text: "/export 2026-09-01 2026-09-30"}); err != nil {
		t.Fatal(err)
	}
	var body string
	var payload []byte
	if err = p.QueryRow(ctx, `SELECT body,export_payload FROM notification_outbox WHERE update_id=3`).Scan(&body, &payload); err != nil || len(payload) > 0 || !strings.Contains(body, "smaller date range") {
		t.Fatal(body, err)
	}
}
