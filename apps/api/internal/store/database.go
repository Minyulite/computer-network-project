package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string, maxConns int32) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	if maxConns < 1 {
		return nil, fmt.Errorf("database pool size must be positive")
	}
	config.MaxConns = maxConns
	// Serverless hosts such as Neon suspend idle connections after a few minutes; retire ours first.
	config.MaxConnIdleTime = 4 * time.Minute
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	db := &DB{Pool: pool}
	// A suspended serverless database can take several seconds to wake, so startup uses the caller's deadline.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database unavailable")
	}
	return db, nil
}

func (db *DB) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return db.Pool.Ping(ctx)
}

func (db *DB) Close() { db.Pool.Close() }
