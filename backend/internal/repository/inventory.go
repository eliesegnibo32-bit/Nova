// Inventory repository — handles the `inventory` and `stock_movements` tables.
//
// All queries go through db.WithTenantTx so the RLS policies from migration
// 011 are enforced:
//
//      inventory      : shop_id = current_shop_id() OR is_platform_admin()
//      stock_movements: SELECT + INSERT only (no UPDATE/DELETE policies;
//                       a trigger also blocks UPDATE/DELETE — migration 004).
//
// shopID comes from the authenticated session, NOT from the URL.
//
// CRITICAL: Reserve() implements the ATOMIC RESERVATION required by the
// cahier des charges (ch. 4.3). Two clients ordering the last item
// simultaneously must NOT both succeed. The reservation is an atomic UPDATE
// that succeeds only if (on_hand - reserved) >= requested_quantity:
//
//      UPDATE inventory SET reserved = reserved + $1
//       WHERE variant_id = $2 AND shop_id = $3
//         AND on_hand - reserved >= $1
//
// If NO rows are updated, the reservation FAILED (insufficient stock) — we
// return ErrInsufficientStock. There is NO application-level locking: the
// atomicity is guaranteed by the single SQL UPDATE statement under READ
// COMMITTED isolation (Postgres holds a row lock for the duration of the
// UPDATE so concurrent updaters block; the WHERE predicate is re-evaluated
// after the lock is acquired, ensuring only one of them can succeed).
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

// AdjustStockInput is the data needed for a manual adjustment (casse,
// inventory, error...). Delta can be negative. Reason is mandatory.
type AdjustStockInput struct {
        Delta    int
        Reason   string
        AuthorID uuid.UUID
}

// ReceiveStockInput is the data needed for a stock reception (réception,
// retour validé). Quantity must be > 0.
type ReceiveStockInput struct {
        Quantity int
        Reason   string
        AuthorID uuid.UUID
}

// ReserveInput is the data needed for an atomic reservation. OrderID is
// optional (the order may not exist yet when the cart is being prepared).
type ReserveInput struct {
        Quantity int
        OrderID  *uuid.UUID
        AuthorID *uuid.UUID
}

// ReleaseInput is the data needed to release a reservation (cancellation,
// delivery failure).
type ReleaseInput struct {
        Quantity int
        OrderID  *uuid.UUID
        AuthorID *uuid.UUID
}

// ExitStockInput is the data needed to physically exit stock (delivery
// success: on_hand -= q AND reserved -= q).
type ExitStockInput struct {
        Quantity int
        OrderID  *uuid.UUID
        AuthorID *uuid.UUID
}

// ReturnStockInput is the data needed to re-enter returned stock (return
// after delivery: on_hand += q).
type ReturnStockInput struct {
        Quantity int
        OrderID  *uuid.UUID
        AuthorID *uuid.UUID
}

// ListMovementsRepoParams is the input to ListMovements.
type ListMovementsRepoParams struct {
        Page     int
        Limit    int
        Type     string
        From     *time.Time
        To       *time.Time
}

// Offset returns the SQL OFFSET.
func (p ListMovementsRepoParams) Offset() int {
        if p.Page < 1 {
                p.Page = 1
        }
        return (p.Page - 1) * p.Limit
}

// ListInventoryRepoParams is the input to ListByShop.
type ListInventoryRepoParams struct {
        Page   int
        Limit  int
        Filter string // "low_stock" | "out_of_stock" | "all"
        Search string
}

// Offset returns the SQL OFFSET.
func (p ListInventoryRepoParams) Offset() int {
        if p.Page < 1 {
                p.Page = 1
        }
        return (p.Page - 1) * p.Limit
}

// InventoryRepository wraps the inventory + stock_movements tables.
type InventoryRepository struct {
        pool *pgxpool.Pool
}

