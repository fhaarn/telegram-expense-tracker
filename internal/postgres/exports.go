package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	report "telegram-expense-tracker/internal/export"
	"telegram-expense-tracker/internal/user"
	"time"
)

func exportExpenses(ctx context.Context, tx pgx.Tx, uid int64, text, timezone string) (user.Reply, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return user.Reply{}, err
	}
	start, end, err := report.ParseRange(text, time.Now().In(loc))
	if err != nil {
		return reply("📄 Use /export for this month, or /export YYYY-MM-DD YYYY-MM-DD (up to 366 days)."), nil
	}
	var recent bool
	var queued int
	err = tx.QueryRow(ctx, `SELECT COALESCE(last_export_at>now()-interval '5 minutes',false) OR EXISTS(SELECT 1 FROM notification_outbox WHERE user_id=$1 AND export_payload IS NOT NULL AND status IN ('pending','sending')) FROM users WHERE id=$1`, uid).Scan(&recent)
	if err != nil {
		return user.Reply{}, err
	}
	if recent {
		return reply("⏳ Please wait five minutes between exports and let your queued report finish."), nil
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM notification_outbox WHERE export_payload IS NOT NULL AND status IN ('pending','sending')`).Scan(&queued)
	if err != nil {
		return user.Reply{}, err
	}
	if queued >= 20 {
		return reply("⏳ Report queue is busy. Please try again shortly."), nil
	}
	rows, err := tx.Query(ctx, `SELECT e.expense_date::text,c.display_name,e.description,e.amount_minor/100 FROM expenses e JOIN categories c ON c.user_id=e.user_id AND c.id=e.category_id WHERE e.user_id=$1 AND e.deleted_at IS NULL AND e.expense_date BETWEEN $2::date AND $3::date ORDER BY e.expense_date,e.id LIMIT $4`, uid, start, end, report.MaxRows+1)
	if err != nil {
		return user.Reply{}, err
	}
	r := report.Report{Start: start, End: end, Rows: []report.Row{}}
	var total int64
	tooLarge := false
	for rows.Next() {
		var row report.Row
		if err = rows.Scan(&row.Date, &row.Category, &row.Notes, &row.Amount); err != nil {
			rows.Close()
			return user.Reply{}, err
		}
		if row.Amount > report.MaxTotalRupiah-total {
			tooLarge = true
		} else {
			total += row.Amount
		}
		r.Rows = append(r.Rows, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return user.Reply{}, err
	}
	if len(r.Rows) > report.MaxRows || tooLarge {
		return reply("📄 This report is too large. Choose a smaller date range (up to 2,000 expenses)."), nil
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return user.Reply{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE users SET last_export_at=now() WHERE id=$1`, uid)
	return user.Reply{Text: "📄 Your expense report · " + start + " to " + end, ExportPayload: payload}, err
}
