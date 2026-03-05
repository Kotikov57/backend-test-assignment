package app

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"backend-test-assignment/internal/config"
	apihttp "backend-test-assignment/internal/http"
	"backend-test-assignment/internal/store"
	"backend-test-assignment/internal/withdrawal"

	_ "github.com/lib/pq"
)

type App struct {
	db      *sql.DB
	handler http.Handler
}

func New(cfg config.Config) (*App, error) {
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	repository := store.NewWithdrawalRepository(db)
	service := withdrawal.NewService(repository)
	handler := apihttp.NewHandler(service, cfg.AuthToken)

	return &App{db: db, handler: handler}, nil
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) Close() error {
	return a.db.Close()
}
