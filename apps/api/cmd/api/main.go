package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/httpapi"
	"example.com/computer-network/project/apps/api/internal/realtime"
	"example.com/computer-network/project/apps/api/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		log.Fatalf("database startup failed: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db.Pool); err != nil {
		log.Fatal("database migration failed; check PostgreSQL and migration files")
	}
	authService, err := auth.New(db)
	if err != nil {
		log.Fatal("authentication initialization failed")
	}
	hub := realtime.New(authService, chat.New(db), cfg.AllowedOrigins)
	authService.SetRevocationNotifier(hub)
	server := &http.Server{Addr: cfg.ListenAddr(), Handler: httpapi.NewHandler(db, authService, cfg, hub), ReadHeaderTimeout: 5 * time.Second}
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signals.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = hub.Shutdown(shutdownCtx)
	}()
	log.Printf("API listening on %s; database %s", cfg.ListenAddr(), cfg.DatabaseTarget)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := hub.Shutdown(shutdownCtx); err != nil {
		log.Printf("WebSocket shutdown: %v", err)
	}
}
