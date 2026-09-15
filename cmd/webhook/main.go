// Command webhook explicitly registers the deployed HTTPS endpoint with Telegram.
package main

import (
	"context"
	"github.com/go-telegram/bot"
	"log"
	"net/url"
	"os"
	"strings"
	"telegram-expense-tracker/internal/config"
	"telegram-expense-tracker/internal/telegram"
	"time"
)

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	endpoint := os.Getenv("TELEGRAM_WEBHOOK_URL")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, "/telegram/webhook") {
		log.Fatal("TELEGRAM_WEBHOOK_URL must be an HTTPS endpoint ending in /telegram/webhook")
	}
	client, err := telegram.NewClient(cfg.BotToken)
	if err != nil {
		log.Fatal("Telegram client initialization failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ok, err := client.SetWebhook(ctx, &bot.SetWebhookParams{URL: endpoint, SecretToken: cfg.WebhookSecret, MaxConnections: 1, AllowedUpdates: []string{"message", "callback_query"}})
	if err != nil || !ok {
		log.Fatal("webhook registration failed; check bot token and public HTTPS endpoint")
	}
	log.Print("webhook registered; pending updates preserved")
}
