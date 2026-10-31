// Subscriptions repository — handles the `subscriptions` and
// `subscription_payments` tables.
//
// Subscriptions are shop-scoped (subscriptions.shop_id) and protected by RLS:
// shop members can read their own subscription; platform admins can read all.
// The owner can read but not mutate (mutations happen through admin actions
// like RecordPayment, ExtendGrace, UpdateStatus).
package repository

import (
        "context"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// SubscriptionRepository wraps the subscriptions + subscription_payments tables.
type SubscriptionRepository struct {
        pool *pgxpool.Pool
}

// NewSubscriptionRepository returns a SubscriptionRepository bound to the pool.
func NewSubscriptionRepository(pool *pgxpool.Pool) *SubscriptionRepository {
        return &SubscriptionRepository{pool: pool}
}

// GetByShopID returns the active subscription for a shop. RLS: only members
// of the shop OR platform admins can read it. Returns ErrNotFound if the
// shop has no subscription.
func (r *SubscriptionRepository) GetByShopID(ctx context.Context, requesterID uuid.UUID, role string, shopID uuid.UUID) (*models.Subscription, error) {
        var sub models.Subscription
        err := db.WithTenantTx(ctx, r.pool, &shopID, requesterID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, plan_id, status, started_at, next_billing_at,
                               grace_until, suspended_at, terminated_at, created_at, updated_at
                          FROM subscriptions
                         WHERE shop_id = $1 AND terminated_at IS NULL
                         ORDER BY created_at DESC
                         LIMIT 1
                `
                return scanSubscription(tx.QueryRow(ctx, q, shopID), &sub)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("subscription repo: get by shop: %w", err)
        }
        return &sub, nil
}

// Create inserts a new subscription with status='trial' and
// next_billing_at=now+14days. The ShopRepository.Create already does this
// inline; this method is exposed for completeness / future use (e.g. plan
// changes after termination).
func (r *SubscriptionRepository) Create(ctx context.Context, actorID uuid.UUID, shopID, planID uuid.UUID) (*models.Subscription, error) {
        var sub models.Subscription
        err := db.WithTenantTx(ctx, r.pool, &shopID, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        INSERT INTO subscriptions (shop_id, plan_id, status, started_at, next_billing_at)
                        VALUES ($1, $2, 'trial', now(), now() + interval '14 days')
                        RETURNING id, shop_id, plan_id, status, started_at, next_billing_at,
                                  grace_until, suspended_at, terminated_at, created_at, updated_at
                `
                return scanSubscription(tx.QueryRow(ctx, q, shopID, planID), &sub)
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: create: %w", err)
        }
        return &sub, nil
}

// UpdateStatus changes the status of a subscription. Use this for the cron
// transitions: trial → active → late → grace_period → suspended → terminated.
// The actor must be a platform admin (the service verifies).
func (r *SubscriptionRepository) UpdateStatus(ctx context.Context, actorID uuid.UUID, id uuid.UUID, status models.SubscriptionStatus) (*models.Subscription, error) {
        var sub models.Subscription
        err := db.WithTenantTx(ctx, r.pool, nil, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // $2 is used twice (SET status + CASE WHEN comparisons). We cast it
                // to subscription_status on the SET and to text on the CASE WHENs so
                // PostgreSQL can resolve a single type per placeholder (avoids
                // "inconsistent types deduced for parameter $2" SQLSTATE 42P08).
                q := `
                        UPDATE subscriptions
                           SET status = $2::subscription_status,
                               suspended_at = CASE WHEN $2::text = 'suspended' THEN COALESCE(suspended_at, now()) ELSE suspended_at END,
                               terminated_at = CASE WHEN $2::text = 'terminated' THEN COALESCE(terminated_at, now()) ELSE terminated_at END,
                               updated_at = now()
                         WHERE id = $1
                        RETURNING id, shop_id, plan_id, status, started_at, next_billing_at,
                                  grace_until, suspended_at, terminated_at, created_at, updated_at
                `
                if err := scanSubscription(tx.QueryRow(ctx, q, id, string(status)), &sub); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return err
                }
                return nil
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: update status: %w", err)
        }
        return &sub, nil
}

