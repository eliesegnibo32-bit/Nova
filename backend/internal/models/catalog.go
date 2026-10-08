// Catalog DTOs for the HTTP layer — products, variants, images, inventory,
// stock movements, and delivery zones.
//
// Field semantics follow the cahier des charges:
//   - ch. 4.2 Catalogue: product, variant, images, status lifecycle
//     (draft|published|archived), SKU unique per shop, promo_price < price.
//   - ch. 4.3 Stock: atomic reservation (UPDATE inventory SET reserved =
//     reserved + q WHERE variant_id AND shop_id AND on_hand - reserved >= q),
//     immutable stock_movements journal, alert_threshold, low/out of stock
//     derived states.
//   - ch. 4.4 Zones: shop-defined zones + tariffs, aliases for AI matching,
//     free_from threshold, min_order_amount, no zone ambiguity tolerance.
//
// All prices and amounts are integer FCFA (no decimals).
package models

import (
        "encoding/json"
        "time"
)

// ============================================================================
// Products
// ============================================================================

// CreateProductRequest is the body of POST /api/shops/{shopId}/products.
// If no Variants are supplied, the service creates a default variant (no
// size/color, price=Price, sku=SKU).
type CreateProductRequest struct {
        Name        string                 `json:"name"        validate:"required,min=1,max=200"`
        Description string                 `json:"description" validate:"omitempty,max=5000"`
        Category    string                 `json:"category"    validate:"omitempty,max=80"`
        Brand       string                 `json:"brand"       validate:"omitempty,max=80"`
        Status      string                 `json:"status"      validate:"omitempty,oneof=draft published archived"`
        ImageURL    *string                `json:"image_url,omitempty" validate:"omitempty,max=2048"`
        // Optional initial variant data. If absent, a default variant is
        // created using Price/SKU at the product level. Both ways produce a
        // variant row + inventory row (on_hand=0, reserved=0, alert_threshold=5).
        SKU   string  `json:"sku,omitempty"   validate:"omitempty,max=100"`
        Price int64   `json:"price,omitempty" validate:"omitempty,min=0"`
        Variants []CreateVariantRequest `json:"variants,omitempty"`
}

// UpdateProductRequest is the body of PATCH /api/shops/{shopId}/products/{id}.
// Only the fields present are updated. Status changes go through
// /publish and /archive.
type UpdateProductRequest struct {
        Name        *string `json:"name,omitempty"        validate:"omitempty,min=1,max=200"`
        Description *string `json:"description,omitempty" validate:"omitempty,max=5000"`
        Category    *string `json:"category,omitempty"    validate:"omitempty,max=80"`
        Brand       *string `json:"brand,omitempty"       validate:"omitempty,max=80"`
}

// ListProductsParams holds the query parameters for GET /products.
type ListProductsParams struct {
        Page     int
        Limit    int
        Search   string
        Category string
        Status   string
}

// Normalize fills sane defaults for missing pagination fields.
func (p *ListProductsParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 200 {
                p.Limit = 20
        }
}

// Offset returns the SQL OFFSET for the page/limit.
func (p *ListProductsParams) Offset() int { return (p.Page - 1) * p.Limit }

// ProductResponse is the wire shape for a single product (with its variants
// and images pre-loaded).
type ProductResponse struct {
        ID          string           `json:"id"`
        ShopID      string           `json:"shop_id"`
        Name        string           `json:"name"`
        Description *string          `json:"description,omitempty"`
        Category    *string          `json:"category,omitempty"`
        Brand       *string          `json:"brand,omitempty"`
        Status      string           `json:"status"`
        Variants    []VariantResponse `json:"variants"`
        Images      []ProductImage   `json:"images"`
        CreatedAt   string           `json:"created_at"`
        UpdatedAt   string           `json:"updated_at"`
}

// ProductListItemResponse is the lighter shape returned in list view — only
// the first image and a count of variants, no full variant details.
type ProductListItemResponse struct {
        ID           string          `json:"id"`
        Name         string          `json:"name"`
        Category     *string         `json:"category,omitempty"`
        Brand        *string         `json:"brand,omitempty"`
        Status       string          `json:"status"`
        VariantCount int             `json:"variant_count"`
        FirstImage   *string         `json:"first_image,omitempty"`
        CreatedAt    string          `json:"created_at"`
        UpdatedAt    string          `json:"updated_at"`
}

// ============================================================================
// Variants
// ============================================================================

