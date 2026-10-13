// Product repository — handles the `products`, `product_variants`, and
// `product_images` tables. All queries go through db.WithTenantTx so the RLS
// policies from migration 011 are enforced:
//
//      shop_id = current_shop_id() OR is_platform_admin()
//
// The shopID comes from the authenticated session (NOT from the URL — the
// URL shopId is validated against the session shopId by the handler).
//
// Key invariants:
//   - Every product has AT LEAST one variant. If the caller doesn't supply
//     variants, Create inserts a default variant (no size/color, price = the
//     product's price, sku = the caller-supplied SKU).
//   - Every variant INSERT is followed by an inventory INSERT (on_hand=0,
//     reserved=0, alert_threshold=5).
//   - SKU is unique per shop (UNIQUE (shop_id, sku) — see migration 003).
//   - promo_price < price is enforced by a DB CHECK constraint AND validated
//     in the service layer before insert.
//   - Soft delete: deleted_at on products (cascades to variants/images via
//     FK ON DELETE CASCADE — but we use soft delete, so we also set
//     deleted_at on variants explicitly? No — variants have no deleted_at
//     column, so soft-deleting a product effectively hides its variants via
//     the join with products.deleted_at. For full hard delete later we'd
//     cascade. For MVP, soft delete on products is enough.)
package repository

import (
        "context"
        "errors"
        "fmt"
        "strings"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// CreateProductInput is the data needed to create a product (+ optional
// initial variant). The service validates promo_price < price before
// calling.
type CreateProductInput struct {
        Name        string
        Description string
        Category    string
        Brand       string
        Status      string // "draft" (default) or "published"
        ImageURL    *string
        // Default-variant fields — used only when no explicit Variants are
        // supplied. Price can be 0 (will be set on the default variant).
        DefaultSKU   string
        DefaultPrice int64
        Variants     []CreateVariantInput
}

// CreateVariantInput is the data needed to create a variant.
type CreateVariantInput struct {
        SKU        string
        Size       string
        Color      string
        Price      int64
        PromoPrice *int64
        PromoStart *time.Time
        PromoEnd   *time.Time
        Active     bool
}

// UpdateProductInput mirrors models.UpdateProductRequest but with raw
// pointers (no JSON tags) so the repository doesn't depend on the HTTP DTO.
type UpdateProductInput struct {
        Name        *string
        Description *string
        Category    *string
        Brand       *string
}

// UpdateVariantInput mirrors models.UpdateVariantRequest but with raw
// pointers.
type UpdateVariantInput struct {
        SKU        *string
        Size       *string
        Color      *string
        Price      *int64
        PromoPrice *int64
        PromoStart *time.Time
        PromoEnd   *time.Time
        Active     *bool
        PromoClear bool // if true, set promo_price/promo_start/promo_end to NULL
}

// ProductRepository wraps products/variants/images.
type ProductRepository struct {
        pool *pgxpool.Pool
}

// NewProductRepository returns a ProductRepository bound to the given pool.
func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
        return &ProductRepository{pool: pool}
}

// ProductWithRelations is the return shape of GetByID — the product row
// plus its variants and images, ready to be converted to a response.
type ProductWithRelations struct {
        Product  models.Product
        Variants []models.ProductVariant
        Images   []models.ProductImage
}

