package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrUserNotFound = errors.New("user not found")

type Admin struct{ Pool *pgxpool.Pool }
type AdminUser struct {
	ID              int64     `json:"id"`
	TelegramID      int64     `json:"telegram_user_id"`
	Username        *string   `json:"telegram_username"`
	Name            *string   `json:"display_name"`
	Status          string    `json:"status"`
	Access          string    `json:"access_status"`
	CreatedAt       time.Time `json:"created_at"`
	AccessUpdatedAt time.Time `json:"access_updated_at"`
}

func (a Admin) List(ctx context.Context, after int64, limit int, access string) ([]AdminUser, error) {
	rows, err := a.Pool.Query(ctx, `SELECT id,telegram_user_id,telegram_username,display_name,status,access_status,created_at,access_updated_at FROM users WHERE id>$1 AND ($2='all' OR access_status=$2) ORDER BY id LIMIT $3`, after, access, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		if err = rows.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Name, &u.Status, &u.Access, &u.CreatedAt, &u.AccessUpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (a Admin) SetAccess(ctx context.Context, id int64, status, reason string) error {
	if status != "allowed" && status != "blocked" {
		return errors.New("invalid access status")
	}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7312041)`); err != nil {
		return err
	}
	var previous string
	err = tx.QueryRow(ctx, `SELECT access_status FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if previous == status {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET access_status=$2,access_updated_at=now() WHERE id=$1`, id, status); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_access_events(user_id,previous_status,new_status,reason) VALUES($1,$2,$3,NULLIF($4,''))`, id, previous, status, reason); err != nil {
		return err
	}
	if status == "blocked" {
		if _, err = tx.Exec(ctx, `UPDATE comparison_invites SET state='revoked' WHERE creator_id=$1 AND state='pending'`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE users SET pending_comparison_hash=NULL WHERE id=$1`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE comparison_pairs SET ended_at=now() WHERE id IN (SELECT pair_id FROM active_pair_members WHERE user_id=$1)`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM active_pair_members WHERE pair_id IN (SELECT pair_id FROM active_pair_members WHERE user_id=$1)`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE notification_outbox n SET status='failed',body='',reply_markup='[]',lease_until=NULL WHERE status IN ('pending','sending') AND (user_id=$1 OR (pair_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM active_pair_members m WHERE m.user_id=n.user_id AND m.pair_id=n.pair_id)))`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
