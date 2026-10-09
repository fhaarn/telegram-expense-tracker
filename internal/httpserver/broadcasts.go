package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"telegram-expense-tracker/internal/postgres"
)

type BroadcastStore interface {
	PreviewBroadcast(context.Context) (int64, error)
	CreateBroadcast(context.Context, string, string) (int64, error)
	Broadcast(context.Context, int64) (postgres.BroadcastStatus, error)
}

func registerBroadcasts(mux *http.ServeMux, store AdminStore, wake func()) {
	b, ok := store.(BroadcastStore)
	if !ok {
		return
	}
	output := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /admin/broadcasts", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Message string `json:"message"`
			DryRun  *bool  `json:"dry_run"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			http.Error(w, "invalid JSON body", 400)
			return
		}
		var extra any
		if dec.Decode(&extra) != io.EOF || input.DryRun == nil || !postgres.ValidBroadcastMessage(input.Message) {
			http.Error(w, "message must contain 1–4000 UTF-16 units and dry_run must be explicit", 400)
			return
		}
		if *input.DryRun {
			n, err := b.PreviewBroadcast(r.Context())
			if err != nil {
				http.Error(w, "admin operation unavailable", 503)
				return
			}
			output(w, 200, map[string]any{"dry_run": true, "eligible_recipients": n})
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !postgres.ValidBroadcastKey(key) {
			http.Error(w, "Idempotency-Key required: 1–128 letters, digits, dots, hyphens or underscores", 400)
			return
		}
		id, err := b.CreateBroadcast(r.Context(), input.Message, key)
		if errors.Is(err, postgres.ErrBroadcastConflict) {
			http.Error(w, "idempotency key already used for another message", 409)
			return
		}
		if errors.Is(err, postgres.ErrBroadcastBusy) {
			http.Error(w, "too many queued broadcasts", 429)
			return
		}
		if err != nil {
			http.Error(w, "admin operation unavailable", 503)
			return
		}
		if wake != nil {
			wake()
		}
		url := "/admin/broadcasts/" + strconv.FormatInt(id, 10)
		w.Header().Set("Location", url)
		output(w, 202, map[string]any{"id": id, "status_url": url})
	})
	mux.HandleFunc("GET /admin/broadcasts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid broadcast ID", 400)
			return
		}
		status, err := b.Broadcast(r.Context(), id)
		if errors.Is(err, postgres.ErrBroadcastNotFound) {
			http.Error(w, "broadcast not found", 404)
			return
		}
		if err != nil {
			http.Error(w, "admin operation unavailable", 503)
			return
		}
		output(w, 200, status)
	})
}
