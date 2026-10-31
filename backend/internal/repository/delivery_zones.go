// Delivery zones repository — handles the `delivery_zones` table.
//
// All queries go through db.WithTenantTx so the RLS policies from migration
// 011 are enforced:
//
//      shop_id = current_shop_id() OR is_platform_admin()
//
// shopID comes from the authenticated session, NOT from the URL.
//
// Per cahier des charges (ch. 4.4):
//   - Each zone can have aliases (text[]) recognized by the AI for fuzzy
//     matching.
//   - Each zone has a fee (FCFA), an optional estimated_delay (text), an
//     optional free_from threshold (above which delivery is free), and an
//     optional min_order_amount (below which the zone refuses the order).
//   - No tariff is imposed by NOVA — each shop defines its own.
//   - Unknown/ambiguous zone queries must NOT silently pick a zone; the AI
//     must ask for clarification. MatchByQuery returns ErrZoneAmbiguous
//     when more than one zone matches.
package repository

import (
        "context"
        "errors"
        "fmt"
        "strings"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// CreateDeliveryZoneInput is the data needed to create a delivery zone.
type CreateDeliveryZoneInput struct {
        Name           string
        Aliases        []string
        Fee            int64
        EstimatedDelay string
        FreeFrom       *int64
        MinOrderAmount *int64
        Active         bool
}

// UpdateDeliveryZoneInput mirrors models.UpdateDeliveryZoneRequest but with
// raw pointers.
type UpdateDeliveryZoneInput struct {
        Name           *string
        Aliases        *[]string
        Fee            *int64
        EstimatedDelay *string
        FreeFrom       *int64
        MinOrderAmount *int64
        Active         *bool
        // FreeFromClear / MinOrderAmountClear let the caller explicitly NULL
        // these fields.
        FreeFromClear       bool
        MinOrderAmountClear bool
        EstimatedDelayClear bool
}

// DeliveryZoneRepository wraps the delivery_zones table.
type DeliveryZoneRepository struct {
        pool *pgxpool.Pool
}

// NewDeliveryZoneRepository returns a DeliveryZoneRepository bound to the pool.
func NewDeliveryZoneRepository(pool *pgxpool.Pool) *DeliveryZoneRepository {
        return &DeliveryZoneRepository{pool: pool}
}

// Create inserts a delivery zone.
func (r *DeliveryZoneRepository) Create(ctx context.Context, shopID, userID uuid.UUID, role string, in CreateDeliveryZoneInput) (*models.DeliveryZone, error) {
        var z models.DeliveryZone
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                aliases := in.Aliases
                if aliases == nil {
                        aliases = []string{}
                }
                q := `
                        INSERT INTO delivery_zones
                            (shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active)
                        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                        RETURNING id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active, created_at, updated_at
                `
                return scanDeliveryZone(tx.QueryRow(ctx, q,
                        shopID,
                        in.Name,
                        aliases,
                        in.Fee,
                        nullableString(in.EstimatedDelay),
                        in.FreeFrom,
                        in.MinOrderAmount,
                        in.Active,
                ), &z)
        })
        if err != nil {
                return nil, fmt.Errorf("delivery zone repo: create: %w", err)
        }
        return &z, nil
}

