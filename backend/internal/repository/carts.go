// Cart repository — handles the `carts` and `cart_items` tables (ch. 4.5).
//
// All queries go through db.WithTenantTx so the RLS policies from migration
// 011 are enforced:
//
//      carts / cart_items : shop_id = current_shop_id() OR is_platform_admin()
//
// shopID comes from the authenticated session, NOT from the URL.
//
// Invariants:
//   - A conversation has at most one ACTIVE cart at a time. GetOrCreateActive
//     enforces this by returning the existing active cart if one exists, or
//     creating a new one.
//   - cart_items.product_name + unit_price are SNAPSHOTS at insertion time
//     so the cart display stays stable even if the catalog changes between
//     add-to-cart and confirm.
//   - When the same variant is added twice, AddItem increments the quantity
//     on the existing row (instead of creating a duplicate line).
//   - cart_items.quantity is CHECK > 0 in the DB. UpdateItemQuantity with 0
//     removes the row.
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

// CartRepository wraps the carts + cart_items tables.
type CartRepository struct {
        pool *pgxpool.Pool
}

// NewCartRepository returns a CartRepository bound to the pool.
func NewCartRepository(pool *pgxpool.Pool) *CartRepository {
        return &CartRepository{pool: pool}
}

// --- Sentinels --------------------------------------------------------------

// ErrCartNotFound is returned when a cart doesn't exist or is not visible to
// the tenant.
var ErrCartNotFound = errors.New("cart not found")

// ErrCartItemNotFound is returned when a cart item doesn't exist.
var ErrCartItemNotFound = errors.New("cart item not found")

// ErrCartNotActive is returned when an operation requires an active cart but
// the cart is abandoned or converted.
var ErrCartNotActive = errors.New("cart is not active")

// ErrInvalidQuantity is returned when a quantity argument is not positive.
var ErrInvalidQuantity = errors.New("quantity must be greater than zero")

// --- Cart methods -----------------------------------------------------------

