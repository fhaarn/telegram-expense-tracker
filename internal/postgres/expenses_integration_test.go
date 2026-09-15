//go:build integration

package postgres

import (
	"context"
	"fmt"
	"strings"
	"telegram-expense-tracker/internal/user"
	"testing"
	"time"
)

func TestExpensesLearningRevisionsAndIsolation(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := &Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR"}
	seq := int64(0)
	send := func(who int64, text, callback string) user.Reply {
		t.Helper()
		seq++
		if err := s.Accept(ctx, user.Message{UpdateID: seq, TelegramID: who, ChatID: who, Text: text, Callback: callback}); err != nil {
			t.Fatal(err)
		}
		var r user.Reply
		if err := p.QueryRow(ctx, "SELECT body FROM notification_outbox WHERE update_id=$1", seq).Scan(&r.Text); err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, u := range []int64{101, 202} {
		send(u, "/start", "")
		send(u, "Person", "")
	}
	var uid, other int64
	p.QueryRow(ctx, "SELECT id FROM users WHERE telegram_user_id=101").Scan(&uid)
	p.QueryRow(ctx, "SELECT id FROM users WHERE telegram_user_id=202").Scan(&other)
	latest := func() expenseDraft {
		t.Helper()
		tx, e := p.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		var id int64
		if e = tx.QueryRow(ctx, "SELECT max(id) FROM expense_drafts WHERE user_id=$1", uid).Scan(&id); e != nil {
			t.Fatal(e)
		}
		d, e := loadDraft(ctx, tx, uid, id)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	cb := func(action string, d expenseDraft, arg string) user.Reply {
		return send(101, "", eb("", action, d, arg).Data)
	}
	send(101, "bensin 100k", "")
	d := latest()
	if d.Category != 0 {
		t.Fatal("unexpected initial category")
	}
	var transport int64
	p.QueryRow(ctx, "SELECT id FROM categories WHERE user_id=$1 AND normalized_name='transport'", uid).Scan(&transport)
	if r := send(202, "", eb("", "cat", d, fmt.Sprint(transport)).Data); !strings.Contains(r.Text, "unavailable") {
		t.Fatal("owner isolation", r)
	}
	cb("cat", d, fmt.Sprint(transport))
	d = latest()
	cb("save", d, "")
	cb("save", d, "")
	var count int
	p.QueryRow(ctx, "SELECT count(*) FROM expenses WHERE user_id=$1", uid).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate save", count)
	}
	send(101, " BENSIN 25k", "")
	d = latest()
	if d.Category != transport || d.Learn {
		t.Fatal("memory not loaded", d)
	}
	cb("save", d, "")
	send(101, "gym 150k", "")
	d = latest()
	cb("field", d, "category")
	send(101, "Fitness", "")
	d = latest()
	fitness := d.Category
	cb("save", d, "")
	send(101, "gym 200k", "")
	d = latest()
	if d.Category != fitness {
		t.Fatal("custom memory")
	}
	cb("cancel", d, "")
	if r := send(101, "/month", ""); !strings.Contains(r.Text, "Rp275,000") || !strings.Contains(r.Text, "Fitness") {
		t.Fatal("report", r)
	}
	if r := send(202, "/today", ""); !strings.Contains(r.Text, "Rp0") {
		t.Fatal("report isolation", r)
	}
	// Persistent interaction resumes across service recreation; duplicate category normalizes.
	send(101, "training 10k", "")
	d = latest()
	cb("field", d, "category")
	s = &Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR"}
	send(101, "  FITNESS  ", "")
	d = latest()
	if d.Category != fitness {
		t.Fatal("duplicate category")
	}
	cb("cancel", d, "")
	// Revision amount is invisible until saved, then soft deletion removes it.
	var eid, version int64
	p.QueryRow(ctx, "SELECT id,version FROM expenses WHERE user_id=$1 ORDER BY id LIMIT 1", uid).Scan(&eid, &version)
	cb("revise", expenseDraft{ID: eid, Version: version}, "")
	d = latest()
	cb("field", d, "amount")
	send(101, "80k", "")
	d = latest()
	cb("save", d, "")
	cb("remove", expenseDraft{ID: eid, Version: version}, "")
	p.QueryRow(ctx, "SELECT version FROM expenses WHERE id=$1", eid).Scan(&version)
	cb("remove", expenseDraft{ID: eid, Version: version}, "")
	if r := send(101, "/today", ""); !strings.Contains(r.Text, "Rp175,000") {
		t.Fatal("edited deleted report", r)
	}
	// Stale mapping cannot overwrite a newer confirmed category.
	send(101, "new 1k", "")
	first := latest()
	cb("cat", first, fmt.Sprint(transport))
	first = latest()
	send(101, "new 2k", "")
	second := latest()
	cb("cat", second, fmt.Sprint(fitness))
	second = latest()
	cb("save", second, "")
	if r := cb("save", first, ""); !strings.Contains(r.Text, "remembered category changed") {
		t.Fatal("stale mapping", r)
	}
	// Expired drafts and cancelled category entry do not save anything.
	send(101, "expired 1k", "")
	d = latest()
	p.Exec(ctx, "UPDATE expense_drafts SET expires_at=now()-interval '1 second' WHERE id=$1", d.ID)
	if r := cb("save", d, ""); !strings.Contains(r.Text, "expired") {
		t.Fatal("expiry")
	}
	send(101, "cancelcat 1k", "")
	d = latest()
	cb("field", d, "category")
	send(101, "/cancel", "")
	var interactions int
	p.QueryRow(ctx, "SELECT count(*) FROM user_interactions WHERE user_id=$1", uid).Scan(&interactions)
	if interactions != 0 {
		t.Fatal("interaction cancellation")
	}
	_ = other
}
func TestExpenseDateReportsAndRollback(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	var uid, cat int64
	if err := p.QueryRow(ctx, "INSERT INTO users(telegram_user_id,telegram_chat_id,status,display_name) VALUES(999,999,'active','Test') RETURNING id").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	p.QueryRow(ctx, "INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,'Food','food') RETURNING id", uid).Scan(&cat)
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := HandleExpense(ctx, tx, uid, "Test", "Asia/Jakarta", user.Message{Text: "Tahu Telor 20k"})
	if err != nil {
		t.Fatal(err)
	}
	_ = r
	var id int64
	tx.QueryRow(ctx, "SELECT id FROM expense_drafts WHERE user_id=$1", uid).Scan(&id)
	d, err := loadDraft(ctx, tx, uid, id)
	if err != nil {
		t.Fatal(err)
	}
	d.Category = cat
	d.Date = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	if err = persistDraft(ctx, tx, uid, &d); err != nil {
		t.Fatal(err)
	}
	if _, err = saveDraft(ctx, tx, uid, d, "2026-09-01"); err != nil {
		t.Fatal(err)
	}
	if r, err = expenseReport(ctx, tx, uid, "month", "2026-09-01", 0); err != nil || !strings.Contains(r.Text, "Rp0") {
		t.Fatal("month boundary", r, err)
	}
	if r, err = expenseReport(ctx, tx, uid, "today", "2026-08-31", 0); err != nil || !strings.Contains(r.Text, "Rp20,000") {
		t.Fatal("date boundary", r, err)
	}
	tx.Rollback(ctx)
	var count int
	p.QueryRow(ctx, "SELECT count(*) FROM expense_drafts").Scan(&count)
	if count != 0 {
		t.Fatal("rollback")
	}
}

func TestExpenseScopesAndStaleInteractions(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var uid, a, b int64
	if err = tx.QueryRow(ctx, "INSERT INTO users(telegram_user_id,telegram_chat_id,status,display_name) VALUES(901,901,'active','Test') RETURNING id").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Transport", "Food"} {
		var id int64
		if err = tx.QueryRow(ctx, "INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,$2,$2) RETURNING id", uid, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			a = id
		} else {
			b = id
		}
	}
	send := func(text, callback string) user.Reply {
		t.Helper()
		r, e := HandleExpense(ctx, tx, uid, "Test", "Asia/Jakarta", user.Message{Text: text, Callback: callback})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	latest := func() expenseDraft {
		t.Helper()
		var id int64
		if e := tx.QueryRow(ctx, "SELECT max(id) FROM expense_drafts WHERE user_id=$1", uid).Scan(&id); e != nil {
			t.Fatal(e)
		}
		d, e := loadDraft(ctx, tx, uid, id)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	cb := func(action string, d expenseDraft, arg string) user.Reply {
		return send("", eb("", action, d, arg).Data)
	}
	send("bensin 1k", "")
	d := latest()
	cb("cat", d, fmt.Sprint(a))
	d = latest()
	cb("save", d, "")
	send("bensin 2k", "")
	d = latest()
	cb("cat", d, fmt.Sprint(b))
	d = latest()
	cb("scope", d, "once")
	d = latest()
	cb("save", d, "")
	mapped, _, err := mapping(ctx, tx, uid, "bensin")
	if err != nil || mapped != a {
		t.Fatal("one-off changed memory", mapped, err)
	}
	send("bensin 3k", "")
	older := latest()
	send("bensin 4k", "")
	d = latest()
	cb("cat", d, fmt.Sprint(b))
	d = latest()
	cb("scope", d, "remember")
	d = latest()
	cb("save", d, "")
	mapped, _, err = mapping(ctx, tx, uid, "bensin")
	if err != nil || mapped != b {
		t.Fatal("remember not applied", mapped, err)
	}
	fresh, err := loadDraft(ctx, tx, uid, older.ID)
	if err != nil || fresh.Category != a {
		t.Fatal("pending category changed", fresh, err)
	}
	cb("save", older, "")
	mapped, _, err = mapping(ctx, tx, uid, "bensin")
	if err != nil || mapped != b {
		t.Fatal("automatic save rewrote mapping", err)
	}
	// Editing description resets both the prefill and staged learning.
	send("unlearned 1k", "")
	d = latest()
	cb("cat", d, fmt.Sprint(a))
	d = latest()
	cb("field", d, "description")
	send("bensin", "")
	d = latest()
	if d.Key != "bensin" || d.Category != b || d.Learn {
		t.Fatal("description lookup", d)
	}
	cb("save", d, "")
	mapped, _, err = mapping(ctx, tx, uid, "unlearned")
	if err != nil || mapped != 0 {
		t.Fatal("old description learned")
	}
	// Choosing an existing category after opening text entry clears that interaction.
	send("pending category 1k", "")
	d = latest()
	prompt := cb("field", d, "category")
	if !strings.Contains(prompt.Text, "pending category") || !strings.Contains(prompt.Text, fmt.Sprintf("#%d", d.ID)) {
		t.Fatal("prompt missing draft", prompt)
	}
	cb("cat", d, fmt.Sprint(a))
	send("next expense 2k", "")
	d = latest()
	if d.Description != "next expense" {
		t.Fatal("stale category interaction consumed expense", d)
	}
	cb("field", d, "amount")
	if _, err = tx.Exec(ctx, "UPDATE expense_drafts SET expires_at=now()-interval '1 second' WHERE id=$1", d.ID); err != nil {
		t.Fatal(err)
	}
	send("3k", "")
	d = latest()
	if d.Status != "expired" {
		t.Fatal("text expiry not persisted", d.Status)
	}
	// Two revisions preserve the newer saved expense and reject stale save.
	var eid, ver int64
	if err = tx.QueryRow(ctx, "SELECT id,version FROM expenses WHERE user_id=$1 ORDER BY id LIMIT 1", uid).Scan(&eid, &ver); err != nil {
		t.Fatal(err)
	}
	cb("revise", expenseDraft{ID: eid, Version: ver}, "")
	first := latest()
	cb("revise", expenseDraft{ID: eid, Version: ver}, "")
	second := latest()
	cb("field", second, "amount")
	send("9k", "")
	second = latest()
	cb("save", second, "")
	if r := cb("save", first, ""); !strings.Contains(r.Text, "changed or was deleted") {
		t.Fatal("stale revision saved", r)
	}
	var amount int64
	if err = tx.QueryRow(ctx, "SELECT amount_minor FROM expenses WHERE id=$1", eid).Scan(&amount); err != nil || amount != 900000 {
		t.Fatal("stale revision overwritten", amount, err)
	}
}

func TestExpenseReportUnicodePagination(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var uid int64
	if err = tx.QueryRow(ctx, "INSERT INTO users(telegram_user_id,telegram_chat_id,status,display_name) VALUES(902,902,'active','Test') RETURNING id").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		var cat, draft int64
		name := strings.Repeat("😀", 39) + fmt.Sprint(i)
		desc := strings.Repeat("😀", 199) + fmt.Sprint(i)
		if err = tx.QueryRow(ctx, "INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,$2,$2) RETURNING id", uid, name).Scan(&cat); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRow(ctx, "INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) VALUES($1,$2,$2,100000,$3,'2026-09-14') RETURNING id", uid, desc, cat).Scan(&draft); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO expenses(user_id,originating_draft_id,description,amount_minor,category_id,expense_date) VALUES($1,$2,$3,100000,$4,'2026-09-14')", uid, draft, desc, cat); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"today", "month", "recent"} {
		for page := 0; page < 2; page++ {
			r, e := expenseReport(ctx, tx, uid, kind, "2026-09-14", page)
			if e != nil {
				t.Fatal(e)
			}
			units := 0
			for _, runeValue := range r.Text {
				units++
				if runeValue > 0xffff {
					units++
				}
			}
			if units > 4096 {
				t.Fatalf("%s page %d has %d UTF16 units", kind, page, units)
			}
			if page == 0 {
				found := false
				for _, row := range r.Buttons {
					for _, b := range row {
						if strings.Contains(b.Text, "Next") {
							found = true
						}
					}
				}
				if !found {
					t.Fatal("missing next page", kind)
				}
			}
		}
	}
}