// CreateVariantRequest is the body for POST /products/{id}/variants.
type CreateVariantRequest struct {
        SKU        string     `json:"sku"          validate:"required,min=1,max=100"`
        Size       string     `json:"size"         validate:"omitempty,max=40"`
        Color      string     `json:"color"        validate:"omitempty,max=60"`
        Price      int64      `json:"price"        validate:"required,min=0"`
        PromoPrice *int64     `json:"promo_price,omitempty" validate:"omitempty,min=0"`
        PromoStart *time.Time `json:"promo_start,omitempty"`
        PromoEnd   *time.Time `json:"promo_end,omitempty"`
        Active     *bool      `json:"active,omitempty"`
}

// UpdateVariantRequest is the body for PATCH /products/{id}/variants/{variantId}.
type UpdateVariantRequest struct {
        SKU        *string    `json:"sku,omitempty"          validate:"omitempty,min=1,max=100"`
        Size       *string    `json:"size,omitempty"         validate:"omitempty,max=40"`
        Color      *string    `json:"color,omitempty"        validate:"omitempty,max=60"`
        Price      *int64     `json:"price,omitempty"        validate:"omitempty,min=0"`
        PromoPrice *int64     `json:"promo_price,omitempty"  validate:"omitempty,min=0"`
        PromoStart *time.Time `json:"promo_start,omitempty"`
        PromoEnd   *time.Time `json:"promo_end,omitempty"`
        Active     *bool      `json:"active,omitempty"`
        // PromoClear lets the client clear promo fields explicitly.
        PromoClear bool `json:"promo_clear,omitempty"`
}

// VariantResponse is the wire shape for a variant. EffectivePrice is the
// current price (promo_price if within the date range, else price).
type VariantResponse struct {
        ID             string     `json:"id"`
        ProductID      string     `json:"product_id"`
        SKU            string     `json:"sku"`
        Size           *string    `json:"size,omitempty"`
        Color          *string    `json:"color,omitempty"`
        Price          int64      `json:"price"`
        PromoPrice     *int64     `json:"promo_price,omitempty"`
        PromoStart     *string    `json:"promo_start,omitempty"`
        PromoEnd       *string    `json:"promo_end,omitempty"`
        EffectivePrice int64      `json:"effective_price"`
        Active         bool       `json:"active"`
        CreatedAt      string     `json:"created_at"`
        UpdatedAt      string     `json:"updated_at"`
}

// ============================================================================
// Inventory + stock movements
// ============================================================================

// AdjustStockRequest is the body for POST /inventory/{variantId}/adjust.
// Delta can be negative (casse, inventory error) — the repository adjusts
// on_hand by that delta. Reason is MANDATORY (per ch. 4.3).
type AdjustStockRequest struct {
        Delta  int    `json:"delta"  validate:"required,ne=0"`
        Reason string `json:"reason" validate:"required,min=3,max=500"`
}

// ReceiveStockRequest is the body for POST /inventory/{variantId}/receive.
// Quantity must be > 0 (validated at service layer; we don't put it in the
// validator tag so we can return a clearer error).
type ReceiveStockRequest struct {
        Quantity int    `json:"quantity" validate:"required"`
        Reason   string `json:"reason"   validate:"omitempty,max=500"`
}

// SetAlertThresholdRequest is the body for PATCH /inventory/{variantId}/threshold.
type SetAlertThresholdRequest struct {
        Threshold int `json:"threshold" validate:"min=0,max=100000"`
}

// ListInventoryParams holds the query parameters for GET /inventory.
type ListInventoryParams struct {
        Page    int
        Limit   int
        Filter  string // "low_stock" | "out_of_stock" | "all"
        Search  string
}

// Normalize fills sane defaults.
func (p *ListInventoryParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 500 {
                p.Limit = 50
        }
        if p.Filter == "" {
                p.Filter = "all"
        }
}

// Offset returns the SQL OFFSET.
func (p *ListInventoryParams) Offset() int { return (p.Page - 1) * p.Limit }

// ListMovementsParams holds the query parameters for GET /inventory/{variantId}/movements.
type ListMovementsParams struct {
        Page  int
        Limit int
        Type  string // filter by movement type
        From  *time.Time
        To    *time.Time
}

// Normalize fills sane defaults.
func (p *ListMovementsParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 500 {
                p.Limit = 50
        }
}

// Offset returns the SQL OFFSET.
func (p *ListMovementsParams) Offset() int { return (p.Page - 1) * p.Limit }

// InventoryResponse is the wire shape for a variant's inventory.
type InventoryResponse struct {
        VariantID      string `json:"variant_id"`
        ShopID         string `json:"shop_id"`
        OnHand         int    `json:"on_hand"`
        Reserved       int    `json:"reserved"`
        Available      int    `json:"available"`
        AlertThreshold int    `json:"alert_threshold"`
        IsLowStock     bool   `json:"is_low_stock"`
        IsOutOfStock   bool   `json:"is_out_of_stock"`
        UpdatedAt      string `json:"updated_at"`
}