// Create inserts a product (and a default variant if none supplied) plus
// an inventory row per variant. The whole operation runs in one transaction
// so a failure in any step rolls back the product insert.
//
// shopID/userID/role come from the session; the service is responsible for
// verifying membership before calling.
func (r *ProductRepository) Create(ctx context.Context, shopID, userID uuid.UUID, role string, in CreateProductInput) (*ProductWithRelations, error) {
        if in.Status == "" {
                in.Status = "draft"
        }
        out := &ProductWithRelations{}
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // 1. Insert product.
                const pq = `
                        INSERT INTO products (shop_id, name, description, category, brand, status, image_url)
                        VALUES ($1, $2, $3, $4, $5, $6, $7)
                        RETURNING id, shop_id, name, description, category, brand, status, image_url, created_at, updated_at, deleted_at
                `
                if err := scanProduct(tx.QueryRow(ctx, pq,
                        shopID,
                        in.Name,
                        nullableString(in.Description),
                        nullableString(in.Category),
                        nullableString(in.Brand),
                        in.Status,
                        in.ImageURL,
                ), &out.Product); err != nil {
                        return fmt.Errorf("insert product: %w", err)
                }

                // 2. Insert variants (or a default variant if none supplied).
                if len(in.Variants) == 0 {
                        in.Variants = []CreateVariantInput{{
                                SKU:   in.DefaultSKU,
                                Price: in.DefaultPrice,
                        }}
                }
                for i := range in.Variants {
                        v, err := insertVariant(ctx, tx, out.Product.ID, shopID, in.Variants[i])
                        if err != nil {
                                if isUniqueViolation(err) || strings.Contains(err.Error(), "product_variants_shop_id_sku_key") {
                                        return ErrSKUTaken
                                }
                                return err
                        }
                        out.Variants = append(out.Variants, *v)

                        // Insert the inventory row for this variant.
                        if err := insertInventoryRow(ctx, tx, v.ID, shopID); err != nil {
                                return fmt.Errorf("insert inventory for variant: %w", err)
                        }
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return out, nil
}

// GetByID returns the product + its variants + its images, all in one tx.
// Variants are returned ordered by created_at; images by ord.
func (r *ProductRepository) GetByID(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID) (*ProductWithRelations, error) {
        out := &ProductWithRelations{}
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const pq = `
                        SELECT id, shop_id, name, description, category, brand, status, image_url, created_at, updated_at, deleted_at
                          FROM products
                         WHERE id = $1 AND deleted_at IS NULL
                `
                if err := scanProduct(tx.QueryRow(ctx, pq, productID), &out.Product); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("get product: %w", err)
                }

                const vq = `
                        SELECT id, product_id, shop_id, sku, size, color, price, promo_price, promo_start, promo_end, active, created_at, updated_at
                          FROM product_variants
                         WHERE product_id = $1
                         ORDER BY created_at ASC
                `
                rows, err := tx.Query(ctx, vq, productID)
                if err != nil {
                        return fmt.Errorf("list variants: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var v models.ProductVariant
                        if err := scanVariant(rows, &v); err != nil {
                                return err
                        }
                        out.Variants = append(out.Variants, v)
                }
                if err := rows.Err(); err != nil {
                        return err
                }

                const iq = `
                        SELECT id, product_id, shop_id, url, ord, created_at
                          FROM product_images
                         WHERE product_id = $1
                         ORDER BY ord ASC, created_at ASC
                `
                irows, err := tx.Query(ctx, iq, productID)
                if err != nil {
                        return fmt.Errorf("list images: %w", err)
                }
                defer irows.Close()
                for irows.Next() {
                        var img models.ProductImage
                        if err := irows.Scan(&img.ID, &img.ProductID, &img.ShopID, &img.URL, &img.Ord, &img.CreatedAt); err != nil {
                                return err
                        }
                        out.Images = append(out.Images, img)
                }
                return irows.Err()
        })
        if err != nil {
                return nil, err
        }
        return out, nil
}

// ProductListItem is the list-view shape — only the columns needed by the
// catalog UI, plus a variant count and the first image URL.
type ProductListItem struct {
        Product      models.Product
        VariantCount int
        FirstImage   *string
}

