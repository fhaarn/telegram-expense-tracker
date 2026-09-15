package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telegram-expense-tracker/internal/user"
	"time"
)

type Outbox struct{ Pool *pgxpool.Pool }
type Delivery struct {
	ID, ChatID int64
	Body       string
	Attempts   int
	Buttons    [][]user.Button
}

func (o Outbox) Claim(ctx context.Context) (Delivery, bool, error) {
	var d Delivery
	var markup []byte
	if _, err := o.Pool.Exec(ctx, `UPDATE notification_outbox n SET status='failed',body='',reply_markup='[]' WHERE n.status IN ('pending','sending') AND n.pair_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM active_pair_members a WHERE a.pair_id=n.pair_id AND a.user_id=n.user_id)`); err != nil {
		return d, false, err
	}
	err := o.Pool.QueryRow(ctx, `WITH candidate AS (
 SELECT n.id FROM notification_outbox n WHERE
 ((n.status='pending' AND n.available_at<=now()) OR (n.status='sending' AND n.lease_until<=now()))
 AND NOT EXISTS(SELECT 1 FROM notification_outbox earlier WHERE earlier.user_id=n.user_id AND earlier.id<n.id AND earlier.status IN ('pending','sending'))
 ORDER BY n.id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE notification_outbox n SET status='sending',attempts=attempts+1,lease_until=now()+interval '30 seconds'
 FROM candidate c WHERE n.id=c.id RETURNING n.id,n.chat_id,n.body,n.attempts,n.reply_markup`).Scan(&d.ID, &d.ChatID, &d.Body, &d.Attempts, &markup)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, false, nil
	}
	if err == nil {
		err = json.Unmarshal(markup, &d.Buttons)
	}
	return d, err == nil, err
}
func (o Outbox) Finish(ctx context.Context, d Delivery, sendErr error) error {
	status := "sent"
	if sendErr != nil {
		status = "pending"
		if d.Attempts >= 5 {
			status = "failed"
		}
	}
	delay := time.Duration(1<<min(d.Attempts, 8)) * time.Second
	_, err := o.Pool.Exec(ctx, `UPDATE notification_outbox SET status=$2,available_at=now()+$3::interval,lease_until=NULL,
 body=CASE WHEN $2 IN ('sent','failed') THEN '' ELSE body END, reply_markup=CASE WHEN $2 IN ('sent','failed') THEN '[]'::jsonb ELSE reply_markup END
 WHERE id=$1 AND status='sending' AND attempts=$4`, d.ID, status, delay.String(), d.Attempts)
	return err
}
func (o Outbox) Next(ctx context.Context) (time.Duration, bool, error) {
	var seconds *float64
	err := o.Pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM min(CASE WHEN status='sending' THEN lease_until ELSE available_at END)-now())::double precision
 FROM notification_outbox n WHERE status IN ('pending','sending')
 AND NOT EXISTS(SELECT 1 FROM notification_outbox earlier WHERE earlier.user_id=n.user_id AND earlier.id<n.id AND earlier.status IN ('pending','sending'))`).Scan(&seconds)
	if err != nil || seconds == nil {
		return 0, false, err
	}
	return max(time.Second, time.Duration(*seconds*float64(time.Second))), true, nil
}
