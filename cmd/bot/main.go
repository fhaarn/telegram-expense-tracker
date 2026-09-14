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
	server := httpserver.New(cfg.Port, pool)
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	logger.Info("HTTP server starting", "port", cfg.Port, "telegram_handlers", "not implemented")
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
