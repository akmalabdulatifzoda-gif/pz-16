package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/akmalabdulatifzoda-gif/pz-16/internal/db"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/httpapi"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/repo"
	"github.com/akmalabdulatifzoda-gif/pz-16/internal/service"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://test:test@localhost:54321/notes_test?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn, "")
	if err != nil {
		log.Error("connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	applied, err := db.ApplyMigrations(ctx, pool)
	if err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}
	log.Info("migrations applied", "versions", applied)

	svc := &service.Service{Notes: repo.NoteRepo{DB: pool}}
	h := httpapi.Router{Svc: svc, Ping: pool.Ping, Log: log}.Handler()
	srv := &http.Server{Addr: ":8080", Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}
