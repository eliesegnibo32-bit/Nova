// Catalog service — orchestrates product + variant + image lifecycle.
//
// The service is the single entry point for catalog business logic. HTTP
// handlers in internal/api/handlers/catalog.go call into this service and
// translate the results into JSON responses. The service never touches HTTP
// directly — it returns plain Go values and sentinel errors.
//
// Responsibilities:
//   - Permission checks (member of the shop OR platform admin for every
//     call; owner-or-admin for writes — for MVP, all members can write;
//     fine-grained permissions will come with the team-management module).
//   - Validating promo_price < price (DB also enforces via CHECK).
//   - Validating SKU uniqueness per shop (DB also enforces via UNIQUE).
//   - Audit logging every write.
//   - Returning the effective price (promo if currently active) when
//     serving a variant.
package services

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// Catalog service sentinel errors.
var (
        ErrProductNotFound    = errors.New("product not found")
        ErrVariantNotFound    = errors.New("variant not found")
        ErrImageNotFound      = errors.New("image not found")
        ErrInvalidPromoPrice  = errors.New("promo price must be less than normal price")
        ErrInvalidPromoDates  = errors.New("promo_start and promo_end must both be set (or both null)")
        ErrCannotDeleteVariant = errors.New("cannot delete variant with reserved stock")
        ErrSKUTaken           = errors.New("SKU already taken in this shop")
)

// CatalogService is the catalog business-logic layer.
type CatalogService struct {
        productRepo   *repository.ProductRepository
        inventoryRepo *repository.InventoryRepository
        auditRepo     *repository.AuditRepository
        pool          *pgxpool.Pool
}

// NewCatalogService constructs a CatalogService.
func NewCatalogService(
        productRepo *repository.ProductRepository,
        inventoryRepo *repository.InventoryRepository,
        auditRepo *repository.AuditRepository,
        pool *pgxpool.Pool,
) *CatalogService {
        return &CatalogService{
                productRepo:   productRepo,
                inventoryRepo: inventoryRepo,
                auditRepo:     auditRepo,
                pool:          pool,
        }
}

// CreateProduct creates a product + (default variant if none supplied) +
// inventory row(s).
func (s *CatalogService) CreateProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, req models.CreateProductRequest, ip, userAgent string) (*repository.ProductWithRelations, error) {
        // Permission: must be a member of the shop (the handler ensures
        // shopID == session.shopID; the membership check is implicitly
        // enforced by RLS — if the user isn't a member, the INSERT will
        // fail with RLS violation).
        // For MVP we don't do explicit membership checks here.

        // Validate variants: promo_price < price, promo dates both set.
        for i := range req.Variants {
                if err := validateVariantPricing(req.Variants[i].PromoPrice, req.Variants[i].PromoStart, req.Variants[i].PromoEnd, req.Variants[i].Price); err != nil {
                        return nil, err
                }
        }

        // Build the repo input.
        in := repository.CreateProductInput{
                Name:         req.Name,
                Description:  req.Description,
                Category:     req.Category,
                Brand:        req.Brand,
                Status:       req.Status,
                ImageURL:     req.ImageURL,
                DefaultSKU:   req.SKU,
                DefaultPrice: req.Price,
        }
        if req.Status == "" {
                in.Status = "draft"
        }
        if len(req.Variants) == 0 {
                // Default variant — explicit Active=true so it's sellable.
                in.Variants = []repository.CreateVariantInput{{
                        SKU:   req.SKU,
                        Price: req.Price,
                        Active: true,
                }}
        } else {
                for i := range req.Variants {
                        v := req.Variants[i]
                        active := true
                        if v.Active != nil {
                                active = *v.Active
                        }
                        in.Variants = append(in.Variants, repository.CreateVariantInput{
                                SKU:        v.SKU,
                                Size:       v.Size,
                                Color:      v.Color,
                                Price:      v.Price,
                                PromoPrice: v.PromoPrice,
                                PromoStart: v.PromoStart,
                                PromoEnd:   v.PromoEnd,
                                Active:     active,
                        })
                }
        }

        result, err := s.productRepo.Create(ctx, shopID, userID, userRole, in)
        if err != nil {
                if errors.Is(err, repository.ErrSKUTaken) {
                        return nil, ErrSKUTaken
                }
                return nil, fmt.Errorf("create product: %w", err)
        }

        // Audit log.
        shopIDForLog := shopID
        prodIDForLog := result.Product.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "product.create",
                ObjectType: "product",
                ObjectID:   &prodIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      productSnapshotFrom(&result.Product),
        })

        return result, nil
}

