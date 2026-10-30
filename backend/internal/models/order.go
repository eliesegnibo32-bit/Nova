// Order DTOs for the HTTP layer — carts, orders, order items, order events,
// and the state-machine transition inputs.
//
// Field semantics follow the cahier des charges:
//   - ch. 4.5 Commandes: panier multi-produits, machine à états déterministe,
//     statut de paiement séparé, idempotence (clé unique par boutique),
//     prix figé à la confirmation (snapshot dans order_items).
//   - ch. 10 (tables carts/cart_items/orders/order_items/order_events).
//   - ch. 13 (erreurs): double confirmation = idempotence, paiement
//     "déclaré" jamais compté comme payé sans validation.
//
// All amounts (subtotal, delivery_fee, total, unit_price, line_total) are
// integer FCFA.
package models

import (
        "time"

        "github.com/google/uuid"
)

// ============================================================================
// Carts
// ============================================================================

// GetOrCreateCartRequest is the body of POST /api/shops/{shopId}/carts.
// conversation_id is required; customer_id is required (the cart needs a
// customer to convert into an order).
type GetOrCreateCartRequest struct {
        ConversationID string `json:"conversation_id,omitempty" validate:"omitempty,uuid"`
        CustomerID     string `json:"customer_id"        validate:"required,uuid"`
}

// AddCartItemRequest is the body of POST /api/shops/{shopId}/carts/{cartId}/items.
type AddCartItemRequest struct {
        VariantID string `json:"variant_id" validate:"required,uuid"`
        Quantity  int    `json:"quantity"   validate:"required,min=1,max=100000"`
}

// UpdateCartItemRequest is the body of PATCH
// /api/shops/{shopId}/carts/{cartId}/items/{itemId}.
// Quantity = 0 removes the item.
type UpdateCartItemRequest struct {
        Quantity int `json:"quantity" validate:"min=0,max=100000"`
}

// CartItemResponse is the wire shape for a cart line.
type CartItemResponse struct {
        ID          string  `json:"id"`
        VariantID   *string `json:"variant_id,omitempty"`
        ProductName string  `json:"product_name"`
        VariantInfo *string `json:"variant_info,omitempty"`
        SKU         *string `json:"sku,omitempty"`
        UnitPrice   int64   `json:"unit_price"`
        Quantity    int     `json:"quantity"`
        LineTotal   int64   `json:"line_total"`
        Available   *int    `json:"available,omitempty"`
}

// CartResponse is the wire shape for a cart (with items).
type CartResponse struct {
        ID             string            `json:"id"`
        ShopID         string            `json:"shop_id"`
        ConversationID *string           `json:"conversation_id,omitempty"`
        CustomerID     *string           `json:"customer_id,omitempty"`
        Status         string            `json:"status"`
        ExpiresAt      *string           `json:"expires_at,omitempty"`
        CreatedAt      string            `json:"created_at"`
        UpdatedAt      string            `json:"updated_at"`
        Items          []CartItemResponse `json:"items"`
        ItemsCount     int               `json:"items_count"`
        Subtotal       int64             `json:"subtotal"`
}

// ============================================================================
// Order recap (pre-confirmation) — ch. 4.5 step 7
// ============================================================================

// RecapRequest is the body of POST /api/shops/{shopId}/carts/{cartId}/recap.
type RecapRequest struct {
        ZoneID      string `json:"zone_id"      validate:"required,uuid"`
        PaymentMode string `json:"payment_mode" validate:"required,oneof=cash mobile_money wave orange_money mtn_momo"`
}

// OrderRecap is the wire shape for the recap returned by GenerateRecap.
// It does NOT create an order — it's just a preview for the client.
type OrderRecap struct {
        Items       []CartItemResponse `json:"items"`
        Subtotal    int64              `json:"subtotal"`
        DeliveryFee int64              `json:"delivery_fee"`
        FreeDelivery bool              `json:"free_delivery"`
        Total       int64              `json:"total"`
        Zone        *DeliveryZoneResponse `json:"zone,omitempty"`
        PaymentMode string             `json:"payment_mode"`
}

// ============================================================================
// Orders — create / list / update
// ============================================================================

// ConfirmOrderRequest is the body of POST /api/shops/{shopId}/orders.
// cart_id is required — orders are created from carts.
// idempotency_key is required — same key returns the same order (ch. 4.5).
type ConfirmOrderRequest struct {
        CartID          string `json:"cart_id"          validate:"required,uuid"`
        ZoneID          string `json:"zone_id"          validate:"required,uuid"`
        DeliveryAddress string `json:"delivery_address" validate:"required,min=3,max=500"`
        PaymentMode     string `json:"payment_mode"     validate:"required,oneof=cash mobile_money wave orange_money mtn_momo"`
        PaymentStatus   string `json:"payment_status,omitempty" validate:"omitempty,oneof=on_delivery pending_payment declared paid failed refunded"`
        IdempotencyKey  string `json:"idempotency_key"  validate:"required,min=1,max=200"`
        CustomerID      string `json:"customer_id,omitempty" validate:"omitempty,uuid"`
        Note            string `json:"note,omitempty"         validate:"omitempty,max=1000"`
}

