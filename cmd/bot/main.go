package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"telegram-expense-tracker/internal/config"
	"telegram-expense-tracker/internal/httpserver"
	"telegram-expense-tracker/internal/postgres"
	"telegram-expense-tracker/internal/telegram"
	"telegram-expense-tracker/migrations"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	migrationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = migrations.Apply(migrationCtx, pool)
	cancel()
	if err != nil {
		return errors.New("database migration failed")
	}
	client, err := telegram.NewClient(cfg.BotToken)
	if err != nil {
		return errors.New("Telegram client initialization failed")
	}
	wake := make(chan struct{}, 1)
	notify := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	store := &postgres.Onboarding{Pool: pool, Timezone: cfg.Timezone.String(), Currency: cfg.Currency, BotUsername: cfg.BotUsername}
	server := httpserver.New(cfg.Port, pool, telegram.Webhook(cfg.WebhookSecret, store, notify))
	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		telegram.RunDelivery(workerCtx, postgres.Outbox{Pool: pool}, telegram.BotSender{Bot: client}, wake, logger)
	}()
	defer func() { stopWorker(); <-workerDone }()
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	logger.Info("HTTP server starting", "port", cfg.Port, "feature", "phase 1 expense tracker")
	select {
	case err := <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server failed")
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return errors.New("HTTP shutdown timed out")
		}
	}
	logger.Info("shutdown complete")
	return nil
}
