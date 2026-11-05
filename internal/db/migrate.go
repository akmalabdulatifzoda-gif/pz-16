// Package db — подключение к PostgreSQL и применение миграций.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect открывает пул. schema != "" — все запросы идут в эту схему
// (search_path); так каждый интеграционный тест получает собственную копию таблиц.
func Connect(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	if schema != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Migration — один файл migrations/NNN_name.sql.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations возвращает встроенные миграции по возрастанию версии.
func Migrations() ([]Migration, error) {
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, f := range files {
		base := strings.TrimPrefix(f, "migrations/")
		num, _, ok := strings.Cut(base, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("bad migration name %q", base)
		}
		b, err := migrationsFS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: base, SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// ApplyMigrations применяет ещё не выполненные миграции, каждую в своей транзакции,
// и записывает версию в schema_migrations. Повторный вызов ничего не делает.
// pg_advisory_xact_lock не даёт двум процессам применять миграции одновременно.
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) ([]int, error) {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INT PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	ms, err := Migrations()
	if err != nil {
		return nil, err
	}
	var applied []int
	for _, m := range ms {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return applied, err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(727274)`); err != nil {
			_ = tx.Rollback(ctx)
			return applied, err
		}
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.Version).Scan(&done); err != nil {
			_ = tx.Rollback(ctx)
			return applied, err
		}
		if done {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("migration %s: %w", m.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name); err != nil {
			_ = tx.Rollback(ctx)
			return applied, err
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, err
		}
		applied = append(applied, m.Version)
	}
	return applied, nil
}
