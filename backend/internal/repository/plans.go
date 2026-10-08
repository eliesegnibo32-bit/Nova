// Plans repository — handles the `plans` table.
//
// Plans are platform-level (not shop-scoped): there is no shop_id column on
// plans and no RLS policy. All methods run with role='super_admin' for the
// tenant context (which is a no-op here since plans isn't RLS-protected, but
// we keep the pattern consistent for connection-pool reuse).
package repository

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// PlanRepository wraps the plans table.
type PlanRepository struct {
        pool *pgxpool.Pool
}

// NewPlanRepository returns a PlanRepository bound to the given pool.
func NewPlanRepository(pool *pgxpool.Pool) *PlanRepository {
        return &PlanRepository{pool: pool}
}

// GetByID returns the plan with the given UUID. Returns ErrNotFound if no
// such plan exists.
func (r *PlanRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
        var p models.Plan
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, name, price, setup_fee, message_quota, product_limit,
                               employee_limit, features, active, created_at, updated_at
                          FROM plans WHERE id = $1
                `
                return scanPlan(tx.QueryRow(ctx, q, id), &p)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("plan repo: get by id: %w", err)
        }
        return &p, nil
}

// GetByName returns the plan with the given name (e.g. "Essentiel"). Returns
// ErrNotFound if no such plan exists.
func (r *PlanRepository) GetByName(ctx context.Context, name string) (*models.Plan, error) {
        var p models.Plan
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, name, price, setup_fee, message_quota, product_limit,
                               employee_limit, features, active, created_at, updated_at
                          FROM plans WHERE name = $1
                `
                return scanPlan(tx.QueryRow(ctx, q, name), &p)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("plan repo: get by name: %w", err)
        }
        return &p, nil
}

// List returns all active plans. The result is ordered by price ascending so
// the cheapest plan is shown first to merchants.
func (r *PlanRepository) List(ctx context.Context) ([]models.Plan, error) {
        var out []models.Plan
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, name, price, setup_fee, message_quota, product_limit,
                               employee_limit, features, active, created_at, updated_at
                          FROM plans
                         WHERE active = true
                         ORDER BY price ASC, name ASC
                `
                rows, err := tx.Query(ctx, q)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var p models.Plan
                        if err := scanPlan(rows, &p); err != nil {
                                return err
                        }
                        out = append(out, p)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("plan repo: list: %w", err)
        }
        return out, nil
}

// CreatePlanInput is the data needed to create a new plan.
type CreatePlanInput struct {
        Name          string
        Price         int64
        SetupFee      int64
        MessageQuota  int
        ProductLimit  *int
        EmployeeLimit *int
        Features      json.RawMessage
        Active        bool
}

// Create inserts a new plan. Admin only (the service verifies).
func (r *PlanRepository) Create(ctx context.Context, in CreatePlanInput) (*models.Plan, error) {
        var p models.Plan
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                featuresArg := []byte("{}")
                if len(in.Features) > 0 {
                        featuresArg = in.Features
                }
                q := `
                        INSERT INTO plans (name, price, setup_fee, message_quota, product_limit, employee_limit, features, active)
                        VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
                        RETURNING id, name, price, setup_fee, message_quota, product_limit,
                                  employee_limit, features, active, created_at, updated_at
                `
                return scanPlan(tx.QueryRow(ctx, q,
                        in.Name, in.Price, in.SetupFee, in.MessageQuota,
                        in.ProductLimit, in.EmployeeLimit,
                        featuresArg, in.Active,
                ), &p)
        })
        if err != nil {
                return nil, fmt.Errorf("plan repo: create: %w", err)
        }
        return &p, nil
}

// UpdatePlanInput holds the optional fields for an update. nil pointers mean
// "don't change"; non-nil means "update to this value".
type UpdatePlanInput struct {
        Name          *string
        Price         *int64
        SetupFee      *int64
        MessageQuota  *int
        ProductLimit  *int
        EmployeeLimit *int
        Features      json.RawMessage
        Active        *bool
}

// Update applies a partial update to a plan.
func (r *PlanRepository) Update(ctx context.Context, id uuid.UUID, in UpdatePlanInput) (*models.Plan, error) {
        var p models.Plan
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // Re-fetch then update — simpler than building a dynamic SET.
                // The plans table is small (a handful of rows) so the extra
                // round-trip is acceptable.
                if err := scanPlan(tx.QueryRow(ctx, `
                        SELECT id, name, price, setup_fee, message_quota, product_limit,
                               employee_limit, features, active, created_at, updated_at
                          FROM plans WHERE id = $1
                `, id), &p); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return err
                }
                if in.Name != nil {
                        p.Name = *in.Name
                }
                if in.Price != nil {
                        p.Price = *in.Price
                }
                if in.SetupFee != nil {
                        p.SetupFee = *in.SetupFee
                }
                if in.MessageQuota != nil {
                        p.MessageQuota = *in.MessageQuota
                }
                if in.ProductLimit != nil {
                        p.ProductLimit = in.ProductLimit
                }
                if in.EmployeeLimit != nil {
                        p.EmployeeLimit = in.EmployeeLimit
                }
                if in.Active != nil {
                        p.Active = *in.Active
                }
                featuresArg := p.Features
                if len(in.Features) > 0 {
                        featuresArg = in.Features
                }
                if featuresArg == nil {
                        featuresArg = []byte("{}")
                }
                q := `
                        UPDATE plans
                           SET name = $2, price = $3, setup_fee = $4, message_quota = $5,
                               product_limit = $6, employee_limit = $7, features = $8::jsonb,
                               active = $9, updated_at = now()
                         WHERE id = $1
                        RETURNING id, name, price, setup_fee, message_quota, product_limit,
                                  employee_limit, features, active, created_at, updated_at
                `
                return scanPlan(tx.QueryRow(ctx, q,
                        id, p.Name, p.Price, p.SetupFee, p.MessageQuota,
                        p.ProductLimit, p.EmployeeLimit, featuresArg, p.Active,
                ), &p)
        })
        if err != nil {
                return nil, fmt.Errorf("plan repo: update: %w", err)
        }
        return &p, nil
}

// --- helpers ----------------------------------------------------------------

func scanPlan(s scanner, p *models.Plan) error {
        var (
                productLimit  *int
                employeeLimit *int
                features      []byte
        )
        err := s.Scan(
                &p.ID,
                &p.Name,
                &p.Price,
                &p.SetupFee,
                &p.MessageQuota,
                &productLimit,
                &employeeLimit,
                &features,
                &p.Active,
                &p.CreatedAt,
                &p.UpdatedAt,
        )
        if err != nil {
                return err
        }
        p.ProductLimit = productLimit
        p.EmployeeLimit = employeeLimit
        p.Features = features
        return nil
}

// Compile-time guard: ensure time is used (kept for future use when plans
// gain expiry columns).
var _ = time.Now
