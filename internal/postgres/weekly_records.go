package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"telegram-expense-tracker/internal/category"
	"telegram-expense-tracker/internal/parser"
)

// weeklyRecord runs in the intake transaction under the shared advisory lock.
// Normal saves compare one cached row; edits/deletions rebuild cached baselines.
func weeklyRecord(ctx context.Context, tx pgx.Tx, uid, expenseID, updateID int64, timezone, today string) (string, error) {
	day, err := time.Parse("2006-01-02", today)
	if err != nil {
		return "", err
	}
	monday := day.AddDate(0, 0, -(int(day.Weekday())+6)%7).Format("2006-01-02")
	end := day.AddDate(0, 0, 7-(int(day.Weekday())+6)%7).Format("2006-01-02")
	var pair, partner, amount int64
	var name, cat, key, date, connected string
	err = tx.QueryRow(ctx, `SELECT p.id,other.user_id,u.display_name,c.display_name,c.normalized_name,e.amount_minor,e.expense_date::text,(p.connected_at AT TIME ZONE $3)::date::text
 FROM active_pair_members me JOIN comparison_pairs p ON p.id=me.pair_id AND p.ended_at IS NULL
 JOIN active_pair_members other ON other.pair_id=p.id AND other.user_id<>me.user_id
 JOIN users u ON u.id=me.user_id
 JOIN expenses e ON e.user_id=me.user_id AND e.id=$2 AND e.deleted_at IS NULL
 JOIN categories c ON c.user_id=e.user_id AND c.id=e.category_id
 WHERE me.user_id=$1`, uid, expenseID, timezone).Scan(&pair, &partner, &name, &cat, &key, &amount, &date, &connected)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Only current-week entries send alerts. Keep already materialized records
	// accurate for historical additions too.
	if date < monday || date < connected || date >= end || date > today {
		return "", refreshWeeklyRecords(ctx, tx, uid)
	}
	var previous *int64
	var through string
	err = tx.QueryRow(ctx, `SELECT amount_minor,evaluated_through::text FROM weekly_category_records WHERE pair_id=$1 AND week_start=$2::date AND category_key=$3 FOR UPDATE`, pair, monday, key).Scan(&previous, &through)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && through != today) {
		// Initialize existing pairs lazily. Refresh once per date so future-dated
		// entries become eligible without needing a scheduler.
		previous, err = rebuildWeeklyRecord(ctx, tx, pair, monday, key, timezone, today, expenseID)
	}
	if err != nil {
		return "", err
	}
	if previous != nil && amount <= *previous {
		return "", nil
	}
	_, err = tx.Exec(ctx, `UPDATE weekly_category_records SET expense_id=$4,amount_minor=$5,updated_at=now() WHERE pair_id=$1 AND week_start=$2::date AND category_key=$3`, pair, monday, key, expenseID, amount)
	if err != nil {
		return "", err
	}
	if previous == nil {
		return "", nil
	}
	label, money := category.Label(cat), parser.FormatAmount(amount)
	body := fmt.Sprintf("😱 %s just set a new weekly record!\n%s · %s", name, label, money)
	_, err = tx.Exec(ctx, `INSERT INTO notification_outbox(update_id,user_id,chat_id,body,pair_id)
 SELECT $1,id,telegram_chat_id,$3,$4 FROM users WHERE id=$2`, updateID, partner, body, pair)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("🏆 You just hit a weekly record!\n%s · %s 😱", label, money), nil
}

// rebuildWeeklyRecord is used only for cache initialization, date rollover, and
// corrections. Empty baselines are retained to avoid repeating empty scans.
func rebuildWeeklyRecord(ctx context.Context, tx pgx.Tx, pair int64, week, key, timezone, today string, exclude int64) (*int64, error) {
	var id, amount *int64
	err := tx.QueryRow(ctx, `SELECT e.id,e.amount_minor FROM expenses e
 JOIN active_pair_members m ON m.user_id=e.user_id AND m.pair_id=$1
 JOIN comparison_pairs p ON p.id=m.pair_id AND p.ended_at IS NULL
 JOIN categories c ON c.user_id=e.user_id AND c.id=e.category_id
 WHERE c.normalized_name=$3 AND e.deleted_at IS NULL AND e.id<>$6
 AND e.expense_date >= GREATEST($2::date,(p.connected_at AT TIME ZONE $4)::date)
 AND e.expense_date < $2::date+7 AND e.expense_date <= $5::date
 ORDER BY e.amount_minor DESC,e.id ASC LIMIT 1`, pair, week, key, timezone, today, exclude).Scan(&id, &amount)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO weekly_category_records(pair_id,week_start,category_key,expense_id,amount_minor,evaluated_through)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(pair_id,week_start,category_key) DO UPDATE SET expense_id=excluded.expense_id,amount_minor=excluded.amount_minor,evaluated_through=excluded.evaluated_through,updated_at=now()`, pair, week, key, id, amount, today)
	return amount, err
}

// Corrections are infrequent: rebuild this pair's materialized buckets in the
// same transaction. This covers both old and new category/date on an edit.
func refreshWeeklyRecords(ctx context.Context, tx pgx.Tx, uid int64) error {
	rows, err := tx.Query(ctx, `SELECT r.pair_id,r.week_start::text,r.category_key,u.timezone,(now() AT TIME ZONE u.timezone)::date::text
 FROM weekly_category_records r JOIN active_pair_members m ON m.pair_id=r.pair_id
 JOIN users u ON u.id=m.user_id WHERE m.user_id=$1`, uid)
	if err != nil {
		return err
	}
	type bucket struct {
		pair                   int64
		week, key, zone, today string
	}
	var buckets []bucket
	for rows.Next() {
		var b bucket
		if err = rows.Scan(&b.pair, &b.week, &b.key, &b.zone, &b.today); err != nil {
			rows.Close()
			return err
		}
		buckets = append(buckets, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, b := range buckets {
		if _, err = rebuildWeeklyRecord(ctx, tx, b.pair, b.week, b.key, b.zone, b.today, 0); err != nil {
			return err
		}
	}
	return nil
}
