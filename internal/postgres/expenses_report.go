package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math/big"
	"strings"
	"telegram-expense-tracker/internal/category"
	"telegram-expense-tracker/internal/parser"
	"telegram-expense-tracker/internal/user"
	"time"
)

// Numeric aggregates may exceed int64 even though each expense does not.
func formatAggregate(value string) string {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return "Rp0"
	}
	n.Quo(n, big.NewInt(100))
	s := n.String()
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "Rp" + s
}
func reportTotal(ctx context.Context, tx pgx.Tx, uid int64, start, end string) (string, error) {
	var total string
	err := tx.QueryRow(ctx, "SELECT COALESCE(sum(amount_minor),0)::text FROM expenses WHERE user_id=$1 AND expense_date BETWEEN $2::date AND $3::date AND deleted_at IS NULL", uid, start, end).Scan(&total)
	return formatAggregate(total), err
}
func expenseReport(ctx context.Context, tx pgx.Tx, uid int64, kind, today string, page int) (user.Reply, error) {
	start, end := today, today
	if kind == "month" {
		d, _ := time.Parse("2006-01-02", today)
		start = d.Format("2006-01") + "-01"
		end = time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	if kind == "recent" {
		start = "1900-01-01"
		end = "9999-12-31"
	}
	total, err := reportTotal(ctx, tx, uid, start, end)
	if err != nil {
		return user.Reply{}, err
	}
	var count int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM expenses WHERE user_id=$1 AND expense_date BETWEEN $2::date AND $3::date AND deleted_at IS NULL", uid, start, end).Scan(&count); err != nil {
		return user.Reply{}, err
	}
	title := map[string]string{"today": "Today's expenses", "month": "This month's expenses", "recent": "Recent expenses"}[kind]
	r := reply(fmt.Sprintf("📊 %s\nTotal: %s · %d expenses", title, total, count))
	if count == 0 {
		r.Text = "🍃 No expenses recorded for this period.\nTotal: Rp0"
		return r, nil
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.description,e.amount_minor,c.display_name,e.expense_date,e.version FROM expenses e JOIN categories c ON (c.user_id,c.id)=(e.user_id,e.category_id) WHERE e.user_id=$1 AND e.expense_date BETWEEN $2::date AND $3::date AND e.deleted_at IS NULL ORDER BY e.expense_date DESC,e.id DESC LIMIT 6 OFFSET $4`, uid, start, end, page*5)
	if err != nil {
		return r, err
	}
	n := 0
	for rows.Next() {
		var id, amount, ver int64
		var desc, cat string
		var date time.Time
		if err = rows.Scan(&id, &desc, &amount, &cat, &date, &ver); err != nil {
			rows.Close()
			return r, err
		}
		n++
		if n > 5 {
			continue
		}
		if kind != "month" {
			r.Text += fmt.Sprintf("\n\n#%d %s · %s\n%s · %s", id, desc, parser.FormatAmount(amount), category.Label(cat), date.Format("2006-01-02"))
		}
		if kind == "recent" {
			d := expenseDraft{ID: id, Version: ver}
			r.Buttons = append(r.Buttons, []user.Button{eb(fmt.Sprintf("✏️ Edit #%d", id), "revise", d, ""), eb(fmt.Sprintf("🗑️ Delete #%d", id), "delete", d, "")})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	// Category breakdown has independent pages using the same index; both sections
	// remain bounded even with hundreds of custom categories and expenses.
	more := n > 5
	if kind != "recent" {
		rows, err = tx.Query(ctx, `SELECT c.display_name,sum(e.amount_minor)::text FROM expenses e JOIN categories c ON (c.user_id,c.id)=(e.user_id,e.category_id) WHERE e.user_id=$1 AND e.expense_date BETWEEN $2::date AND $3::date AND e.deleted_at IS NULL GROUP BY c.id,c.display_name ORDER BY sum(e.amount_minor) DESC,c.id LIMIT 6 OFFSET $4`, uid, start, end, page*5)
		if err != nil {
			return r, err
		}
		n = 0
		lines := []string{}
		for rows.Next() {
			var cat, sum string
			if err = rows.Scan(&cat, &sum); err != nil {
				rows.Close()
				return r, err
			}
			n++
			if n <= 5 {
				lines = append(lines, category.Label(cat)+": "+formatAggregate(sum))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return r, err
		}
		if len(lines) > 0 {
			r.Text += "\n\n🏷️ By category:\n" + strings.Join(lines, "\n")
		}
		if kind == "month" {
			more = n > 5
		} else {
			more = more || n > 5
		}
	}
	nav := []user.Button{}
	if page > 0 {
		nav = append(nav, user.Button{Text: "⬅️ Previous", Data: fmt.Sprintf("e:%s:%d:0", kind, page-1)})
	}
	if more {
		nav = append(nav, user.Button{Text: "Next ➡️", Data: fmt.Sprintf("e:%s:%d:0", kind, page+1)})
	}
	if len(nav) > 0 {
		r.Buttons = append(r.Buttons, nav)
	}
	return r, nil
}
