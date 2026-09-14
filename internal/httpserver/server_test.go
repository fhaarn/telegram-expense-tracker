package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeDB struct {
	calls int
	err   error
}

func (d *fakeDB) Ping(ctx context.Context) error {
	d.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("missing database timeout")
	}
	return d.err
}
func TestHealthDoesNotWakeDatabase(t *testing.T) {
	db := &fakeDB{err: errors.New("offline")}
	w := httptest.NewRecorder()
	Handler(db).ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != http.StatusOK || db.calls != 0 {
		t.Fatalf("status %d, calls %d", w.Code, db.calls)
	}
}
func TestReadiness(t *testing.T) {
	for _, offline := range []bool{false, true} {
		db := &fakeDB{}
		expected := http.StatusOK
		if offline {
			db.err = errors.New("private connection detail")
			expected = http.StatusServiceUnavailable
		}
		w := httptest.NewRecorder()
		Handler(db).ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
		if w.Code != expected || db.calls != 1 {
			t.Fatalf("status %d, calls %d", w.Code, db.calls)
		}
	}
}
func TestNoPrematureWebhook(t *testing.T) {
	w := httptest.NewRecorder()
	Handler(&fakeDB{}).ServeHTTP(w, httptest.NewRequest("POST", "/telegram/webhook", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unimplemented webhook returned %d", w.Code)
	}
}
