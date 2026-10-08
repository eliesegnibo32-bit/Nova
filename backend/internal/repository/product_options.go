// Product options repository — handles the `product_options` table
// (NOVA v3 — spec section 3).
//
// All queries go through db.WithTenantTx so the RLS policies from migration
// 015 are enforced:
//
//      shop_id = current_shop_id() OR is_platform_admin()
//
// shopID comes from the authenticated session, NOT from the URL.
//
// Stock management for options mirrors inventory:
//   - quantite: stock_qty is decremented on order (caller-managed)
//   - epuise:   option is unavailable (NOVA won't propose)
//   - illimite: stock_qty is NULL, never decremented
package repository

import (
        "context"
        "errors"
        "fmt"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// --- Sentinels --------------------------------------------------------------

// ErrProductOptionNotFound is returned when a product option doesn't exist.
var ErrProductOptionNotFound = errors.New("product option not found")

// --- Inputs -----------------------------------------------------------------

// CreateProductOptionInput is the data needed to create a product option.
type CreateProductOptionInput struct {
        ProductID *uuid.UUID
        Type      string // plat | accompagnement | boisson
        Name      string
        Price     int64
        StockMode string // quantite | epuise | illimite
        StockQty  *int
        Active    bool
}

// UpdateProductOptionInput mirrors models.UpdateProductOptionRequest.
type UpdateProductOptionInput struct {
        Name      *string
        Price     *int64
        StockMode *string
        StockQty  *int
        Active    *bool
}

// --- Repository -------------------------------------------------------------

// ProductOptionRepository wraps the product_options table.
type ProductOptionRepository struct {
        pool *pgxpool.Pool
}

// NewProductOptionRepository returns a ProductOptionRepository bound to the pool.
func NewProductOptionRepository(pool *pgxpool.Pool) *ProductOptionRepository {
        return &ProductOptionRepository{pool: pool}
}

// Create inserts a new product option.
func (r *ProductOptionRepository) Create(ctx context.Context, shopID, userID uuid.UUID, role string, in CreateProductOptionInput) (*models.ProductOption, error) {
        if in.StockMode == "" {
                in.StockMode = string(models.StockModeQuantite)
        }
        var opt models.ProductOption
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                var productArg any
                if in.ProductID != nil {
                        productArg = *in.ProductID
                }
                var stockQtyArg any
                if in.StockQty != nil {
                        stockQtyArg = *in.StockQty
                }
                const q = `
                        INSERT INTO product_options (shop_id, product_id, type, name, price, stock_mode, stock_qty, active)
                        VALUES ($1, $2, $3::product_option_type, $4, $5, $6::stock_mode, $7, $8)
                        RETURNING id, shop_id, product_id, type, name, price, stock_mode, stock_qty, active, created_at, updated_at
                `
                return scanProductOption(tx.QueryRow(ctx, q,
                        shopID, productArg, in.Type, in.Name, in.Price, in.StockMode, stockQtyArg, in.Active,
                ), &opt)
        })
        if err != nil {
                return nil, fmt.Errorf("product option repo: create: %w", err)
        }
        return &opt, nil
}

