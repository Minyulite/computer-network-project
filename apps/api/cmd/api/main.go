package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"example.com/computer-network/project/apps/api/internal/ai/botai"
	"example.com/computer-network/project/apps/api/internal/ai/grammar"
	"example.com/computer-network/project/apps/api/internal/ai/llm"
	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/chat"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/httpapi"
	"example.com/computer-network/project/apps/api/internal/realtime"
	"example.com/computer-network/project/apps/api/internal/store"
)

const (
	startupTimeout  = 20 * time.Second
	shutdownTimeout = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run wires the services together and serves until SIGINT or SIGTERM. It
// returns instead of exiting so deferred cleanup always runs.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	startCtx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	db, err := store.Open(startCtx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return fmt.Errorf("database startup failed: %w", err)
	}
	defer db.Close()
	if err := store.Migrate(startCtx, db.Pool); err != nil {
		return errors.New("database migration failed; check PostgreSQL and migration files")
	}

	authService, err := auth.New(db)
	if err != nil {
		return errors.New("authentication initialization failed")
	}
	ai := llm.New(llm.Config{APIKey: cfg.GeminiAPIKey, Model: cfg.GeminiModel, Fallbacks: cfg.GeminiFallbackModels})
	if ai.Configured() {
		log.Printf("AI features enabled; model %s, fallbacks %v", cfg.GeminiModel, cfg.GeminiFallbackModels)
	} else {
		log.Print("GEMINI_API_KEY is not set; grammar correction and bot answers are disabled")
	}
	chatService := chat.New(db)
	hub := realtime.New(authService, chatService, grammar.New(ai), botai.New(ai, chatService), cfg.AllowedOrigins)
	authService.SetRevocationNotifier(hub)

	server := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           httpapi.NewHandler(httpapi.Deps{DB: db, Auth: authService, Config: cfg, WebSocket: hub}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signals.Done()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = hub.Shutdown(ctx)
	}()

	log.Printf("API listening on %s; database %s", cfg.ListenAddr(), cfg.DatabaseTarget)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	ctx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	if err := hub.Shutdown(ctx); err != nil {
		log.Printf("WebSocket shutdown: %v", err)
	}
	return nil
}
