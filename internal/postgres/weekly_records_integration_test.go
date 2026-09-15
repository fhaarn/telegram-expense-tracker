//go:build integration

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWeeklyRecords(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, baselineDate, newDate, connected, baselineCategory string
		baseline, amount                                         int64
		deleted, paired, want                                    bool
	}{
		{"first quiet", "2026-09-15", "2026-09-15", "2026-09-14", "food", 0, 20000000, false, true, false},
		{"partner record", "2026-09-14", "2026-09-15", "2026-09-14", "food", 10000000, 20000000, false, true, true},
		{"tie quiet", "2026-09-15", "2026-09-15", "2026-09-14", "food", 20000000, 20000000, false, true, false},
		{"lower quiet", "2026-09-15", "2026-09-15", "2026-09-14", "food", 30000000, 20000000, false, true, false},
		{"previous Sunday excluded", "2026-09-13", "2026-09-15", "2026-09-01", "food", 10000000, 20000000, false, true, false},
		{"before connection excluded", "2026-09-14", "2026-09-15", "2026-09-15", "food", 10000000, 20000000, false, true, false},
		{"different category", "2026-09-14", "2026-09-15", "2026-09-14", "transport", 10000000, 20000000, false, true, false},
		{"deleted baseline", "2026-09-14", "2026-09-15", "2026-09-14", "food", 10000000, 20000000, true, true, false},
		{"historical entry", "2026-09-14", "2026-09-13", "2026-09-01", "food", 10000000, 20000000, false, true, false},
		{"future entry", "2026-09-14", "2026-09-16", "2026-09-14", "food", 10000000, 20000000, false, true, false},
		{"unpaired", "2026-09-14", "2026-09-15", "2026-09-14", "food", 10000000, 20000000, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var ids, cats [2]int64
			for i := range ids {
				must(tx.QueryRow(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id,display_name,status) VALUES($1,$1,$2,'active') RETURNING id`, i+100, fmt.Sprintf("Person%d", i)).Scan(&ids[i]))
				key := "food"
				if i == 1 {
					key = tc.baselineCategory
				}
				must(tx.QueryRow(ctx, `INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,'Food', $2) RETURNING id`, ids[i], key).Scan(&cats[i]))
			}
			var pair int64
			must(tx.QueryRow(ctx, `INSERT INTO comparison_pairs(connected_at) VALUES($1::date::timestamp AT TIME ZONE 'Asia/Jakarta') RETURNING id`, tc.connected).Scan(&pair))
			if tc.paired {
				_, err = tx.Exec(ctx, `INSERT INTO active_pair_members(user_id,pair_id,slot) VALUES($1,$3,1),($2,$3,2)`, ids[0], ids[1], pair)
				must(err)
			}
			add := func(i int, amount int64, date string) int64 {
				var draft, id int64
				must(tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) VALUES($1,'secret purchase','secret purchase',$2,$3,$4) RETURNING id`, ids[i], amount, cats[i], date).Scan(&draft))
				must(tx.QueryRow(ctx, `INSERT INTO expenses(user_id,originating_draft_id,description,amount_minor,category_id,expense_date) VALUES($1,$2,'secret purchase',$3,$4,$5) RETURNING id`, ids[i], draft, amount, cats[i], date).Scan(&id))
				return id
			}
			if tc.baseline > 0 {
				id := add(1, tc.baseline, tc.baselineDate)
				if tc.deleted {
					_, err = tx.Exec(ctx, `UPDATE expenses SET deleted_at=now() WHERE id=$1`, id)
					must(err)
				}
			}
			id := add(0, tc.amount, tc.newDate)
			_, err = tx.Exec(ctx, `INSERT INTO inbound_updates(update_id) VALUES(999)`)
			must(err)
			alert, err := weeklyRecord(ctx, tx, ids[0], id, 999, "Asia/Jakarta", "2026-09-15")
			must(err)
			if (alert != "") != tc.want {
				t.Fatalf("unexpected alert: %q", alert)
			}
			var count int
			must(tx.QueryRow(ctx, `SELECT count(*) FROM notification_outbox`).Scan(&count))
			if tc.want {
				if count != 1 {
					t.Fatalf("notifications: %d", count)
				}
				var body string
				var recipient, gotPair int64
				must(tx.QueryRow(ctx, `SELECT body,user_id,pair_id FROM notification_outbox`).Scan(&body, &recipient, &gotPair))
				if recipient != ids[1] || gotPair != pair || !strings.Contains(body, "Person0") || !strings.Contains(body, "Rp200,000") || strings.Contains(body, "secret purchase") {
					t.Fatalf("incorrect partner notification: %s", body)
				}

				// The next save reads the stored winner. A tie stays quiet.
				next := add(1, tc.amount, tc.newDate)
				follow, e := weeklyRecord(ctx, tx, ids[1], next, 999, "Asia/Jakarta", "2026-09-15")
				must(e)
				if follow != "" {
					t.Fatal("cached tie alerted")
				}
				var winner, cached int64
				must(tx.QueryRow(ctx, `SELECT expense_id,amount_minor FROM weekly_category_records WHERE pair_id=$1`, pair).Scan(&winner, &cached))
				if winner != id || cached != tc.amount {
					t.Fatal("cache lost original winner on tie")
				}
				// Delete the winning expense through the real callback path.
				_, e = savedCallback(ctx, tx, ids[0], "remove", id, 1, "2026-09-15")
				must(e)
				must(tx.QueryRow(ctx, `SELECT expense_id,amount_minor FROM weekly_category_records WHERE pair_id=$1`, pair).Scan(&winner, &cached))
				if winner != next {
					t.Fatal("delete did not promote tied runner up")
				}
				// Edit the remaining winner downward through Save.
				var editID int64
				must(tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) VALUES($1,'edited','edited',5000000,$2,$3) RETURNING id`, ids[1], cats[1], tc.newDate).Scan(&editID))
				parsed, _ := time.Parse("2006-01-02", tc.newDate)
				_, e = saveDraft(ctx, tx, ids[1], expenseDraft{ID: editID, Category: cats[1], Description: "edited", Amount: 5000000, Date: parsed, Target: next, TargetVersion: 1}, "2026-09-15")
				must(e)
				must(tx.QueryRow(ctx, `SELECT amount_minor FROM weekly_category_records WHERE pair_id=$1`, pair).Scan(&cached))
				if cached != tc.baseline {
					t.Fatal("edit did not restore baseline")
				}
				must(tx.QueryRow(ctx, `SELECT count(*) FROM notification_outbox`).Scan(&count))
				if count != 1 {
					t.Fatal("corrections sent extra notifications")
				}
			} else if count != 0 {
				t.Fatal("unexpected notification")
			}
		})
	}
}

func TestWeeklySaveMarkerOnlyForNewExpense(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var uid, cat, id int64
	if err = tx.QueryRow(ctx, `INSERT INTO users(telegram_user_id,telegram_chat_id) VALUES(123,123) RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,'Food','food') RETURNING id`, uid).Scan(&cat); err != nil {
		t.Fatal(err)
	}
	date, _ := time.Parse("2006-01-02", "2026-09-15")
	create := func(target int64) expenseDraft {
		if err = tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date) VALUES($1,'food','food',20000000,$2,$3) RETURNING id`, uid, cat, date).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return expenseDraft{ID: id, Category: cat, Description: "food", Amount: 20000000, Date: date, Target: target, TargetVersion: 1}
	}
	r, err := saveDraft(ctx, tx, uid, create(0), "2026-09-15")
	if err != nil || r.SavedExpenseID == 0 {
		t.Fatalf("new save: %+v %v", r, err)
	}
	r, err = saveDraft(ctx, tx, uid, create(r.SavedExpenseID), "2026-09-15")
	if err != nil || r.SavedExpenseID != 0 {
		t.Fatalf("edit: %+v %v", r, err)
	}
}
