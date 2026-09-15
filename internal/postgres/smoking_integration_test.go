//go:build integration

package postgres

import (
	"context"
	"fmt"
	"strings"
	"telegram-expense-tracker/internal/user"
	"testing"
)

func TestSmokingPreferenceAndCategoryVisibility(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	service := Onboarding{Pool: pool, Timezone: "Asia/Jakarta", Currency: "IDR"}
	var update int64
	send := func(person int64, text, callback string) string {
		t.Helper()
		update++
		if err := service.Accept(ctx, user.Message{UpdateID: update, TelegramID: person, ChatID: person, Text: text, Callback: callback}); err != nil {
			t.Fatal(err)
		}
		var body string
		if err := pool.QueryRow(ctx, `SELECT body FROM notification_outbox WHERE update_id=$1 AND user_id=(SELECT id FROM users WHERE telegram_user_id=$2)`, update, person).Scan(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	for i, answer := range []string{"yes", "no"} {
		person := int64(100 + i)
		send(person, "/start", "")
		body := send(person, "Test", "")
		if !strings.Contains(body, "Do you smoke or vape?") || !strings.Contains(body, "/help") || strings.Contains(body, "Coffee & drinks is included") {
			t.Fatal(body)
		}
		var uid, cat int64
		if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE telegram_user_id=$1`, person).Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT id FROM categories WHERE user_id=$1 AND normalized_name='smoking & vaping'`, uid).Scan(&cat); err != nil {
			t.Fatal(err)
		}
		check := func(want bool) {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			found, coffee := false, false
			for page := 0; page < 2; page++ {
				r, err := picker(ctx, tx, uid, expenseDraft{}, page)
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range r.Buttons {
					for _, b := range row {
						found = found || strings.Contains(b.Text, "Smoking & vaping")
						coffee = coffee || strings.Contains(b.Text, "Coffee & drinks")
					}
				}
			}
			if found != want || !coffee {
				t.Fatalf("smoking=%v want %v coffee=%v", found, want, coffee)
			}
		}
		check(false)
		send(person, "", "profile:smoker:"+answer)
		check(answer == "yes")
		opposite := "yes"
		if answer == "yes" {
			opposite = "no"
		}
		send(person, "", "profile:smoker:"+opposite)
		var smoker bool
		if err := pool.QueryRow(ctx, `SELECT is_smoker FROM users WHERE id=$1`, uid).Scan(&smoker); err != nil || smoker != (answer == "yes") {
			t.Fatalf("stale button overwrote preference: %v %v", smoker, err)
		}
		if strings.Contains(send(person, "/start", ""), "Do you smoke") {
			t.Fatal("asked again after answer")
		}
		// Crafted category callbacks cannot select the hidden built-in category.
		send(person, "test 20k", "")
		var draft, version int64
		if err := pool.QueryRow(ctx, `SELECT id,version FROM expense_drafts WHERE user_id=$1 AND status='pending' ORDER BY id DESC LIMIT 1`, uid).Scan(&draft, &version); err != nil {
			t.Fatal(err)
		}
		body = send(person, "", fmt.Sprintf("e:cat:%d:%d:%d", draft, version, cat))
		if answer == "no" && !strings.Contains(body, "Category unavailable") {
			t.Fatal(body)
		}
	}
	// Existing users with an unanswered preference are prompted on /start.
	if _, err := pool.Exec(ctx, `UPDATE users SET is_smoker=NULL WHERE telegram_user_id=100`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(send(100, "/start", ""), "Do you smoke or vape?") {
		t.Fatal("existing user not prompted")
	}
}