// ListOrdersParams holds the query parameters for GET /orders.
type ListOrdersParams struct {
        Page           int
        Limit          int
        Status         string
        PaymentStatus  string
        CustomerID     string
        Search         string // search by order number
        From           *time.Time
        To             *time.Time
}

// Normalize fills sane defaults.
func (p *ListOrdersParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 200 {
                p.Limit = 20
        }
}

// Offset returns the SQL OFFSET.
func (p *ListOrdersParams) Offset() int { return (p.Page - 1) * p.Limit }

// OrderItemResponse is the wire shape for an order line (frozen price).
type OrderItemResponse struct {
        ID          string  `json:"id"`
        VariantID   *string `json:"variant_id,omitempty"`
        ProductName string  `json:"product_name"`
        VariantInfo *string `json:"variant_info,omitempty"`
        UnitPrice   int64   `json:"unit_price"`
        Quantity    int     `json:"quantity"`
        LineTotal   int64   `json:"line_total"`
}

// OrderEventResponse is the wire shape for an order event entry.
type OrderEventResponse struct {
        ID         string  `json:"id"`
        FromStatus *string `json:"from_status,omitempty"`
        ToStatus   string  `json:"to_status"`
        AuthorID   *string `json:"author_id,omitempty"`
        Reason     *string `json:"reason,omitempty"`
        CreatedAt  string  `json:"created_at"`
}

// OrderResponse is the wire shape for a single order (with items + events).
type OrderResponse struct {
        ID                string                `json:"id"`
        ShopID            string                `json:"shop_id"`
        Number            string                `json:"number"`
        CustomerID        string                `json:"customer_id"`
        CartID            *string               `json:"cart_id,omitempty"`
        Status            string                `json:"status"`
        PaymentStatus     string                `json:"payment_status"`
        PaymentMode       string                `json:"payment_mode"`
        PaymentReference  *string               `json:"payment_reference,omitempty"`
        Subtotal          int64                 `json:"subtotal"`
        DeliveryFee       int64                 `json:"delivery_fee"`
        Total             int64                 `json:"total"`
        IdempotencyKey    *string               `json:"idempotency_key,omitempty"`
        DeliveryZoneID    *string               `json:"delivery_zone_id,omitempty"`
        DeliveryAddress   *string               `json:"delivery_address,omitempty"`
        Note              *string               `json:"note,omitempty"`
        Items             []OrderItemResponse   `json:"items"`
        Events            []OrderEventResponse  `json:"events,omitempty"`
        CreatedAt         string                `json:"created_at"`
        ConfirmedAt       *string               `json:"confirmed_at,omitempty"`
        UpdatedAt         string                `json:"updated_at"`
}

// OrderListItemResponse is the lighter shape returned in list view.
type OrderListItemResponse struct {
        ID              string  `json:"id"`
        Number          string  `json:"number"`
        Status          string  `json:"status"`
        PaymentStatus   string  `json:"payment_status"`
        PaymentMode     string  `json:"payment_mode"`
        CustomerID      string  `json:"customer_id"`
        CustomerName    *string `json:"customer_name,omitempty"`
        ItemsCount      int     `json:"items_count"`
        Total           int64   `json:"total"`
        CreatedAt       string  `json:"created_at"`
        UpdatedAt       string  `json:"updated_at"`
}

// UpdateOrderStatusRequest is the body of POST /orders/{id}/advance.
type UpdateOrderStatusRequest struct {
        Status string `json:"status" validate:"required,oneof=draft pending confirmed preparing delivering delivered delivery_failed returned cancelled"`
        Reason string `json:"reason,omitempty" validate:"omitempty,max=500"`
}

// CancelOrderRequest is the body of POST /orders/{id}/cancel.
type CancelOrderRequest struct {
        Reason string `json:"reason" validate:"required,min=3,max=500"`
}

// UpdatePaymentStatusRequest is the body of PATCH /orders/{id}/payment.
type UpdatePaymentStatusRequest struct {
        PaymentStatus  string `json:"payment_status"  validate:"required,oneof=on_delivery pending_payment declared paid failed refunded"`
        PaymentReference string `json:"payment_reference,omitempty" validate:"omitempty,max=200"`
}

