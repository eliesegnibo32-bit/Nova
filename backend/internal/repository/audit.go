// Audit log repository — writes structured audit events to `audit_logs`.
//
// The audit_logs table (migration 010) is append-only: a trigger
// (`audit_logs_append_only`) blocks UPDATE and DELETE unless the session
// variable `app.allow_audit_mutation = 'true'` is set (which the application
// never sets outside of one-off admin operations). All inserts go through
// this repository so the audit shape stays consistent.
//
// RLS: audit_logs has policies for SELECT and INSERT that allow
// `is_platform_admin()` or `shop_id = current_shop_id()`. The audit
// repository inserts with `app.user_role = 'super_admin'` so it can write
// platform-level events (shop_id = NULL) like login failures that happen
// before a shop context exists.
package repository

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "net/netip"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
)

// AuditEntry is the input shape for a single audit event. Before/After are
// JSON snapshots of the affected object (may be nil for actions like
// `auth.login` that don't mutate a row).
type AuditEntry struct {
        ShopID     *uuid.UUID     // NULL = platform-level event
        ActorID    *uuid.UUID     // NULL = anonymous (e.g. failed login with unknown email)
        ActorRole  string         // role at the time of the action
        Action     string         // dotted identifier: "auth.login.success", "shop.create", ...
        ObjectType string         // "user", "shop", "order", ...
        ObjectID   *uuid.UUID     // NULL when not applicable
        Before     json.Marshaler // optional pre-state; nil = no snapshot
        After      json.Marshaler // optional post-state; nil = no snapshot
        IPAddress  string         // raw IP string ("192.0.2.1") or ""
        UserAgent  string         // raw UA header or ""
}

// AuditLog is the persisted record (as read back from the DB).
type AuditLog struct {
        ID         uuid.UUID  `json:"id"`
        ShopID     *uuid.UUID `json:"shop_id,omitempty"`
        ActorID    *uuid.UUID `json:"actor_id,omitempty"`
        ActorRole  string     `json:"actor_role,omitempty"`
        Action     string     `json:"action"`
        ObjectType string     `json:"object_type"`
        ObjectID   *uuid.UUID `json:"object_id,omitempty"`
        Before     []byte     `json:"before,omitempty"` // jsonb
        After      []byte     `json:"after,omitempty"`  // jsonb
        IPAddress  string     `json:"ip_address,omitempty"`
        UserAgent  string     `json:"user_agent,omitempty"`
        CreatedAt  time.Time  `json:"created_at"`
}

// AuditRepository wraps the audit_logs table.
type AuditRepository struct {
        pool *pgxpool.Pool
}

// NewAuditRepository returns an AuditRepository bound to the given pool.
func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
        return &AuditRepository{pool: pool}
}

// Log inserts one audit event. It never returns a non-nil error to the
// caller unless the insert itself fails — a nil ActorID or empty IP are
// tolerated (inserted as NULL/NULL). The Before/After JSON marshaling is
// done here so the caller doesn't have to.
func (r *AuditRepository) Log(ctx context.Context, e AuditEntry) error {
        var beforeArg, afterArg any
        if e.Before != nil {
                b, err := e.Before.MarshalJSON()
                if err != nil {
                        return fmt.Errorf("audit: marshal before: %w", err)
                }
                beforeArg = b
        }
        if e.After != nil {
                a, err := e.After.MarshalJSON()
                if err != nil {
                        return fmt.Errorf("audit: marshal after: %w", err)
                }
                afterArg = a
        }

        var shopArg, actorArg, objectArg any
        if e.ShopID != nil {
                shopArg = *e.ShopID
        }
        if e.ActorID != nil {
                actorArg = *e.ActorID
        }
        if e.ObjectID != nil {
                objectArg = *e.ObjectID
        }

        var ipArg any
        if e.IPAddress != "" {
                // Normalize: strip surrounding brackets from IPv6 addresses
                // (e.g. "[::1]" → "::1") so netip.ParseAddr accepts them.
                ipStr := e.IPAddress
                if len(ipStr) >= 2 && ipStr[0] == '[' && ipStr[len(ipStr)-1] == ']' {
                        ipStr = ipStr[1 : len(ipStr)-1]
                }
                // Validate so we don't insert garbage into an inet column.
                if addr, err := netip.ParseAddr(ipStr); err == nil {
                        ipArg = addr.String()
                } else {
                        // Best-effort: store as NULL if we can't parse the IP
                        // (inserting an unparseable string into an inet column
                        // would fail the whole INSERT, losing the audit event).
                        ipArg = nil
                }
        }

        return db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, "super_admin", func(tx pgx.Tx) error {
                const q = `
                        INSERT INTO audit_logs
                            (shop_id, actor_id, actor_role, action, object_type, object_id,
                             before, after, ip_address, user_agent)
                        VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::inet, $10)
                `
                _, err := tx.Exec(ctx, q,
                        shopArg, actorArg, e.ActorRole, e.Action, e.ObjectType, objectArg,
                        beforeArg, afterArg, ipArg, e.UserAgent,
                )
                if err != nil {
                        return fmt.Errorf("audit: insert: %w", err)
                }
                return nil
        })
}

