package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"github.com/go-telegram/bot/models"
	"io"
	"net/http"
	"telegram-expense-tracker/internal/user"
)

type Acceptor interface {
	Accept(context.Context, user.Message) error
}

func Webhook(secret string, store Acceptor, wake func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), []byte(secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid or oversized update", http.StatusRequestEntityTooLarge)
			return
		}
		var u models.Update
		if err = json.Unmarshal(body, &u); err != nil {
			http.Error(w, "invalid update", http.StatusBadRequest)
			return
		}
		var envelope map[string]json.RawMessage
		_ = json.Unmarshal(body, &envelope)
		if _, ok := envelope["update_id"]; !ok || string(envelope["update_id"]) == "null" || u.ID < 0 {
			http.Error(w, "missing update ID", http.StatusBadRequest)
			return
		}
		var input user.Message
		if q := u.CallbackQuery; q != nil {
			var chat models.Chat
			if q.Message.Message != nil {
				chat = q.Message.Message.Chat
			} else if q.Message.InaccessibleMessage != nil {
				chat = q.Message.InaccessibleMessage.Chat
			}
			if q.From.IsBot || q.From.ID <= 0 || chat.Type != models.ChatTypePrivate || chat.ID != q.From.ID || len(q.Data) > 64 {
				w.WriteHeader(http.StatusOK)
				return
			}
			input = user.Message{UpdateID: u.ID, TelegramID: q.From.ID, ChatID: chat.ID, Username: q.From.Username, Callback: q.Data}
		} else {
			m := u.Message
			if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != models.ChatTypePrivate || m.From.ID <= 0 || m.Chat.ID != m.From.ID {
				w.WriteHeader(http.StatusOK)
				return
			}
			input = user.Message{UpdateID: u.ID, TelegramID: m.From.ID, ChatID: m.Chat.ID, Username: m.From.Username, Text: m.Text, Image: len(m.Photo) > 0 || m.Document != nil}
		}

		if err = store.Accept(r.Context(), input); err != nil {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if wake != nil {
			wake()
		}
		if u.CallbackQuery != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"method": "answerCallbackQuery", "callback_query_id": u.CallbackQuery.ID})
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
