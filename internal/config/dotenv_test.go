package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotEnvPrecedence(t *testing.T) {
	const added = "EXPENSE_TEST_DOTENV_ADDED"
	const existing = "EXPENSE_TEST_DOTENV_EXISTING"
	const empty = "EXPENSE_TEST_DOTENV_EMPTY"
	t.Setenv(added, "")
	if err := os.Unsetenv(added); err != nil {
		t.Fatal(err)
	}
	t.Setenv(existing, "from-host")
	t.Setenv(empty, "")
	path := filepath.Join(t.TempDir(), ".env")
	text := added + "=\"local value # with spaces\"\n" + existing + "=from-file\n" + empty + "=from-file\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(added) != "local value # with spaces" {
		t.Fatal("file value was not loaded")
	}
	if os.Getenv(existing) != "from-host" {
		t.Fatal("host environment was overwritten")
	}
	if value, present := os.LookupEnv(empty); !present || value != "" {
		t.Fatal("explicit empty value was overwritten")
	}
}
func TestDotEnvMissingAndMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := LoadDotEnv(filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("SECRET='private-value-without-closing-quote"), 0600); err != nil {
		t.Fatal(err)
	}
	err := LoadDotEnv(path)
	if err == nil {
		t.Fatal("expected malformed-file error")
	}
	if strings.Contains(err.Error(), "private-value") {
		t.Fatal("error leaked file contents")
	}
	if err = LoadDotEnv(dir); err == nil {
		t.Fatal("expected unreadable-file error")
	}
}
