// NOVA v3 — Order repository extensions for the new state machine
// (en_attente_confirmation → en_attente_paiement → paiement_signalé → en_cours → prete → terminee)
//
// These methods are SEPARATE from the v1/v2 methods in orders.go. They use the
// SAME orderTransitions map (now extended with the v3 statuses) so the state
// machine stays consistent.
//
// Stock + revenue side-effects are the responsibility of the SERVICE layer
// (order_service.go). The repository only persists the status change + the
// order_event.
package repository

import (
        "context"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// --- Sentinels (v3) ---------------------------------------------------------

// ErrPaymentDeadlineMissing is returned by SetPaymentDeadline when the order
// is not in a status that allows setting a payment deadline.
var ErrPaymentDeadlineMissing = fmt.Errorf("payment deadline cannot be set on this order status")

// --- UpdateStatusV3 ---------------------------------------------------------

// UpdateStatusV3Input is the input to UpdateStatusV3.
type UpdateStatusV3Input struct {
        NewStatus        string
        AuthorID         *uuid.UUID
        Reason           string
        RevenueCounted   *bool      // optional override (true → en_cours, false → annulee)
        PaymentDeadline  *time.Time // optional (set when entering en_attente_paiement)
}

// UpdateStatusV3 applies a v3 state machine transition + optionally sets
// payment_deadline + revenue_counted. The transition MUST be allowed by
// orderTransitions; otherwise ErrInvalidTransition is returned.
//
// On success:
//   - The order's status is updated.
//   - confirmed_at is set (idempotently) if the new status implies the order
//     is past the 'pending' stage (v1 statuses) OR en_cours (v3 statuses).
//   - payment_deadline is set if PaymentDeadline != nil.
//   - revenue_counted is set if RevenueCounted != nil.
//   - An order_event row is inserted with from_status=old, to_status=new.
func (r *OrderRepository) UpdateStatusV3(ctx context.Context, shopID, userID uuid.UUID, role string, orderID uuid.UUID, in UpdateStatusV3Input) (*models.Order, *models.OrderEvent, error) {
        var (
                order models.Order
                ev    models.OrderEvent
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const sq = `
                        SELECT id, shop_id, number, customer_id, cart_id, status, payment_status,
                               payment_mode, payment_reference, subtotal, delivery_fee, total,
                               idempotency_key, delivery_zone_id, delivery_address, note,
                               created_at, confirmed_at, updated_at,
                               payment_deadline, revenue_counted
                          FROM orders WHERE id = $1
                          FOR UPDATE
                `
                if err := scanOrder(tx.QueryRow(ctx, sq, orderID), &order); err != nil {
                        if err == pgx.ErrNoRows {
                                return ErrOrderNotFound
                        }
                        return fmt.Errorf("order repo v3: update status: get: %w", err)
                }
                oldStatus := string(order.Status)
                if !IsTransitionAllowed(oldStatus, in.NewStatus) {
                        return ErrInvalidTransition
                }
                // Build the UPDATE query.
                var (
                        sets       = []string{"status = $2::order_status", "updated_at = now()"}
                        args       = []any{orderID, in.NewStatus}
                        idx        = 3
                        confirmed  any
                        deadline   any
                        revenue    any
                        addedConf  bool
                        addedDead  bool
                        addedRev   bool
                )
                // confirmed_at: set if transitioning to a "post-pending" v1 status
                // OR to en_cours (v3).
                if order.ConfirmedAt != nil {
                        confirmed = *order.ConfirmedAt
                } else if isPostPendingV3(in.NewStatus) {
                        confirmed = time.Now()
                }
                if confirmed != nil {
                        sets = append(sets, fmt.Sprintf("confirmed_at = COALESCE($%d, confirmed_at)", idx))
                        args = append(args, confirmed)
                        idx++
                        addedConf = true
                }
                // payment_deadline
                if in.PaymentDeadline != nil {
                        sets = append(sets, fmt.Sprintf("payment_deadline = $%d", idx))
                        args = append(args, *in.PaymentDeadline)
                        idx++
                        addedDead = true
                } else if in.NewStatus == "en_cours" || in.NewStatus == "prete" || in.NewStatus == "terminee" || in.NewStatus == "annulee" {
                        // Clear the payment deadline once we're past en_attente_paiement.
                        sets = append(sets, "payment_deadline = NULL")
                        addedDead = true
                }
                // revenue_counted
                if in.RevenueCounted != nil {
                        sets = append(sets, fmt.Sprintf("revenue_counted = $%d", idx))
                        args = append(args, *in.RevenueCounted)
                        idx++
                        addedRev = true
                }
                _ = addedConf
                _ = addedDead
                _ = addedRev
                _ = deadline
                _ = revenue
                uq := fmt.Sprintf(`
                        UPDATE orders SET %s WHERE id = $1
                        RETURNING id, shop_id, number, customer_id, cart_id, status, payment_status,
                                  payment_mode, payment_reference, subtotal, delivery_fee, total,
                                  idempotency_key, delivery_zone_id, delivery_address, note,
                                  created_at, confirmed_at, updated_at,
                                  payment_deadline, revenue_counted
                `, joinStringsV3(sets, ", "))
                if err := scanOrder(tx.QueryRow(ctx, uq, args...), &order); err != nil {
                        return fmt.Errorf("order repo v3: update status: update: %w", err)
                }
                // Insert the order_event.
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                const eq = `
                        INSERT INTO order_events (order_id, shop_id, from_status, to_status, author_id, reason)
                        VALUES ($1, $2, $3::order_status, $4::order_status, $5, $6)
                        RETURNING id, order_id, shop_id, from_status, to_status, author_id, reason, created_at
                `
                var (
                        fromStatus *string
                        authorID   *uuid.UUID
                        reason     *string
                )
                if err := tx.QueryRow(ctx, eq,
                        orderID, shopID,
                        oldStatus,
                        in.NewStatus,
                        authorArg,
                        nullableString(in.Reason),
                ).Scan(
                        &ev.ID, &ev.OrderID, &ev.ShopID, &fromStatus, &ev.ToStatus, &authorID, &reason, &ev.CreatedAt,
                ); err != nil {
                        return fmt.Errorf("order repo v3: update status: insert event: %w", err)
                }
                if fromStatus != nil {
                        s := models.OrderStatus(*fromStatus)
                        ev.FromStatus = &s
                }
                ev.AuthorID = authorID
                ev.Reason = reason
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &order, &ev, nil
}

// isPostPendingV3 returns true if the given v3 status implies the order is
// past the "pending" stage (i.e. confirmed_at should be set).
func isPostPendingV3(status string) bool {
        switch status {
        case "en_cours", "prete", "terminee":
                return true
        }
        return false
}

// joinStringsV3 joins a slice of strings with the given separator.
// (Local copy to avoid name conflicts with other repository files.)
func joinStringsV3(parts []string, sep string) string {
        if len(parts) == 0 {
                return ""
        }
        out := parts[0]
        for _, p := range parts[1:] {
                out += sep + p
        }
        return out
}

// --- ListExpiredPaymentOrders -----------------------------------------------

// ListExpiredPaymentOrders returns all orders in 'en_attente_paiement' status
// whose payment_deadline is in the past. Used by the cron job to auto-cancel
// expired payment orders + release stock.
//
// We use a super_admin role to bypass RLS (the cron runs server-side).
func (r *OrderRepository) ListExpiredPaymentOrders(ctx context.Context) ([]uuid.UUID, []uuid.UUID, error) {
        var orderIDs []uuid.UUID
        var shopIDs []uuid.UUID
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, "super_admin", func(tx pgx.Tx) error {
                rows, err := tx.Query(ctx, `
                        SELECT id, shop_id
                          FROM orders
                         WHERE status = 'en_attente_paiement'
                           AND payment_deadline IS NOT NULL
                           AND payment_deadline < now()
                         ORDER BY payment_deadline
                         LIMIT 200
                `)
                if err != nil {
                        return fmt.Errorf("list expired payment orders: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var id, shopID uuid.UUID
                        if err := rows.Scan(&id, &shopID); err != nil {
                                return err
                        }
                        orderIDs = append(orderIDs, id)
                        shopIDs = append(shopIDs, shopID)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, nil, fmt.Errorf("order repo v3: list expired: %w", err)
        }
        return orderIDs, shopIDs, nil
}

// --- MonthRevenueV3 ---------------------------------------------------------

// MonthRevenueV3 returns the sum of totals of orders where revenue_counted =
// true, created this month, plus the count of those orders.
//
// This replaces the v1 MonthRevenue for the v3 flow. The v1 method is kept
// for backward compatibility (it counts based on status).
func (r *OrderRepository) MonthRevenueV3(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, int64, error) {
        var (
                revenue int64
                count   int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COALESCE(SUM(total), 0), COUNT(*)
                          FROM orders
                         WHERE shop_id = $1
                           AND created_at >= date_trunc('month', now())
                           AND revenue_counted = true
                `, shopID).Scan(&revenue, &count)
        })
        if err != nil {
                return 0, 0, fmt.Errorf("order repo v3: month revenue: %w", err)
        }
        return revenue, count, nil
}