// GetProduct returns a product with its variants and images.
func (s *CatalogService) GetProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID) (*repository.ProductWithRelations, error) {
        result, err := s.productRepo.GetByID(ctx, shopID, userID, userRole, productID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrProductNotFound
                }
                return nil, fmt.Errorf("get product: %w", err)
        }
        return result, nil
}

// ListProducts returns a paginated list of products for the shop.
func (s *CatalogService) ListProducts(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, params models.ListProductsParams) ([]ProductListItem, int64, error) {
        params.Normalize()
        items, total, err := s.productRepo.List(ctx, shopID, userID, userRole, repository.ListProductsRepoParams{
                Page:     params.Page,
                Limit:    params.Limit,
                Search:   params.Search,
                Category: params.Category,
                Status:   params.Status,
        })
        if err != nil {
                return nil, 0, fmt.Errorf("list products: %w", err)
        }
        out := make([]ProductListItem, 0, len(items))
        for _, it := range items {
                out = append(out, ProductListItem{
                        Product:      it.Product,
                        VariantCount: it.VariantCount,
                        FirstImage:   it.FirstImage,
                })
        }
        return out, total, nil
}

// ProductListItem is the service-level list-view shape, mirroring
// repository.ProductListItem but exposed in the services package so
// handlers don't need to import repository.
type ProductListItem struct {
        Product      models.Product
        VariantCount int
        FirstImage   *string
}

// UpdateProduct applies a partial update to a product.
func (s *CatalogService) UpdateProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, req models.UpdateProductRequest, ip, userAgent string) (*models.Product, error) {
        // Fetch before-state for audit.
        before, _ := s.productRepo.GetByID(ctx, shopID, userID, userRole, productID)

        p, err := s.productRepo.Update(ctx, shopID, userID, userRole, productID, repository.UpdateProductInput{
                Name:        req.Name,
                Description: req.Description,
                Category:    req.Category,
                Brand:       req.Brand,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrProductNotFound
                }
                return nil, fmt.Errorf("update product: %w", err)
        }

        shopIDForLog := shopID
        prodIDForLog := productID
        var beforeSnap json.Marshaler
        if before != nil {
                beforeSnap = productSnapshotFrom(&before.Product)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "product.update",
                ObjectType: "product",
                ObjectID:   &prodIDForLog,
                Before:     beforeSnap,
                After:      productSnapshotFrom(p),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return p, nil
}

// DeleteProduct soft-deletes a product.
func (s *CatalogService) DeleteProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, ip, userAgent string) error {
        before, _ := s.productRepo.GetByID(ctx, shopID, userID, userRole, productID)
        if err := s.productRepo.Delete(ctx, shopID, userID, userRole, productID); err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return ErrProductNotFound
                }
                return fmt.Errorf("delete product: %w", err)
        }
        shopIDForLog := shopID
        prodIDForLog := productID
        var beforeSnap json.Marshaler
        if before != nil {
                beforeSnap = productSnapshotFrom(&before.Product)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "product.delete",
                ObjectType: "product",
                ObjectID:   &prodIDForLog,
                Before:     beforeSnap,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// PublishProduct sets status='published'.
func (s *CatalogService) PublishProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, ip, userAgent string) (*models.Product, error) {
        return s.setProductStatus(ctx, userID, userRole, shopID, productID, "published", "product.publish", ip, userAgent)
}

// ArchiveProduct sets status='archived'.
func (s *CatalogService) ArchiveProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, ip, userAgent string) (*models.Product, error) {
        return s.setProductStatus(ctx, userID, userRole, shopID, productID, "archived", "product.archive", ip, userAgent)
}

