package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"telegram-expense-tracker/internal/user"
	"testing"
)

type acceptor struct {
	calls int
	err   error
	got   user.Message
}

func (a *acceptor) Accept(_ context.Context, m user.Message) error {
	a.calls++
	a.got = m
	return a.err
}

const validUpdate = `{"update_id":1,"message":{"message_id":1,"from":{"id":42,"is_bot":false,"first_name":"Test"},"chat":{"id":42,"type":"private"},"text":"/start"}}`

func TestWebhookBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, body, secret string
		status, calls      int
		failed             bool
	}{
		{"valid", validUpdate, "secret", 200, 1, false},
		{"unauthenticated", validUpdate, "wrong", 401, 0, false},
		{"bad json", "{", "secret", 400, 0, false},
		{"missing id", `{"message":{}}`, "secret", 400, 0, false},
		{"oversized", strings.Repeat("x", (1<<20)+1), "secret", 413, 0, false},
		{"group", strings.Replace(validUpdate, `"private"`, `"group"`, 1), "secret", 200, 0, false},
		{"bot", strings.Replace(validUpdate, `"is_bot":false`, `"is_bot":true`, 1), "secret", 200, 0, false},
		{"storage failure", validUpdate, "secret", 503, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &acceptor{}
			if tt.failed {
				a.err = errors.New("private database detail")
			}
			woke := false
			req := httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(tt.body))
			req.Header.Set("X-Telegram-Bot-Api-Secret-Token", tt.secret)
			w := httptest.NewRecorder()
			Webhook("secret", a, func() { woke = true }).ServeHTTP(w, req)
			if w.Code != tt.status || a.calls != tt.calls {
				t.Fatalf("status %d calls %d", w.Code, a.calls)
			}
			if woke != (tt.status == 200 && tt.calls == 1) {
				t.Fatal("incorrect worker wake")
			}
			if strings.Contains(w.Body.String(), "private database detail") {
				t.Fatal("leaked database error")
			}
		})
	}
}

func TestCallbackIdentity(t *testing.T) {
	for _, tt := range []struct {
		sender int
		calls  int
	}{{42, 1}, {99, 0}} {
		body := fmt.Sprintf(`{"update_id":8,"callback_query":{"id":"query-8","from":{"id":%d},"data":"e:save:1:1:","message":{"message_id":9,"date":123,"from":{"id":999,"is_bot":true},"chat":{"id":42,"type":"private"}}}}`, tt.sender)
		a := &acceptor{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/telegram/webhook", strings.NewReader(body))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
		Webhook("secret", a, nil).ServeHTTP(w, r)
		if w.Code != 200 || a.calls != tt.calls {
			t.Fatalf("status %d calls %d", w.Code, a.calls)
		}
		if tt.calls == 1 && (a.got.TelegramID != 42 || a.got.Callback != "e:save:1:1:" || !strings.Contains(w.Body.String(), "answerCallbackQuery")) {
			t.Fatal("callback not preserved/acknowledged")
		}
	}
}
