package telegram

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"telegram-expense-tracker/internal/postgres"
)

func broadcastFailure(err error) (string, time.Duration, bool) {
	if err == nil {
		return "", 0, false
	}
	var limited *bot.TooManyRequestsError
	if errors.As(err, &limited) {
		return "rate_limited", time.Duration(max(1, limited.RetryAfter)) * time.Second, false
	}
	if errors.Is(err, bot.ErrorForbidden) {
		return "forbidden", 0, true
	}
	if errors.Is(err, bot.ErrorBadRequest) || errors.Is(err, bot.ErrorNotFound) {
		return "invalid_recipient", 0, true
	}
	if errors.Is(err, bot.ErrorUnauthorized) {
		return "unauthorized", 0, true
	}
	return "temporary_failure", 0, false
}

// RunBroadcasts is a separate, paced worker; no polling when its queue is empty.
func RunBroadcasts(ctx context.Context, q postgres.BroadcastQueue, s Sender, wake <-chan struct{}, logger *slog.Logger) {
	for ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		d, found, err := q.Claim(queryCtx)
		cancel()
		if err == nil && found {
			sendCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			sendErr := s.Send(sendCtx, d.ChatID, d.Message)
			stop()
			failure, retry, permanent := broadcastFailure(sendErr)
			saveCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = q.Finish(saveCtx, d, failure, retry, permanent)
			stop()
			if sendErr != nil {
				logger.Warn("announcement delivery failed", "delivery_id", d.ID, "attempt", d.Attempts, "failure", failure)
			}
			if err == nil {
				continue
			}
		}
		wait := 30 * time.Second
		pending := true
		if err == nil {
			queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			wait, pending, err = q.Next(queryCtx)
			cancel()
		}
		if err != nil {
			logger.Warn("announcement queue temporarily unavailable")
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
