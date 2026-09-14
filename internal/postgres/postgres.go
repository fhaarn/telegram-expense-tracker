// Package postgres owns database connections and, later, expense persistence.
package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["application_name"] = "telegram-expense-tracker"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("could not create PostgreSQL pool")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL connection check failed")
	}
	return pool, nil
}