// ExtendGrace sets grace_until = until on the given subscription. Used by the
// billing cron job when an invoice is late but we want to grant a few more
// days before suspending.
func (r *SubscriptionRepository) ExtendGrace(ctx context.Context, actorID uuid.UUID, id uuid.UUID, until time.Time) error {
        return db.WithTenantTx(ctx, r.pool, nil, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE subscriptions
                           SET grace_until = $2,
                               status = CASE WHEN status IN ('late') THEN 'grace_period' ELSE status END,
                               updated_at = now()
                         WHERE id = $1
                `, id, until)
                if err != nil {
                        return fmt.Errorf("extend grace: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// ListLate returns all subscriptions in 'late' or 'grace_period' status.
// Used by the billing cron job to send reminders and transition to suspended.
func (r *SubscriptionRepository) ListLate(ctx context.Context) ([]models.Subscription, error) {
        var out []models.Subscription
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, plan_id, status, started_at, next_billing_at,
                               grace_until, suspended_at, terminated_at, created_at, updated_at
                          FROM subscriptions
                         WHERE status IN ('late', 'grace_period')
                           AND terminated_at IS NULL
                         ORDER BY next_billing_at ASC
                `
                rows, err := tx.Query(ctx, q)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var s models.Subscription
                        if err := scanSubscription(rows, &s); err != nil {
                                return err
                        }
                        out = append(out, s)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: list late: %w", err)
        }
        return out, nil
}

// RecordPaymentInput is the data needed to record a payment.
type RecordPaymentInput struct {
        SubscriptionID uuid.UUID
        ShopID         uuid.UUID
        Amount         int64
        Mode           models.PaymentMode
        Reference      string
        PeriodStart    time.Time // date
        PeriodEnd      time.Time // date
        RecordedBy     uuid.UUID
}

