// Notifications repository (ch. 5.5 — migration 010).
//
// notifications: internal alerts for shop owners / employees (low_stock,
// new_order, payment_due, human_takeover_requested, ...). The AI uses this
// when escalader_vers_humain is called.
package repository

import (
        "context"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
)

// NotificationsRepository wraps the notifications table.
type NotificationsRepository struct {
        pool *pgxpool.Pool
}

// NewNotificationsRepository returns a NotificationsRepository bound to the pool.
func NewNotificationsRepository(pool *pgxpool.Pool) *NotificationsRepository {
        return &NotificationsRepository{pool: pool}
}

// Notification is a persisted notification row.
type Notification struct {
        ID          uuid.UUID  `json:"id"`
        ShopID      *uuid.UUID `json:"shop_id,omitempty"`
        RecipientID *uuid.UUID `json:"recipient_id,omitempty"`
        Type        string     `json:"type"`
        Payload     []byte     `json:"payload"`
        Read        bool       `json:"read"`
        CreatedAt   time.Time  `json:"created_at"`
        ReadAt      *time.Time `json:"read_at,omitempty"`
}

// Create inserts a notification. shopID may be nil for platform-wide
// notifications. recipientID may be nil for shop-broadcast notifications.
// payload is a JSON blob.
func (r *NotificationsRepository) Create(ctx context.Context, shopID, recipientID *uuid.UUID, notifType string, payload []byte) (*Notification, error) {
        var n Notification
        err := db.WithTenantTx(ctx, r.pool, shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                var shopArg, recipArg any
                if shopID != nil {
                        shopArg = *shopID
                }
                if recipientID != nil {
                        recipArg = *recipientID
                }
                var payloadArg any = payload
                if len(payload) == 0 {
                        payloadArg = []byte("{}")
                }
                const q = `
                        INSERT INTO notifications (shop_id, recipient_id, type, payload)
                        VALUES ($1, $2, $3, $4::jsonb)
                        RETURNING id, shop_id, recipient_id, type, payload, read, created_at, read_at
                `
                if err := tx.QueryRow(ctx, q, shopArg, recipArg, notifType, payloadArg).Scan(
                        &n.ID, &n.ShopID, &n.RecipientID, &n.Type, &n.Payload, &n.Read, &n.CreatedAt, &n.ReadAt,
                ); err != nil {
                        return fmt.Errorf("notif repo: create: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &n, nil
}

// ListByShop returns the most recent notifications for a shop.
func (r *NotificationsRepository) ListByShop(ctx context.Context, shopID uuid.UUID, limit int, onlyUnread bool) ([]Notification, error) {
        if limit <= 0 || limit > 200 {
                limit = 50
        }
        var out []Notification
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                q := `
                        SELECT id, shop_id, recipient_id, type, payload, read, created_at, read_at
                          FROM notifications
                         WHERE shop_id = $1
                `
                args := []any{shopID}
                if onlyUnread {
                        q += " AND read = false"
                }
                q += " ORDER BY created_at DESC LIMIT $2"
                args = append(args, limit)
                rows, err := tx.Query(ctx, q, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var n Notification
                        if err := rows.Scan(&n.ID, &n.ShopID, &n.RecipientID, &n.Type, &n.Payload, &n.Read, &n.CreatedAt, &n.ReadAt); err != nil {
                                return err
                        }
                        out = append(out, n)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("notif repo: list by shop: %w", err)
        }
        return out, nil
}

// MarkRead marks a notification as read.
func (r *NotificationsRepository) MarkRead(ctx context.Context, shopID, id uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `UPDATE notifications SET read = true, read_at = now() WHERE shop_id = $1 AND id = $2`
                _, err := tx.Exec(ctx, q, shopID, id)
                return err
        })
}

// ============================================================================
// Spec Task 10 — Notifications repository extensions
// ============================================================================

// ListByUser returns the most recent notifications for a specific user (as
// identified by recipient_id). Used by the merchant dashboard to surface
// alerts assigned to a given owner/employee. unreadOnly filters to read=false.
func (r *NotificationsRepository) ListByUser(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]Notification, error) {
        if limit <= 0 || limit > 200 {
                limit = 50
        }
        var out []Notification
        // Recipient-scoped notifications may live on any shop — use the platform
        // admin tenant context (the caller is the recipient, no shop scope).
        err := db.WithTenantTx(ctx, r.pool, nil, userID, "owner", func(tx pgx.Tx) error {
                q := `
                        SELECT id, shop_id, recipient_id, type, payload, read, created_at, read_at
                          FROM notifications
                         WHERE recipient_id = $1
                `
                args := []any{userID}
                if unreadOnly {
                        q += " AND read = false"
                }
                q += " ORDER BY created_at DESC LIMIT $2"
                args = append(args, limit)
                rows, err := tx.Query(ctx, q, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var n Notification
                        if err := rows.Scan(&n.ID, &n.ShopID, &n.RecipientID, &n.Type, &n.Payload, &n.Read, &n.CreatedAt, &n.ReadAt); err != nil {
                                return err
                        }
                        out = append(out, n)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("notif repo: list by user: %w", err)
        }
        return out, nil
}

// MarkAsRead marks a single notification as read by ID. Spec Task 10 alias for
// MarkRead — kept for naming parity with the spec.
func (r *NotificationsRepository) MarkAsRead(ctx context.Context, notificationID uuid.UUID) error {
        // We look up the shop_id first (RLS requires it), then update. Best-effort
        // shop scope — if the row is platform-level (shop_id NULL), we use the
        // platform admin context.
        var shopID *uuid.UUID
        _ = db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, "super_admin", func(tx pgx.Tx) error {
                var sid *uuid.UUID
                if err := tx.QueryRow(ctx, `SELECT shop_id FROM notifications WHERE id = $1`, notificationID).Scan(&sid); err != nil {
                        return err
                }
                shopID = sid
                return nil
        })
        return db.WithTenantTx(ctx, r.pool, shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `UPDATE notifications SET read = true, read_at = now() WHERE id = $1`
                _, err := tx.Exec(ctx, q, notificationID)
                return err
        })
}

