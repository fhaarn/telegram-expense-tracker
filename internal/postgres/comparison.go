package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"telegram-expense-tracker/internal/user"
)

func inviteHash(token string) (string, error) {
	if len(token) != 32 {
		return "", errors.New("invalid invitation")
	}
	b, err := hex.DecodeString(token)
	if err != nil || hex.EncodeToString(b) != token {
		return "", errors.New("invalid invitation")
	}
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:]), nil
}

// SavePendingInvite retains only a digest, including across name onboarding.
func SavePendingInvite(ctx context.Context, tx pgx.Tx, userID int64, token string) error {
	h, err := inviteHash(token)
	if err != nil {
		h = ""
	}
	_, err = tx.Exec(ctx, `UPDATE users SET pending_comparison_hash=NULLIF($2,'') WHERE id=$1`, userID, h)
	return err
}
func PendingInviteReply(ctx context.Context, tx pgx.Tx, userID int64) (user.Reply, error) {
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT pending_comparison_hash IS NOT NULL FROM users WHERE id=$1`, userID).Scan(&pending); err != nil {
		return user.Reply{}, err
	}
	if !pending {
		return user.Reply{}, nil
	}
	var id, creator int64
	var name string
	err := tx.QueryRow(ctx, `SELECT i.id,i.creator_id,u.display_name FROM users me JOIN comparison_invites i ON i.token_hash=me.pending_comparison_hash JOIN users u ON u.id=i.creator_id WHERE me.id=$1 AND i.state='pending' AND i.expires_at>now() AND u.access_status='allowed'`, userID).Scan(&id, &creator, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return user.Reply{Text: "🤔 This invite is invalid, expired, or no longer available."}, nil
	}
	if err != nil {
		return user.Reply{}, err
	}
	if creator == userID {
		return user.Reply{Text: "🤔 You can’t connect with yourself. Share your invite with someone else."}, nil
	}
	var paired bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM active_pair_members WHERE user_id IN ($1,$2))`, userID, creator).Scan(&paired)
	if err != nil {
		return user.Reply{}, err
	}
	if paired {
		return user.Reply{Text: "🤝 One of you already has a partner. Disconnect before connecting again."}, nil
	}
	return user.Reply{Text: fmt.Sprintf("🤝 Connect with %s?\nYou’ll share spending totals, expense counts, and weekly category record alerts (category and amount) from the connection date onward.", name), Buttons: [][]user.Button{{{Text: "🤝 Connect", Data: fmt.Sprintf("c:accept:%d", id)}, {Text: "❌ Cancel", Data: "c:cancel"}}}}, nil
}