// GetByID returns a single delivery zone.
func (r *DeliveryZoneRepository) GetByID(ctx context.Context, shopID, userID uuid.UUID, role string, zoneID uuid.UUID) (*models.DeliveryZone, error) {
        var z models.DeliveryZone
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active, created_at, updated_at
                          FROM delivery_zones WHERE id = $1
                `
                if err := scanDeliveryZone(tx.QueryRow(ctx, q, zoneID), &z); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("get delivery zone: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &z, nil
}

// List returns all delivery zones for the shop (active + inactive).
func (r *DeliveryZoneRepository) List(ctx context.Context, shopID, userID uuid.UUID, role string) ([]models.DeliveryZone, error) {
        return r.listByActive(ctx, shopID, userID, role, false)
}

// ListActive returns only active delivery zones.
func (r *DeliveryZoneRepository) ListActive(ctx context.Context, shopID, userID uuid.UUID, role string) ([]models.DeliveryZone, error) {
        return r.listByActive(ctx, shopID, userID, role, true)
}

func (r *DeliveryZoneRepository) listByActive(ctx context.Context, shopID, userID uuid.UUID, role string, onlyActive bool) ([]models.DeliveryZone, error) {
        var out []models.DeliveryZone
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                q := `
                        SELECT id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active, created_at, updated_at
                          FROM delivery_zones
                `
                if onlyActive {
                        q += " WHERE shop_id = $1 AND active = true"
                } else {
                        q += " WHERE shop_id = $1"
                }
                q += " ORDER BY created_at ASC"
                rows, err := tx.Query(ctx, q, shopID)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var z models.DeliveryZone
                        if err := scanDeliveryZone(rows, &z); err != nil {
                                return err
                        }
                        out = append(out, z)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("delivery zone repo: list: %w", err)
        }
        return out, nil
}

// Update applies a partial update to a delivery zone.
func (r *DeliveryZoneRepository) Update(ctx context.Context, shopID, userID uuid.UUID, role string, zoneID uuid.UUID, in UpdateDeliveryZoneInput) (*models.DeliveryZone, error) {
        var z models.DeliveryZone
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                sets := []string{}
                args := []any{}
                idx := 1
                addString := func(col string, v *string) {
                        if v != nil {
                                sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
                                args = append(args, *v)
                                idx++
                        }
                }
                addInt64 := func(col string, v *int64) {
                        if v != nil {
                                sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
                                args = append(args, *v)
                                idx++
                        }
                }
                addString("name", in.Name)
                if in.Aliases != nil {
                        sets = append(sets, fmt.Sprintf("aliases = $%d", idx))
                        args = append(args, *in.Aliases)
                        idx++
                }
                addInt64("fee", in.Fee)
                if in.EstimatedDelayClear {
                        sets = append(sets, "estimated_delay = NULL")
                } else {
                        addString("estimated_delay", in.EstimatedDelay)
                }
                if in.FreeFromClear {
                        sets = append(sets, "free_from = NULL")
                } else {
                        addInt64("free_from", in.FreeFrom)
                }
                if in.MinOrderAmountClear {
                        sets = append(sets, "min_order_amount = NULL")
                } else {
                        addInt64("min_order_amount", in.MinOrderAmount)
                }
                if in.Active != nil {
                        sets = append(sets, fmt.Sprintf("active = $%d", idx))
                        args = append(args, *in.Active)
                        idx++
                }
                if len(sets) == 0 {
                        const q = `
                                SELECT id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active, created_at, updated_at
                                  FROM delivery_zones WHERE id = $1
                        `
                        return scanDeliveryZone(tx.QueryRow(ctx, q, zoneID), &z)
                }
                sets = append(sets, "updated_at = now()")
                args = append(args, zoneID)
                whereIdx := idx
                q := fmt.Sprintf(`
                        UPDATE delivery_zones
                           SET %s
                         WHERE id = $%d
                        RETURNING id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active, created_at, updated_at
                `, strings.Join(sets, ", "), whereIdx)
                if err := scanDeliveryZone(tx.QueryRow(ctx, q, args...), &z); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("update delivery zone: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &z, nil
}

// Delete hard-deletes a delivery zone (existing orders keep their
// delivery_zone_id via ON DELETE SET NULL).
func (r *DeliveryZoneRepository) Delete(ctx context.Context, shopID, userID uuid.UUID, role string, zoneID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `DELETE FROM delivery_zones WHERE id = $1`, zoneID)
                if err != nil {
                        return fmt.Errorf("delete delivery zone: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// MatchByQuery finds a zone whose name OR one of its aliases matches the
// query (case-insensitive). Used by the AI to compute delivery fees.
//
// Returns:
//   - the zone, nil if exactly one zone matches
//   - nil, ErrZoneAmbiguous if multiple zones match (the AI must ask the
//     client to clarify)
//   - nil, ErrNotFound if no zone matches
func (r *DeliveryZoneRepository) MatchByQuery(ctx context.Context, shopID, userID uuid.UUID, role string, query string) (*models.DeliveryZone, error) {
        query = strings.TrimSpace(strings.ToLower(query))
        if query == "" {
                return nil, ErrNotFound
        }
        var matches []models.DeliveryZone
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                q := `
                        SELECT id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active, created_at, updated_at
                          FROM delivery_zones
                         WHERE shop_id = $1
                           AND active = true
                           AND (
                                 lower(name) = $2
                                 OR $2 = ANY(string_to_array(lower(array_to_string(aliases, '|')), '|'))
                                 OR lower(name) ILIKE '%' || $2 || '%'
                                 OR EXISTS (SELECT 1 FROM unnest(aliases) AS a WHERE lower(a) = $2)
                           )
                `
                rows, err := tx.Query(ctx, q, shopID, query)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var z models.DeliveryZone
                        if err := scanDeliveryZone(rows, &z); err != nil {
                                return err
                        }
                        matches = append(matches, z)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("delivery zone repo: match by query: %w", err)
        }
        switch len(matches) {
        case 0:
                return nil, ErrNotFound
        case 1:
                return &matches[0], nil
        default:
                return nil, ErrZoneAmbiguous
        }
}

// CalculateFeeResult is the return shape of CalculateFee.
type CalculateFeeResult struct {
        Zone         models.DeliveryZone
        Fee          int64
        FreeDelivery bool
        OrderAmount  int64
        TotalPayable int64
}

// CalculateFee computes the delivery fee for the given zone and order
// amount. Applies:
//   - if free_from != nil AND orderAmount >= *free_from → fee = 0 (free)
//   - if min_order_amount != nil AND orderAmount < *min_order_amount →
//     return ErrOrderBelowMinimum
//   - else fee = zone.Fee
//
// The total payable = orderAmount + fee.
func (r *DeliveryZoneRepository) CalculateFee(ctx context.Context, shopID, userID uuid.UUID, role string, zoneID uuid.UUID, orderAmount int64) (*CalculateFeeResult, error) {
        z, err := r.GetByID(ctx, shopID, userID, role, zoneID)
        if err != nil {
                return nil, err
        }
        if !z.Active {
                return nil, ErrZoneInactive
        }
        if z.MinOrderAmount != nil && orderAmount < *z.MinOrderAmount {
                return nil, ErrOrderBelowMinimum
        }
        fee := z.Fee
        free := false
        if z.FreeFrom != nil && orderAmount >= *z.FreeFrom {
                fee = 0
                free = true
        }
        return &CalculateFeeResult{
                Zone:         *z,
                Fee:          fee,
                FreeDelivery: free,
                OrderAmount:  orderAmount,
                TotalPayable: orderAmount + fee,
        }, nil
}

// CountActive returns the number of active delivery zones for the shop.
// Used for shop activation validation.
func (r *DeliveryZoneRepository) CountActive(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM delivery_zones WHERE shop_id = $1 AND active = true
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("count active zones: %w", err)
        }
        return n, nil
}

// --- Sentinels --------------------------------------------------------------

// ErrZoneAmbiguous is returned when more than one zone matches a query.
var ErrZoneAmbiguous = errors.New("ambiguous zone match")

// ErrZoneInactive is returned when CalculateFee is called on an inactive zone.
var ErrZoneInactive = errors.New("zone is inactive")

// ErrOrderBelowMinimum is returned when the order amount is below the
// zone's min_order_amount.
var ErrOrderBelowMinimum = errors.New("order amount below minimum for zone")

// --- helpers ----------------------------------------------------------------

// scanDeliveryZone maps a delivery_zones row into a models.DeliveryZone.
func scanDeliveryZone(s scanner, z *models.DeliveryZone) error {
        var (
                aliases        []string
                estimatedDelay *string
                freeFrom       *int64
                minOrderAmount *int64
        )
        err := s.Scan(
                &z.ID, &z.ShopID, &z.Name, &aliases, &z.Fee,
                &estimatedDelay, &freeFrom, &minOrderAmount, &z.Active,
                &z.CreatedAt, &z.UpdatedAt,
        )
        if err != nil {
                return err
        }
        z.Aliases = aliases
        z.EstimatedDelay = estimatedDelay
        z.FreeFrom = freeFrom
        z.MinOrderAmount = minOrderAmount
        return nil
}
