package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"telegram-expense-tracker/internal/postgres"
	"testing"
)

type fakeAdmin struct {
	calls  int
	status string
}

func (f *fakeAdmin) List(_ context.Context, after int64, limit int, access string) ([]postgres.AdminUser, error) {
	f.calls++
	return []postgres.AdminUser{{ID: 1}, {ID: 2}}, nil
}
func (f *fakeAdmin) SetAccess(_ context.Context, id int64, status, reason string) error {
	f.calls++
	f.status = status
	if id == 99 {
		return postgres.ErrUserNotFound
	}
	return nil
}
func TestAdminHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, key, header, method, path, body string
		want                                  int
	}{
		{"disabled", "", "", "GET", "/admin/users", "", 503},
		{"missing", "secret", "", "GET", "/admin/users", "", 401},
		{"wrong", "secret", "wrong", "PUT", "/admin/users/1/blacklist", "", 401},
		{"list", "secret", "secret", "GET", "/admin/users?limit=1", "", 200},
		{"bad limit", "secret", "secret", "GET", "/admin/users?limit=101", "", 400},
		{"bad cursor", "secret", "secret", "GET", "/admin/users?cursor=invalid", "", 400},
		{"bad filter", "secret", "secret", "GET", "/admin/users?access=no", "", 400},
		{"block", "secret", "secret", "PUT", "/admin/users/1/blacklist", `{"reason":"test"}`, 200},
		{"allow", "secret", "secret", "PUT", "/admin/users/1/whitelist", "", 200},
		{"missing user", "secret", "secret", "PUT", "/admin/users/99/blacklist", "", 404},
		{"bad id", "secret", "secret", "PUT", "/admin/users/0/blacklist", "", 400},
		{"unknown field", "secret", "secret", "PUT", "/admin/users/1/blacklist", `{"token":"oops"}`, 400},
		{"trailing JSON", "secret", "secret", "PUT", "/admin/users/1/blacklist", `{} {}`, 400},
		{"oversized reason", "secret", "secret", "PUT", "/admin/users/1/blacklist", `{"reason":"` + strings.Repeat("x", 501) + `"}`, 400},
		{"oversized body", "secret", "secret", "PUT", "/admin/users/1/blacklist", strings.Repeat(" ", 4097), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAdmin{}
			h := AdminHandler(tc.key, f)
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-API-Key", tc.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.want == 401 || tc.want == 503 {
				if f.calls != 0 {
					t.Fatal("unauthorized store access")
				}
			}
			if tc.name == "list" {
				var v struct {
					Users []postgres.AdminUser `json:"users"`
					Next  string               `json:"next_cursor"`
				}
				if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Users) != 1 || v.Next == "" {
					t.Fatal(w.Body.String())
				}
			}
		})
	}
}
func TestAdminRateLimit(t *testing.T) {
	h := AdminHandler("secret", &fakeAdmin{})
	for i := 0; i < 61; i++ {
		r := httptest.NewRequest("GET", "/admin/users", nil)
		r.Header.Set("X-API-Key", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 60 {
			want = 429
		}
		if w.Code != want {
			t.Fatal(w.Code)
		}
	}
}
