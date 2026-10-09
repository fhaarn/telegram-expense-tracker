package telegram

import (
	"errors"
	"fmt"
	"github.com/go-telegram/bot"
	"testing"
	"time"
)

func TestBroadcastFailure(t *testing.T) {
	for _, tc := range []struct {
		err       error
		code      string
		retry     time.Duration
		permanent bool
	}{
		{nil, "", 0, false}, {errors.New("network"), "temporary_failure", 0, false},
		{fmt.Errorf("wrapped: %w", &bot.TooManyRequestsError{RetryAfter: 30}), "rate_limited", 30 * time.Second, false},
		{fmt.Errorf("wrapped: %w", bot.ErrorForbidden), "forbidden", 0, true},
		{bot.ErrorBadRequest, "invalid_recipient", 0, true}, {bot.ErrorUnauthorized, "unauthorized", 0, true},
	} {
		code, retry, permanent := broadcastFailure(tc.err)
		if code != tc.code || retry != tc.retry || permanent != tc.permanent {
			t.Fatal(code, retry, permanent)
		}
	}
}
