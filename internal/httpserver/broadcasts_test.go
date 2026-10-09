package httpserver

import (
	"context"
	"net/http/httptest"
	"strings"
	"telegram-expense-tracker/internal/postgres"
	"testing"
)

type fakeBroadcast struct {
	fakeAdmin
	previews, creates int
}

func (f *fakeBroadcast) PreviewBroadcast(context.Context) (int64, error) { f.previews++; return 3, nil }
func (f *fakeBroadcast) CreateBroadcast(_ context.Context, message, key string) (int64, error) {
	f.creates++
	if key == "conflict" {
		return 0, postgres.ErrBroadcastConflict
	}
	return 1, nil
}
func (f *fakeBroadcast) Broadcast(_ context.Context, id int64) (postgres.BroadcastStatus, error) {
	if id == 99 {
		return postgres.BroadcastStatus{}, postgres.ErrBroadcastNotFound
	}
	return postgres.BroadcastStatus{ID: id, Total: 3, Pending: 3, State: "queued"}, nil
}
func TestBroadcastHTTPUsesExistingKey(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, key, idempotency string
		code, previews, creates                    int
	}{
		{"preview", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":true}`, "secret", "", 200, 1, 0},
		{"send", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":false}`, "secret", "release-1", 202, 0, 1},
		{"missing auth", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":false}`, "", "release-1", 401, 0, 0},
		{"wrong auth", "GET", "/admin/broadcasts/1", "", "wrong", "", 401, 0, 0},
		{"missing dry run", "POST", "/admin/broadcasts", `{"message":"hello"}`, "secret", "release-1", 400, 0, 0},
		{"missing idempotency", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":false}`, "secret", "", 400, 0, 0},
		{"unknown field", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":true,"all":true}`, "secret", "", 400, 0, 0},
		{"trailing JSON", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":true} {}`, "secret", "", 400, 0, 0},
		{"blank", "POST", "/admin/broadcasts", `{"message":" ","dry_run":true}`, "secret", "", 400, 0, 0},
		{"emoji cap", "POST", "/admin/broadcasts", `{"message":"` + strings.Repeat("😱", 2001) + `","dry_run":true}`, "secret", "", 400, 0, 0},
		{"unicode body", "POST", "/admin/broadcasts", `{"message":"` + strings.Repeat("😱", 2000) + `","dry_run":true}`, "secret", "", 200, 1, 0},
		{"conflict", "POST", "/admin/broadcasts", `{"message":"hello","dry_run":false}`, "secret", "conflict", 409, 0, 1},
		{"status", "GET", "/admin/broadcasts/1", "", "secret", "", 200, 0, 0},
		{"missing job", "GET", "/admin/broadcasts/99", "", "secret", "", 404, 0, 0},
		{"bad id", "GET", "/admin/broadcasts/0", "", "secret", "", 400, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeBroadcast{}
			wakes := 0
			h := AdminHandler("secret", f, func() { wakes++ })
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-API-Key", tc.key)
			r.Header.Set("Idempotency-Key", tc.idempotency)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code || f.previews != tc.previews || f.creates != tc.creates {
				t.Fatalf("code %d, body %s, store %+v", w.Code, w.Body.String(), f)
			}
			if (tc.code == 202) != (wakes == 1) {
				t.Fatal("incorrect wake", wakes)
			}
		})
	}
}