// RecordPayment inserts a row in subscription_payments and updates the
// subscription status to 'active' with next_billing_at = +1 month from now.
// The actor must be a platform admin (the service verifies).
func (r *SubscriptionRepository) RecordPayment(ctx context.Context, actorID uuid.UUID, in RecordPaymentInput) (*models.SubscriptionPayment, error) {
        var pay models.SubscriptionPayment
        err := db.WithTenantTx(ctx, r.pool, &in.ShopID, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // 1. Insert the payment row.
                var refArg any
                if in.Reference != "" {
                        refArg = in.Reference
                }
                const payQ = `
                        INSERT INTO subscription_payments
                            (subscription_id, shop_id, amount, mode, reference, period_start, period_end, recorded_by)
                        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                        RETURNING id, subscription_id, shop_id, amount, mode, reference,
                                  period_start, period_end, recorded_by, recorded_at
                `
                if err := tx.QueryRow(ctx, payQ,
                        in.SubscriptionID, in.ShopID, in.Amount, string(in.Mode),
                        refArg, in.PeriodStart, in.PeriodEnd, in.RecordedBy,
                ).Scan(
                        &pay.ID, &pay.SubscriptionID, &pay.ShopID, &pay.Amount,
                        &pay.Mode, &pay.Reference, &pay.PeriodStart, &pay.PeriodEnd,
                        &pay.RecordedBy, &pay.RecordedAt,
                ); err != nil {
                        return fmt.Errorf("insert subscription_payment: %w", err)
                }

                // 2. Update the subscription: status='active', next_billing_at=+1 month,
                //    clear grace/suspended timestamps.
                if _, err := tx.Exec(ctx, `
                        UPDATE subscriptions
                           SET status = 'active',
                               next_billing_at = now() + interval '1 month',
                               grace_until = NULL,
                               suspended_at = NULL,
                               updated_at = now()
                         WHERE id = $1
                `, in.SubscriptionID); err != nil {
                        return fmt.Errorf("update subscription after payment: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: record payment: %w", err)
        }
        return &pay, nil
}

// --- helpers ----------------------------------------------------------------

func scanSubscription(s scanner, sub *models.Subscription) error {
        var (
                status        string
                nextBillingAt *time.Time
                graceUntil    *time.Time
                suspendedAt   *time.Time
                terminatedAt  *time.Time
        )
        err := s.Scan(
                &sub.ID,
                &sub.ShopID,
                &sub.PlanID,
                &status,
                &sub.StartedAt,
                &nextBillingAt,
                &graceUntil,
                &suspendedAt,
                &terminatedAt,
                &sub.CreatedAt,
                &sub.UpdatedAt,
        )
        if err != nil {
                return err
        }
        sub.Status = models.SubscriptionStatus(status)
        sub.NextBillingAt = nextBillingAt
        sub.GraceUntil = graceUntil
        sub.SuspendedAt = suspendedAt
        sub.TerminatedAt = terminatedAt
        return nil
}

// ============================================================================
// Spec Task 10 — Subscription lifecycle helpers (admin endpoints + cron jobs)
// ============================================================================

// GetByID returns the subscription with the given UUID. Used by the
// SubscriptionService.Suspend/Reactivate/Terminate methods (admin actions
// that target a specific subscription row rather than the latest one for a
// shop). Returns ErrNotFound if no such subscription exists.
func (r *SubscriptionRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Subscription, error) {
        var sub models.Subscription
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, plan_id, status, started_at, next_billing_at,
                               grace_until, suspended_at, terminated_at, created_at, updated_at
                          FROM subscriptions
                         WHERE id = $1
                `
                return scanSubscription(tx.QueryRow(ctx, q, id), &sub)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("subscription repo: get by id: %w", err)
        }
        return &sub, nil
}

// ListAllParams holds the optional filters for ListAll.
type ListAllParams struct {
        Status string // optional: trial|active|late|grace_period|suspended|terminated
        Page   int
        Limit  int
}

// Normalize fills sane defaults.
func (p *ListAllParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 200 {
                p.Limit = 50
        }
}

// Offset returns the SQL OFFSET value.
func (p *ListAllParams) Offset() int { return (p.Page - 1) * p.Limit }

// ListAll returns a paginated list of all subscriptions (optionally filtered
// by status). Used by the platform admin dashboard. Joined with the plan +
// shop names so the dashboard can render a meaningful table without a second
// round-trip.
type SubscriptionListItem struct {
        models.Subscription
        ShopName string `json:"shop_name"`
        ShopSlug string `json:"shop_slug"`
        PlanName string `json:"plan_name"`
}

// ListAll returns all subscriptions (optionally filtered by status), joined
// with the shop + plan names. Paginated.
func (r *SubscriptionRepository) ListAll(ctx context.Context, params ListAllParams) ([]SubscriptionListItem, int64, error) {
        params.Normalize()
        var out []SubscriptionListItem
        var total int64
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // Count query.
                countQ := `SELECT COUNT(*) FROM subscriptions WHERE terminated_at IS NULL`
                args := []any{}
                if params.Status != "" {
                        countQ += " AND status = $1"
                        args = append(args, params.Status)
                }
                if err := tx.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
                        return fmt.Errorf("subscription repo: list all count: %w", err)
                }
                // List query — join shops + plans.
                listQ := `
                        SELECT s.id, s.shop_id, s.plan_id, s.status, s.started_at, s.next_billing_at,
                               s.grace_until, s.suspended_at, s.terminated_at, s.created_at, s.updated_at,
                               COALESCE(sh.name, ''), COALESCE(sh.slug, ''), COALESCE(p.name, '')
                          FROM subscriptions s
                          LEFT JOIN shops sh ON sh.id = s.shop_id
                          LEFT JOIN plans  p  ON p.id  = s.plan_id
                         WHERE s.terminated_at IS NULL
                `
                listArgs := []any{}
                if params.Status != "" {
                        listQ += " AND s.status = $1"
                        listArgs = append(listArgs, params.Status)
                }
                listQ += " ORDER BY s.next_billing_at ASC NULLS LAST, s.created_at DESC"
                listQ += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(listArgs)+1, len(listArgs)+2)
                listArgs = append(listArgs, params.Limit, params.Offset())
                rows, err := tx.Query(ctx, listQ, listArgs...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var item SubscriptionListItem
                        if err := scanSubscriptionWithShopPlan(rows, &item); err != nil {
                                return err
                        }
                        out = append(out, item)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("subscription repo: list all: %w", err)
        }
        return out, total, nil
}

// ListByStatuses returns all subscriptions whose status is in the given list.
// Used by the SubscriptionService cron jobs (CheckAndAdvanceLifecycle and
// SendPaymentReminders) to iterate the relevant subset.
func (r *SubscriptionRepository) ListByStatuses(ctx context.Context, statuses ...models.SubscriptionStatus) ([]models.Subscription, error) {
        if len(statuses) == 0 {
                return nil, nil
        }
        var out []models.Subscription
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // Build the IN list dynamically ( statuses is small + trusted → OK to inline).
                args := make([]any, 0, len(statuses))
                placeholders := make([]string, 0, len(statuses))
                for i, st := range statuses {
                        args = append(args, string(st))
                        placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
                }
                q := fmt.Sprintf(`
                        SELECT id, shop_id, plan_id, status, started_at, next_billing_at,
                               grace_until, suspended_at, terminated_at, created_at, updated_at
                          FROM subscriptions
                         WHERE status IN (%s)
                           AND terminated_at IS NULL
                         ORDER BY next_billing_at ASC NULLS LAST, created_at ASC
                `, joinStrings(placeholders, ","))
                rows, err := tx.Query(ctx, q, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var s models.Subscription
                        if err := scanSubscription(rows, &s); err != nil {
                                return err
                        }
                        out = append(out, s)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: list by statuses: %w", err)
        }
        return out, nil
}

// ListPaymentsByShop returns the payment history for a shop, most recent first.
func (r *SubscriptionRepository) ListPaymentsByShop(ctx context.Context, shopID uuid.UUID, limit int) ([]models.SubscriptionPayment, error) {
        if limit <= 0 || limit > 200 {
                limit = 50
        }
        var out []models.SubscriptionPayment
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `
                        SELECT id, subscription_id, shop_id, amount, mode, reference,
                               period_start, period_end, recorded_by, recorded_at
                          FROM subscription_payments
                         WHERE shop_id = $1
                         ORDER BY recorded_at DESC
                         LIMIT $2
                `
                rows, err := tx.Query(ctx, q, shopID, limit)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var p models.SubscriptionPayment
                        if err := scanPayment(rows, &p); err != nil {
                                return err
                        }
                        out = append(out, p)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: list payments by shop: %w", err)
        }
        return out, nil
}

// RevenueStats is the aggregate revenue shape returned by GetRevenueStats.
type RevenueStats struct {
        MRR             int64           `json:"mrr"`              // Monthly Recurring Revenue (sum of active subscription plan prices, FCFA)
        TotalRevenue    int64           `json:"total_revenue"`    // All-time sum of subscription_payments.amount, FCFA
        ActiveCount     int64           `json:"active_count"`     // # subscriptions in trial|active|late|grace_period
        SuspendedCount  int64           `json:"suspended_count"`  // # subscriptions in suspended
        TerminatedCount int64           `json:"terminated_count"` // # subscriptions in terminated
        ByPlan          []RevenueByPlan `json:"by_plan"`          // revenue per plan
}

// RevenueByPlan is the per-plan revenue breakdown.
type RevenueByPlan struct {
        PlanID       uuid.UUID `json:"plan_id"`
        PlanName     string    `json:"plan_name"`
        ActiveCount  int64     `json:"active_count"`
        TotalRevenue int64     `json:"total_revenue"`
        MonthlyPrice int64     `json:"monthly_price"`
}

// GetRevenueStats returns the revenue statistics for the platform admin
// dashboard. MRR is computed as the sum of plan.price for all subscriptions
// in trial|active|late|grace_period status (the cahier des charges ch. 8
// defines MRR as "abonnements actifs"). TotalRevenue is the all-time sum of
// subscription_payments.amount. ByPlan is a per-plan breakdown.
func (r *SubscriptionRepository) GetRevenueStats(ctx context.Context) (*RevenueStats, error) {
        out := &RevenueStats{}
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // MRR + counts.
                const mrrQ = `
                        SELECT
                                COALESCE(SUM(p.price), 0),
                                COUNT(*) FILTER (WHERE s.status IN ('trial', 'active', 'late', 'grace_period')),
                                COUNT(*) FILTER (WHERE s.status = 'suspended'),
                                COUNT(*) FILTER (WHERE s.status = 'terminated')
                          FROM subscriptions s
                          JOIN plans p ON p.id = s.plan_id
                `
                if err := tx.QueryRow(ctx, mrrQ).Scan(
                        &out.MRR, &out.ActiveCount, &out.SuspendedCount, &out.TerminatedCount,
                ); err != nil {
                        return fmt.Errorf("subscription repo: revenue mrr: %w", err)
                }
                // Total revenue (all-time sum of payments).
                const totQ = `SELECT COALESCE(SUM(amount), 0) FROM subscription_payments`
                if err := tx.QueryRow(ctx, totQ).Scan(&out.TotalRevenue); err != nil {
                        return fmt.Errorf("subscription repo: revenue total: %w", err)
                }
                // Per-plan breakdown. We use COUNT(DISTINCT s.id) so a plan with
                // multiple payments on the same subscription isn't double-counted.
                const planQ = `
                        SELECT p.id, p.name, p.price,
                               COUNT(DISTINCT s.id) FILTER (WHERE s.status IN ('trial', 'active', 'late', 'grace_period')),
                               COALESCE(SUM(sp.amount), 0)
                          FROM plans p
                          LEFT JOIN subscriptions s ON s.plan_id = p.id
                          LEFT JOIN subscription_payments sp ON sp.subscription_id = s.id
                         WHERE p.active = true
                         GROUP BY p.id, p.name, p.price
                         ORDER BY p.price ASC
                `
                rows, err := tx.Query(ctx, planQ)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var rp RevenueByPlan
                        if err := rows.Scan(&rp.PlanID, &rp.PlanName, &rp.MonthlyPrice, &rp.ActiveCount, &rp.TotalRevenue); err != nil {
                                return err
                        }
                        out.ByPlan = append(out.ByPlan, rp)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("subscription repo: revenue stats: %w", err)
        }
        return out, nil
}

// SetNextBilling updates next_billing_at on a subscription (used by the cron
// job when transitioning trial→late to advance the billing due date).
func (r *SubscriptionRepository) SetNextBilling(ctx context.Context, id uuid.UUID, next time.Time) error {
        return db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `UPDATE subscriptions SET next_billing_at = $2, updated_at = now() WHERE id = $1`, id, next)
                if err != nil {
                        return fmt.Errorf("subscription repo: set next billing: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// SetGrace sets grace_until and the status (to grace_period) on a subscription.
// Used by the cron job when late → grace_period.
func (r *SubscriptionRepository) SetGrace(ctx context.Context, id uuid.UUID, until time.Time) error {
        return db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE subscriptions
                           SET grace_until = $2,
                               status = 'grace_period',
                               updated_at = now()
                         WHERE id = $1
                `, id, until)
                if err != nil {
                        return fmt.Errorf("subscription repo: set grace: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// --- helpers ----------------------------------------------------------------

// scanSubscriptionWithShopPlan scans a subscription row joined with shop +
// plan name. Used by ListAll.
func scanSubscriptionWithShopPlan(s scanner, item *SubscriptionListItem) error {
        var (
                status        string
                nextBillingAt *time.Time
                graceUntil    *time.Time
                suspendedAt   *time.Time
                terminatedAt  *time.Time
        )
        err := s.Scan(
                &item.ID,
                &item.ShopID,
                &item.PlanID,
                &status,
                &item.StartedAt,
                &nextBillingAt,
                &graceUntil,
                &suspendedAt,
                &terminatedAt,
                &item.CreatedAt,
                &item.UpdatedAt,
                &item.ShopName,
                &item.ShopSlug,
                &item.PlanName,
        )
        if err != nil {
                return err
        }
        item.Status = models.SubscriptionStatus(status)
        item.NextBillingAt = nextBillingAt
        item.GraceUntil = graceUntil
        item.SuspendedAt = suspendedAt
        item.TerminatedAt = terminatedAt
        return nil
}

// scanPayment scans a subscription_payments row.
func scanPayment(s scanner, p *models.SubscriptionPayment) error {
        var (
                reference  *string
                recordedBy *uuid.UUID
        )
        err := s.Scan(
                &p.ID,
                &p.SubscriptionID,
                &p.ShopID,
                &p.Amount,
                &p.Mode,
                &reference,
                &p.PeriodStart,
                &p.PeriodEnd,
                &recordedBy,
                &p.RecordedAt,
        )
        if err != nil {
                return err
        }
        p.Reference = reference
        p.RecordedBy = recordedBy
        return nil
}

// joinStrings is a tiny strings.Join clone kept locally to avoid importing
// "strings" in this file (the existing imports are minimal).
func joinStrings(parts []string, sep string) string {
        if len(parts) == 0 {
                return ""
        }
        out := parts[0]
        for _, p := range parts[1:] {
                out += sep + p
        }
        return out
}