// List returns a paginated list of products for the shop. Filters:
// category, status, search (substring on name OR brand). Returns the
// product rows + total count for pagination.
func (r *ProductRepository) List(ctx context.Context, shopID, userID uuid.UUID, role string, params ListProductsRepoParams) ([]ProductListItem, int64, error) {
        var (
                out   []ProductListItem
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                where := "WHERE deleted_at IS NULL"
                args := []any{}
                idx := 1
                if params.Category != "" {
                        where += fmt.Sprintf(" AND category = $%d", idx)
                        args = append(args, params.Category)
                        idx++
                }
                if params.Status != "" {
                        where += fmt.Sprintf(" AND status = $%d", idx)
                        args = append(args, params.Status)
                        idx++
                }
                if params.Search != "" {
                        where += fmt.Sprintf(" AND (name ILIKE $%d OR brand ILIKE $%d)", idx, idx)
                        args = append(args, "%"+params.Search+"%")
                        idx++
                }

                // Count total.
                if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM products "+where, args...).Scan(&total); err != nil {
                        return fmt.Errorf("count products: %w", err)
                }

                listQ := `
                        SELECT p.id, p.shop_id, p.name, p.description, p.category, p.brand, p.status, p.image_url,
                               p.created_at, p.updated_at, p.deleted_at,
                               (SELECT COUNT(*) FROM product_variants v WHERE v.product_id = p.id) AS variant_count,
                               (SELECT url FROM product_images im WHERE im.product_id = p.id ORDER BY im.ord ASC LIMIT 1) AS first_image
                          FROM products p
                ` + where + `
                        ORDER BY p.created_at DESC
                        LIMIT $` + fmt.Sprintf("%d", idx) + ` OFFSET $` + fmt.Sprintf("%d", idx+1)
                args = append(args, params.Limit, params.Offset())

                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var (
                                p            models.Product
                                variantCount int
                                firstImage   *string
                        )
                        if err := scanProductWithExtras(rows, &p, &variantCount, &firstImage); err != nil {
                                return err
                        }
                        out = append(out, ProductListItem{Product: p, VariantCount: variantCount, FirstImage: firstImage})
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("product repo: list: %w", err)
        }
        return out, total, nil
}

// ListProductsRepoParams is the input to List.
type ListProductsRepoParams struct {
        Page     int
        Limit    int
        Search   string
        Category string
        Status   string
}

// Offset returns the SQL OFFSET value for the current page/limit.
func (p ListProductsRepoParams) Offset() int {
        if p.Page < 1 {
                p.Page = 1
        }
        return (p.Page - 1) * p.Limit
}

