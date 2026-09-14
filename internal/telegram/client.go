// Package telegram will translate Telegram messages and callbacks into application actions.
package telegram

import "github.com/go-telegram/bot"

// NewClient constructs the SDK client without registering a webhook or starting polling.
// The token is verified by Telegram only when an API request is made.
func NewClient(token string) (*bot.Bot, error) {
	return bot.New(token, bot.WithSkipGetMe())
}
