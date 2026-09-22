package telegram

import (
	"context"
	"encoding/json"
	"github.com/go-telegram/bot"
	"io"
	"net/http"
	"net/http/httptest"
	"telegram-expense-tracker/internal/user"
	"testing"
)

func TestBotSenderPreservesPlainTextAndButtons(t *testing.T) {
	var payload map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		payload = map[string]json.RawMessage{}
		for key, values := range r.Form {
			if len(values) > 0 {
				if key == "reply_markup" {
					payload[key] = json.RawMessage(values[0])
				} else {
					payload[key], _ = json.Marshal(values[0])
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1}}`))
	}))
	defer server.Close()
	client, err := bot.New("123:fake", bot.WithSkipGetMe(), bot.WithServerURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err = (BotSender{Bot: client}).SendButtons(context.Background(), 42, "<b>Literal name</b>", [][]user.Button{{{Text: "✅ Save", Data: "e:save:1:2:"}}}); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["parse_mode"]; exists {
		t.Fatal("user text must not be interpreted")
	}
	var text string
	if err = json.Unmarshal(payload["text"], &text); err != nil || text != "<b>Literal name</b>" {
		t.Fatal("text changed")
	}
	var markup struct {
		InlineKeyboard [][]user.Button `json:"inline_keyboard"`
	}
	if err = json.Unmarshal(payload["reply_markup"], &markup); err != nil || len(markup.InlineKeyboard) != 1 || markup.InlineKeyboard[0][0].Data != "e:save:1:2:" {
		t.Fatal("keyboard lost")
	}
}

func TestBotSenderDocument(t *testing.T) {
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		file, header, err := r.FormFile("document")
		if err != nil {
			t.Error(err)
		} else {
			defer file.Close()
			got, _ = io.ReadAll(file)
			if header.Filename != "expenses.xlsx" {
				t.Error(header.Filename)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"},"date":1}}`))
	}))
	defer server.Close()
	client, err := bot.New("123:fake", bot.WithSkipGetMe(), bot.WithServerURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err = (BotSender{Bot: client}).SendDocument(context.Background(), 42, "expenses.xlsx", "Report", []byte("test-file")); err != nil {
		t.Fatal(err)
	}
	if string(got) != "test-file" {
		t.Fatal("document not uploaded")
	}
}
