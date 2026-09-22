// Package httpserver exposes operational HTTP endpoints.
package httpserver

import (
	"context"
	"net/http"
	"time"
)

type Pinger interface{ Ping(context.Context) error }

func Handler(db Pinger, webhook ...http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	if len(webhook) > 0 {
		mux.Handle("POST /telegram/webhook", webhook[0])
	}
	if len(webhook) > 1 {
		mux.Handle("/admin/", webhook[1])
	}
	return mux
}

func New(port string, db Pinger, webhook ...http.Handler) *http.Server {
	return &http.Server{Addr: ":" + port, Handler: Handler(db, webhook...), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
}