// Update applies a partial update to a product. Only fields present in req
// are updated.
func (r *ProductRepository) Update(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID, in UpdateProductInput) (*models.Product, error) {
        var p models.Product
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
                addString("name", in.Name)
                addString("description", in.Description)
                addString("category", in.Category)
                addString("brand", in.Brand)

                if len(sets) == 0 {
                        // No fields to update — just fetch and return.
                        const q = `
                                SELECT id, shop_id, name, description, category, brand, status, image_url, created_at, updated_at, deleted_at
                                  FROM products WHERE id = $1 AND deleted_at IS NULL
                        `
                        return scanProduct(tx.QueryRow(ctx, q, productID), &p)
                }
                sets = append(sets, "updated_at = now()")
                args = append(args, productID)
                whereIdx := idx

                q := fmt.Sprintf(`
                        UPDATE products
                           SET %s
                         WHERE id = $%d AND deleted_at IS NULL
                        RETURNING id, shop_id, name, description, category, brand, status, image_url, created_at, updated_at, deleted_at
                `, strings.Join(sets, ", "), whereIdx)
                if err := scanProduct(tx.QueryRow(ctx, q, args...), &p); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("update product: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &p, nil
}

// Delete soft-deletes a product (sets deleted_at = now()). Variants and
// images are preserved (they're joined via product_id; the catalog list
// view filters out products with deleted_at != NULL).
func (r *ProductRepository) Delete(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE products SET deleted_at = now(), updated_at = now()
                         WHERE id = $1 AND deleted_at IS NULL
                `, productID)
                if err != nil {
                        return fmt.Errorf("soft-delete product: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// SetStatus changes the product's status (publish / archive / revert to
// draft). The service enforces the only-allowed transitions.
func (r *ProductRepository) SetStatus(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID, status string) (*models.Product, error) {
        var p models.Product
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                q := `
                        UPDATE products
                           SET status = $2, updated_at = now()
                         WHERE id = $1 AND deleted_at IS NULL
                        RETURNING id, shop_id, name, description, category, brand, status, image_url, created_at, updated_at, deleted_at
                `
                if err := scanProduct(tx.QueryRow(ctx, q, productID, status), &p); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("update product status: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &p, nil
}

// CountPublished returns the number of published products for the shop.
// Used for shop activation validation.
func (r *ProductRepository) CountPublished(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM products
                         WHERE shop_id = $1 AND status = 'published' AND deleted_at IS NULL
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("product repo: count published: %w", err)
        }
        return n, nil
}

// CreateVariant inserts a new variant on an existing product + creates its
// inventory row.
func (r *ProductRepository) CreateVariant(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID, in CreateVariantInput) (*models.ProductVariant, error) {
        var v models.ProductVariant
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // Verify the product exists and is in this shop (RLS will also enforce).
                var exists bool
                if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1 AND deleted_at IS NULL)`, productID).Scan(&exists); err != nil {
                        return fmt.Errorf("check product exists: %w", err)
                }
                if !exists {
                        return ErrNotFound
                }
                vv, err := insertVariant(ctx, tx, productID, shopID, in)
                if err != nil {
                        if isUniqueViolation(err) || strings.Contains(err.Error(), "product_variants_shop_id_sku_key") {
                                return ErrSKUTaken
                        }
                        return err
                }
                v = *vv
                return insertInventoryRow(ctx, tx, v.ID, shopID)
        })
        if err != nil {
                return nil, err
        }
        return &v, nil
}