func (s *CatalogService) setProductStatus(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, status, action, ip, userAgent string) (*models.Product, error) {
        before, _ := s.productRepo.GetByID(ctx, shopID, userID, userRole, productID)
        p, err := s.productRepo.SetStatus(ctx, shopID, userID, userRole, productID, status)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrProductNotFound
                }
                return nil, fmt.Errorf("set product status: %w", err)
        }
        shopIDForLog := shopID
        prodIDForLog := productID
        var beforeSnap json.Marshaler
        if before != nil {
                beforeSnap = productSnapshotFrom(&before.Product)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     action,
                ObjectType: "product",
                ObjectID:   &prodIDForLog,
                Before:     beforeSnap,
                After:      productSnapshotFrom(p),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return p, nil
}

// CreateVariant creates a new variant on an existing product + its inventory row.
func (s *CatalogService) CreateVariant(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, req models.CreateVariantRequest, ip, userAgent string) (*models.ProductVariant, error) {
        if err := validateVariantPricing(req.PromoPrice, req.PromoStart, req.PromoEnd, req.Price); err != nil {
                return nil, err
        }
        active := true
        if req.Active != nil {
                active = *req.Active
        }
        v, err := s.productRepo.CreateVariant(ctx, shopID, userID, userRole, productID, repository.CreateVariantInput{
                SKU:        req.SKU,
                Size:       req.Size,
                Color:      req.Color,
                Price:      req.Price,
                PromoPrice: req.PromoPrice,
                PromoStart: req.PromoStart,
                PromoEnd:   req.PromoEnd,
                Active:     active,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrProductNotFound
                }
                if errors.Is(err, repository.ErrSKUTaken) {
                        return nil, ErrSKUTaken
                }
                return nil, fmt.Errorf("create variant: %w", err)
        }
        shopIDForLog := shopID
        varIDForLog := v.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "variant.create",
                ObjectType: "product_variant",
                ObjectID:   &varIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      variantSnapshotFrom(v),
        })
        return v, nil
}

// UpdateVariant updates a variant.
func (s *CatalogService) UpdateVariant(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, req models.UpdateVariantRequest, ip, userAgent string) (*models.ProductVariant, error) {
        // Fetch existing variant for promo validation when patching.
        before, err := s.productRepo.GetVariantByID(ctx, shopID, userID, userRole, variantID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrVariantNotFound
                }
                return nil, fmt.Errorf("get variant for update: %w", err)
        }
        // Build the effective values for validation.
        price := before.Price
        if req.Price != nil {
                price = *req.Price
        }
        var promoPrice *int64
        var promoStart, promoEnd *time.Time
        if req.PromoClear {
                promoPrice = nil
        } else {
                promoPrice = before.PromoPrice
                if req.PromoPrice != nil {
                        promoPrice = req.PromoPrice
                }
                promoStart = before.PromoStart
                if req.PromoStart != nil {
                        promoStart = req.PromoStart
                }
                promoEnd = before.PromoEnd
                if req.PromoEnd != nil {
                        promoEnd = req.PromoEnd
                }
        }
        if err := validateVariantPricing(promoPrice, promoStart, promoEnd, price); err != nil {
                return nil, err
        }

        v, err := s.productRepo.UpdateVariant(ctx, shopID, userID, userRole, variantID, repository.UpdateVariantInput{
                SKU:        req.SKU,
                Size:       req.Size,
                Color:      req.Color,
                Price:      req.Price,
                PromoPrice: req.PromoPrice,
                PromoStart: req.PromoStart,
                PromoEnd:   req.PromoEnd,
                Active:     req.Active,
                PromoClear: req.PromoClear,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrVariantNotFound
                }
                if errors.Is(err, repository.ErrSKUTaken) {
                        return nil, ErrSKUTaken
                }
                return nil, fmt.Errorf("update variant: %w", err)
        }
        shopIDForLog := shopID
        varIDForLog := variantID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "variant.update",
                ObjectType: "product_variant",
                ObjectID:   &varIDForLog,
                Before:     variantSnapshotFrom(before),
                After:      variantSnapshotFrom(v),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return v, nil
}

