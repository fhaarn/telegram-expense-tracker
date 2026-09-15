package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"telegram-expense-tracker/internal/category"
	"telegram-expense-tracker/internal/parser"
	"telegram-expense-tracker/internal/user"
)

type expenseDraft struct {
	ID, Version, Amount, Category, MappingVersion int64
	Description, Key, Status                      string
	Date                                          time.Time
	Learn                                         bool
	Target, TargetVersion, Result                 int64
	Expired                                       bool
}

func reply(text string) user.Reply { return user.Reply{Text: text} }
func eb(text, action string, d expenseDraft, arg string) user.Button {
	return user.Button{Text: text, Data: fmt.Sprintf("e:%s:%d:%d:%s", action, d.ID, d.Version, arg)}
}
func loadDraft(ctx context.Context, tx pgx.Tx, uid, id int64) (expenseDraft, error) {
	var d expenseDraft
	err := tx.QueryRow(ctx, `SELECT id,version,description,description_key,amount_minor,COALESCE(category_id,0),expense_date,status,learn,mapping_version,COALESCE(target_id,0),COALESCE(target_version,0),COALESCE(result_id,0),expires_at<=now() FROM expense_drafts WHERE user_id=$1 AND id=$2 FOR UPDATE`, uid, id).Scan(&d.ID, &d.Version, &d.Description, &d.Key, &d.Amount, &d.Category, &d.Date, &d.Status, &d.Learn, &d.MappingVersion, &d.Target, &d.TargetVersion, &d.Result, &d.Expired)
	return d, err
}
func mapping(ctx context.Context, tx pgx.Tx, uid int64, key string) (cat, version int64, err error) {
	err = tx.QueryRow(ctx, "SELECT m.category_id,m.version FROM category_mappings m JOIN categories c ON c.user_id=m.user_id AND c.id=m.category_id JOIN users u ON u.id=m.user_id WHERE m.user_id=$1 AND m.description_key=$2 AND (c.normalized_name <> 'smoking & vaping' OR u.is_smoker IS TRUE)", uid, key).Scan(&cat, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
func persistDraft(ctx context.Context, tx pgx.Tx, uid int64, d *expenseDraft) error {
	d.Version++
	_, err := tx.Exec(ctx, `UPDATE expense_drafts SET description=$3,description_key=$4,amount_minor=$5,category_id=NULLIF($6,0),expense_date=$7,version=$8,learn=$9,mapping_version=$10 WHERE user_id=$1 AND id=$2`, uid, d.ID, d.Description, d.Key, d.Amount, d.Category, d.Date, d.Version, d.Learn, d.MappingVersion)
	return err
}
func preview(ctx context.Context, tx pgx.Tx, uid int64, d expenseDraft) (user.Reply, error) {
	if d.Category == 0 {
		return picker(ctx, tx, uid, d, 0)
	}
	var cat string
	if err := tx.QueryRow(ctx, "SELECT display_name FROM categories WHERE user_id=$1 AND id=$2", uid, d.Category).Scan(&cat); err != nil {
		return user.Reply{}, err
	}
	return user.Reply{Text: fmt.Sprintf("🧾 %s\n%s · %s · %s", d.Description, parser.FormatAmount(d.Amount), category.Label(cat), d.Date.Format("2006-01-02")), Buttons: [][]user.Button{{eb("✅ Save", "save", d, ""), eb("✏️ Edit", "edit", d, ""), eb("❌ Cancel", "cancel", d, "")}}}, nil
}
func picker(ctx context.Context, tx pgx.Tx, uid int64, d expenseDraft, page int) (user.Reply, error) {
	if page < 0 || page > 100000 {
		page = 0
	}
	rows, err := tx.Query(ctx, "SELECT c.id,c.display_name FROM categories c JOIN users u ON u.id=c.user_id WHERE c.user_id=$1 AND (c.normalized_name <> 'smoking & vaping' OR u.is_smoker IS TRUE) ORDER BY CASE c.normalized_name WHEN 'food & drinks' THEN 1 WHEN 'food' THEN 1 WHEN 'coffee & drinks' THEN 2 WHEN 'smoking & vaping' THEN 3 WHEN 'transport' THEN 4 WHEN 'entertainment' THEN 5 WHEN 'shopping' THEN 6 WHEN 'groceries' THEN 7 WHEN 'bills' THEN 8 WHEN 'health' THEN 100 WHEN 'other' THEN 101 ELSE 9 END, c.id LIMIT 6 OFFSET $2", uid, page*5)
	if err != nil {
		return user.Reply{}, err
	}
	defer rows.Close()
	r := reply("🏷️ What category is “" + d.Description + "”?")
	n := 0
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return r, err
		}
		n++
		if n <= 5 {
			r.Buttons = append(r.Buttons, []user.Button{eb(category.Label(name), "cat", d, strconv.FormatInt(id, 10))})
		}
	}
	if err = rows.Err(); err != nil {
		return r, err
	}
	nav := []user.Button{}
	if page > 0 {
		nav = append(nav, eb("⬅️ Previous", "pick", d, strconv.Itoa(page-1)))
	}
	if n > 5 {
		nav = append(nav, eb("Next ➡️", "pick", d, strconv.Itoa(page+1)))
	}
	if len(nav) > 0 {
		r.Buttons = append(r.Buttons, nav)
	}
	r.Buttons = append(r.Buttons, []user.Button{eb("✨ New category", "field", d, "category"), eb("❌ Cancel", "cancel", d, "")})
	return r, nil
}

