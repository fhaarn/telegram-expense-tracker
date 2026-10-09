package postgres

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBroadcastConflict = errors.New("idempotency key already used for another message")
var ErrBroadcastNotFound = errors.New("broadcast not found")
var ErrBroadcastInvalid = errors.New("invalid broadcast")
var ErrBroadcastBusy = errors.New("too many queued broadcasts")

type BroadcastStatus struct {
	ID        int64     `json:"id"`
	State     string    `json:"state"`
	Total     int64     `json:"total"`
	Pending   int64     `json:"pending"`
	Sent      int64     `json:"sent"`
	Failed    int64     `json:"failed"`
	Skipped   int64     `json:"skipped"`
	CreatedAt time.Time `json:"created_at"`
}

// UTF-16 accounting is conservative for emoji and keeps plain text below Telegram's cap.
func ValidBroadcastMessage(message string) bool {
	return utf8.ValidString(message) && strings.TrimSpace(message) != "" && len(utf16.Encode([]rune(message))) <= 4000
}
func ValidBroadcastKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
func (a Admin) PreviewBroadcast(ctx context.Context) (int64, error) {
	var n int64
	err := a.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE status='active' AND access_status='allowed'`).Scan(&n)
	return n, err
}
func (a Admin) CreateBroadcast(ctx context.Context, message, key string) (int64, error) {
	if !ValidBroadcastMessage(message) || !ValidBroadcastKey(key) {
		return 0, ErrBroadcastInvalid
	}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7312041)`); err != nil {
		return 0, err
	}
	var id int64
	var previous string
	err = tx.QueryRow(ctx, `SELECT id,message FROM admin_broadcasts WHERE idempotency_key=$1`, key).Scan(&id, &previous)
	if err == nil {
		if previous != message {
			return 0, ErrBroadcastConflict
		}
		return id, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(DISTINCT broadcast_id) FROM admin_broadcast_recipients WHERE status IN ('pending','sending')`).Scan(&count); err != nil {
		return 0, err
	}
	if count >= 10 {
		return 0, ErrBroadcastBusy
	}
	if err = tx.QueryRow(ctx, `INSERT INTO admin_broadcasts(message,idempotency_key) VALUES($1,$2) RETURNING id`, message, key).Scan(&id); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_broadcast_recipients(broadcast_id,user_id) SELECT $1,id FROM users WHERE status='active' AND access_status='allowed'`, id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}
func (a Admin) Broadcast(ctx context.Context, id int64) (BroadcastStatus, error) {
	var b BroadcastStatus
	err := a.Pool.QueryRow(ctx, `SELECT b.id,b.created_at,count(r.id),count(r.id) FILTER(WHERE r.status IN ('pending','sending')),count(r.id) FILTER(WHERE r.status='sent'),count(r.id) FILTER(WHERE r.status='failed'),count(r.id) FILTER(WHERE r.status='skipped') FROM admin_broadcasts b LEFT JOIN admin_broadcast_recipients r ON r.broadcast_id=b.id WHERE b.id=$1 GROUP BY b.id`, id).Scan(&b.ID, &b.CreatedAt, &b.Total, &b.Pending, &b.Sent, &b.Failed, &b.Skipped)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrBroadcastNotFound
	}
	b.State = "completed"
	if b.Pending > 0 {
		b.State = "queued"
	} else if b.Failed > 0 {
		b.State = "completed_with_failures"
	}
	return b, err
}

type BroadcastQueue struct{ Pool *pgxpool.Pool }
type BroadcastDelivery struct {
	ID, ChatID int64
	Message    string
	Attempts   int
}

func (q BroadcastQueue) Claim(ctx context.Context) (BroadcastDelivery, bool, error) {
	var d BroadcastDelivery
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return d, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7312041)`); err != nil {
		return d, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE admin_broadcast_recipients r SET status='skipped',lease_until=NULL,failure_code='access_revoked' FROM users u WHERE u.id=r.user_id AND (u.status<>'active' OR u.access_status<>'allowed') AND r.status IN ('pending','sending')`); err != nil {
		return d, false, err
	}
	// One announcement per second globally, no concurrent announcements; queued bot replies take priority.
	err = tx.QueryRow(ctx, `WITH candidate AS (
 SELECT r.id FROM admin_broadcast_recipients r JOIN users u ON u.id=r.user_id
 WHERE u.status='active' AND u.access_status='allowed'
 AND ((r.status='pending' AND r.available_at<=now()) OR (r.status='sending' AND r.lease_until<=now()))
 AND (SELECT available_at<=now() FROM broadcast_delivery_state WHERE id=1)
 AND NOT EXISTS(SELECT 1 FROM admin_broadcast_recipients WHERE status='sending' AND lease_until>now())
 AND NOT EXISTS(SELECT 1 FROM notification_outbox WHERE (status='pending' AND available_at<=now()) OR (status='sending' AND lease_until>now()))
 ORDER BY r.id FOR UPDATE OF r SKIP LOCKED LIMIT 1)
 UPDATE admin_broadcast_recipients r SET status='sending',attempts=attempts+1,lease_until=now()+interval '120 seconds'
 FROM candidate c,users u,admin_broadcasts b WHERE r.id=c.id AND u.id=r.user_id AND b.id=r.broadcast_id RETURNING r.id,u.telegram_chat_id,b.message,r.attempts`).Scan(&d.ID, &d.ChatID, &d.Message, &d.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, false, tx.Commit(ctx)
	}
	if err != nil {
		return d, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE broadcast_delivery_state SET available_at=now()+interval '1 second' WHERE id=1`); err != nil {
		return d, false, err
	}
	return d, true, tx.Commit(ctx)
}
func (q BroadcastQueue) Finish(ctx context.Context, d BroadcastDelivery, failure string, retryAfter time.Duration, permanent bool) error {
	status := "sent"
	if failure != "" {
		status = "pending"
		if permanent || d.Attempts >= 5 {
			status = "failed"
		}
	}
	delay := max(time.Duration(1<<min(d.Attempts, 8))*time.Second, retryAfter)
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE admin_broadcast_recipients SET status=$2,available_at=now()+$3::interval,lease_until=NULL,failure_code=NULLIF($4,'') WHERE id=$1 AND status='sending' AND attempts=$5`, d.ID, status, delay.String(), failure, d.Attempts)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 && retryAfter > 0 {
		if _, err = tx.Exec(ctx, `UPDATE broadcast_delivery_state SET available_at=GREATEST(available_at,now()+$1::interval) WHERE id=1`, retryAfter.String()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (q BroadcastQueue) Next(ctx context.Context) (time.Duration, bool, error) {
	var seconds *float64
	err := q.Pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM GREATEST(min(CASE WHEN status='sending' THEN lease_until ELSE available_at END),(SELECT available_at FROM broadcast_delivery_state WHERE id=1))-now())::double precision FROM admin_broadcast_recipients WHERE status IN ('pending','sending') HAVING count(*)>0`).Scan(&seconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil || seconds == nil {
		return 0, false, err
	}
	return max(time.Second, time.Duration(*seconds*float64(time.Second))), true, nil
}