// ListByShop returns the most recent audit events for a shop, paginated.
// Caller must be a member of the shop (RLS-protected).
func (r *AuditRepository) ListByShop(ctx context.Context, shopID, requesterID uuid.UUID, limit, offset int) ([]AuditLog, error) {
        if limit <= 0 || limit > 500 {
                limit = 100
        }
        if offset < 0 {
                offset = 0
        }
        var out []AuditLog
        err := db.WithTenantTx(ctx, r.pool, &shopID, requesterID, string("owner"), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, actor_id, actor_role, action, object_type, object_id,
                               before, after, ip_address::text, user_agent, created_at
                          FROM audit_logs
                         WHERE shop_id = $1
                         ORDER BY created_at DESC
                         LIMIT $2 OFFSET $3
                `
                rows, err := tx.Query(ctx, q, shopID, limit, offset)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var a AuditLog
                        if err := scanAuditLog(rows, &a); err != nil {
                                return err
                        }
                        out = append(out, a)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("audit repo: list by shop: %w", err)
        }
        return out, nil
}

// ListByActor returns the most recent audit events performed by a user.
// Used by the admin console to inspect what an employee has been doing.
func (r *AuditRepository) ListByActor(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]AuditLog, error) {
        if limit <= 0 || limit > 500 {
                limit = 100
        }
        if offset < 0 {
                offset = 0
        }
        var out []AuditLog
        err := db.WithTenantTx(ctx, r.pool, nil, actorID, "super_admin", func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, actor_id, actor_role, action, object_type, object_id,
                               before, after, ip_address::text, user_agent, created_at
                          FROM audit_logs
                         WHERE actor_id = $1
                         ORDER BY created_at DESC
                         LIMIT $2 OFFSET $3
                `
                rows, err := tx.Query(ctx, q, actorID, limit, offset)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var a AuditLog
                        if err := scanAuditLog(rows, &a); err != nil {
                                return err
                        }
                        out = append(out, a)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("audit repo: list by actor: %w", err)
        }
        return out, nil
}

// --- helpers ----------------------------------------------------------------

// auditScanner is the subset of pgx.Row / pgx.Rows used by scanAuditLog.
type auditScanner interface {
        Scan(dest ...any) error
}

func scanAuditLog(s auditScanner, a *AuditLog) error {
        var (
                shopID    *uuid.UUID
                actorID   *uuid.UUID
                actorRole *string
                objectID  *uuid.UUID
                before    []byte
                after     []byte
                ipAddr    *string
                userAgent *string
        )
        err := s.Scan(
                &a.ID,
                &shopID,
                &actorID,
                &actorRole,
                &a.Action,
                &a.ObjectType,
                &objectID,
                &before,
                &after,
                &ipAddr,
                &userAgent,
                &a.CreatedAt,
        )
        if err != nil {
                return err
        }
        a.ShopID = shopID
        a.ActorID = actorID
        if actorRole != nil {
                a.ActorRole = *actorRole
        }
        a.ObjectID = objectID
        a.Before = before
        a.After = after
        if ipAddr != nil {
                a.IPAddress = *ipAddr
        }
        if userAgent != nil {
                a.UserAgent = *userAgent
        }
        return nil
}

// IsNotFound returns true if err is the repository's ErrNotFound sentinel.
// Exported as a convenience for handlers that want to map it to 404 without
// importing errors directly.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
