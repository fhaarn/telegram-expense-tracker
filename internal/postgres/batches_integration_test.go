//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"telegram-expense-tracker/internal/user"
)

type batchHarness struct {
	t   *testing.T
	p   *pgxpool.Pool
	s   *Onboarding
	seq int64
	uid int64
}

func newBatchHarness(t *testing.T) *batchHarness {
	p := testPool(t)
	h := &batchHarness{t: t, p: p, s: &Onboarding{Pool: p, Timezone: "Asia/Jakarta", Currency: "IDR"}}
	h.send(101, "/start", "")
	h.send(101, "Farhan", "")
	if err := p.QueryRow(context.Background(), `SELECT id FROM users WHERE telegram_user_id=101`).Scan(&h.uid); err != nil {
		t.Fatal(err)
	}
	return h
}
func (h *batchHarness) send(who int64, text, callback string) user.Reply {
	h.t.Helper()
	h.seq++
	if err := h.s.Accept(context.Background(), user.Message{UpdateID: h.seq, TelegramID: who, ChatID: who, Text: text, Callback: callback}); err != nil {
		h.t.Fatal(err)
	}
	var r user.Reply
	var raw []byte
	if err := h.p.QueryRow(context.Background(), `SELECT body,reply_markup FROM notification_outbox WHERE update_id=$1 AND user_id=(SELECT id FROM users WHERE telegram_user_id=$2)`, h.seq, who).Scan(&r.Text, &raw); err != nil {
		h.t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &r.Buttons); err != nil {
		h.t.Fatal(err)
	}
	return r
}
func (h *batchHarness) tap(r user.Reply, label string) user.Reply {
	h.t.Helper()
	return h.send(101, "", h.button(r, label))
}
func (h *batchHarness) button(r user.Reply, label string) string {
	h.t.Helper()
	for _, row := range r.Buttons {
		for _, b := range row {
			if strings.Contains(b.Text, label) {
				return b.Data
			}
		}
	}
	h.t.Fatalf("button %q missing: %+v", label, r)
	return ""
}
func (h *batchHarness) count(table string) int {
	h.t.Helper()
	var n int
	if err := h.p.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}
func (h *batchHarness) exec(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.p.Exec(context.Background(), sql, args...); err != nil {
		h.t.Fatal(err)
	}
}

