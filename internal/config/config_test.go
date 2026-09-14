package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db?sslmode=disable", "TELEGRAM_BOT_TOKEN": "test-token", "TELEGRAM_ALLOWED_USER_ID": "123", "TELEGRAM_WEBHOOK_SECRET": "test_secret"}
	tests := []struct {
		name, key, value string
		bad              bool
	}{
		{name: "defaults"},
		{"bad port", "PORT", "0", true},
		{"missing token", "TELEGRAM_BOT_TOKEN", "", true},
		{"bad owner", "TELEGRAM_ALLOWED_USER_ID", "-1", true},
		{"bad timezone", "TZ", "invalid/zone", true},
		{"bad currency", "DEFAULT_CURRENCY", "USD", true},
		{"bad secret", "TELEGRAM_WEBHOOK_SECRET", "spaces forbidden", true},
		{"bad database", "DATABASE_URL", "https://secret-password@example.com/db", true},
		{"remote insecure", "DATABASE_URL", "postgres://u:secret-password@db.example.com/db?sslmode=disable", true},
		{"remote secure", "DATABASE_URL", "postgres://u:p@db.example.com/db?sslmode=verify-full", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(func(k string) string {
				if k == tt.key {
					return tt.value
				}
				return base[k]
			})
			if (err != nil) != tt.bad {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-password") {
				t.Fatal("credential leaked")
			}
			if err == nil && (cfg.Currency != "IDR" || cfg.Timezone.String() != "Asia/Jakarta" || cfg.Port != "8080") {
				t.Fatal("incorrect defaults")
			}
		})
	}
}
