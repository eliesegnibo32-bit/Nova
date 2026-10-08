// Package db wraps the pgx connection pool and goose migrations runner.
//
// The pool is shared across HTTP handlers. Each request that needs DB access
// either acquires a connection directly, or (recommended for shop-scoped
// work) begins a transaction via WithTx/WithTenantTx which sets the
// app.current_shop_id / app.user_id / app.user_role session variables so
// PostgreSQL RLS policies enforce multi-tenant isolation (see migrations
// 011_rls_policies.up.sql).
package db

import (
        "context"
        "database/sql"
        "fmt"
        "io/fs"
        "time"

        // Register pgx as a database/sql driver so goose (which uses database/sql)
        // can open connections via "pgx" driver name.
        _ "github.com/jackc/pgx/v5/stdlib"
        "github.com/jackc/pgx/v5/pgxpool"
        "github.com/pressly/goose/v3"

        novaapi "nova-api"
)

// NewPool creates a pgxpool.Pool with sensible defaults tuned for a small
// SaaS API (matches cahier des charges ch.9.1 — pgxpool + RLS).
//
//   - MaxConns: 25  (PG default max_connections ~100, leaves headroom)
//   - MinConns: 5
//   - MaxConnLifetime: 1h (Neon recycles idle serverless endpoints)
//   - MaxConnIdleTime: 30m
//   - HealthCheckPeriod: 1m
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
        if databaseURL == "" {
                return nil, fmt.Errorf("DATABASE_URL is empty")
        }

        cfg, err := pgxpool.ParseConfig(databaseURL)
        if err != nil {
                return nil, fmt.Errorf("parse database url: %w", err)
        }

        cfg.MaxConns = 25
        cfg.MinConns = 5
        cfg.MaxConnLifetime = 1 * time.Hour
        cfg.MaxConnIdleTime = 30 * time.Minute
        cfg.HealthCheckPeriod = 1 * time.Minute

        pool, err := pgxpool.NewWithConfig(ctx, cfg)
        if err != nil {
                return nil, fmt.Errorf("create pgxpool: %w", err)
        }

        // Verify connectivity up-front so we fail fast on bad URLs.
        pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()
        if err := pool.Ping(pingCtx); err != nil {
                pool.Close()
                return nil, fmt.Errorf("ping database: %w", err)
        }

        return pool, nil
}

// RunMigrations applies all embedded goose migrations in the "up" direction.
//
// Uses goose v3's modern Provider API which correctly handles up/down file
// pairs (NNN_name.up.sql + NNN_name.down.sql) from an embed.FS.
//
// goose requires a *sql.DB, so we open a transient database/sql connection
// via the pgx stdlib driver on the same DATABASE_URL.
func RunMigrations(ctx context.Context, databaseURL string) error {
        if databaseURL == "" {
                return fmt.Errorf("DATABASE_URL is empty; cannot run migrations")
        }

        db, err := sql.Open("pgx", databaseURL)
        if err != nil {
                return fmt.Errorf("open sql.DB for migrations: %w", err)
        }
        defer db.Close()

        if err := db.PingContext(ctx); err != nil {
                return fmt.Errorf("ping before migrate: %w", err)
        }

        // Create a sub-FS rooted at "migrations" so the provider sees the .sql
        // files directly (without the leading "migrations/" path component).
        migrationsFS, err := fs.Sub(novaapi.MigrationsFS, "migrations")
        if err != nil {
                return fmt.Errorf("create sub FS for migrations: %w", err)
        }

        provider, err := goose.NewProvider(
                goose.DialectPostgres,
                db,
                migrationsFS,
                goose.WithVerbose(false),
        )
        if err != nil {
                return fmt.Errorf("create goose provider: %w", err)
        }

        res, err := provider.Up(ctx)
        if err != nil {
                return fmt.Errorf("goose up: %w", err)
        }
        _ = res // migrations applied
        return nil
}