// NewInventoryRepository returns an InventoryRepository bound to the pool.
func NewInventoryRepository(pool *pgxpool.Pool) *InventoryRepository {
        return &InventoryRepository{pool: pool}
}

// GetByVariant returns the inventory row for a single variant. If the
// variant has no inventory row yet (shouldn't happen — we always create one
// with the variant — but defensive), we return ErrNotFound.
func (r *InventoryRepository) GetByVariant(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID) (*models.Inventory, error) {
        var inv models.Inventory
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                          FROM inventory WHERE variant_id = $1
                `
                return scanInventory(tx.QueryRow(ctx, q, variantID), &inv)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("inventory repo: get by variant: %w", err)
        }
        return &inv, nil
}

// SetStockMode updates the stock_mode of a variant's inventory row.
// mode must be one of: quantite, epuise, illimite.
// When switching to illimite, the on_hand/reserved/available columns are
// NOT touched — the application should treat the variant as having infinite
// stock regardless of those values.
func (r *InventoryRepository) SetStockMode(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, mode string) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE inventory SET stock_mode = $2::stock_mode, updated_at = now()
                         WHERE variant_id = $1
                `, variantID, mode)
                if err != nil {
                        return fmt.Errorf("set stock mode: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// GetStockMode returns the stock_mode for a variant. Returns "quantite" (the
// default) if the variant has no inventory row.
func (r *InventoryRepository) GetStockMode(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID) (string, error) {
        var mode string
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                err := tx.QueryRow(ctx, `SELECT stock_mode::text FROM inventory WHERE variant_id = $1`, variantID).Scan(&mode)
                if err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                mode = "quantite"
                                return nil
                        }
                        return err
                }
                return nil
        })
        if err != nil {
                return "quantite", err
        }
        return mode, nil
}