// InventoryWithVariantResponse is the wire shape for the inventory list view.
type InventoryWithVariantResponse struct {
        InventoryResponse
        ProductID   string  `json:"product_id"`
        ProductName string  `json:"product_name"`
        SKU         string  `json:"sku"`
        Size        *string `json:"size,omitempty"`
        Color       *string `json:"color,omitempty"`
        Price       int64   `json:"price"`
        Active      bool    `json:"active"`
}

// StockMovementResponse is the wire shape for a stock movement entry.
type StockMovementResponse struct {
        ID        string  `json:"id"`
        VariantID string  `json:"variant_id"`
        Type      string  `json:"type"`
        Quantity  int     `json:"quantity"`
        Reason    *string `json:"reason,omitempty"`
        OrderID   *string `json:"order_id,omitempty"`
        AuthorID  *string `json:"author_id,omitempty"`
        CreatedAt string  `json:"created_at"`
}

// StockDashboardStats is the wire shape for GET /inventory/stats.
type StockDashboardStats struct {
        TotalVariants    int64 `json:"total_variants"`
        LowStockCount    int64 `json:"low_stock_count"`
        OutOfStockCount  int64 `json:"out_of_stock_count"`
        TotalValueCFA    int64 `json:"total_value_cfa"`
        ReservedValueCFA int64 `json:"reserved_value_cfa"`
}

// ============================================================================
// Delivery zones
// ============================================================================

// CreateDeliveryZoneRequest is the body for POST /delivery-zones.
type CreateDeliveryZoneRequest struct {
        Name           string   `json:"name"             validate:"required,min=1,max=120"`
        Aliases        []string `json:"aliases,omitempty"`
        Fee            int64    `json:"fee"              validate:"min=0"`
        EstimatedDelay string   `json:"estimated_delay"  validate:"omitempty,max=40"`
        FreeFrom       *int64   `json:"free_from,omitempty"       validate:"omitempty,min=0"`
        MinOrderAmount *int64   `json:"min_order_amount,omitempty" validate:"omitempty,min=0"`
        Active         *bool    `json:"active,omitempty"`
}

// UpdateDeliveryZoneRequest is the body for PATCH /delivery-zones/{id}.
type UpdateDeliveryZoneRequest struct {
        Name           *string  `json:"name,omitempty"             validate:"omitempty,min=1,max=120"`
        Aliases        *[]string `json:"aliases,omitempty"`
        Fee            *int64   `json:"fee,omitempty"              validate:"omitempty,min=0"`
        EstimatedDelay *string  `json:"estimated_delay,omitempty"  validate:"omitempty,max=40"`
        FreeFrom       *int64   `json:"free_from,omitempty"`
        MinOrderAmount *int64   `json:"min_order_amount,omitempty"`
        Active         *bool    `json:"active,omitempty"`
}

// DeliveryZoneResponse is the wire shape for a delivery zone.
type DeliveryZoneResponse struct {
        ID             string   `json:"id"`
        ShopID         string   `json:"shop_id"`
        Name           string   `json:"name"`
        Aliases        []string `json:"aliases"`
        Fee            int64    `json:"fee"`
        EstimatedDelay *string  `json:"estimated_delay,omitempty"`
        FreeFrom       *int64   `json:"free_from,omitempty"`
        MinOrderAmount *int64   `json:"min_order_amount,omitempty"`
        Active         bool     `json:"active"`
        CreatedAt      string   `json:"created_at"`
        UpdatedAt      string   `json:"updated_at"`
}

// DeliveryFeeResult is the wire shape for the fee calculation endpoint.
type DeliveryFeeResult struct {
        ZoneID        string `json:"zone_id"`
        ZoneName      string `json:"zone_name"`
        Fee           int64  `json:"fee"`
        FreeDelivery  bool   `json:"free_delivery"`
        OrderAmount   int64  `json:"order_amount"`
        TotalPayable  int64  `json:"total_payable"`
}

// MatchZoneRequest is the body for POST /delivery-zones/match.
type MatchZoneRequest struct {
        Query string `json:"query" validate:"required,min=1,max=200"`
}

// CalculateFeeRequest is the body for POST /delivery-zones/{id}/calculate.
type CalculateFeeRequest struct {
        OrderAmount int64 `json:"order_amount" validate:"min=0"`
}

// ============================================================================
// Conversion helpers
// ============================================================================