// ============================================================================
// Dashboard
// ============================================================================

// OrderDashboardStats is the wire shape for GET /orders/stats.
type OrderDashboardStats struct {
        ByStatus          map[string]int64 `json:"by_status"`
        ByPaymentStatus   map[string]int64 `json:"by_payment_status"`
        PendingCount      int64            `json:"pending_count"`
        TodayCount        int64            `json:"today_count"`
        MonthRevenue      int64            `json:"month_revenue"`
        MonthOrderCount   int64            `json:"month_order_count"`
        TotalOrders       int64            `json:"total_orders"`
}

// ============================================================================
// Conversion helpers
// ============================================================================

// ToCartResponse converts a models.Cart into the wire shape.
func ToCartResponse(c *Cart) CartResponse {
        if c == nil {
                return CartResponse{}
        }
        items := make([]CartItemResponse, 0, len(c.Items))
        var subtotal int64
        for i := range c.Items {
                it := &c.Items[i]
                lineTotal := it.UnitPrice * int64(it.Quantity)
                items = append(items, CartItemResponse{
                        ID:          it.ID.String(),
                        VariantID:   uuidToStr(it.VariantID),
                        ProductName: it.ProductName,
                        VariantInfo: it.VariantInfo,
                        SKU:         it.SKU,
                        UnitPrice:   it.UnitPrice,
                        Quantity:    it.Quantity,
                        LineTotal:   lineTotal,
                        Available:   it.Available,
                })
                subtotal += lineTotal
        }
        return CartResponse{
                ID:             c.ID.String(),
                ShopID:         c.ShopID.String(),
                ConversationID: uuidToStr(c.ConversationID),
                CustomerID:     uuidToStr(c.CustomerID),
                Status:         string(c.Status),
                ExpiresAt:      timeToStr(c.ExpiresAt),
                CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:      c.UpdatedAt.UTC().Format(time.RFC3339),
                Items:          items,
                ItemsCount:     len(items),
                Subtotal:       subtotal,
        }
}

// ToOrderResponse converts a models.Order (with items + events) into the wire shape.
func ToOrderResponse(o *Order, items []OrderItem, events []OrderEvent) OrderResponse {
        if o == nil {
                return OrderResponse{}
        }
        ir := make([]OrderItemResponse, 0, len(items))
        for i := range items {
                it := &items[i]
                ir = append(ir, OrderItemResponse{
                        ID:          it.ID.String(),
                        VariantID:   uuidToStr(it.VariantID),
                        ProductName: it.ProductName,
                        VariantInfo: it.VariantInfo,
                        UnitPrice:   it.UnitPrice,
                        Quantity:    it.Quantity,
                        LineTotal:   it.LineTotal,
                })
        }
        er := make([]OrderEventResponse, 0, len(events))
        for i := range events {
                ev := &events[i]
                var fromS *string
                if ev.FromStatus != nil {
                        s := string(*ev.FromStatus)
                        fromS = &s
                }
                er = append(er, OrderEventResponse{
                        ID:         ev.ID.String(),
                        FromStatus: fromS,
                        ToStatus:   string(ev.ToStatus),
                        AuthorID:   uuidToStr(ev.AuthorID),
                        Reason:     ev.Reason,
                        CreatedAt:  ev.CreatedAt.UTC().Format(time.RFC3339),
                })
        }
        return OrderResponse{
                ID:                o.ID.String(),
                ShopID:            o.ShopID.String(),
                Number:            o.Number,
                CustomerID:        o.CustomerID.String(),
                CartID:            uuidToStr(o.CartID),
                Status:            string(o.Status),
                PaymentStatus:     string(o.PaymentStatus),
                PaymentMode:       string(o.PaymentMode),
                PaymentReference:  o.PaymentReference,
                Subtotal:          o.Subtotal,
                DeliveryFee:       o.DeliveryFee,
                Total:             o.Total,
                IdempotencyKey:    o.IdempotencyKey,
                DeliveryZoneID:    uuidToStr(o.DeliveryZoneID),
                DeliveryAddress:   o.DeliveryAddress,
                Note:              o.Note,
                Items:             ir,
                Events:            er,
                CreatedAt:         o.CreatedAt.UTC().Format(time.RFC3339),
                ConfirmedAt:       timeToStr(o.ConfirmedAt),
                UpdatedAt:         o.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// --- helpers ----------------------------------------------------------------

func uuidToStr(u *uuid.UUID) *string {
        if u == nil {
                return nil
        }
        s := u.String()
        return &s
}

func timeToStr(t *time.Time) *string {
        if t == nil {
                return nil
        }
        s := t.UTC().Format(time.RFC3339)
        return &s
}