// MarkAllAsRead marks every notification for the (shopID, userID) tuple as
// read. Used by the dashboard "mark all as read" button.
func (r *NotificationsRepository) MarkAllAsRead(ctx context.Context, shopID, userID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, "owner", func(tx pgx.Tx) error {
                const q = `
                        UPDATE notifications
                           SET read = true, read_at = now()
                         WHERE shop_id = $1
                           AND (recipient_id = $2 OR recipient_id IS NULL)
                           AND read = false
                `
                _, err := tx.Exec(ctx, q, shopID, userID)
                return err
        })
}

// CountUnread returns the number of unread notifications for a user (across
// all shops where they're a recipient). Used by the dashboard badge counter.
func (r *NotificationsRepository) CountUnread(ctx context.Context, userID uuid.UUID) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, nil, userID, "owner", func(tx pgx.Tx) error {
                const q = `
                        SELECT COUNT(*)
                          FROM notifications
                         WHERE recipient_id = $1
                           AND read = false
                `
                return tx.QueryRow(ctx, q, userID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("notif repo: count unread: %w", err)
        }
        return n, nil
}

// ExistsThisMonth returns true iff at least one notification of the given
// `notifType` exists for the shop in the current calendar month (server-local
// time). Used by the QuotaService to dedupe the 80%/100% quota alerts —
// without this check, every IncrementUsage call after the threshold is
// crossed would create a new notification (potentially hundreds per day).
//
// shopID may be nil (platform-wide notification). notifType is the exact
// `type` column value (e.g. "quota_80_reached").
func (r *NotificationsRepository) ExistsThisMonth(ctx context.Context, shopID *uuid.UUID, notifType string) (bool, error) {
        var n int64
        // Use the platform-admin role when shopID is nil (platform-wide notif).
        role := "owner"
        if shopID == nil {
                role = "super_admin"
        }
        err := db.WithTenantTx(ctx, r.pool, shopID, uuid.Nil, role, func(tx pgx.Tx) error {
                if shopID != nil {
                        return tx.QueryRow(ctx, `
                                SELECT COUNT(*)
                                  FROM notifications
                                 WHERE shop_id = $1
                                   AND type = $2
                                   AND created_at >= date_trunc('month', now())
                        `, *shopID, notifType).Scan(&n)
                }
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*)
                          FROM notifications
                         WHERE type = $1
                           AND created_at >= date_trunc('month', now())
                `, notifType).Scan(&n)
        })
        if err != nil {
                return false, fmt.Errorf("notif repo: exists this month: %w", err)
        }
        return n > 0, nil
}
