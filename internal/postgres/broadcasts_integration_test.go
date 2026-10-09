//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBroadcastAudienceIdempotencyAndDelivery(t *testing.T) {
	h := newBatchHarness(t)
	ctx := context.Background()
	a := Admin{Pool: h.p}
	h.send(202, "/start", "")
	h.send(202, "Budi", "")
	h.send(303, "/start", "")
	n, err := a.PreviewBroadcast(ctx)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if h.count("admin_broadcasts") != 0 {
		t.Fatal("preview wrote")
	}
	id, err := a.CreateBroadcast(ctx, "📣 Bulk expenses are ready!", "release-1")
	if err != nil {
		t.Fatal(err)
	}
	same, err := a.CreateBroadcast(ctx, "📣 Bulk expenses are ready!", "release-1")
	if err != nil || same != id {
		t.Fatal(same, err)
	}
	if _, err = a.CreateBroadcast(ctx, "different", "release-1"); !errors.Is(err, ErrBroadcastConflict) {
		t.Fatal(err)
	}
	if h.count("admin_broadcast_recipients") != 2 {
		t.Fatal("duplicate recipients")
	}
	// Completing registration after submission must not change the audience snapshot.
	h.send(303, "New", "")
	q := BroadcastQueue{Pool: h.p}
	if _, found, err := q.Claim(ctx); err != nil || found {
		t.Fatal("bot replies should have priority", found, err)
	}
	h.exec(`UPDATE notification_outbox SET status='sent'`)
	d, found, err := q.Claim(ctx)
	if err != nil || !found || d.ChatID != 101 {
		t.Fatal(d, found, err)
	}
	// Global lease/pacing prevents another worker from concurrently claiming a broadcast.
	if _, found, err := q.Claim(ctx); err != nil || found {
		t.Fatal(found, err)
	}
	if err = q.Finish(ctx, d, "rate_limited", 30*time.Second, false); err != nil {
		t.Fatal(err)
	}
	if _, found, err := q.Claim(ctx); err != nil || found {
		t.Fatal("cooldown ignored", found, err)
	}
	if wait, pending, err := q.Next(ctx); err != nil || !pending || wait < 29*time.Second {
		t.Fatal(wait, pending, err)
	}
	var other int64
	h.p.QueryRow(ctx, `SELECT id FROM users WHERE telegram_user_id=202`).Scan(&other)
	if err = a.SetAccess(ctx, other, "blocked", ""); err != nil {
		t.Fatal(err)
	}
	if err = a.SetAccess(ctx, other, "allowed", ""); err != nil {
		t.Fatal(err)
	}
	h.exec(`UPDATE broadcast_delivery_state SET available_at=now()`)
	h.exec(`UPDATE admin_broadcast_recipients SET available_at=now()`)
	q = BroadcastQueue{Pool: h.p} // new process, persisted retries
	d, found, err = q.Claim(ctx)
	if err != nil || !found || d.Attempts != 2 {
		t.Fatal(d, found, err)
	}
	if err = q.Finish(ctx, d, "", 0, false); err != nil {
		t.Fatal(err)
	}
	b, err := a.Broadcast(ctx, id)
	if err != nil || b.Total != 2 || b.Sent != 1 || b.Skipped != 1 || b.Pending != 0 || b.State != "completed" {
		t.Fatal(b, err)
	}
	if _, pending, err := q.Next(ctx); err != nil || pending {
		t.Fatal(pending, err)
	}
}
func TestBroadcastLeaseRetriesAndTerminalFailure(t *testing.T) {
	h := newBatchHarness(t)
	ctx := context.Background()
	a := Admin{Pool: h.p}
	q := BroadcastQueue{Pool: h.p}
	id, err := a.CreateBroadcast(ctx, "hello", "lease")
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`UPDATE notification_outbox SET status='sent'`)
	stale, found, err := q.Claim(ctx)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	h.exec(`UPDATE admin_broadcast_recipients SET lease_until=now()-interval '1 second'`)
	h.exec(`UPDATE broadcast_delivery_state SET available_at=now()`)
	d, found, err := q.Claim(ctx)
	if err != nil || !found || d.Attempts != 2 {
		t.Fatal(d, found, err)
	}
	if err = q.Finish(ctx, stale, "", 0, false); err != nil {
		t.Fatal(err)
	}
	b, err := a.Broadcast(ctx, id)
	if err != nil || b.Pending != 1 {
		t.Fatal("stale lease acknowledged", b, err)
	}
	if err = q.Finish(ctx, d, "forbidden", 0, true); err != nil {
		t.Fatal(err)
	}
	b, err = a.Broadcast(ctx, id)
	if err != nil || b.Failed != 1 || b.State != "completed_with_failures" {
		t.Fatal(b, err)
	}
	id, err = a.CreateBroadcast(ctx, "retry", "retry")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		h.exec(`UPDATE broadcast_delivery_state SET available_at=now()`)
		h.exec(`UPDATE admin_broadcast_recipients SET available_at=now()`)
		d, found, err = q.Claim(ctx)
		if err != nil || !found || d.Attempts != i {
			t.Fatal(d, found, err)
		}
		if err = q.Finish(ctx, d, "temporary_failure", 0, false); err != nil {
			t.Fatal(err)
		}
	}
	b, err = a.Broadcast(ctx, id)
	if err != nil || b.Failed != 1 || b.Pending != 0 {
		t.Fatal(b, err)
	}
}