// ToProductResponse converts a models.Product (with variants + images) into
// the wire shape. The EffectivePrice field on each variant is computed using
// the passed GetCurrentPrice function (declared in the catalog service) —
// we compute it here using the same logic so the helper is self-contained.
func ToProductResponse(p *Product, variants []ProductVariant, images []ProductImage) ProductResponse {
        if p == nil {
                return ProductResponse{}
        }
        vr := make([]VariantResponse, 0, len(variants))
        for i := range variants {
                vr = append(vr, ToVariantResponse(&variants[i]))
        }
        ir := make([]ProductImage, 0, len(images))
        for i := range images {
                ir = append(ir, images[i])
        }
        return ProductResponse{
                ID:          p.ID.String(),
                ShopID:      p.ShopID.String(),
                Name:        p.Name,
                Description: p.Description,
                Category:    p.Category,
                Brand:       p.Brand,
                Status:      string(p.Status),
                Variants:    vr,
                Images:      ir,
                CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// ToVariantResponse converts a models.ProductVariant into the wire shape,
// computing the effective price (promo if currently active).
func ToVariantResponse(v *ProductVariant) VariantResponse {
        if v == nil {
                return VariantResponse{}
        }
        var ps, pe *string
        if v.PromoStart != nil {
                s := v.PromoStart.UTC().Format(time.RFC3339)
                ps = &s
        }
        if v.PromoEnd != nil {
                e := v.PromoEnd.UTC().Format(time.RFC3339)
                pe = &e
        }
        return VariantResponse{
                ID:             v.ID.String(),
                ProductID:      v.ProductID.String(),
                SKU:            v.SKU,
                Size:           v.Size,
                Color:          v.Color,
                Price:          v.Price,
                PromoPrice:     v.PromoPrice,
                PromoStart:     ps,
                PromoEnd:       pe,
                EffectivePrice: CurrentVariantPrice(v),
                Active:         v.Active,
                CreatedAt:      v.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:      v.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// ToInventoryResponse converts a models.Inventory into the wire shape.
func ToInventoryResponse(i *Inventory) InventoryResponse {
        if i == nil {
                return InventoryResponse{}
        }
        return InventoryResponse{
                VariantID:      i.VariantID.String(),
                ShopID:         i.ShopID.String(),
                OnHand:         i.OnHand,
                Reserved:       i.Reserved,
                Available:      i.Available,
                AlertThreshold: i.AlertThreshold,
                IsLowStock:     i.Available > 0 && i.Available <= i.AlertThreshold,
                IsOutOfStock:   i.Available <= 0,
                UpdatedAt:      i.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// ToStockMovementResponse converts a models.StockMovement into the wire shape.
func ToStockMovementResponse(m *StockMovement) StockMovementResponse {
        if m == nil {
                return StockMovementResponse{}
        }
        var orderID, authorID *string
        if m.OrderID != nil {
                s := m.OrderID.String()
                orderID = &s
        }
        if m.AuthorID != nil {
                s := m.AuthorID.String()
                authorID = &s
        }
        return StockMovementResponse{
                ID:        m.ID.String(),
                VariantID: m.VariantID.String(),
                Type:      m.Type,
                Quantity:  m.Quantity,
                Reason:    m.Reason,
                OrderID:   orderID,
                AuthorID:  authorID,
                CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
        }
}

// ToDeliveryZoneResponse converts a models.DeliveryZone into the wire shape.
func ToDeliveryZoneResponse(z *DeliveryZone) DeliveryZoneResponse {
        if z == nil {
                return DeliveryZoneResponse{}
        }
        aliases := z.Aliases
        if aliases == nil {
                aliases = []string{}
        }
        return DeliveryZoneResponse{
                ID:             z.ID.String(),
                ShopID:         z.ShopID.String(),
                Name:           z.Name,
                Aliases:        aliases,
                Fee:            z.Fee,
                EstimatedDelay: z.EstimatedDelay,
                FreeFrom:       z.FreeFrom,
                MinOrderAmount: z.MinOrderAmount,
                Active:         z.Active,
                CreatedAt:      z.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:      z.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// CurrentVariantPrice returns the effective price of a variant at the given
// moment: promo_price if (PromoStart <= now <= PromoEnd AND PromoPrice != nil),
// else Price. Implemented here so handlers + services can share the logic.
func CurrentVariantPrice(v *ProductVariant) int64 {
        if v == nil {
                return 0
        }
        if v.PromoPrice != nil && v.PromoStart != nil && v.PromoEnd != nil {
                now := time.Now().UTC()
                start := v.PromoStart.UTC()
                end := v.PromoEnd.UTC()
                if (now.Equal(start) || now.After(start)) && (now.Before(end) || now.Equal(end)) {
                        return *v.PromoPrice
                }
        }
        return v.Price
}

// jsonString is a tiny helper for serializing a string slice as JSON safely.
func jsonString(s []string) json.RawMessage {
        if s == nil {
                return json.RawMessage("[]")
        }
        b, _ := json.Marshal(s)
        return b
}
