// Command dev-admin is a development-only tool that promotes a user to a
// given platform role (default: super_admin). It connects directly to the
// database using DATABASE_URL and runs a single UPDATE on the users table
// with role='super_admin' to bypass RLS.
//
// Usage:
//
//      DATABASE_URL=... go run ./cmd/dev-admin -email alice@example.ci
//      DATABASE_URL=... go run ./cmd/dev-admin -email alice@example.ci -role admin
//
// This binary is NOT included in production builds; it exists purely so we
// can test admin-only endpoints (POST /api/shops, POST /api/shops/{id}/suspend,
// etc.) against the live Neon database without a separate admin console.
package main

import (
        "context"
        "flag"
        "fmt"
        "log"
        "os"
        "time"

        "github.com/jackc/pgx/v5/pgxpool"
)

func main() {
        email := flag.String("email", "", "email of the user to promote (required)")
        role := flag.String("role", "super_admin", "new role: super_admin | admin | owner | employee")
        demote := flag.Bool("demote", false, "demote to 'owner' instead of promoting")
        flag.Parse()

        if *email == "" {
                log.Fatal("-email is required")
        }

        dbURL := os.Getenv("DATABASE_URL")
        if dbURL == "" {
                log.Fatal("DATABASE_URL is required")
        }

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        pool, err := pgxpool.New(ctx, dbURL)
        if err != nil {
                log.Fatalf("connect db: %v", err)
        }
        defer pool.Close()

        targetRole := *role
        if *demote {
                targetRole = "owner"
        }

        // Validate role.
        switch targetRole {
        case "super_admin", "admin", "owner", "employee":
        default:
                log.Fatalf("invalid role %q", targetRole)
        }

        // Run the UPDATE with role='super_admin' to bypass RLS.
        tx, err := pool.Begin(ctx)
        if err != nil {
                log.Fatalf("begin tx: %v", err)
        }
        defer tx.Rollback(ctx) //nolint:errcheck

        if _, err := tx.Exec(ctx, "SELECT set_config('app.user_role', 'super_admin', true)"); err != nil {
                log.Fatalf("set tenant: %v", err)
        }

        var (
                userID  string
                current string
        )
        err = tx.QueryRow(ctx, `
                UPDATE users SET role = $2, updated_at = now()
                 WHERE email = $1 AND deleted_at IS NULL
             RETURNING id, role::text
        `, *email, targetRole).Scan(&userID, &current)
        if err != nil {
                log.Fatalf("update user: %v", err)
        }

        if err := tx.Commit(ctx); err != nil {
                log.Fatalf("commit: %v", err)
        }

        fmt.Printf("✅ User %s (id=%s) is now role=%s\n", *email, userID, current)
}
