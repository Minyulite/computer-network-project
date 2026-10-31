package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/httpapi"
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
	server := &http.Server{Addr: cfg.ListenAddr(), Handler: httpapi.NewHandler(db, authService, cfg), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("API listening on %s; database %s", cfg.ListenAddr(), cfg.DatabaseTarget)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