// GetByID returns a single product option.
func (r *ProductOptionRepository) GetByID(ctx context.Context, shopID, userID uuid.UUID, role string, id uuid.UUID) (*models.ProductOption, error) {
        var opt models.ProductOption
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, product_id, type, name, price, stock_mode, stock_qty, active, created_at, updated_at
                          FROM product_options WHERE id = $1
                `
                return scanProductOption(tx.QueryRow(ctx, q, id), &opt)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrProductOptionNotFound
                }
                return nil, fmt.Errorf("product option repo: get by id: %w", err)
        }
        return &opt, nil
}

// ListByShop returns the product options for a shop, optionally filtered by type.
func (r *ProductOptionRepository) ListByShop(ctx context.Context, shopID, userID uuid.UUID, role string, optType string, onlyActive bool) ([]models.ProductOption, error) {
        var out []models.ProductOption
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                q := `SELECT id, shop_id, product_id, type, name, price, stock_mode, stock_qty, active, created_at, updated_at
                        FROM product_options WHERE shop_id = $1`
                args := []any{shopID}
                idx := 2
                if optType != "" {
                        q += fmt.Sprintf(" AND type = $%d::product_option_type", idx)
                        args = append(args, optType)
                        idx++
                }
                if onlyActive {
                        q += " AND active = true"
                }
                q += " ORDER BY type, name"
                rows, err := tx.Query(ctx, q, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var opt models.ProductOption
                        if err := scanProductOption(rows, &opt); err != nil {
                                return err
                        }
                        out = append(out, opt)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("product option repo: list by shop: %w", err)
        }
        if out == nil {
                out = []models.ProductOption{}
        }
        return out, nil
}

// ListByProduct returns the options attached to a product.
func (r *ProductOptionRepository) ListByProduct(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID) ([]models.ProductOption, error) {
        var out []models.ProductOption
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                rows, err := tx.Query(ctx, `
                        SELECT id, shop_id, product_id, type, name, price, stock_mode, stock_qty, active, created_at, updated_at
                          FROM product_options
                         WHERE shop_id = $1 AND product_id = $2 AND active = true
                         ORDER BY type, name
                `, shopID, productID)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var opt models.ProductOption
                        if err := scanProductOption(rows, &opt); err != nil {
                                return err
                        }
                        out = append(out, opt)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("product option repo: list by product: %w", err)
        }
        if out == nil {
                out = []models.ProductOption{}
        }
        return out, nil
}

// Update applies a partial update to a product option.
func (r *ProductOptionRepository) Update(ctx context.Context, shopID, userID uuid.UUID, role string, id uuid.UUID, in UpdateProductOptionInput) (*models.ProductOption, error) {
        var opt models.ProductOption
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                sets := []string{"updated_at = now()"}
                args := []any{id}
                idx := 2
                if in.Name != nil {
                        sets = append(sets, fmt.Sprintf("name = $%d", idx))
                        args = append(args, *in.Name)
                        idx++
                }
                if in.Price != nil {
                        sets = append(sets, fmt.Sprintf("price = $%d", idx))
                        args = append(args, *in.Price)
                        idx++
                }
                if in.StockMode != nil {
                        sets = append(sets, fmt.Sprintf("stock_mode = $%d::stock_mode", idx))
                        args = append(args, *in.StockMode)
                        idx++
                }
                if in.StockQty != nil {
                        sets = append(sets, fmt.Sprintf("stock_qty = $%d", idx))
                        args = append(args, *in.StockQty)
                        idx++
                }
                if in.Active != nil {
                        sets = append(sets, fmt.Sprintf("active = $%d", idx))
                        args = append(args, *in.Active)
                        idx++
                }
                q := fmt.Sprintf(`
                        UPDATE product_options SET %s WHERE id = $1
                        RETURNING id, shop_id, product_id, type, name, price, stock_mode, stock_qty, active, created_at, updated_at
                `, joinStringsOpts(sets, ", "))
                return scanProductOption(tx.QueryRow(ctx, q, args...), &opt)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrProductOptionNotFound
                }
                return nil, fmt.Errorf("product option repo: update: %w", err)
        }
        return &opt, nil
}

// Delete removes a product option.
func (r *ProductOptionRepository) Delete(ctx context.Context, shopID, userID uuid.UUID, role string, id uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `DELETE FROM product_options WHERE id = $1 AND shop_id = $2`, id, shopID)
                if err != nil {
                        return fmt.Errorf("delete product option: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrProductOptionNotFound
                }
                return nil
        })
}

// DecrementStock atomically decrements the stock_qty of an option (only if
// stock_mode = 'quantite'). Returns ErrInsufficientStock if the option is
// out of stock or the decrement would go below 0. No-op for illimite.
// Returns ErrProductOptionNotFound if the option doesn't exist.
func (r *ProductOptionRepository) DecrementStock(ctx context.Context, shopID, userID uuid.UUID, role string, id uuid.UUID, qty int) error {
        if qty <= 0 {
                return nil
        }
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE product_options
                           SET stock_qty = stock_qty - $2,
                               updated_at = now()
                         WHERE id = $1
                           AND shop_id = $3
                           AND stock_mode = 'quantite'
                           AND stock_qty >= $2
                `, id, qty, shopID)
                if err != nil {
                        return fmt.Errorf("decrement product option stock: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        // Either not found, or insufficient stock, or illimite (no-op).
                        var mode string
                        err := tx.QueryRow(ctx, `SELECT stock_mode FROM product_options WHERE id = $1 AND shop_id = $2`, id, shopID).Scan(&mode)
                        if err != nil {
                                if errors.Is(err, pgx.ErrNoRows) {
                                        return ErrProductOptionNotFound
                                }
                                return fmt.Errorf("decrement product option stock (check): %w", err)
                        }
                        if mode == string(models.StockModeIllimite) {
                                return nil // no-op for illimite
                        }
                        if mode == string(models.StockModeEpuise) {
                                return ErrInsufficientStock
                        }
                        return ErrInsufficientStock
                }
                return nil
        })
}

// --- helpers ----------------------------------------------------------------

// scanProductOption maps a product_options row into a models.ProductOption.
func scanProductOption(s scanner, o *models.ProductOption) error {
        var (
                productID *uuid.UUID
                optType   string
                stockMode string
                stockQty  *int
        )
        err := s.Scan(
                &o.ID, &o.ShopID, &productID, &optType, &o.Name, &o.Price,
                &stockMode, &stockQty, &o.Active, &o.CreatedAt, &o.UpdatedAt,
        )
        if err != nil {
                return err
        }
        o.ProductID = productID
        o.Type = models.ProductOptionType(optType)
        o.StockMode = models.StockMode(stockMode)
        o.StockQty = stockQty
        return nil
}

// joinStringsOpts joins a slice of strings with the given separator.
// (Avoids importing strings just for one call.)
func joinStringsOpts(parts []string, sep string) string {
        if len(parts) == 0 {
                return ""
        }
        out := parts[0]
        for _, p := range parts[1:] {
                out += sep + p
        }
        return out
}