func TestBulkInteractionLearningDateAndIdempotency(t *testing.T) {
	h := newBatchHarness(t)
	r := h.send(101, "Tahu Telor 20k\nNice 8 Ball Cafe 100k\nTAHU TELOR 25k", "")
	if !strings.Contains(r.Text, "Item 1/3") {
		t.Fatal(r)
	}
	old := h.button(r, "Food")
	r = h.tap(r, "Food")
	if !strings.Contains(r.Text, "Item 2/3") {
		t.Fatal("repeated description not categorized", r)
	}
	if got := h.send(101, "", old); !strings.Contains(got.Text, "outdated") {
		t.Fatal(got)
	}
	r = h.tap(r, "Coffee")
	if !strings.Contains(r.Text, "What date") {
		t.Fatal(r)
	}
	if h.count("expenses") != 0 || h.count("category_mappings") != 0 {
		t.Fatal("saved before date")
	}
	// Simulate process restart while waiting for a custom date.
	h.s = &Onboarding{Pool: h.p, Timezone: "Asia/Jakarta", Currency: "IDR"}
	if got := h.send(101, "2026-02-30", ""); !strings.Contains(got.Text, "YYYY-MM-DD") {
		t.Fatal(got)
	}
	r = h.send(101, "2026-01-29", "")
	if !strings.Contains(r.Text, "Rp145,000") {
		t.Fatal(r)
	}
	save := h.button(r, "Save all")
	r = h.tap(r, "Save all")
	if !strings.Contains(r.Text, "Saved 3") {
		t.Fatal(r)
	}
	h.send(101, "", save)
	// Replaying the same Telegram update is also a no-op.
	if err := h.s.Accept(context.Background(), user.Message{UpdateID: h.seq, TelegramID: 101, ChatID: 101, Callback: save}); err != nil {
		t.Fatal(err)
	}
	if h.count("expenses") != 3 || h.count("category_mappings") != 2 {
		t.Fatal("duplicate or missing entries")
	}
	var n int
	if err := h.p.QueryRow(context.Background(), `SELECT count(*) FROM expenses WHERE expense_date='2026-01-29'`).Scan(&n); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	r = h.send(101, "Tahu Telor 35k\nNice 8 Ball Cafe 40k", "")
	if !strings.Contains(r.Text, "What date") {
		t.Fatal("remembered categories ignored", r)
	}
	h.send(101, "/cancel", "")
	if h.count("expenses") != 3 {
		t.Fatal("cancel saved")
	}
}
func TestBulkValidationIsolationEditsAndExpiry(t *testing.T) {
	h := newBatchHarness(t)
	if r := h.send(101, "item a 20k\nitem b invalid", ""); !strings.Contains(r.Text, "line 2") {
		t.Fatal(r)
	}
	if h.count("expense_batches") != 0 {
		t.Fatal("invalid batch persisted")
	}
	r := h.send(101, "item a 20k\nitem b 35k", "")
	foreign := h.button(r, "Food")
	h.send(202, "/start", "")
	h.send(202, "Other", "")
	if r := h.send(202, "", foreign); !strings.Contains(r.Text, "unavailable") {
		t.Fatal(r)
	}
	r = h.tap(r, "New category")
	r = h.send(101, "Lunch", "")
	r = h.tap(r, "Food")
	r = h.tap(r, "Yesterday")
	r = h.tap(r, "Edit")
	r = h.tap(r, "1. ")
	r = h.tap(r, "Amount")
	r = h.send(101, "30k", "")
	if !strings.Contains(r.Text, "Rp65,000") {
		t.Fatal(r)
	}
	r = h.tap(r, "Edit")
	r = h.tap(r, "2. ")
	r = h.tap(r, "Description")
	r = h.send(101, "item a", "")
	if !strings.Contains(r.Text, "Lunch") {
		t.Fatal(r)
	}
	r = h.tap(r, "Save all")
	if !strings.Contains(r.Text, "Saved 2") {
		t.Fatal(r)
	}
	var mappings int
	h.p.QueryRow(context.Background(), `SELECT count(*) FROM category_mappings WHERE user_id=$1`, h.uid).Scan(&mappings)
	if mappings != 1 {
		t.Fatal(mappings)
	}
	r = h.send(101, "new one 20k\nnew two 30k", "")
	cancel := h.button(r, "Cancel")
	h.exec(`UPDATE expense_batches SET expires_at=now()-interval '1 second' WHERE status='pending'`)
	if r = h.send(101, "", cancel); !strings.Contains(r.Text, "expired") {
		t.Fatal(r)
	}
	if h.count("expenses") != 2 {
		t.Fatal("expired saved")
	}
}
func TestBulkGuardsAndBlacklisting(t *testing.T) {
	h := newBatchHarness(t)
	r := h.send(101, "single 20k", "")
	if got := h.send(101, "first 20k\nsecond 30k", ""); !strings.Contains(got.Text, "existing draft") {
		t.Fatal(got)
	}
	h.tap(r, "Cancel")
	r = h.send(101, "first 20k\nsecond 30k", "")
	r = h.tap(r, "Food")
	r = h.tap(r, "Food")
	r = h.tap(r, "Today")
	save := h.button(r, "Save all")
	if got := h.send(101, "third 50k", ""); !strings.Contains(got.Text, "Use the buttons") {
		t.Fatal(got)
	}
	if err := (Admin{Pool: h.p}).SetAccess(context.Background(), h.uid, "blocked", ""); err != nil {
		t.Fatal(err)
	}
	if err := (Admin{Pool: h.p}).SetAccess(context.Background(), h.uid, "allowed", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.send(101, "", save); !strings.Contains(got.Text, "cancelled") {
		t.Fatal(got)
	}
	if h.count("expenses") != 0 || h.count("category_mappings") != 0 {
		t.Fatal("blocked batch saved")
	}
}
func TestBulkAtomicRollback(t *testing.T) {
	h := newBatchHarness(t)
	r := h.send(101, "first 20k\nsecond 30k", "")
	r = h.tap(r, "Food")
	r = h.tap(r, "Food")
	r = h.tap(r, "Today")
	// Force an error during the second insert, after the first expense and mapping.
	h.exec(`CREATE FUNCTION fail_batch_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.description='first' THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$`)
	h.exec(`CREATE TRIGGER batch_failure BEFORE INSERT ON expenses FOR EACH ROW EXECUTE FUNCTION fail_batch_item()`)
	h.seq++
	m := user.Message{UpdateID: h.seq, TelegramID: 101, ChatID: 101, Callback: h.button(r, "Save all")}
	if err := h.s.Accept(context.Background(), m); err == nil {
		t.Fatal("expected failure")
	}
	if h.count("expenses") != 0 || h.count("category_mappings") != 0 {
		t.Fatal("partial save")
	}
	h.exec(`DROP TRIGGER batch_failure ON expenses`)
	if err := h.s.Accept(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if h.count("expenses") != 2 {
		t.Fatal("retry did not save")
	}
}
func TestBulkWeeklyRecordsCoalesced(t *testing.T) {
	h := newBatchHarness(t)
	h.send(202, "/start", "")
	h.send(202, "Budi", "")
	var other, pair int64
	h.p.QueryRow(context.Background(), `SELECT id FROM users WHERE telegram_user_id=202`).Scan(&other)
	h.p.QueryRow(context.Background(), `INSERT INTO comparison_pairs(connected_at) VALUES(now()-interval '7 days') RETURNING id`).Scan(&pair)
	h.exec(`INSERT INTO active_pair_members(user_id,pair_id,slot) VALUES($1,$3,1),($2,$3,2)`, h.uid, other, pair)
	// Establish both categories, then save a batch with several competing food entries.
	r := h.send(101, "baseline food 10k", "")
	r = h.tap(r, "Food")
	h.tap(r, "Save")
	r = h.send(101, "baseline coffee 5k", "")
	r = h.tap(r, "Coffee")
	h.tap(r, "Save")
	r = h.send(101, "food a 20k\nfood b 80k\ncoffee 30k", "")
	r = h.tap(r, "Food")
	r = h.tap(r, "Food")
	r = h.tap(r, "Coffee")
	r = h.tap(r, "Today")
	r = h.tap(r, "Save all")
	if strings.Count(r.Text, "weekly record") != 2 {
		t.Fatal(r)
	}
	var body string
	if err := h.p.QueryRow(context.Background(), `SELECT body FROM notification_outbox WHERE update_id=$1 AND user_id=$2`, h.seq, other).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Rp80,000") || !strings.Contains(body, "Rp30,000") || strings.Contains(body, "Rp20,000") {
		t.Fatal(body)
	}
	var max int64
	if err := h.p.QueryRow(context.Background(), `SELECT amount_minor FROM weekly_category_records WHERE pair_id=$1 AND category_key='food & drinks'`, pair).Scan(&max); err != nil || max != 8000000 {
		t.Fatal(max, err)
	}
	// Historical batch must not send a partner alert.
	r = h.send(101, "food a 200k\nfood b 300k", "")
	r = h.send(101, time.Now().AddDate(0, 0, -30).Format("2006-01-02"), "")
	r = h.tap(r, "Save all")
	var n int
	h.p.QueryRow(context.Background(), `SELECT count(*) FROM notification_outbox WHERE update_id=$1 AND user_id=$2`, h.seq, other).Scan(&n)
	if n != 0 {
		t.Fatal("historical announcement", fmt.Sprint(n))
	}
}

func TestBulkPaginationAndSmoking(t *testing.T) {
	h := newBatchHarness(t)
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("same item %dk", i+1)
	}
	r := h.send(101, strings.Join(lines, "\n"), "")
	for _, row := range r.Buttons {
		for _, b := range row {
			if strings.Contains(b.Text, "Smoking") {
				t.Fatal("smoking visible")
			}
		}
	}
	var cat int64
	if err := h.p.QueryRow(context.Background(), `SELECT id FROM categories WHERE user_id=$1 AND normalized_name='smoking & vaping'`, h.uid).Scan(&cat); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(h.button(r, "Food"), ":")
	parts[4] = fmt.Sprint(cat)
	if got := h.send(101, "", strings.Join(parts, ":")); !strings.Contains(got.Text, "unavailable") {
		t.Fatal(got)
	}
	r = h.tap(r, "Food")
	r = h.tap(r, "Today")
	for page := 1; page <= 4; page++ {
		if !strings.Contains(r.Text, fmt.Sprintf("page %d/4", page)) {
			t.Fatal(r)
		}
		if page < 4 {
			r = h.tap(r, "Next")
		}
	}
	r = h.tap(r, "Save all")
	if !strings.Contains(r.Text, "Saved 20") || h.count("expenses") != 20 {
		t.Fatal(r)
	}
	if h.count("category_mappings") != 1 {
		t.Fatal("repeated description learned multiple times")
	}
}