// GetOrCreateActive returns the active cart for the (shopID, conversationID)
// tuple, or creates a new one if none exists. If conversationID is nil, a
// fresh cart is always created (this is used for testing/direct API access).
//
// The cart is created with expires_at = now + 24h (per ch. 4.5 "expiration
// du panier après 24h").
func (r *CartRepository) GetOrCreateActive(ctx context.Context, shopID, userID uuid.UUID, role string, conversationID, customerID *uuid.UUID) (*models.Cart, error) {
        var cart models.Cart
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // Try to find an existing active cart for this conversation.
                if conversationID != nil {
                        const selQ = `
                                SELECT id, shop_id, conversation_id, customer_id, status, expires_at, created_at, updated_at
                                  FROM carts
                                 WHERE shop_id = $1 AND conversation_id = $2 AND status = 'active'
                                 LIMIT 1
                        `
                        err := scanCart(tx.QueryRow(ctx, selQ, shopID, *conversationID), &cart)
                        if err == nil {
                                return nil
                        }
                        if !errors.Is(err, pgx.ErrNoRows) {
                                return fmt.Errorf("cart repo: get active: %w", err)
                        }
                }
                // Insert a new cart. expires_at = now + 24h.
                expires := time.Now().Add(24 * time.Hour)
                var convArg, custArg any
                if conversationID != nil {
                        convArg = *conversationID
                }
                if customerID != nil {
                        custArg = *customerID
                }
                const insQ = `
                        INSERT INTO carts (shop_id, conversation_id, customer_id, status, expires_at)
                        VALUES ($1, $2, $3, 'active', $4)
                        RETURNING id, shop_id, conversation_id, customer_id, status, expires_at, created_at, updated_at
                `
                if err := scanCart(tx.QueryRow(ctx, insQ, shopID, convArg, custArg, expires), &cart); err != nil {
                        return fmt.Errorf("cart repo: create: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        // Load items for the freshly-created/retrieved cart.
        items, err := r.listItems(ctx, shopID, userID, role, cart.ID)
        if err != nil {
                return nil, err
        }
        cart.Items = items
        return &cart, nil
}

// GetByID returns a cart with items joined with variant info (variant_info,
// sku, available) for display.
func (r *CartRepository) GetByID(ctx context.Context, shopID, userID uuid.UUID, role string, cartID uuid.UUID) (*models.Cart, error) {
        var cart models.Cart
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, conversation_id, customer_id, status, expires_at, created_at, updated_at
                          FROM carts WHERE id = $1
                `
                if err := scanCart(tx.QueryRow(ctx, q, cartID), &cart); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrCartNotFound
                        }
                        return fmt.Errorf("cart repo: get by id: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        items, err := r.listItems(ctx, shopID, userID, role, cart.ID)
        if err != nil {
                return nil, err
        }
        cart.Items = items
        return &cart, nil
}

// GetActiveByConversation returns the active cart for a conversation, or
// (nil, nil) if no active cart exists. Not-found is NOT an error here.
func (r *CartRepository) GetActiveByConversation(ctx context.Context, shopID, userID uuid.UUID, role string, conversationID uuid.UUID) (*models.Cart, error) {
        var cart models.Cart
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, conversation_id, customer_id, status, expires_at, created_at, updated_at
                          FROM carts
                         WHERE shop_id = $1 AND conversation_id = $2 AND status = 'active'
                         LIMIT 1
                `
                if err := scanCart(tx.QueryRow(ctx, q, shopID, conversationID), &cart); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return nil
                        }
                        return fmt.Errorf("cart repo: get active by conv: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        if cart.ID == uuid.Nil {
                return nil, nil
        }
        items, err := r.listItems(ctx, shopID, userID, role, cart.ID)
        if err != nil {
                return nil, err
        }
        cart.Items = items
        return &cart, nil
}

// AddItemInput is the data needed to add a line to a cart.
type AddItemInput struct {
        VariantID   uuid.UUID
        ProductName string // snapshot
        VariantInfo string // snapshot ("Taille M, Bleu")
        UnitPrice   int64  // snapshot (FCFA, effective price at add time)
        Quantity    int
}

// AddItem inserts a new cart_items row or increments the quantity of an
// existing one (same cart + same variant). Returns the new/updated item.
//
// Caller is responsible for validating stock availability — the DB does NOT
// check inventory here (the reservation happens at order confirmation time).
func (r *CartRepository) AddItem(ctx context.Context, shopID, userID uuid.UUID, role string, cartID uuid.UUID, in AddItemInput) (*models.CartItem, error) {
        if in.Quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        var item models.CartItem
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // Verify cart is active + belongs to shop.
                var status string
                if err := tx.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, cartID).Scan(&status); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrCartNotFound
                        }
                        return fmt.Errorf("cart repo: add item: check cart: %w", err)
                }
                if status != "active" {
                        return ErrCartNotActive
                }
                // Try to increment an existing row.
                var variantArg any = in.VariantID
                const upQ = `
                        UPDATE cart_items
                           SET quantity = quantity + $1
                         WHERE cart_id = $2 AND variant_id = $3
                         RETURNING id, cart_id, shop_id, variant_id, product_name, unit_price, quantity
                `
                err := scanCartItem(tx.QueryRow(ctx, upQ, in.Quantity, cartID, variantArg), &item)
                if err == nil {
                        return nil
                }
                if !errors.Is(err, pgx.ErrNoRows) {
                        return fmt.Errorf("cart repo: add item: update: %w", err)
                }
                // No existing row — insert a new one.
                const insQ = `
                        INSERT INTO cart_items (cart_id, shop_id, variant_id, product_name, unit_price, quantity)
                        VALUES ($1, $2, $3, $4, $5, $6)
                        RETURNING id, cart_id, shop_id, variant_id, product_name, unit_price, quantity
                `
                if err := scanCartItem(tx.QueryRow(ctx, insQ, cartID, shopID, variantArg, in.ProductName, in.UnitPrice, in.Quantity), &item); err != nil {
                        return fmt.Errorf("cart repo: add item: insert: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &item, nil
}

// UpdateItemQuantity sets the quantity of a cart item. If quantity <= 0,
// removes the item. Returns ErrCartItemNotFound if the item doesn't exist.
func (r *CartRepository) UpdateItemQuantity(ctx context.Context, shopID, userID uuid.UUID, role string, cartID, itemID uuid.UUID, quantity int) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                if quantity <= 0 {
                        ct, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE id = $1 AND cart_id = $2`, itemID, cartID)
                        if err != nil {
                                return fmt.Errorf("cart repo: update item: delete: %w", err)
                        }
                        if ct.RowsAffected() == 0 {
                                return ErrCartItemNotFound
                        }
                        return nil
                }
                ct, err := tx.Exec(ctx, `
                        UPDATE cart_items SET quantity = $1
                         WHERE id = $2 AND cart_id = $3
                `, quantity, itemID, cartID)
                if err != nil {
                        return fmt.Errorf("cart repo: update item: update: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCartItemNotFound
                }
                return nil
        })
}

// RemoveItem deletes a single cart item.
func (r *CartRepository) RemoveItem(ctx context.Context, shopID, userID uuid.UUID, role string, cartID, itemID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE id = $1 AND cart_id = $2`, itemID, cartID)
                if err != nil {
                        return fmt.Errorf("cart repo: remove item: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCartItemNotFound
                }
                return nil
        })
}