// HandleExpense runs inside the caller's transaction, after locking the owner row.
func HandleExpense(ctx context.Context, tx pgx.Tx, uid int64, name, timezone string, m user.Message) (user.Reply, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return user.Reply{}, err
	}
	today := time.Now().In(loc).Format("2006-01-02")
	text := strings.TrimSpace(m.Text)
	if m.Callback != "" {
		r, err := expenseCallback(ctx, tx, uid, m.Callback, today)
		if err != nil || r.SavedExpenseID == 0 {
			return r, err
		}
		alert, err := weeklyRecord(ctx, tx, uid, r.SavedExpenseID, m.UpdateID, timezone, today)
		if err != nil {
			return user.Reply{}, err
		}
		if alert != "" {
			r.Text += "\n\n" + alert
		}
		return r, nil
	}
	cmd := strings.SplitN(text, "@", 2)[0]
	if cmd == "/today" || cmd == "/month" || cmd == "/recent" {
		return expenseReport(ctx, tx, uid, strings.TrimPrefix(cmd, "/"), today, 0)
	}
	var did int64
	var field string
	err = tx.QueryRow(ctx, "SELECT draft_id,field FROM user_interactions WHERE user_id=$1 AND kind='expense'", uid).Scan(&did, &field)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return user.Reply{}, err
	}
	if err == nil {
		d, e := loadDraft(ctx, tx, uid, did)
		if e != nil {
			return user.Reply{}, e
		}
		if d.Status != "pending" || d.Expired {
			if d.Status == "pending" && d.Expired {
				if _, e = tx.Exec(ctx, "UPDATE expense_drafts SET status='expired',version=version+1 WHERE user_id=$1 AND id=$2", uid, d.ID); e != nil {
					return user.Reply{}, e
				}
			}
			_, e = tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1", uid)
			return reply("⌛ That draft is no longer active. Send a new expense."), e
		}
		if text == "/cancel" {
			_, e = tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1", uid)
			if e != nil {
				return user.Reply{}, e
			}
			if field == "category" {
				return picker(ctx, tx, uid, d, 0)
			}
			return preview(ctx, tx, uid, d)
		}
		if strings.HasPrefix(text, "/") {
			return reply("✏️ Finish this edit or use /cancel first."), nil
		}
		switch field {
		case "category":
			cname, e := user.NormalizeName(text)
			if e != nil {
				return reply("🤔 Use a category name with 1–40 characters on one line."), nil
			}
			if parser.Normalize(cname) == "smoking & vaping" {
				var allowed bool
				if e = tx.QueryRow(ctx, "SELECT is_smoker IS TRUE FROM users WHERE id=$1", uid).Scan(&allowed); e != nil {
					return user.Reply{}, e
				}
				if !allowed {
					return reply("🚬 Smoking & vaping is available only when your smoking preference is Yes."), nil
				}
			}
			e = tx.QueryRow(ctx, `INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,$2,$3) ON CONFLICT(user_id,normalized_name) DO UPDATE SET normalized_name=excluded.normalized_name RETURNING id`, uid, cname, parser.Normalize(cname)).Scan(&d.Category)
			if e != nil {
				return user.Reply{}, e
			}
			d.Learn = d.MappingVersion == 0
		case "amount":
			amount, e := parser.ParseAmount(text)
			if e != nil {
				return reply("🤔 Send a positive amount like 20k or 20000."), nil
			}
			d.Amount = amount
		case "description":
			entry, e := parser.Parse(text + " 1")
			if e != nil {
				return reply("🤔 Please send a valid description."), nil
			}
			d.Description = entry.Description
			d.Key = entry.Key
			d.Category, d.MappingVersion, e = mapping(ctx, tx, uid, d.Key)
			if e != nil {
				return user.Reply{}, e
			}
			d.Learn = false
		case "date":
			date, e := time.Parse("2006-01-02", text)
			if e != nil || date.Year() < 1900 {
				return reply("📅 Use a date like 2026-09-14 (year 1900 or later)."), nil
			}
			d.Date = date
		default:
			return reply("🤔 Cancel this interaction and try again."), nil
		}
		if err = persistDraft(ctx, tx, uid, &d); err != nil {
			return user.Reply{}, err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1", uid); err != nil {
			return user.Reply{}, err
		}
		if field == "category" && d.MappingVersion > 0 {
			return scope(d), nil
		}
		return preview(ctx, tx, uid, d)
	}
	if text == "/cancel" {
		return reply("👌 No active text entry. Use the Cancel button on a draft to discard it."), nil
	}
	if m.Image {
		return reply("🧾 Screenshot reading is coming in phase 2."), nil
	}
	if strings.HasPrefix(text, "/") {
		return reply("🤔 Try /today, /month, /recent, or send an expense like bensin 100k."), nil
	}
	entry, err := parser.Parse(text)
	if err != nil {
		return reply("🤔 Put the amount last, like Tahu Telor 20k or Nice 8 Ball Cafe 100k."), nil
	}
	cat, ver, err := mapping(ctx, tx, uid, entry.Key)
	if err != nil {
		return user.Reply{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date,mapping_version) VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7) RETURNING id`, uid, entry.Description, entry.Key, entry.AmountMinor, cat, today, ver).Scan(&id)
	if err != nil {
		return user.Reply{}, err
	}
	d, err := loadDraft(ctx, tx, uid, id)
	if err != nil {
		return user.Reply{}, err
	}
	return preview(ctx, tx, uid, d)
}
func scope(d expenseDraft) user.Reply {
	return user.Reply{Text: "🏷️ Apply this category just here, or remember it for future entries?", Buttons: [][]user.Button{{eb("This expense only", "scope", d, "once")}, {eb("Remember for this description", "scope", d, "remember")}}}
}
func expenseCallback(ctx context.Context, tx pgx.Tx, uid int64, data, today string) (user.Reply, error) {
	parts := strings.Split(data, ":")
	if len(parts) < 4 || parts[0] != "e" {
		return reply("🤔 Invalid button."), nil
	}
	action := parts[1]
	id, e1 := strconv.ParseInt(parts[2], 10, 64)
	version, e2 := strconv.ParseInt(parts[3], 10, 64)
	arg := ""
	if len(parts) > 4 {
		arg = parts[4]
	}
	if e1 != nil || e2 != nil {
		return reply("🤔 Invalid button."), nil
	}
	if action == "today" || action == "month" || action == "recent" {
		if id < 0 || id > 100000 {
			return reply("🤔 Invalid page."), nil
		}
		return expenseReport(ctx, tx, uid, action, today, int(id))
	}
	if action == "revise" || action == "delete" || action == "remove" {
		return savedCallback(ctx, tx, uid, action, id, version, today)
	}
	d, err := loadDraft(ctx, tx, uid, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return reply("🔒 That draft is unavailable."), nil
	}
	if err != nil {
		return user.Reply{}, err
	}
	if d.Status == "confirmed" && action == "save" {
		return reply(fmt.Sprintf("✅ Already saved as expense #%d.", d.Result)), nil
	}
	if d.Status != "pending" {
		return reply("⌛ That draft is no longer active."), nil
	}
	if d.Expired {
		_, err = tx.Exec(ctx, "UPDATE expense_drafts SET status='expired' WHERE user_id=$1 AND id=$2", uid, id)
		return reply("⌛ That draft expired. Send the expense again."), err
	}
	if version != d.Version {
		return reply("🔄 This button is outdated. Use the latest preview."), nil
	}
	switch action {
	case "save":
		return saveDraft(ctx, tx, uid, d, today)
	case "cancel":
		_, err = tx.Exec(ctx, "UPDATE expense_drafts SET status='cancelled',version=version+1 WHERE user_id=$1 AND id=$2", uid, id)
		if err == nil {
			_, err = tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1 AND draft_id=$2", uid, id)
		}
		return reply("👌 Expense cancelled."), err
	case "edit":
		return user.Reply{Text: "✏️ What would you like to edit?", Buttons: [][]user.Button{{eb("Description", "field", d, "description"), eb("Amount", "field", d, "amount")}, {eb("Category", "pick", d, "0"), eb("Date", "field", d, "date")}, {eb("Back", "preview", d, "")}}}, nil
	case "preview":
		return preview(ctx, tx, uid, d)
	case "pick":
		page, _ := strconv.Atoi(arg)
		return picker(ctx, tx, uid, d, page)
	case "field":
		prompts := map[string]string{"description": "✏️ Send the new description (without the amount).", "amount": "💰 Send the new amount, like 25k.", "date": "📅 Send the date as YYYY-MM-DD.", "category": "✨ What’s your new category called?"}
		prompt, ok := prompts[arg]
		if !ok {
			return reply("🤔 Invalid field."), nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO user_interactions(user_id,kind,draft_id,field) VALUES($1,'expense',$2,$3) ON CONFLICT(user_id) DO UPDATE SET kind='expense',draft_id=excluded.draft_id,field=excluded.field,created_at=now()`, uid, id, arg)
		return reply(fmt.Sprintf("🧾 Draft #%d · %s\n%s\nUse /cancel to go back.", d.ID, d.Description, prompt)), err
	case "cat":
		cat, e := strconv.ParseInt(arg, 10, 64)
		if e != nil {
			return reply("🤔 Invalid category."), nil
		}
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM categories c JOIN users u ON u.id=c.user_id WHERE c.user_id=$1 AND c.id=$2 AND (c.normalized_name <> 'smoking & vaping' OR u.is_smoker IS TRUE))", uid, cat).Scan(&exists); err != nil {
			return user.Reply{}, err
		}
		if !exists {
			return reply("🔒 Category unavailable."), nil
		}
		if _, err = tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1 AND draft_id=$2", uid, d.ID); err != nil {
			return user.Reply{}, err
		}
		d.Category = cat
		d.Learn = d.MappingVersion == 0
		if err = persistDraft(ctx, tx, uid, &d); err != nil {
			return user.Reply{}, err
		}
		if d.MappingVersion > 0 {
			return scope(d), nil
		}
		return preview(ctx, tx, uid, d)
	case "scope":
		if arg != "once" && arg != "remember" {
			return reply("🤔 Invalid choice."), nil
		}
		d.Learn = arg == "remember"
		if d.Learn {
			_, d.MappingVersion, err = mapping(ctx, tx, uid, d.Key)
			if err != nil {
				return user.Reply{}, err
			}
		}
		if err = persistDraft(ctx, tx, uid, &d); err != nil {
			return user.Reply{}, err
		}
		return preview(ctx, tx, uid, d)
	}
	return reply("🤔 Invalid button."), nil
}
func saveDraft(ctx context.Context, tx pgx.Tx, uid int64, d expenseDraft, today string) (user.Reply, error) {
	if d.Category == 0 {
		return picker(ctx, tx, uid, d, 0)
	}
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories c JOIN users u ON u.id=c.user_id WHERE c.user_id=$1 AND c.id=$2 AND (c.normalized_name <> 'smoking & vaping' OR u.is_smoker IS TRUE))`, uid, d.Category).Scan(&allowed); err != nil {
		return user.Reply{}, err
	}
	if !allowed {
		return picker(ctx, tx, uid, d, 0)
	}
	if d.Learn {
		_, ver, err := mapping(ctx, tx, uid, d.Key)
		if err != nil {
			return user.Reply{}, err
		}
		if ver != d.MappingVersion {
			d.MappingVersion = ver
			d.Learn = false
			if err = persistDraft(ctx, tx, uid, &d); err != nil {
				return user.Reply{}, err
			}
			r := scope(d)
			r.Text = "🔄 The remembered category changed since this draft. Choose how to save this category."
			return r, nil
		}
	}
	var result int64
	if d.Target > 0 {
		err := tx.QueryRow(ctx, `UPDATE expenses SET description=$3,amount_minor=$4,category_id=$5,expense_date=$6,version=version+1,updated_at=now() WHERE user_id=$1 AND id=$2 AND version=$7 AND deleted_at IS NULL RETURNING id`, uid, d.Target, d.Description, d.Amount, d.Category, d.Date, d.TargetVersion).Scan(&result)
		if errors.Is(err, pgx.ErrNoRows) {
			return reply("🔄 This expense changed or was deleted. Open /recent and edit its latest version."), nil
		}
		if err != nil {
			return user.Reply{}, err
		}
	} else {
		err := tx.QueryRow(ctx, `INSERT INTO expenses(user_id,originating_draft_id,description,amount_minor,category_id,expense_date) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, uid, d.ID, d.Description, d.Amount, d.Category, d.Date).Scan(&result)
		if err != nil {
			return user.Reply{}, err
		}
	}
	if d.Learn {
		_, err := tx.Exec(ctx, `INSERT INTO category_mappings(user_id,description_key,category_id) VALUES($1,$2,$3) ON CONFLICT(user_id,description_key) DO UPDATE SET category_id=excluded.category_id,version=category_mappings.version+1`, uid, d.Key, d.Category)
		if err != nil {
			return user.Reply{}, err
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE expense_drafts SET status='confirmed',result_id=$3,version=version+1 WHERE user_id=$1 AND id=$2", uid, d.ID, result); err != nil {
		return user.Reply{}, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM user_interactions WHERE user_id=$1 AND draft_id=$2", uid, d.ID); err != nil {
		return user.Reply{}, err
	}
	total, err := reportTotal(ctx, tx, uid, today, today)
	if err != nil {
		return user.Reply{}, err
	}
	if d.Target > 0 {
		if err := refreshWeeklyRecords(ctx, tx, uid); err != nil {
			return user.Reply{}, err
		}
	}
	r := reply(fmt.Sprintf("✅ Saved expense #%d!\nToday's spending: %s", result, total))
	if d.Target == 0 {
		r.SavedExpenseID = result
	}
	return r, nil
}
func savedCallback(ctx context.Context, tx pgx.Tx, uid int64, action string, id, version int64, today string) (user.Reply, error) {
	var desc string
	var amount, cat, v int64
	var date time.Time
	err := tx.QueryRow(ctx, "SELECT description,amount_minor,category_id,expense_date,version FROM expenses WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE", uid, id).Scan(&desc, &amount, &cat, &date, &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return reply("🔒 Expense unavailable or already deleted."), nil
	}
	if err != nil {
		return user.Reply{}, err
	}
	if v != version {
		return reply("🔄 That expense changed. Open /recent for its latest version."), nil
	}
	d := expenseDraft{ID: id, Version: v}
	if action == "delete" {
		return user.Reply{Text: fmt.Sprintf("🗑️ Delete %s · %s?", desc, parser.FormatAmount(amount)), Buttons: [][]user.Button{{eb("Yes, delete", "remove", d, ""), {Text: "Keep it", Data: "e:recent:0:0"}}}}, nil
	}
	if action == "remove" {
		_, err = tx.Exec(ctx, "UPDATE expenses SET deleted_at=now(),version=version+1,updated_at=now() WHERE user_id=$1 AND id=$2", uid, id)
		if err == nil {
			err = refreshWeeklyRecords(ctx, tx, uid)
		}
		return reply("🗑️ Expense deleted."), err
	}
	_, mv, err := mapping(ctx, tx, uid, parser.Normalize(desc))
	if err != nil {
		return user.Reply{}, err
	}
	var draftID int64
	err = tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date,mapping_version,target_id,target_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, uid, desc, parser.Normalize(desc), amount, cat, date, mv, id, v).Scan(&draftID)
	if err != nil {
		return user.Reply{}, err
	}
	d, err = loadDraft(ctx, tx, uid, draftID)
	if err != nil {
		return user.Reply{}, err
	}
	return preview(ctx, tx, uid, d)
}
