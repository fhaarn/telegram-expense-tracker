//go:build integration

package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"telegram-expense-tracker/internal/user"
	"testing"
)

func TestComparisonConsentConcurrencyAndDisconnect(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var ids []int64
	for i := int64(1); i <= 3; i++ {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,display_name,status) VALUES($1,$1,$2,'active') RETURNING id`, i, fmt.Sprintf("Person%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	run := func(uid, update int64, m user.Message) (user.Reply, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return user.Reply{}, err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7312041)`); err != nil {
			return user.Reply{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO inbound_updates(update_id,user_id) VALUES($1,$2)`, update, uid); err != nil {
			return user.Reply{}, err
		}
		m.UpdateID = update
		r, err := HandleComparison(ctx, tx, uid, fmt.Sprintf("Person%d", uid), "Asia/Jakarta", "test_bot", m)
		if err != nil {
			return r, err
		}
		return r, tx.Commit(ctx)
	}
	r, err := run(ids[0], 1, user.Message{Text: "/compare"})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Split(strings.Split(r.Text, "compare_")[1], "\n")[0]
	if len(token) != 32 {
		t.Fatal("incorrect token length")
	}
	// A forged callback without having opened the invitation cannot establish a pair.
	var invite int64
	if err = pool.QueryRow(ctx, `SELECT id FROM comparison_invites WHERE creator_id=$1`, ids[0]).Scan(&invite); err != nil {
		t.Fatal(err)
	}
	if r, err = run(ids[1], 2, user.Message{Callback: fmt.Sprintf("c:accept:%d", invite)}); err != nil || !strings.Contains(r.Text, "no longer available") {
		t.Fatalf("forged consent accepted: %+v %v", r, err)
	}
	var callbacks []string
	for i, uid := range ids[1:] {
		r, err = run(uid, int64(3+i), user.Message{Text: "/start compare_" + token})
		if err != nil {
			t.Fatal(err)
		}
		callbacks = append(callbacks, r.Buttons[0][0].Data)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, uid := range ids[1:] {
		wg.Add(1)
		go func(i int, uid int64) {
			defer wg.Done()
			_, err := run(uid, int64(5+i), user.Message{Callback: callbacks[i]})
			errs <- err
		}(i, uid)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM active_pair_members`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("pair members: %d %v", n, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE pair_id IS NOT NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("notification count: %d %v", n, err)
	}
	r, err = run(ids[0], 7, user.Message{Text: "/compare"})
	if err != nil || !strings.Contains(r.Text, "No recorded expenses") {
		t.Fatalf("empty report %+v %v", r, err)
	}
	r, err = run(ids[0], 8, user.Message{Text: "/disconnect"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = run(ids[0], 9, user.Message{Callback: r.Buttons[0][0].Data})
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM active_pair_members`).Scan(&n); err != nil || n != 0 {
		t.Fatal("disconnect did not remove access")
	}
	r, err = run(ids[1], 10, user.Message{Callback: callbacks[0]})
	if err != nil || !strings.Contains(r.Text, "no longer available") {
		t.Fatal("consumed invite restored pair")
	}
}

func TestComparisonDateBoundaryAndPrivacy(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var pair int64
	if err = tx.QueryRow(ctx, `INSERT INTO comparison_pairs DEFAULT VALUES RETURNING id`).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 3; i++ {
		var uid, cat int64
		if err = tx.QueryRow(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,display_name,status) VALUES($1,$1,$2,'active') RETURNING id`, i, fmt.Sprintf("Person%d", i)).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			if _, err = tx.Exec(ctx, `INSERT INTO active_pair_members(user_id,pair_id,slot) VALUES($1,$2,$3)`, uid, pair, i); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.QueryRow(ctx, `INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,'Secret category','secret') RETURNING id`, uid).Scan(&cat); err != nil {
			t.Fatal(err)
		}
		for offset := -1; offset <= 0; offset++ {
			var draft int64
			if err = tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) VALUES($1,'Private merchant','private',10000,$2,(now() AT TIME ZONE 'Asia/Jakarta')::date+$3::integer) RETURNING id`, uid, cat, offset).Scan(&draft); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO expenses(user_id,originating_draft_id,description,amount_minor,category_id,expense_date) SELECT user_id,id,description,amount_minor,category_id,expense_date FROM expense_drafts WHERE id=$1`, draft); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, err := comparisonReport(ctx, tx, 1, "Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Text, "Private") || strings.Contains(r.Text, "Secret") || strings.Contains(r.Text, "Person3") || strings.Contains(r.Text, "Rp200") {
		t.Fatalf("leaked prior/private data: %s", r.Text)
	}
	if strings.Count(r.Text, "Rp100 · 1 expenses") != 4 {
		t.Fatalf("boundary totals wrong: %s", r.Text)
	}
}

func TestComparisonInviteEligibility(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for i := int64(1); i <= 2; i++ {
		if _, err = tx.Exec(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,display_name,status) VALUES($1,$1,'Test','active')`, i); err != nil {
			t.Fatal(err)
		}
	}
	token := "0123456789abcdef0123456789abcdef"
	h, _ := inviteHash(token)
	if _, err = tx.Exec(ctx, `INSERT INTO comparison_invites(creator_id,token_hash) VALUES(1,$1)`, h); err != nil {
		t.Fatal(err)
	}
	if err = SavePendingInvite(ctx, tx, 1, token); err != nil {
		t.Fatal(err)
	}
	r, err := PendingInviteReply(ctx, tx, 1)
	if err != nil || !strings.Contains(r.Text, "yourself") {
		t.Fatal("self invite accepted")
	}
	if err = SavePendingInvite(ctx, tx, 2, token); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE comparison_invites SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	r, err = PendingInviteReply(ctx, tx, 2)
	if err != nil || !strings.Contains(r.Text, "expired") {
		t.Fatal("expired invite accepted")
	}
	if err = SavePendingInvite(ctx, tx, 2, ""); err != nil {
		t.Fatal(err)
	}
	r, err = PendingInviteReply(ctx, tx, 2)
	if err != nil || r.Text != "" {
		t.Fatal("missing pending invite not empty")
	}
}

func TestComparisonOnboardingInvitePersistsAndConsent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	service := &Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR", BotUsername: "test_bot"}
	send := func(update, person int64, text, callback string) {
		t.Helper()
		if err := service.Accept(ctx, user.Message{UpdateID: update, TelegramID: person, ChatID: person, Text: text, Callback: callback}); err != nil {
			t.Fatal(err)
		}
	}
	send(1, 101, "/start", "")
	send(2, 101, "Alice", "")
	send(3, 101, "/compare", "")
	var body string
	if err := pool.QueryRow(ctx, `SELECT body FROM notification_outbox WHERE update_id=3`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	token := strings.Split(strings.Split(body, "compare_")[1], "\n")[0]
	send(4, 202, "/start compare_"+token, "")
	// Recreate application while recipient is still choosing a name.
	service = &Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR", BotUsername: "test_bot"}
	send(5, 202, "Bob", "")
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_pair_members`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("paired before consent: %d %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT body FROM notification_outbox WHERE update_id=5`).Scan(&body); err != nil || !strings.Contains(body, "Connect with Alice?") {
		t.Fatalf("lost invite: %q %v", body, err)
	}
	var invite int64
	if err := pool.QueryRow(ctx, `SELECT id FROM comparison_invites WHERE state='pending'`).Scan(&invite); err != nil {
		t.Fatal(err)
	}
	send(6, 202, "", fmt.Sprintf("c:accept:%d", invite))
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE update_id=6 AND pair_id IS NOT NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("pair notifications: %d %v", count, err)
	}
	var pair int64
	if err := pool.QueryRow(ctx, `SELECT pair_id FROM active_pair_members LIMIT 1`).Scan(&pair); err != nil {
		t.Fatal(err)
	}
	send(7, 101, "/compare", "")
	send(8, 101, "", fmt.Sprintf("c:disconnect:%d", pair))
	out := Outbox{Pool: pool}
	// Draining simulates recovering queued notifications after disconnect.
	for {
		d, ok, err := out.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if strings.Contains(d.Body, "battling") || strings.Contains(d.Body, "partner in crime") || strings.Contains(d.Body, "Comparing since") {
			t.Fatalf("stale relationship data: %q", d.Body)
		}
		if err := out.Finish(ctx, d, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestComparisonInviteRotationAndHourlyLimit(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var uid int64
	if err = tx.QueryRow(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,display_name,status) VALUES(1,1,'Alice','active') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	var last user.Reply
	for i := 0; i < 5; i++ {
		last, err = HandleComparison(ctx, tx, uid, "Alice", "Asia/Jakarta", "test_bot", user.Message{Text: "/compare"})
		if err != nil || !strings.Contains(last.Text, "https://t.me/") {
			t.Fatalf("invite %d: %+v %v", i, last, err)
		}
	}
	r, err := HandleComparison(ctx, tx, uid, "Alice", "Asia/Jakarta", "test_bot", user.Message{Text: "/compare"})
	if err != nil || !strings.Contains(r.Text, "in an hour") {
		t.Fatalf("limit: %+v %v", r, err)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM comparison_invites WHERE state='revoked'`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("rotation: %d %v", count, err)
	}
	_, err = HandleComparison(ctx, tx, uid, "Alice", "Asia/Jakarta", "test_bot", user.Message{Callback: last.Buttons[0][0].Data})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM comparison_invites WHERE state='pending'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("revoke: %d %v", count, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE comparison_invites SET created_at=now()-interval '61 minutes'`); err != nil {
		t.Fatal(err)
	}
	r, err = HandleComparison(ctx, tx, uid, "Alice", "Asia/Jakarta", "test_bot", user.Message{Text: "/compare"})
	if err != nil || !strings.Contains(r.Text, "https://t.me/") {
		t.Fatalf("reset: %+v %v", r, err)
	}
}
