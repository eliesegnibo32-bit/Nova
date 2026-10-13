// Transaction and tenant-context helpers for pgx.
//
// The RLS strategy (migrations/011_rls_policies.up.sql) requires the Go
// application to set three session variables at the start of every
// transaction that touches tenant-scoped tables:
//
//      SET LOCAL app.current_shop_id = '<uuid>';
//      SET LOCAL app.user_id         = '<uuid>';
//      SET LOCAL app.user_role       = 'owner';   -- or 'super_admin' / 'admin'
//
// `SET LOCAL` scopes the value to the current transaction, eliminating the
// risk of leakage between requests when the pool reuses a connection.
package db

import (
        "context"
        "fmt"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"
)

// SetTenantContext sets the three RLS session variables on the given
// transaction. shopID may be nil for platform-admin operations that span
// all shops. userID must always be set (use a zero UUID only in truly
// anonymous flows like the WhatsApp webhook pre-auth).
func SetTenantContext(ctx context.Context, tx pgx.Tx, shopID *uuid.UUID, userID uuid.UUID, role string) error {
        if role == "" {
                role = "employee"
        }

        // set_config(name, value, is_local) is the parameterized equivalent of
        // SET LOCAL. PostgreSQL's SET LOCAL does NOT accept bind parameters ($1),
        // so we use set_config() which does. is_local=true scopes the setting to
        // the current transaction (same as SET LOCAL).
        shopVal := ""
        if shopID != nil {
                shopVal = shopID.String()
        }

        if _, err := tx.Exec(ctx, "SELECT set_config('app.current_shop_id', $1, true)", shopVal); err != nil {
                return fmt.Errorf("set app.current_shop_id: %w", err)
        }
        if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", userID.String()); err != nil {
                return fmt.Errorf("set app.user_id: %w", err)
        }
        if _, err := tx.Exec(ctx, "SELECT set_config('app.user_role', $1, true)", role); err != nil {
                return fmt.Errorf("set app.user_role: %w", err)
        }
        return nil
}

// WithTx runs fn inside a transaction, committing on nil error and rolling
// back on any error (including panics). The pgxpool.Pool acquires a
// connection for the lifetime of the transaction.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) (err error) {
        tx, beginErr := pool.BeginTx(ctx, pgx.TxOptions{})
        if beginErr != nil {
                return fmt.Errorf("begin tx: %w", beginErr)
        }

        // Defer a recover so a panic inside fn is converted to an error and
        // triggers a clean rollback rather than a leaked transaction.
        defer func() {
                switch p := recover(); {
                case p != nil:
                        _ = tx.Rollback(ctx)
                        err = fmt.Errorf("panic in transaction: %v", p)
                case err == nil:
                        if commitErr := tx.Commit(ctx); commitErr != nil {
                                err = fmt.Errorf("commit tx: %w", commitErr)
                        }
                default:
                        // err is non-nil — rollback. If rollback also fails we surface
                        // the original error (already meaningful) and discard the
                        // rollback error.
                        _ = tx.Rollback(ctx)
                }
        }()

        if err = fn(tx); err != nil {
                err = fmt.Errorf("tx body: %w", err)
        }
        return err
}

// WithTenantTx is the recommended helper for any handler that touches
// shop-scoped tables. It begins a transaction, sets the RLS session
// variables, then calls fn. On fn error the transaction is rolled back; on
// success it is committed. See WithTx for the exact semantics.
func WithTenantTx(
        ctx context.Context,
        pool *pgxpool.Pool,
        shopID *uuid.UUID,
        userID uuid.UUID,
        role string,
        fn func(pgx.Tx) error,
) error {
        return WithTx(ctx, pool, func(tx pgx.Tx) error {
                if err := SetTenantContext(ctx, tx, shopID, userID, role); err != nil {
                        return err
                }
                return fn(tx)
        })
}
