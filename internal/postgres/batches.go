package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"telegram-expense-tracker/internal/category"
	"telegram-expense-tracker/internal/parser"
	"telegram-expense-tracker/internal/user"
)

type expenseBatch struct {
	ID, Version   int64
	Status, Stage string
	Index         int
	Date          *time.Time
	Expired       bool
	Items         []expenseDraft
}

func bb(label, action string, b expenseBatch, arg string) user.Button {
	return user.Button{Text: label, Data: fmt.Sprintf("b:%s:%d:%d:%s", action, b.ID, b.Version, arg)}
}
func loadBatch(ctx context.Context, tx pgx.Tx, uid, id int64) (expenseBatch, error) {
	var b expenseBatch
	err := tx.QueryRow(ctx, `SELECT id,version,status,stage,item_index,expense_date,expires_at<=now() FROM expense_batches WHERE user_id=$1 AND id=$2 FOR UPDATE`, uid, id).Scan(&b.ID, &b.Version, &b.Status, &b.Stage, &b.Index, &b.Date, &b.Expired)
	if err != nil {
		return b, err
	}
	rows, err := tx.Query(ctx, `SELECT draft_id FROM expense_batch_items WHERE user_id=$1 AND batch_id=$2 ORDER BY position`, uid, id)
	if err != nil {
		return b, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return b, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	for _, id := range ids {
		d, e := loadDraft(ctx, tx, uid, id)
		if e != nil {
			return b, e
		}
		b.Items = append(b.Items, d)
	}
	return b, nil
}
func persistBatch(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch) error {
	b.Version++
	_, err := tx.Exec(ctx, `UPDATE expense_batches SET version=$3,stage=$4,item_index=$5,expense_date=$6,status=$7 WHERE user_id=$1 AND id=$2`, uid, b.ID, b.Version, b.Stage, b.Index, b.Date, b.Status)
	return err
}
func closeBatch(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch, status string) error {
	b.Status = status
	if err := persistBatch(ctx, tx, uid, b); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE expense_drafts d SET status=$3,version=version+1 FROM expense_batch_items i WHERE i.batch_id=$2 AND i.user_id=$1 AND d.id=i.draft_id AND d.status='pending'`, uid, b.ID, status)
	return err
}

// handleBatch intercepts only a batch flow or a multiline entry. Intake owns the transaction.
func handleBatch(ctx context.Context, tx pgx.Tx, uid int64, m user.Message, timezone, today string) (user.Reply, bool, error) {
	var id int64
	isCallback := strings.HasPrefix(m.Callback, "b:")
	var parts []string
	if isCallback {
		parts = strings.Split(m.Callback, ":")
		if len(parts) != 5 {
			return reply("🤔 Invalid batch button."), true, nil
		}
		var err error
		id, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return reply("🤔 Invalid batch button."), true, nil
		}
	} else {
		err := tx.QueryRow(ctx, `SELECT id FROM expense_batches WHERE user_id=$1 AND status='pending'`, uid).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return user.Reply{}, true, err
		}
	}
	if id != 0 {
		b, err := loadBatch(ctx, tx, uid, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return reply("🔒 That batch is unavailable."), true, nil
		}
		if err != nil {
			return user.Reply{}, true, err
		}
		if b.Status != "pending" {
			return reply("👌 That batch is already " + b.Status + ". No expenses were added."), true, nil
		}
		if b.Expired {
			err = closeBatch(ctx, tx, uid, &b, "expired")
			return reply("⌛ That batch expired. Send the expense lines again."), true, err
		}
		if isCallback {
			v, e := strconv.ParseInt(parts[3], 10, 64)
			if e != nil || v != b.Version {
				return reply("🔄 This button is outdated. Send /bulk to resume the batch."), true, nil
			}
			r, e := batchCallback(ctx, tx, uid, &b, parts[1], parts[4], m.UpdateID, timezone, today)
			return r, true, e
		}
		text := strings.TrimSpace(m.Text)
		if text == "/cancel" {
			err = closeBatch(ctx, tx, uid, &b, "cancelled")
			return reply("👌 Batch cancelled. Nothing was saved."), true, err
		}
		if text == "/bulk" {
			r, e := batchView(ctx, tx, uid, &b)
			return r, true, e
		}
		if m.Callback != "" || strings.HasPrefix(text, "/") {
			return user.Reply{Text: "🧾 Finish your batch first, or use /cancel.", Buttons: [][]user.Button{{bb("Resume batch", "resume", b, "")}}}, true, nil
		}
		r, e := batchText(ctx, tx, uid, &b, text)
		return r, true, e
	}
	if isCallback {
		return reply("🔒 That batch is unavailable."), true, nil
	}
	if strings.TrimSpace(m.Text) == "/bulk" {
		return reply("🧾 Send 2–20 expenses in one message, one per line:\nitem a 20k\nitem b 35k\n\nI'll ask for missing categories, then one date for the whole batch."), true, nil
	}
	if m.Callback != "" || !strings.Contains(strings.TrimSpace(m.Text), "\n") {
		return user.Reply{}, false, nil
	}
	entries, err := parser.ParseBatch(m.Text)
	if err != nil {
		return reply("🤔 " + err.Error() + ". Correct and resend the whole message; nothing was saved."), true, nil
	}
	var existing int64
	err = tx.QueryRow(ctx, `SELECT id FROM expense_drafts WHERE user_id=$1 AND status='pending' AND expires_at>now() ORDER BY id DESC LIMIT 1`, uid).Scan(&existing)
	if err == nil {
		d, e := loadDraft(ctx, tx, uid, existing)
		if e != nil {
			return user.Reply{}, true, e
		}
		return user.Reply{Text: "🧾 Finish or cancel your existing draft, then resend the batch.", Buttons: [][]user.Button{{eb("Resume draft", "preview", d, ""), eb("Cancel draft", "cancel", d, "")}}}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return user.Reply{}, true, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO expense_batches(user_id) VALUES($1) RETURNING id`, uid).Scan(&id); err != nil {
		return user.Reply{}, true, err
	}
	for i, e := range entries {
		cat, ver, err := mapping(ctx, tx, uid, e.Key)
		if err != nil {
			return user.Reply{}, true, err
		}
		var did int64
		if err = tx.QueryRow(ctx, `INSERT INTO expense_drafts(user_id,description,description_key,amount_minor,category_id,expense_date,mapping_version) VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7) RETURNING id`, uid, e.Description, e.Key, e.AmountMinor, cat, today, ver).Scan(&did); err != nil {
			return user.Reply{}, true, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO expense_batch_items(batch_id,user_id,position,draft_id) VALUES($1,$2,$3,$4)`, id, uid, i, did); err != nil {
			return user.Reply{}, true, err
		}
	}
	b, err := loadBatch(ctx, tx, uid, id)
	if err != nil {
		return user.Reply{}, true, err
	}
	r, err := advanceBatch(ctx, tx, uid, &b)
	return r, true, err
}
func advanceBatch(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch) (user.Reply, error) {
	b.Stage = "review"
	for i, d := range b.Items {
		if d.Category == 0 {
			b.Stage = "categories"
			b.Index = i
			break
		}
	}
	if b.Stage == "review" && b.Date == nil {
		b.Stage = "date"
	}
	if err := persistBatch(ctx, tx, uid, b); err != nil {
		return user.Reply{}, err
	}
	return batchView(ctx, tx, uid, b)
}
func batchView(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch) (user.Reply, error) {
	switch b.Stage {
	case "categories":
		return batchPicker(ctx, tx, uid, *b, 0)
	case "category":
		return reply("✨ Send the new category name. /cancel discards the batch."), nil
	case "date":
		return user.Reply{Text: "📅 What date are these expenses for?\nChoose below or send YYYY-MM-DD. This date applies to every item.", Buttons: [][]user.Button{{bb("Today", "date", *b, "today"), bb("Yesterday", "date", *b, "yesterday")}, {bb("❌ Cancel", "cancel", *b, "")}}}, nil
	case "description":
		return reply("✏️ Send the new description without the amount."), nil
	case "amount":
		return reply("💰 Send the new amount, like 25k."), nil
	case "edit":
		r := reply("✏️ Choose an item to edit.")
		for i, d := range b.Items {
			r.Buttons = append(r.Buttons, []user.Button{bb(fmt.Sprintf("%d. %s", i+1, shortBatchText(d.Description, 30)), "item", *b, strconv.Itoa(i))})
		}
		r.Buttons = append(r.Buttons, []user.Button{bb("📅 Change date", "date", *b, "custom"), bb("Back", "review", *b, "")})
		return r, nil
	}
	// Five items per review page preserves full descriptions without exceeding Telegram's limit.
	page := b.Index / 5
	start := page * 5
	end := min(start+5, len(b.Items))
	var total int64
	for _, d := range b.Items {
		if d.Amount > math.MaxInt64-total {
			return reply("🤔 Batch total is too large. Edit an amount."), nil
		}
		total += d.Amount
	}
	r := reply(fmt.Sprintf("🧾 Review %d expenses · page %d/%d\n📅 %s\n\n", len(b.Items), page+1, (len(b.Items)+4)/5, b.Date.Format("2006-01-02")))
	for i := start; i < end; i++ {
		d := b.Items[i]
		var name string
		if err := tx.QueryRow(ctx, `SELECT display_name FROM categories WHERE user_id=$1 AND id=$2`, uid, d.Category).Scan(&name); err != nil {
			return user.Reply{}, err
		}
		r.Text += fmt.Sprintf("%d. %s · %s\n   %s\n", i+1, d.Description, parser.FormatAmount(d.Amount), category.Label(name))
	}
	r.Text += "\n💰 Batch total: " + parser.FormatAmount(total)
	var nav []user.Button
	if page > 0 {
		nav = append(nav, bb("⬅️ Previous", "page", *b, strconv.Itoa(page-1)))
	}
	if end < len(b.Items) {
		nav = append(nav, bb("Next ➡️", "page", *b, strconv.Itoa(page+1)))
	}
	if len(nav) > 0 {
		r.Buttons = append(r.Buttons, nav)
	}
	r.Buttons = append(r.Buttons, []user.Button{bb("✅ Save all", "save", *b, ""), bb("✏️ Edit", "edit", *b, ""), bb("❌ Cancel", "cancel", *b, "")})
	return r, nil
}
func shortBatchText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func batchPicker(ctx context.Context, tx pgx.Tx, uid int64, b expenseBatch, page int) (user.Reply, error) {
	r, err := picker(ctx, tx, uid, b.Items[b.Index], page)
	if err != nil {
		return r, err
	}
	r.Text = fmt.Sprintf("🧾 Item %d/%d\n", b.Index+1, len(b.Items)) + r.Text
	for i, row := range r.Buttons {
		for j, button := range row {
			p := strings.Split(button.Data, ":")
			r.Buttons[i][j] = bb(button.Text, p[1], b, p[4])
		}
	}
	return r, nil
}
func batchChooseCategory(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch, cat int64) (user.Reply, error) {
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories c JOIN users u ON u.id=c.user_id WHERE c.user_id=$1 AND c.id=$2 AND (c.normalized_name<>'smoking & vaping' OR u.is_smoker IS TRUE))`, uid, cat).Scan(&allowed); err != nil {
		return user.Reply{}, err
	}
	if !allowed {
		return reply("🔒 Category unavailable."), nil
	}
	key := b.Items[b.Index].Key
	for i := range b.Items {
		d := &b.Items[i]
		if d.Key != key {
			continue
		}
		d.Category = cat
		d.Learn = d.MappingVersion == 0
		if err := persistDraft(ctx, tx, uid, d); err != nil {
			return user.Reply{}, err
		}
	}
	return advanceBatch(ctx, tx, uid, b)
}
func batchText(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch, text string) (user.Reply, error) {
	switch b.Stage {
	case "date":
		date, err := time.Parse("2006-01-02", text)
		if err != nil || date.Year() < 1900 {
			return reply("📅 Use YYYY-MM-DD, year 1900 or later."), nil
		}
		b.Date = &date
		return advanceBatch(ctx, tx, uid, b)
	case "category":
		name, err := user.NormalizeName(text)
		if err != nil {
			return reply("🤔 Use a category name with 1–40 characters on one line."), nil
		}
		if parser.Normalize(name) == "smoking & vaping" {
			var allowed bool
			if err = tx.QueryRow(ctx, `SELECT is_smoker IS TRUE FROM users WHERE id=$1`, uid).Scan(&allowed); err != nil {
				return user.Reply{}, err
			}
			if !allowed {
				return reply("🚬 Smoking & vaping is available only when your smoking preference is Yes."), nil
			}
		}
		var cat int64
		if err = tx.QueryRow(ctx, `INSERT INTO categories(user_id,display_name,normalized_name) VALUES($1,$2,$3) ON CONFLICT(user_id,normalized_name) DO UPDATE SET normalized_name=excluded.normalized_name RETURNING id`, uid, name, parser.Normalize(name)).Scan(&cat); err != nil {
			return user.Reply{}, err
		}
		return batchChooseCategory(ctx, tx, uid, b, cat)
	case "description", "amount":
		d := &b.Items[b.Index]
		if b.Stage == "amount" {
			amount, err := parser.ParseAmount(text)
			if err != nil {
				return reply("🤔 Send a positive amount like 20k."), nil
			}
			var total int64
			for i, item := range b.Items {
				a := item.Amount
				if i == b.Index {
					a = amount
				}
				if a > math.MaxInt64-total {
					return reply("🤔 Batch total is too large. Use a smaller amount."), nil
				}
				total += a
			}
			d.Amount = amount
		} else {
			if strings.ContainsAny(text, "\r\n") {
				return reply("🤔 Send the description on one line."), nil
			}
			e, err := parser.Parse(text + " 1")
			if err != nil {
				return reply("🤔 Send a valid description, up to 200 characters."), nil
			}
			d.Description = e.Description
			d.Key = e.Key
			d.Learn = false
			d.Category, d.MappingVersion, err = mapping(ctx, tx, uid, d.Key)
			if err != nil {
				return user.Reply{}, err
			}
			for i, other := range b.Items {
				if i != b.Index && other.Key == d.Key && other.Category != 0 {
					d.Category = other.Category
					d.Learn = other.Learn
					d.MappingVersion = other.MappingVersion
					break
				}
			}
		}
		if err := persistDraft(ctx, tx, uid, d); err != nil {
			return user.Reply{}, err
		}
		return advanceBatch(ctx, tx, uid, b)
	}
	r, err := batchView(ctx, tx, uid, b)
	r.Text = "💡 Use the buttons below, or /cancel to discard the batch.\n\n" + r.Text
	return r, err
}
func batchCallback(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch, action, arg string, updateID int64, timezone, today string) (user.Reply, error) {
	switch action {
	case "cancel":
		err := closeBatch(ctx, tx, uid, b, "cancelled")
		return reply("👌 Batch cancelled. Nothing was saved."), err
	case "resume":
		return batchView(ctx, tx, uid, b)
	case "pick":
		if b.Stage != "categories" {
			return reply("🔄 Send /bulk to resume."), nil
		}
		page, err := strconv.Atoi(arg)
		if err != nil || page < 0 || page > 100000 {
			return reply("🤔 Invalid page."), nil
		}
		return batchPicker(ctx, tx, uid, *b, page)
	case "cat":
		if b.Stage != "categories" {
			return reply("🔄 Send /bulk to resume."), nil
		}
		cat, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return reply("🤔 Invalid category."), nil
		}
		return batchChooseCategory(ctx, tx, uid, b, cat)
	case "field":
		if arg == "category" && b.Stage == "categories" {
			b.Stage = "category"
		} else if b.Stage == "edit" && (arg == "description" || arg == "amount" || arg == "categories") {
			b.Stage = arg
		} else {
			return reply("🤔 Invalid field."), nil
		}
	case "date":
		if b.Stage != "date" && b.Stage != "edit" {
			return reply("🔄 Send /bulk to resume."), nil
		}
		b.Stage = "date"
		if arg == "today" || arg == "yesterday" {
			d, _ := time.Parse("2006-01-02", today)
			if arg == "yesterday" {
				d = d.AddDate(0, 0, -1)
			}
			b.Date = &d
			return advanceBatch(ctx, tx, uid, b)
		}
	case "edit":
		if b.Stage != "review" {
			return reply("🔄 Send /bulk to resume."), nil
		}
		b.Stage = "edit"
	case "item":
		if b.Stage != "edit" {
			return reply("🔄 Send /bulk to resume."), nil
		}
		i, err := strconv.Atoi(arg)
		if err != nil || i < 0 || i >= len(b.Items) {
			return reply("🤔 Invalid item."), nil
		}
		b.Index = i
		if err = persistBatch(ctx, tx, uid, b); err != nil {
			return user.Reply{}, err
		}
		return user.Reply{Text: "✏️ Edit “" + b.Items[i].Description + "”\nCategory changes apply to matching descriptions in this batch. Existing remembered categories stay unchanged.", Buttons: [][]user.Button{{bb("Description", "field", *b, "description"), bb("Amount", "field", *b, "amount"), bb("Category", "field", *b, "categories")}, {bb("Back", "review", *b, "")}}}, nil
	case "review":
		return advanceBatch(ctx, tx, uid, b)
	case "page":
		if b.Stage != "review" {
			return reply("🔄 Send /bulk to resume."), nil
		}
		p, err := strconv.Atoi(arg)
		if err != nil || p < 0 || p >= (len(b.Items)+4)/5 {
			return reply("🤔 Invalid page."), nil
		}
		b.Index = p * 5
	case "save":
		if b.Stage != "review" || b.Date == nil {
			return reply("📅 Finish categorization and choose a date first."), nil
		}
		return saveBatch(ctx, tx, uid, b, updateID, timezone, today)
	default:
		return reply("🤔 Invalid batch button."), nil
	}
	if err := persistBatch(ctx, tx, uid, b); err != nil {
		return user.Reply{}, err
	}
	return batchView(ctx, tx, uid, b)
}
func saveBatch(ctx context.Context, tx pgx.Tx, uid int64, b *expenseBatch, updateID int64, timezone, today string) (user.Reply, error) {
	// Preflight before the first insert so validation never commits a partial batch.
	for i := range b.Items {
		d := &b.Items[i]
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM categories c JOIN users u ON u.id=c.user_id WHERE c.user_id=$1 AND c.id=$2 AND u.access_status='allowed' AND (c.normalized_name<>'smoking & vaping' OR u.is_smoker IS TRUE))`, uid, d.Category).Scan(&allowed); err != nil {
			return user.Reply{}, err
		}
		if !allowed {
			d.Category = 0
			if err := persistDraft(ctx, tx, uid, d); err != nil {
				return user.Reply{}, err
			}
			return advanceBatch(ctx, tx, uid, b)
		}
	}
	// Process each category's largest item first, before its smaller siblings exist.
	// This preserves the pre-batch baseline on cache initialization and emits one record per category.
	order := make([]int, len(b.Items))
	for i := range order {
		order[i] = i
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if b.Items[order[j]].Amount > b.Items[order[i]].Amount {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	var total int64
	var alerts []string
	seen := map[int64]bool{}
	for _, i := range order {
		d := b.Items[i]
		d.Date = *b.Date
		if err := persistDraft(ctx, tx, uid, &d); err != nil {
			return user.Reply{}, err
		}
		// Learn only previously unknown descriptions. Never overwrite a newer mapping.
		_, ver, err := mapping(ctx, tx, uid, d.Key)
		if err != nil {
			return user.Reply{}, err
		}
		d.Learn = d.Learn && ver == 0
		d.MappingVersion = ver
		r, err := saveDraft(ctx, tx, uid, d, today)
		if err != nil {
			return user.Reply{}, err
		}
		if r.SavedExpenseID == 0 {
			return user.Reply{}, errors.New("batch save failed validation")
		}
		total += d.Amount
		if !seen[d.Category] {
			alert, err := weeklyRecord(ctx, tx, uid, r.SavedExpenseID, updateID, timezone, today)
			if err != nil {
				return user.Reply{}, err
			}
			if alert != "" {
				alerts = append(alerts, alert)
			}
			seen[d.Category] = true
		}
	}
	b.Status = "confirmed"
	if err := persistBatch(ctx, tx, uid, b); err != nil {
		return user.Reply{}, err
	}
	text := fmt.Sprintf("✅ Saved %d expenses for %s!\n💰 Total: %s", len(b.Items), b.Date.Format("2006-01-02"), parser.FormatAmount(total))
	if len(alerts) > 0 {
		text += "\n\n" + strings.Join(alerts, "\n\n")
	}
	return reply(text), nil
}
