package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	report "telegram-expense-tracker/internal/export"
	"telegram-expense-tracker/internal/postgres"
	"telegram-expense-tracker/internal/user"
	"time"
)

type Sender interface {
	Send(context.Context, int64, string) error
}
type BotSender struct{ Bot *bot.Bot }

func (s BotSender) Send(ctx context.Context, chatID int64, text string) error {
	_, err := s.Bot.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
	return err // No parse mode: user names are displayed literally.
}

type keyboardSender interface {
	SendButtons(context.Context, int64, string, [][]user.Button) error
}

func (s BotSender) SendButtons(ctx context.Context, chatID int64, text string, buttons [][]user.Button) error {
	rows := make([][]models.InlineKeyboardButton, 0, len(buttons))
	for _, row := range buttons {
		r := []models.InlineKeyboardButton{}
		for _, b := range row {
			r = append(r, models.InlineKeyboardButton{Text: b.Text, CallbackData: b.Data})
		}
		rows = append(rows, r)
	}
	params := &bot.SendMessageParams{ChatID: chatID, Text: text}
	if len(rows) > 0 {
		params.ReplyMarkup = &models.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	_, err := s.Bot.SendMessage(ctx, params)
	return err
}

// RunDelivery sleeps without database polling when the outbox is empty. A webhook
// wakes it; startup drains replies committed before a process restart.
func RunDelivery(ctx context.Context, o postgres.Outbox, s Sender, wake <-chan struct{}, logger *slog.Logger) {
	for ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		d, found, err := o.Claim(queryCtx)
		cancel()
		if err == nil && found {
			sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			var sendErr error
			if len(d.ExportPayload) > 0 {
				var r report.Report
				sendErr = json.Unmarshal(d.ExportPayload, &r)
				if sendErr == nil {
					var data []byte
					data, sendErr = report.Build(r)
					if sendErr == nil {
						if sender, ok := s.(documentSender); ok {
							sendErr = sender.SendDocument(sendCtx, d.ChatID, r.Filename(), d.Body, data)
						} else {
							sendErr = errors.New("document sender unavailable")
						}
					}
				}
			} else if rich, ok := s.(keyboardSender); ok {
				sendErr = rich.SendButtons(sendCtx, d.ChatID, d.Body, d.Buttons)
			} else {
				sendErr = s.Send(sendCtx, d.ChatID, d.Body)
			}
			cancel()
			saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = o.Finish(saveCtx, d, sendErr)
			cancel()
			if sendErr != nil {
				logger.Warn("Telegram reply failed", "delivery_id", d.ID, "attempt", d.Attempts)
			}
			if err == nil {
				continue
			}
		}
		wait := 30 * time.Second
		pending := true
		if err == nil {
			queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			wait, pending, err = o.Next(queryCtx)
			cancel()
		}
		if err != nil {
			logger.Warn("outbox temporarily unavailable")
			wait = 30 * time.Second
			pending = true
		}
		if !pending {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

type documentSender interface {
	SendDocument(context.Context, int64, string, string, []byte) error
}

func (s BotSender) SendDocument(ctx context.Context, chatID int64, filename, caption string, data []byte) error {
	_, err := s.Bot.SendDocument(ctx, &bot.SendDocumentParams{ChatID: chatID, Document: &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)}, Caption: caption})
	return err
}
