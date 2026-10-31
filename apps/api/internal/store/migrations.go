package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int64
	name     string
	sql      string
	checksum [32]byte
}

var migrationName = regexp.MustCompile(`^([0-9]+)_[a-z0-9_]+\.sql$`)

func embeddedMigrations() ([]migration, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no embedded migrations")
	}
	sort.Strings(names)
	result := make([]migration, 0, len(names))
	for _, name := range names {
		match := migrationName.FindStringSubmatch(name[len("migrations/"):])
		if match == nil {
			return nil, fmt.Errorf("invalid migration filename: %s", name)
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version: %s", name)
		}
		if len(result) > 0 && version <= result[len(result)-1].version {
			return nil, fmt.Errorf("duplicate or unordered migration version: %s", name)
		}
		data, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		result = append(result, migration{version: version, name: name, sql: string(data), checksum: sha256.Sum256(data)})
	}
	return result, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	return applyMigrations(ctx, pool, migrations)
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, migrations []migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer tx.Rollback(context.Background())
	// The lock also serializes creation of schema_migrations on fresh databases.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(0x434841545f4d4947)); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return fmt.Errorf("inspect migration table: %w", err)
	}
	if !exists {
		if _, err := tx.Exec(ctx, `CREATE TABLE schema_migrations (
			version bigint CONSTRAINT schema_migrations_pkey PRIMARY KEY,
			checksum bytea NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
			return fmt.Errorf("create migration table: %w", err)
		}
	}
	rows, err := tx.Query(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read migration versions: %w", err)
	}
	applied := map[int64][]byte{}
	for rows.Next() {
		var version int64
		var checksum []byte
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("read migration version: %w", err)
		}
		applied[version] = checksum
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read migration versions: %w", err)
	}
	known := map[int64]bool{}
	for _, m := range migrations {
		known[m.version] = true
		if checksum, ok := applied[m.version]; ok {
			if string(checksum) != string(m.checksum[:]) {
				return fmt.Errorf("migration %d checksum differs from applied version", m.version)
			}
			continue
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", m.version, m.checksum[:]); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("database has unknown migration %d", version)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