// DeleteVariant deletes a variant. Cannot delete if reserved > 0.
func (s *CatalogService) DeleteVariant(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, ip, userAgent string) error {
        inv, err := s.inventoryRepo.GetByVariant(ctx, shopID, userID, userRole, variantID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return ErrVariantNotFound
                }
                return fmt.Errorf("get inventory before variant delete: %w", err)
        }
        if inv.Reserved > 0 {
                return ErrCannotDeleteVariant
        }
        before, _ := s.productRepo.GetVariantByID(ctx, shopID, userID, userRole, variantID)
        if err := s.productRepo.DeleteVariant(ctx, shopID, userID, userRole, variantID); err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return ErrVariantNotFound
                }
                return fmt.Errorf("delete variant: %w", err)
        }
        shopIDForLog := shopID
        varIDForLog := variantID
        var beforeSnap json.Marshaler
        if before != nil {
                beforeSnap = variantSnapshotFrom(before)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "variant.delete",
                ObjectType: "product_variant",
                ObjectID:   &varIDForLog,
                Before:     beforeSnap,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// AddImage adds a product image.
func (s *CatalogService) AddImage(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID, url string, ord int, ip, userAgent string) (*models.ProductImage, error) {
        img, err := s.productRepo.AddImage(ctx, shopID, userID, userRole, productID, url, ord)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrProductNotFound
                }
                return nil, fmt.Errorf("add image: %w", err)
        }
        shopIDForLog := shopID
        imgIDForLog := img.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "product_image.add",
                ObjectType: "product_image",
                ObjectID:   &imgIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return img, nil
}

// RemoveImage removes a product image.
func (s *CatalogService) RemoveImage(ctx context.Context, userID uuid.UUID, userRole string, shopID, imageID uuid.UUID, ip, userAgent string) error {
        if err := s.productRepo.RemoveImage(ctx, shopID, userID, userRole, imageID); err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return ErrImageNotFound
                }
                return fmt.Errorf("remove image: %w", err)
        }
        shopIDForLog := shopID
        imgIDForLog := imageID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "product_image.remove",
                ObjectType: "product_image",
                ObjectID:   &imgIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// GetCurrentPrice is the exported helper that handlers and other services
// can call to get the effective price for a variant.
func (s *CatalogService) GetCurrentPrice(v *models.ProductVariant) int64 {
        return models.CurrentVariantPrice(v)
}

// --- helpers ----------------------------------------------------------------

// validateVariantPricing validates the promo_price < price constraint and
// the date coherence (both start+end must be set together, start < end).
func validateVariantPricing(promoPrice *int64, promoStart, promoEnd *time.Time, price int64) error {
        hasPromo := promoPrice != nil
        hasDates := promoStart != nil && promoEnd != nil
        noDates := promoStart == nil && promoEnd == nil
        if !hasPromo && !hasDates {
                return nil // no promo at all
        }
        if !hasPromo {
                return ErrInvalidPromoDates
        }
        // hasPromo == true
        if !hasDates {
                return ErrInvalidPromoDates
        }
        if *promoPrice >= price {
                return ErrInvalidPromoPrice
        }
        if !promoStart.Before(*promoEnd) {
                return ErrInvalidPromoDates
        }
        _ = noDates
        return nil
}

// productSnapshot is a json.Marshaler for audit log snapshots.
type productSnapshot struct {
        ID     string `json:"id"`
        Name   string `json:"name"`
        Status string `json:"status"`
}

func (p productSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                ID     string `json:"id"`
                Name   string `json:"name"`
                Status string `json:"status"`
        }{ID: p.ID, Name: p.Name, Status: p.Status})
}

func productSnapshotFrom(p *models.Product) productSnapshot {
        if p == nil {
                return productSnapshot{}
        }
        return productSnapshot{
                ID:     p.ID.String(),
                Name:   p.Name,
                Status: string(p.Status),
        }
}

// variantSnapshot for audit logs.
type variantSnapshot struct {
        ID    string `json:"id"`
        SKU   string `json:"sku"`
        Price int64  `json:"price"`
}

func (v variantSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                ID    string `json:"id"`
                SKU   string `json:"sku"`
                Price int64  `json:"price"`
        }{ID: v.ID, SKU: v.SKU, Price: v.Price})
}

func variantSnapshotFrom(v *models.ProductVariant) variantSnapshot {
        if v == nil {
                return variantSnapshot{}
        }
        return variantSnapshot{
                ID:    v.ID.String(),
                SKU:   v.SKU,
                Price: v.Price,
        }
}