// Reinstate reverses a definitive ExitStock (on_hand += q). Used when an
// en_cours order is cancelled (status → annulee): the stock that was
// definitively deducted is reinstated. type='reinstate'. No-op for illimite.
// Note: reserved is NOT touched (it was already decremented during ExitStock).
func (r *InventoryRepository) Reinstate(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in ExitStockInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv models.Inventory
                mut models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // Check stock_mode — illimite is a no-op.
                var mode string
                if err := tx.QueryRow(ctx, `SELECT stock_mode::text FROM inventory WHERE variant_id = $1 AND shop_id = $2`, variantID, shopID).Scan(&mode); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("reinstate inventory (mode check): %w", err)
                }
                if mode == string(models.StockModeIllimite) {
                        // No-op: get the current row to return.
                        return scanInventory(tx.QueryRow(ctx, `
                                SELECT variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                                  FROM inventory WHERE variant_id = $1
                        `, variantID), &inv)
                }
                const uq = `
                        UPDATE inventory
                           SET on_hand = on_hand + $2,
                               updated_at = now()
                         WHERE variant_id = $1 AND shop_id = $3
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                if err := scanInventory(tx.QueryRow(ctx, uq, variantID, in.Quantity, shopID), &inv); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("reinstate inventory: %w", err)
                }
                var orderArg any
                if in.OrderID != nil {
                        orderArg = *in.OrderID
                }
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, order_id, author_id)
                        VALUES ($1, $2, 'return', $3, $4, $5)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, in.Quantity, orderArg, authorArg), &mut); err != nil {
                        return fmt.Errorf("insert reinstate movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// InventoryWithVariant is the list-view shape — the inventory row joined
// with variant + product info.
type InventoryWithVariant struct {
        Inventory   models.Inventory
        ProductID   uuid.UUID
        ProductName string
        SKU         string
        Size        *string
        Color       *string
        Price       int64
        Active      bool
}

// ListByShop returns all inventory rows for the shop, joined with variant +
// product info. Filters: low_stock (available <= alert_threshold AND > 0),
// out_of_stock (available <= 0), all. Search is substring on product name
// OR SKU.
func (r *InventoryRepository) ListByShop(ctx context.Context, shopID, userID uuid.UUID, role string, params ListInventoryRepoParams) ([]InventoryWithVariant, int64, error) {
        var (
                out   []InventoryWithVariant
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                where := "WHERE 1=1"
                args := []any{}
                idx := 1
                switch params.Filter {
                case "low_stock":
                        where += " AND i.available > 0 AND i.available <= i.alert_threshold"
                case "out_of_stock":
                        where += " AND i.available <= 0"
                case "all", "":
                        // no extra filter
                }
                if params.Search != "" {
                        where += fmt.Sprintf(" AND (p.name ILIKE $%d OR v.sku ILIKE $%d)", idx, idx)
                        args = append(args, "%"+params.Search+"%")
                        idx++
                }
                // Count.
                countQ := `
                        SELECT COUNT(*)
                          FROM inventory i
                          JOIN product_variants v ON v.id = i.variant_id
                          JOIN products p ON p.id = v.product_id
                ` + where
                if err := tx.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
                        return fmt.Errorf("count inventory: %w", err)
                }
                listQ := `
                        SELECT i.variant_id, i.shop_id, i.on_hand, i.reserved, i.available, i.alert_threshold, i.stock_mode, i.updated_at,
                               p.id, p.name, v.sku, v.size, v.color, v.price, v.active
                          FROM inventory i
                          JOIN product_variants v ON v.id = i.variant_id
                          JOIN products p ON p.id = v.product_id
                ` + where + `
                        ORDER BY i.updated_at DESC
                        LIMIT $` + fmt.Sprintf("%d", idx) + ` OFFSET $` + fmt.Sprintf("%d", idx+1)
                args = append(args, params.Limit, params.Offset())

                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var iv InventoryWithVariant
                        var size, color *string
                        var stockMode string
                        if err := rows.Scan(
                                &iv.Inventory.VariantID, &iv.Inventory.ShopID, &iv.Inventory.OnHand, &iv.Inventory.Reserved,
                                &iv.Inventory.Available, &iv.Inventory.AlertThreshold, &stockMode, &iv.Inventory.UpdatedAt,
                                &iv.ProductID, &iv.ProductName, &iv.SKU, &size, &color, &iv.Price, &iv.Active,
                        ); err != nil {
                                return err
                        }
                        iv.Inventory.StockMode = stockMode
                        iv.Size = size
                        iv.Color = color
                        out = append(out, iv)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("inventory repo: list by shop: %w", err)
        }
        return out, total, nil
}

// SetAlertThreshold updates the alert_threshold for a variant's inventory row.
func (r *InventoryRepository) SetAlertThreshold(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, threshold int) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE inventory SET alert_threshold = $2, updated_at = now()
                         WHERE variant_id = $1
                `, variantID, threshold)
                if err != nil {
                        return fmt.Errorf("set alert threshold: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// AdjustStock applies a manual adjustment to on_hand (delta can be
// negative). Reason is mandatory (validated by the service before calling).
// Creates a stock_movement with type='adjustment' and quantity=delta.
// Returns the updated inventory + the new movement.
//
// Constraints:
//   - on_hand must stay >= 0 (DB CHECK). If a negative delta would push
//     on_hand below 0, the UPDATE returns 0 rows affected → we return
//     ErrInsufficientStock.
//   - reserved must stay <= on_hand (DB CHECK). Adjustments don't touch
//     reserved, so this is fine as long as on_hand >= reserved after the
//     adjustment. If a negative delta would push on_hand below reserved,
//     the UPDATE returns 0 rows affected → ErrInsufficientStock.
func (r *InventoryRepository) AdjustStock(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in AdjustStockInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv     models.Inventory
                mutType = "adjustment"
                mut     models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // Atomic update — on_hand += delta only if the result stays >= reserved.
                const uq = `
                        UPDATE inventory
                           SET on_hand = on_hand + $2,
                               updated_at = now()
                         WHERE variant_id = $1
                           AND on_hand + $2 >= reserved
                           AND on_hand + $2 >= 0
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                if err := scanInventory(tx.QueryRow(ctx, uq, variantID, in.Delta), &inv); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrInsufficientStock
                        }
                        return fmt.Errorf("adjust inventory: %w", err)
                }
                // Insert the movement.
                reason := nullableString(in.Reason)
                var author any
                if in.AuthorID != uuid.Nil {
                        author = in.AuthorID
                }
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, reason, author_id)
                        VALUES ($1, $2, $3, $4, $5, $6)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, mutType, in.Delta, reason, author), &mut); err != nil {
                        return fmt.Errorf("insert adjustment movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// ReceiveStock adds a positive quantity to on_hand. type='receipt'.
// Quantity must be > 0 (validated by the service).
func (r *InventoryRepository) ReceiveStock(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in ReceiveStockInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv models.Inventory
                mut models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const uq = `
                        UPDATE inventory
                           SET on_hand = on_hand + $2, updated_at = now()
                         WHERE variant_id = $1
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                if err := scanInventory(tx.QueryRow(ctx, uq, variantID, in.Quantity), &inv); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("receive inventory: %w", err)
                }
                reason := nullableString(in.Reason)
                var author any
                if in.AuthorID != uuid.Nil {
                        author = in.AuthorID
                }
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, reason, author_id)
                        VALUES ($1, $2, 'receipt', $3, $4, $5)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, in.Quantity, reason, author), &mut); err != nil {
                        return fmt.Errorf("insert receipt movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// Reserve performs an ATOMIC reservation of `quantity` units.
//
// Per cahier des charges (ch. 4.3) + NOVA v3 (spec section 2):
//   - stock_mode = 'quantite' : atomic UPDATE reserved += q WHERE on_hand - reserved >= q
//   - stock_mode = 'epuise'   : return ErrInsufficientStock (NOVA won't propose)
//   - stock_mode = 'illimite' : NO-OP (no decrement, no movement)
//
// Per cahier des charges (ch. 4.3):
//   "UPDATE inventory SET reserved = reserved + $1
//    WHERE variant_id = $2 AND shop_id = $3 AND on_hand - reserved >= $1"
//
// If NO rows are updated → insufficient stock → ErrInsufficientStock.
// If 1 row updated → success, we then insert a stock_movement (type=
// 'reservation') and return the updated inventory.
//
// Atomicity: Postgres holds a row-level lock for the duration of the
// UPDATE; concurrent updaters block on the same row. The WHERE predicate
// is re-evaluated after the lock is acquired, so only one of two concurrent
// transactions can succeed if there's only enough stock for one.
//
// quantity MUST be > 0 (validated by the service).
func (r *InventoryRepository) Reserve(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in ReserveInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv models.Inventory
                mut models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // NOVA v3: check stock_mode first.
                var mode string
                err := tx.QueryRow(ctx, `SELECT stock_mode::text FROM inventory WHERE variant_id = $1 AND shop_id = $2`, variantID, shopID).Scan(&mode)
                if err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("reserve inventory (mode check): %w", err)
                }
                if mode == string(models.StockModeIllimite) {
                        // No-op — return the current row.
                        return scanInventory(tx.QueryRow(ctx, `
                                SELECT variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                                  FROM inventory WHERE variant_id = $1
                        `, variantID), &inv)
                }
                if mode == string(models.StockModeEpuise) {
                        return ErrInsufficientStock
                }
                const uq = `
                        UPDATE inventory
                           SET reserved = reserved + $2,
                               updated_at = now()
                         WHERE variant_id = $1
                           AND shop_id = $3
                           AND on_hand - reserved >= $2
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                ct, err := tx.Exec(ctx, uq, variantID, in.Quantity, shopID)
                if err != nil {
                        return fmt.Errorf("reserve inventory (exec): %w", err)
                }
                if ct.RowsAffected() == 0 {
                        // Either the variant doesn't exist OR there's
                        // insufficient stock. We distinguish by fetching
                        // the inventory row — if it doesn't exist, return
                        // ErrNotFound; if it does, ErrInsufficientStock.
                        var exists bool
                        if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory WHERE variant_id = $1 AND shop_id = $2)`, variantID, shopID).Scan(&exists); err != nil {
                                return fmt.Errorf("reserve inventory (existence check): %w", err)
                        }
                        if !exists {
                                return ErrNotFound
                        }
                        return ErrInsufficientStock
                }
                // Fetch the updated row (we used Exec, not QueryRow, so we
                // need a separate SELECT).
                const sq = `
                        SELECT variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                          FROM inventory WHERE variant_id = $1
                `
                if err := scanInventory(tx.QueryRow(ctx, sq, variantID), &inv); err != nil {
                        return fmt.Errorf("reserve inventory (select after update): %w", err)
                }
                // Insert the movement. quantity is POSITIVE in the journal
                // (the sign convention is: positive = entry into reserved).
                var orderArg any
                if in.OrderID != nil {
                        orderArg = *in.OrderID
                }
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, order_id, author_id)
                        VALUES ($1, $2, 'reservation', $3, $4, $5)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, in.Quantity, orderArg, authorArg), &mut); err != nil {
                        return fmt.Errorf("insert reservation movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// Release releases a previously-reserved quantity (reserved -= q, clamped
// at 0). Used on order cancellation / delivery failure. type='release'.
// quantity is recorded as NEGATIVE in the journal (release = exit from
// reserved).
//
// NOVA v3 (spec section 2): if stock_mode = 'illimite', Release is a NO-OP
// (no decrement, no movement) — nothing was reserved in the first place.
func (r *InventoryRepository) Release(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in ReleaseInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv models.Inventory
                mut models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // NOVA v3: check stock_mode — illimite is a no-op.
                var mode string
                if err := tx.QueryRow(ctx, `SELECT stock_mode::text FROM inventory WHERE variant_id = $1 AND shop_id = $2`, variantID, shopID).Scan(&mode); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("release inventory (mode check): %w", err)
                }
                if mode == string(models.StockModeIllimite) {
                        // No-op — return the current row.
                        return scanInventory(tx.QueryRow(ctx, `
                                SELECT variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                                  FROM inventory WHERE variant_id = $1
                        `, variantID), &inv)
                }
                const uq = `
                        UPDATE inventory
                           SET reserved = GREATEST(reserved - $2, 0),
                               updated_at = now()
                         WHERE variant_id = $1 AND shop_id = $3
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                if err := scanInventory(tx.QueryRow(ctx, uq, variantID, in.Quantity, shopID), &inv); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("release inventory: %w", err)
                }
                var orderArg any
                if in.OrderID != nil {
                        orderArg = *in.OrderID
                }
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                // Negative quantity (release is exiting reserved).
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, order_id, author_id)
                        VALUES ($1, $2, 'release', $3, $4, $5)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, -in.Quantity, orderArg, authorArg), &mut); err != nil {
                        return fmt.Errorf("insert release movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// ExitStock physically exits stock on delivery: on_hand -= q AND
// reserved -= q. type='exit'. The atomic UPDATE ensures on_hand - q >= 0
// AND reserved - q >= 0; if either would go negative, 0 rows affected
// → ErrInsufficientStock.
//
// NOVA v3 (spec section 2): if stock_mode = 'illimite', ExitStock is a NO-OP
// (no decrement, no movement) — the variant has infinite stock. Only called
// on transition to en_cours (definitive deduction).
func (r *InventoryRepository) ExitStock(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in ExitStockInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv models.Inventory
                mut models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // NOVA v3: check stock_mode — illimite is a no-op.
                var mode string
                if err := tx.QueryRow(ctx, `SELECT stock_mode::text FROM inventory WHERE variant_id = $1 AND shop_id = $2`, variantID, shopID).Scan(&mode); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("exit inventory (mode check): %w", err)
                }
                if mode == string(models.StockModeIllimite) {
                        // No-op — return the current row.
                        return scanInventory(tx.QueryRow(ctx, `
                                SELECT variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                                  FROM inventory WHERE variant_id = $1
                        `, variantID), &inv)
                }
                const uq = `
                        UPDATE inventory
                           SET on_hand = on_hand - $2,
                               reserved = reserved - $2,
                               updated_at = now()
                         WHERE variant_id = $1
                           AND shop_id = $3
                           AND on_hand - $2 >= 0
                           AND reserved - $2 >= 0
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                if err := scanInventory(tx.QueryRow(ctx, uq, variantID, in.Quantity, shopID), &inv); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                // Distinguish not-found from insufficient.
                                var exists bool
                                if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory WHERE variant_id = $1 AND shop_id = $2)`, variantID, shopID).Scan(&exists); err != nil {
                                        return fmt.Errorf("exit inventory (existence check): %w", err)
                                }
                                if !exists {
                                        return ErrNotFound
                                }
                                return ErrInsufficientStock
                        }
                        return fmt.Errorf("exit inventory: %w", err)
                }
                var orderArg any
                if in.OrderID != nil {
                        orderArg = *in.OrderID
                }
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                // Negative quantity (exit is exiting on_hand).
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, order_id, author_id)
                        VALUES ($1, $2, 'exit', $3, $4, $5)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, -in.Quantity, orderArg, authorArg), &mut); err != nil {
                        return fmt.Errorf("insert exit movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// ReturnStock re-enters returned goods: on_hand += q. type='return'.
// Used when a delivery is refused and goods come back.
func (r *InventoryRepository) ReturnStock(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in ReturnStockInput) (*models.Inventory, *models.StockMovement, error) {
        var (
                inv models.Inventory
                mut models.StockMovement
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const uq = `
                        UPDATE inventory
                           SET on_hand = on_hand + $2, updated_at = now()
                         WHERE variant_id = $1 AND shop_id = $3
                        RETURNING variant_id, shop_id, on_hand, reserved, available, alert_threshold, stock_mode, updated_at
                `
                if err := scanInventory(tx.QueryRow(ctx, uq, variantID, in.Quantity, shopID), &inv); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("return inventory: %w", err)
                }
                var orderArg any
                if in.OrderID != nil {
                        orderArg = *in.OrderID
                }
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                const mq = `
                        INSERT INTO stock_movements (variant_id, shop_id, type, quantity, order_id, author_id)
                        VALUES ($1, $2, 'return', $3, $4, $5)
                        RETURNING id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                `
                if err := scanMovement(tx.QueryRow(ctx, mq, variantID, shopID, in.Quantity, orderArg, authorArg), &mut); err != nil {
                        return fmt.Errorf("insert return movement: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &inv, &mut, nil
}

// ListMovements returns the movement history for a variant (or all
// variants if variantID is nil), filtered by type and date range.
func (r *InventoryRepository) ListMovements(ctx context.Context, shopID, userID uuid.UUID, role string, variantID *uuid.UUID, params ListMovementsRepoParams) ([]models.StockMovement, int64, error) {
        var (
                out   []models.StockMovement
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                where := "WHERE shop_id = $1"
                args := []any{shopID}
                idx := 2
                if variantID != nil {
                        where += fmt.Sprintf(" AND variant_id = $%d", idx)
                        args = append(args, *variantID)
                        idx++
                }
                if params.Type != "" {
                        where += fmt.Sprintf(" AND type = $%d::stock_movement_type", idx)
                        args = append(args, params.Type)
                        idx++
                }
                if params.From != nil {
                        where += fmt.Sprintf(" AND created_at >= $%d", idx)
                        args = append(args, *params.From)
                        idx++
                }
                if params.To != nil {
                        where += fmt.Sprintf(" AND created_at <= $%d", idx)
                        args = append(args, *params.To)
                        idx++
                }
                if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM stock_movements "+where, args...).Scan(&total); err != nil {
                        return fmt.Errorf("count movements: %w", err)
                }
                listQ := `
                        SELECT id, variant_id, shop_id, type, quantity, reason, order_id, author_id, created_at
                          FROM stock_movements
                ` + where + `
                        ORDER BY created_at DESC
                        LIMIT $` + fmt.Sprintf("%d", idx) + ` OFFSET $` + fmt.Sprintf("%d", idx+1)
                args = append(args, params.Limit, params.Offset())
                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var m models.StockMovement
                        if err := scanMovement(rows, &m); err != nil {
                                return err
                        }
                        out = append(out, m)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("inventory repo: list movements: %w", err)
        }
        return out, total, nil
}

// CountLowStock returns the number of variants whose available is > 0 AND
// <= alert_threshold. For the dashboard.
func (r *InventoryRepository) CountLowStock(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM inventory
                         WHERE shop_id = $1 AND available > 0 AND available <= alert_threshold
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("count low stock: %w", err)
        }
        return n, nil
}

// CountOutOfStock returns the number of variants with available <= 0.
func (r *InventoryRepository) CountOutOfStock(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM inventory
                         WHERE shop_id = $1 AND available <= 0
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("count out of stock: %w", err)
        }
        return n, nil
}

// TotalValue returns the total catalog value (sum of on_hand * price) and
// the total reserved value (sum of reserved * price) for the shop.
func (r *InventoryRepository) TotalValue(ctx context.Context, shopID, userID uuid.UUID, role string) (totalValue, reservedValue int64, err error) {
        err = db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COALESCE(SUM(i.on_hand * v.price), 0),
                               COALESCE(SUM(i.reserved * v.price), 0)
                          FROM inventory i
                          JOIN product_variants v ON v.id = i.variant_id
                         WHERE i.shop_id = $1
                `, shopID).Scan(&totalValue, &reservedValue)
        })
        if err != nil {
                return 0, 0, fmt.Errorf("total value: %w", err)
        }
        return totalValue, reservedValue, nil
}