// UpdateVariant applies a partial update to a variant.
func (r *ProductRepository) UpdateVariant(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID, in UpdateVariantInput) (*models.ProductVariant, error) {
        var v models.ProductVariant
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                sets := []string{}
                args := []any{}
                idx := 1
                addString := func(col string, val *string) {
                        if val != nil {
                                sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
                                args = append(args, *val)
                                idx++
                        }
                }
                addInt64 := func(col string, val *int64) {
                        if val != nil {
                                sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
                                args = append(args, *val)
                                idx++
                        }
                }
                addTime := func(col string, val *time.Time) {
                        if val != nil {
                                sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
                                args = append(args, *val)
                                idx++
                        }
                }
                addString("sku", in.SKU)
                addString("size", in.Size)
                addString("color", in.Color)
                addInt64("price", in.Price)
                if in.PromoClear {
                        sets = append(sets, "promo_price = NULL", "promo_start = NULL", "promo_end = NULL")
                } else {
                        addInt64("promo_price", in.PromoPrice)
                        addTime("promo_start", in.PromoStart)
                        addTime("promo_end", in.PromoEnd)
                }
                if in.Active != nil {
                        sets = append(sets, fmt.Sprintf("active = $%d", idx))
                        args = append(args, *in.Active)
                        idx++
                }
                if len(sets) == 0 {
                        const q = `
                                SELECT id, product_id, shop_id, sku, size, color, price, promo_price, promo_start, promo_end, active, created_at, updated_at
                                  FROM product_variants WHERE id = $1
                        `
                        return scanVariant(tx.QueryRow(ctx, q, variantID), &v)
                }
                sets = append(sets, "updated_at = now()")
                args = append(args, variantID)
                whereIdx := idx
                q := fmt.Sprintf(`
                        UPDATE product_variants
                           SET %s
                         WHERE id = $%d
                        RETURNING id, product_id, shop_id, sku, size, color, price, promo_price, promo_start, promo_end, active, created_at, updated_at
                `, strings.Join(sets, ", "), whereIdx)
                if err := scanVariant(tx.QueryRow(ctx, q, args...), &v); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        if isUniqueViolation(err) || strings.Contains(err.Error(), "product_variants_shop_id_sku_key") {
                                return ErrSKUTaken
                        }
                        return fmt.Errorf("update variant: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &v, nil
}

// DeleteVariant deletes a variant. The caller (service) MUST verify that
// reserved == 0 before calling — we don't enforce it here so the error
// returned from FK constraints would be cryptic. The inventory row is
// cascaded by FK ON DELETE CASCADE on inventory.variant_id.
func (r *ProductRepository) DeleteVariant(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `DELETE FROM product_variants WHERE id = $1`, variantID)
                if err != nil {
                        return fmt.Errorf("delete variant: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// GetVariantByID returns a single variant by ID.
func (r *ProductRepository) GetVariantByID(ctx context.Context, shopID, userID uuid.UUID, role string, variantID uuid.UUID) (*models.ProductVariant, error) {
        var v models.ProductVariant
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, product_id, shop_id, sku, size, color, price, promo_price, promo_start, promo_end, active, created_at, updated_at
                          FROM product_variants WHERE id = $1
                `
                if err := scanVariant(tx.QueryRow(ctx, q, variantID), &v); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("get variant: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &v, nil
}

// AddImage inserts a product image (URL + ord). If ord <= 0 we default to
// "max(ord)+1" so the new image goes to the end.
func (r *ProductRepository) AddImage(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID, url string, ord int) (*models.ProductImage, error) {
        var img models.ProductImage
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // Verify the product exists in this shop.
                var exists bool
                if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1 AND deleted_at IS NULL)`, productID).Scan(&exists); err != nil {
                        return fmt.Errorf("check product exists: %w", err)
                }
                if !exists {
                        return ErrNotFound
                }
                q := `
                        INSERT INTO product_images (product_id, shop_id, url, ord)
                        VALUES ($1, $2, $3, COALESCE(NULLIF($4, 0), (SELECT COALESCE(MAX(ord), 0) + 1 FROM product_images WHERE product_id = $1)))
                        RETURNING id, product_id, shop_id, url, ord, created_at
                `
                return tx.QueryRow(ctx, q, productID, shopID, url, ord).Scan(
                        &img.ID, &img.ProductID, &img.ShopID, &img.URL, &img.Ord, &img.CreatedAt,
                )
        })
        if err != nil {
                return nil, err
        }
        return &img, nil
}

// RemoveImage deletes a product image.
func (r *ProductRepository) RemoveImage(ctx context.Context, shopID, userID uuid.UUID, role string, imageID uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `DELETE FROM product_images WHERE id = $1`, imageID)
                if err != nil {
                        return fmt.Errorf("delete image: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// ReorderImages reorders images by setting ord = index in the slice for each
// image ID. The caller passes the full ordered list of image IDs.
func (r *ProductRepository) ReorderImages(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID, imageIDs []uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                for i, id := range imageIDs {
                        if _, err := tx.Exec(ctx, `
                                UPDATE product_images SET ord = $3
                                 WHERE id = $1 AND product_id = $2
                        `, id, productID, i); err != nil {
                                return fmt.Errorf("reorder image %s: %w", id, err)
                        }
                }
                return nil
        })
}

// --- Sentinels --------------------------------------------------------------

// ErrSKUTaken is returned when a SKU is already in use for the shop.
var ErrSKUTaken = errors.New("SKU already taken in this shop")

// --- helpers ----------------------------------------------------------------