// Clear removes all items from a cart.
func (r *CartRepository) Clear(ctx context.Context, shopID, userID uuid.UUID, role string, cartID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                _, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cartID)
                if err != nil {
                        return fmt.Errorf("cart repo: clear: %w", err)
                }
                return nil
        })
}

// Abandon sets status='abandoned'. Used on expiration.
func (r *CartRepository) Abandon(ctx context.Context, shopID, userID uuid.UUID, role string, cartID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `UPDATE carts SET status = 'abandoned', updated_at = now() WHERE id = $1 AND status = 'active'`, cartID)
                if err != nil {
                        return fmt.Errorf("cart repo: abandon: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCartNotFound
                }
                return nil
        })
}

// MarkConverted sets status='converted' and links the order_id.
func (r *CartRepository) MarkConverted(ctx context.Context, shopID, userID uuid.UUID, role string, cartID, orderID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `UPDATE carts SET status = 'converted', updated_at = now() WHERE id = $1 AND status = 'active'`, cartID)
                if err != nil {
                        return fmt.Errorf("cart repo: mark converted: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCartNotFound
                }
                // The orders table references cart_id; we don't update it
                // here (cart_id is set on the order at creation time).
                return nil
        })
}

// ExpireOldCarts abandons all active carts whose expires_at < before.
// Returns the number of carts abandoned. Used by a cron job.
func (r *CartRepository) ExpireOldCarts(ctx context.Context, before time.Time) (int64, error) {
        var n int64
        // This is a platform-level cron job; we run it as super_admin.
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, "super_admin", func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE carts SET status = 'abandoned', updated_at = now()
                         WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at < $1
                `, before)
                if err != nil {
                        return fmt.Errorf("cart repo: expire old carts: %w", err)
                }
                n = ct.RowsAffected()
                return nil
        })
        if err != nil {
                return 0, err
        }
        return n, nil
}

// --- helpers ----------------------------------------------------------------

// listItems returns the cart items joined with variant info (variant_info,
// sku, available) for display.
func (r *CartRepository) listItems(ctx context.Context, shopID, userID uuid.UUID, role string, cartID uuid.UUID) ([]models.CartItem, error) {
        var out []models.CartItem
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT ci.id, ci.cart_id, ci.shop_id, ci.variant_id, ci.product_name, ci.unit_price, ci.quantity,
                               COALESCE(NULLIF(CONCAT_WS(' - ',
                                       NULLIF(v.size, ''),
                                       NULLIF(v.color, '')
                               ), ''), NULL) AS variant_info,
                               v.sku,
                               i.available
                          FROM cart_items ci
                          LEFT JOIN product_variants v ON v.id = ci.variant_id
                          LEFT JOIN inventory i       ON i.variant_id = ci.variant_id
                         WHERE ci.cart_id = $1
                         ORDER BY ci.id
                `
                rows, err := tx.Query(ctx, q, cartID)
                if err != nil {
                        return fmt.Errorf("cart repo: list items: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var (
                                it          models.CartItem
                                variantInfo *string
                                sku         *string
                                available   *int
                                variantID   *uuid.UUID
                        )
                        if err := rows.Scan(
                                &it.ID, &it.CartID, &it.ShopID, &variantID, &it.ProductName, &it.UnitPrice, &it.Quantity,
                                &variantInfo, &sku, &available,
                        ); err != nil {
                                return err
                        }
                        it.VariantID = variantID
                        it.VariantInfo = variantInfo
                        it.SKU = sku
                        it.Available = available
                        out = append(out, it)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, err
        }
        if out == nil {
                out = []models.CartItem{}
        }
        return out, nil
}

// scanCart maps a carts row into a models.Cart.
func scanCart(s scanner, c *models.Cart) error {
        var (
                conversationID *uuid.UUID
                customerID     *uuid.UUID
                expiresAt      *time.Time
                status         string
        )
        err := s.Scan(
                &c.ID, &c.ShopID, &conversationID, &customerID, &status, &expiresAt, &c.CreatedAt, &c.UpdatedAt,
        )
        if err != nil {
                return err
        }
        c.ConversationID = conversationID
        c.CustomerID = customerID
        c.ExpiresAt = expiresAt
        c.Status = models.CartStatus(status)
        return nil
}

// scanCartItem maps a cart_items row into a models.CartItem.
func scanCartItem(s scanner, it *models.CartItem) error {
        var variantID *uuid.UUID
        err := s.Scan(
                &it.ID, &it.CartID, &it.ShopID, &variantID, &it.ProductName, &it.UnitPrice, &it.Quantity,
        )
        if err != nil {
                return err
        }
        it.VariantID = variantID
        return nil
}
