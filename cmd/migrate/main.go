package main

import (
	"context"
	"log"
	"os"
	"telegram-expense-tracker/internal/config"
	"telegram-expense-tracker/internal/postgres"
	"telegram-expense-tracker/migrations"
	"time"
)

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("database connection failed")
	}
	defer pool.Close()
	if err = migrations.Apply(ctx, pool); err != nil {
		log.Fatal("migration failed; verify migration version and database access")
	}
	log.Print("migrations applied")
}