// insertVariant inserts one product_variants row and returns the scanned
// model. Used by Create + CreateVariant.
func insertVariant(ctx context.Context, tx pgx.Tx, productID, shopID uuid.UUID, in CreateVariantInput) (*models.ProductVariant, error) {
        var v models.ProductVariant
        // Promotional price + dates must all be set together (or all NULL).
        // If the caller passes a promo_price without dates, we treat that as
        // "no promo" (NULL all).
        var promoPrice any
        var promoStart, promoEnd any
        if in.PromoPrice != nil && in.PromoStart != nil && in.PromoEnd != nil {
                promoPrice = *in.PromoPrice
                promoStart = *in.PromoStart
                promoEnd = *in.PromoEnd
        }
        const q = `
                INSERT INTO product_variants
                    (product_id, shop_id, sku, size, color, price, promo_price, promo_start, promo_end, active)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
                RETURNING id, product_id, shop_id, sku, size, color, price, promo_price, promo_start, promo_end, active, created_at, updated_at
        `
        if err := scanVariant(tx.QueryRow(ctx, q,
                productID, shopID,
                in.SKU,
                nullableString(in.Size),
                nullableString(in.Color),
                in.Price,
                promoPrice,
                promoStart,
                promoEnd,
                in.Active,
        ), &v); err != nil {
                return nil, err
        }
        return &v, nil
}

// insertInventoryRow creates the inventory row for a freshly-inserted
// variant. on_hand=0, reserved=0, alert_threshold=5 (default).
func insertInventoryRow(ctx context.Context, tx pgx.Tx, variantID, shopID uuid.UUID) error {
        _, err := tx.Exec(ctx, `
                INSERT INTO inventory (variant_id, shop_id, on_hand, reserved, alert_threshold)
                VALUES ($1, $2, 0, 0, 5)
                ON CONFLICT (variant_id) DO NOTHING
        `, variantID, shopID)
        return err
}

// scanProduct maps a products row into a models.Product.
func scanProduct(s scanner, p *models.Product) error {
        var (
                description *string
                category    *string
                brand       *string
                status      string
                imageURL    *string
                deletedAt   *time.Time
        )
        err := s.Scan(
                &p.ID, &p.ShopID, &p.Name, &description, &category, &brand,
                &status, &imageURL, &p.CreatedAt, &p.UpdatedAt, &deletedAt,
        )
        if err != nil {
                return err
        }
        p.Description = description
        p.Category = category
        p.Brand = brand
        p.Status = models.ProductStatus(status)
        p.ImageURL = imageURL
        p.DeletedAt = deletedAt
        return nil
}

// scanProductWithExtras maps a products row + extra columns (variant_count,
// first_image) into a models.Product + the two extras.
func scanProductWithExtras(s scanner, p *models.Product, variantCount *int, firstImage **string) error {
        var (
                description *string
                category    *string
                brand       *string
                status      string
                imageURL    *string
                deletedAt   *time.Time
        )
        err := s.Scan(
                &p.ID, &p.ShopID, &p.Name, &description, &category, &brand,
                &status, &imageURL, &p.CreatedAt, &p.UpdatedAt, &deletedAt,
                variantCount, firstImage,
        )
        if err != nil {
                return err
        }
        p.Description = description
        p.Category = category
        p.Brand = brand
        p.Status = models.ProductStatus(status)
        p.ImageURL = imageURL
        p.DeletedAt = deletedAt
        return nil
}

// scanVariant maps a product_variants row into a models.ProductVariant.
func scanVariant(s scanner, v *models.ProductVariant) error {
        var (
                size       *string
                color      *string
                promoPrice *int64
                promoStart *time.Time
                promoEnd   *time.Time
        )
        err := s.Scan(
                &v.ID, &v.ProductID, &v.ShopID, &v.SKU, &size, &color,
                &v.Price, &promoPrice, &promoStart, &promoEnd, &v.Active,
                &v.CreatedAt, &v.UpdatedAt,
        )
        if err != nil {
                return err
        }
        v.Size = size
        v.Color = color
        v.PromoPrice = promoPrice
        v.PromoStart = promoStart
        v.PromoEnd = promoEnd
        return nil
}