// HandleComparison runs inside the intake transaction. Intake acquires the shared
// comparison advisory lock before locking users, so cross-user mutations cannot deadlock.
func HandleComparison(ctx context.Context, tx pgx.Tx, userID int64, name, timezone, botUsername string, m user.Message) (user.Reply, error) {
	fields := strings.Fields(m.Text)
	command := ""
	if len(fields) > 0 {
		command = strings.SplitN(fields[0], "@", 2)[0]
	}
	if command == "/start" && len(fields) == 2 && strings.HasPrefix(fields[1], "compare_") {
		if _, err := inviteHash(strings.TrimPrefix(fields[1], "compare_")); err != nil {
			return user.Reply{Text: "🤔 This invite is invalid."}, nil
		}
		if err := SavePendingInvite(ctx, tx, userID, strings.TrimPrefix(fields[1], "compare_")); err != nil {
			return user.Reply{}, err
		}
		return PendingInviteReply(ctx, tx, userID)
	}
	if m.Callback == "c:cancel" {
		_, err := tx.Exec(ctx, `UPDATE users SET pending_comparison_hash=NULL WHERE id=$1`, userID)
		return user.Reply{Text: "👌 Connection cancelled."}, err
	}
	if strings.HasPrefix(m.Callback, "c:") {
		parts := strings.Split(m.Callback, ":")
		if len(parts) != 3 {
			return user.Reply{Text: "🤔 That action is no longer available."}, nil
		}
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || id <= 0 {
			return user.Reply{Text: "🤔 Invalid action."}, nil
		}
		switch parts[1] {
		case "accept":
			return acceptComparison(ctx, tx, userID, name, id, m.UpdateID)
		case "revoke":
			_, err = tx.Exec(ctx, `UPDATE comparison_invites SET state='revoked' WHERE id=$1 AND creator_id=$2 AND state='pending'`, id, userID)
			return user.Reply{Text: "👌 Invite cancelled. Use /compare to create another."}, err
		case "disconnect":
			return disconnectComparison(ctx, tx, userID, id, m.UpdateID)
		}
		return user.Reply{Text: "🤔 That action is no longer available."}, nil
	}
	var pairID int64
	err := tx.QueryRow(ctx, `SELECT pair_id FROM active_pair_members WHERE user_id=$1`, userID).Scan(&pairID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return user.Reply{}, err
	}
	if command == "/disconnect" {
		if pairID == 0 {
			return user.Reply{Text: "🍃 You don’t have a comparison partner."}, nil
		}
		return user.Reply{Text: "👋 Disconnect from your partner? Both of you will lose comparison access. Your expenses stay saved.", Buttons: [][]user.Button{{{Text: "✅ Disconnect", Data: fmt.Sprintf("c:disconnect:%d", pairID)}, {Text: "❌ Cancel", Data: "c:cancel"}}}}, nil
	}
	if pairID != 0 {
		return comparisonReport(ctx, tx, userID, timezone)
	}
	if botUsername == "" {
		return user.Reply{Text: "🤔 Invite links aren’t available yet. Please try again later."}, nil
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM comparison_invites WHERE creator_id=$1 AND created_at > now()-interval '1 hour'`, userID).Scan(&recent); err != nil {
		return user.Reply{}, err
	}
	if recent >= 5 {
		return user.Reply{Text: "⏳ You’ve created several invites recently. Please try again in an hour."}, nil
	}
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return user.Reply{}, err
	}
	token := hex.EncodeToString(b)
	h, _ := inviteHash(token)
	if _, err = tx.Exec(ctx, `UPDATE comparison_invites SET state='revoked' WHERE creator_id=$1 AND state='pending'`, userID); err != nil {
		return user.Reply{}, err
	}
	var inviteID int64
	err = tx.QueryRow(ctx, `INSERT INTO comparison_invites(creator_id,token_hash) VALUES($1,$2) RETURNING id`, userID, h).Scan(&inviteID)
	return user.Reply{Text: fmt.Sprintf("🤝 Share this invite to connect with one partner! You’ll share totals, expense counts, and weekly category record alerts (category and amount) from your connection date onward.\nhttps://t.me/%s?start=compare_%s\nValid for 24 hours. Older links no longer work.", strings.TrimPrefix(botUsername, "@"), token), Buttons: [][]user.Button{{{Text: "❌ Cancel invite", Data: fmt.Sprintf("c:revoke:%d", inviteID)}}}}, err
}

func acceptComparison(ctx context.Context, tx pgx.Tx, userID int64, name string, inviteID, updateID int64) (user.Reply, error) {
	var priorName string
	var priorPair int64
	priorErr := tx.QueryRow(ctx, `SELECT u.display_name,a.pair_id FROM comparison_invites i JOIN active_pair_members a ON a.user_id=i.creator_id JOIN active_pair_members b ON b.pair_id=a.pair_id AND b.user_id=$2 JOIN users u ON u.id=i.creator_id WHERE i.id=$1 AND i.state='consumed' AND i.consumed_by=$2`, inviteID, userID).Scan(&priorName, &priorPair)
	if priorErr == nil {
		return user.Reply{PairID: priorPair, Text: "🤝 You’re already connected with " + priorName + ". Use /compare for your totals."}, nil
	}
	if !errors.Is(priorErr, pgx.ErrNoRows) {
		return user.Reply{}, priorErr
	}
	var creator int64
	var creatorName string
	err := tx.QueryRow(ctx, `SELECT i.creator_id,u.display_name FROM comparison_invites i JOIN users u ON u.id=i.creator_id JOIN users me ON me.id=$2 AND me.pending_comparison_hash=i.token_hash WHERE i.id=$1 AND i.state='pending' AND i.expires_at>now() AND u.access_status='allowed' AND u.status='active' AND me.status='active' AND me.access_status='allowed' FOR UPDATE OF i`, inviteID, userID).Scan(&creator, &creatorName)
	if errors.Is(err, pgx.ErrNoRows) {
		return user.Reply{Text: "🤔 This invite is no longer available. Use /compare to see your current connection."}, nil
	}
	if err != nil {
		return user.Reply{}, err
	}
	if creator == userID {
		return user.Reply{Text: "🤔 You can’t connect with yourself."}, nil
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM active_pair_members WHERE user_id IN ($1,$2))`, creator, userID).Scan(&exists); err != nil {
		return user.Reply{}, err
	}
	if exists {
		return user.Reply{Text: "🤝 One of you already has a partner."}, nil
	}
	var pair int64
	if err = tx.QueryRow(ctx, `INSERT INTO comparison_pairs DEFAULT VALUES RETURNING id`).Scan(&pair); err != nil {
		return user.Reply{}, err
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO active_pair_members(user_id,pair_id,slot) VALUES($1,$3,1),($2,$3,2)`, []any{creator, userID, pair}},
		{`UPDATE comparison_invites SET state='consumed',consumed_by=$2,consumed_at=now() WHERE id=$1`, []any{inviteID, userID}},
		{`UPDATE comparison_invites SET state='revoked' WHERE creator_id IN ($1,$2) AND state='pending'`, []any{creator, userID}},
		{`UPDATE users SET pending_comparison_hash=NULL WHERE id IN ($1,$2)`, []any{creator, userID}},
		{`INSERT INTO notification_outbox(update_id,user_id,chat_id,body,pair_id) SELECT $1,id,telegram_chat_id,$3,$4 FROM users WHERE id=$2`, []any{updateID, creator, "🦹 " + name + " is your partner in crime now!", pair}},
	} {
		if _, err = tx.Exec(ctx, q.sql, q.args...); err != nil {
			return user.Reply{}, err
		}
	}
	return user.Reply{PairID: pair, Text: "⚔️ You’re battling " + creatorName + " now!\nUse /compare for your spending totals."}, nil
}
func disconnectComparison(ctx context.Context, tx pgx.Tx, userID, pairID, updateID int64) (user.Reply, error) {
	var partner int64
	var partnerName string
	err := tx.QueryRow(ctx, `SELECT other.user_id,u.display_name FROM active_pair_members me JOIN active_pair_members other ON other.pair_id=me.pair_id AND other.user_id<>me.user_id JOIN users u ON u.id=other.user_id WHERE me.user_id=$1 AND me.pair_id=$2`, userID, pairID).Scan(&partner, &partnerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return user.Reply{Text: "🍃 That connection is no longer active."}, nil
	}
	if err != nil {
		return user.Reply{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM active_pair_members WHERE pair_id=$1`, pairID); err != nil {
		return user.Reply{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE comparison_pairs SET ended_at=now() WHERE id=$1`, pairID); err != nil {
		return user.Reply{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notification_outbox(update_id,user_id,chat_id,body) SELECT $1,id,telegram_chat_id,'👋 Your comparison connection has ended. Your expenses remain saved.' FROM users WHERE id=$2`, updateID, partner)
	return user.Reply{Text: "👋 You’re no longer connected with " + partnerName + "."}, err
}

func comparisonMoney(raw string) string {
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return "Rp?"
	}
	n.Quo(n, big.NewInt(100))
	s := n.String()
	start := 0
	if strings.HasPrefix(s, "-") {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "Rp" + s
}
func comparisonReport(ctx context.Context, tx pgx.Tx, userID int64, timezone string) (user.Reply, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return user.Reply{}, err
	}
	today := time.Now().In(loc).Format("2006-01-02")
	rows, err := tx.Query(ctx, `SELECT p.id,u.id,u.display_name,(p.connected_at AT TIME ZONE $2)::date::text,
 count(e.id) FILTER(WHERE e.expense_date=$3::date),COALESCE(sum(e.amount_minor) FILTER(WHERE e.expense_date=$3::date),0)::text,
 count(e.id),COALESCE(sum(e.amount_minor),0)::text
 FROM active_pair_members me JOIN comparison_pairs p ON p.id=me.pair_id AND p.ended_at IS NULL JOIN active_pair_members members ON members.pair_id=p.id JOIN users u ON u.id=members.user_id
 LEFT JOIN expenses e ON e.user_id=u.id AND e.deleted_at IS NULL AND e.expense_date >= GREATEST((p.connected_at AT TIME ZONE $2)::date,date_trunc('month',$3::date)::date) AND e.expense_date < (date_trunc('month',$3::date)+interval '1 month')::date
 WHERE me.user_id=$1 GROUP BY p.id,u.id,p.connected_at ORDER BY u.id`, userID, timezone, today)
	if err != nil {
		return user.Reply{}, err
	}
	defer rows.Close()
	var sections []string
	totals := map[int64]*big.Int{}
	var since string
	var pairID int64
	for rows.Next() {
		var id, dayCount, monthCount int64
		var name, daySum, monthSum string
		if err = rows.Scan(&pairID, &id, &name, &since, &dayCount, &daySum, &monthCount, &monthSum); err != nil {
			return user.Reply{}, err
		}
		day := fmt.Sprintf("%s · %d expenses", comparisonMoney(daySum), dayCount)
		month := fmt.Sprintf("%s · %d expenses", comparisonMoney(monthSum), monthCount)
		if dayCount == 0 {
			day = "No recorded expenses"
		}
		if monthCount == 0 {
			month = "No recorded expenses"
		}
		sections = append(sections, fmt.Sprintf("%s\nToday: %s\nThis month: %s", name, day, month))
		totals[id], _ = new(big.Int).SetString(monthSum, 10)
	}
	if err = rows.Err(); err != nil {
		return user.Reply{}, err
	}
	if len(sections) != 2 {
		return user.Reply{Text: "🍃 You don’t have an active comparison partner."}, nil
	}
	diff := new(big.Int).Set(totals[userID])
	for id, n := range totals {
		if id != userID {
			diff.Sub(diff, n)
		}
	}
	return user.Reply{PairID: pairID, Text: "📊 Comparing since " + since + " (" + timezone + ")\n\n" + strings.Join(sections, "\n\n") + "\n\nMonthly difference (you − partner): " + comparisonMoney(diff.String())}, nil
}
