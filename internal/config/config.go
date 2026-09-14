// Package config reads and validates application configuration.
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Config struct {
	Port          string
	DatabaseURL   string
	BotToken      string
	AllowedUserID int64
	WebhookSecret string
	Timezone      *time.Location
	Currency      string
}

func Load(getenv func(string) string) (Config, error) {
	c := Config{Port: getenv("PORT"), DatabaseURL: getenv("DATABASE_URL"), BotToken: getenv("TELEGRAM_BOT_TOKEN"), WebhookSecret: getenv("TELEGRAM_WEBHOOK_SECRET"), Currency: getenv("DEFAULT_CURRENCY")}
	if c.Port == "" {
		c.Port = "8080"
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be between 1 and 65535")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path == "" || u.Path == "/" {
		return Config{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with a host and database")
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		mode := u.Query().Get("sslmode")
		if mode != "verify-full" {
			return Config{}, fmt.Errorf("remote DATABASE_URL must use sslmode=verify-full")
		}
	}
	if strings.TrimSpace(c.BotToken) == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	c.AllowedUserID, err = strconv.ParseInt(getenv("TELEGRAM_ALLOWED_USER_ID"), 10, 64)
	if err != nil || c.AllowedUserID <= 0 {
		return Config{}, fmt.Errorf("TELEGRAM_ALLOWED_USER_ID must be a positive integer")
	}
	if len(c.WebhookSecret) < 1 || len(c.WebhookSecret) > 256 {
		return Config{}, fmt.Errorf("TELEGRAM_WEBHOOK_SECRET must contain 1 to 256 allowed characters")
	}
	for _, r := range c.WebhookSecret {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return Config{}, fmt.Errorf("TELEGRAM_WEBHOOK_SECRET may contain only letters, digits, underscores and hyphens")
		}
	}
	zone := getenv("TZ")
	if zone == "" {
		zone = "Asia/Jakarta"
	}
	c.Timezone, err = time.LoadLocation(zone)
	if err != nil {
		return Config{}, fmt.Errorf("TZ must name a valid timezone")
	}
	if c.Currency == "" {
		c.Currency = "IDR"
	}
	if c.Currency != "IDR" {
		return Config{}, fmt.Errorf("DEFAULT_CURRENCY currently supports only IDR")
	}
	return c, nil
}