// CountVariants returns the total number of variants for the shop.
func (r *InventoryRepository) CountVariants(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM product_variants WHERE shop_id = $1
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("count variants: %w", err)
        }
        return n, nil
}

// --- Sentinels --------------------------------------------------------------

// ErrInsufficientStock is returned when a reservation/exit/adjustment
// cannot be performed because the available stock is insufficient.
var ErrInsufficientStock = errors.New("insufficient stock")

// --- helpers ----------------------------------------------------------------

// scanInventory maps an inventory row into a models.Inventory.
// Expects 8 columns: variant_id, shop_id, on_hand, reserved, available,
// alert_threshold, stock_mode, updated_at.
func scanInventory(s scanner, i *models.Inventory) error {
        var stockMode string
        err := s.Scan(
                &i.VariantID, &i.ShopID, &i.OnHand, &i.Reserved,
                &i.Available, &i.AlertThreshold, &stockMode, &i.UpdatedAt,
        )
        if err != nil {
                return err
        }
        i.StockMode = stockMode
        return nil
}

// scanMovement maps a stock_movements row into a models.StockMovement.
func scanMovement(s scanner, m *models.StockMovement) error {
        var (
                reason   *string
                orderID  *uuid.UUID
                authorID *uuid.UUID
                typ      string
        )
        err := s.Scan(
                &m.ID, &m.VariantID, &m.ShopID, &typ, &m.Quantity,
                &reason, &orderID, &authorID, &m.CreatedAt,
        )
        if err != nil {
                return err
        }
        m.Type = typ
        m.Reason = reason
        m.OrderID = orderID
        m.AuthorID = authorID
        return nil
}
