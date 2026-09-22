package httpserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"telegram-expense-tracker/internal/postgres"
	"time"
	"unicode/utf8"
)

type AdminStore interface {
	List(context.Context, int64, int, string) ([]postgres.AdminUser, error)
	SetAccess(context.Context, int64, string, string) error
}

// AdminHandler has one bounded counter, shared across the single owner's key.
func AdminHandler(key string, store AdminStore) http.Handler {
	var mu sync.Mutex
	var window time.Time
	count := 0
	expected := sha256.Sum256([]byte(key))
	mux := http.NewServeMux()
	output := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /admin/users", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit := 50
		var err error
		if q.Has("limit") {
			limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil || limit < 1 || limit > 100 {
				http.Error(w, "invalid limit", 400)
				return
			}
		}
		access := q.Get("access")
		if access == "" {
			access = "all"
		}
		if access != "all" && access != "allowed" && access != "blocked" {
			http.Error(w, "invalid access filter", 400)
			return
		}
		var after int64
		if q.Has("cursor") {
			raw, e := base64.RawURLEncoding.DecodeString(q.Get("cursor"))
			if e != nil {
				http.Error(w, "invalid cursor", 400)
				return
			}
			after, e = strconv.ParseInt(string(raw), 10, 64)
			if e != nil || after <= 0 {
				http.Error(w, "invalid cursor", 400)
				return
			}
		}
		users, e := store.List(r.Context(), after, limit+1, access)
		if e != nil {
			http.Error(w, "admin operation unavailable", 503)
			return
		}
		var next *string
		if len(users) > limit {
			users = users[:limit]
			c := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(users[len(users)-1].ID, 10)))
			next = &c
		}
		output(w, struct {
			Users []postgres.AdminUser `json:"users"`
			Next  *string              `json:"next_cursor"`
		}{users, next})
	})
	for _, action := range []string{"blacklist", "whitelist"} {
		mux.HandleFunc("PUT /admin/users/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "invalid user ID", 400)
				return
			}
			var body struct {
				Reason string `json:"reason"`
			}
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			err = dec.Decode(&body)
			if err != nil && err != io.EOF {
				http.Error(w, "invalid JSON body", 400)
				return
			}
			if err == nil {
				var extra any
				if dec.Decode(&extra) != io.EOF {
					http.Error(w, "invalid JSON body", 400)
					return
				}
			}
			if !utf8.ValidString(body.Reason) || utf8.RuneCountInString(body.Reason) > 500 {
				http.Error(w, "invalid reason", 400)
				return
			}
			status := "allowed"
			if action == "blacklist" {
				status = "blocked"
			}
			err = store.SetAccess(r.Context(), id, status, body.Reason)
			if errors.Is(err, postgres.ErrUserNotFound) {
				http.Error(w, "user not found", 404)
				return
			}
			if err != nil {
				http.Error(w, "admin operation unavailable", 503)
				return
			}
			output(w, struct {
				ID     int64  `json:"id"`
				Access string `json:"access_status"`
			}{id, status})
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if strings.TrimSpace(key) == "" {
			http.Error(w, "admin API disabled", 503)
			return
		}
		supplied := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
		if subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		mu.Lock()
		if time.Since(window) >= time.Minute {
			window = time.Now()
			count = 0
		}
		count++
		allowed := count <= 60
		mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests", 429)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
