// Package models defines plain Go structs for the main database entities of
// NOVA. They mirror the schema created by migrations 001-012 and carry json
// tags (for HTTP serialization) and db tags (for future sqlc/pgxcollect
// integration). For now handlers scan rows into these structs manually.
//
// Status enums are typed strings (not PostgreSQL enum types) so the Go side
// stays decoupled from PG type OIDs — we validate at the service layer.
package models

import (
        "time"

        "github.com/google/uuid"
)

// --- Status / enum types (typed strings for readability) -------------------

type UserRole string // super_admin | admin | owner | employee

const (
        RoleSuperAdmin UserRole = "super_admin"
        RoleAdmin      UserRole = "admin"
        RoleOwner      UserRole = "owner"
        RoleEmployee   UserRole = "employee"
)

type ProductStatus string // draft | published | archived

const (
        ProductDraft     ProductStatus = "draft"
        ProductPublished ProductStatus = "published"
        ProductArchived  ProductStatus = "archived"
)

type OrderStatus string

const (
        OrderDraft          OrderStatus = "draft"
        OrderPending        OrderStatus = "pending"
        OrderConfirmed      OrderStatus = "confirmed"
        OrderPreparing      OrderStatus = "preparing"
        OrderDelivering     OrderStatus = "delivering"
        OrderDelivered      OrderStatus = "delivered"
        OrderDeliveryFailed OrderStatus = "delivery_failed"
        OrderReturned       OrderStatus = "returned"
        OrderCancelled      OrderStatus = "cancelled"
)

type PaymentStatus string

const (
        PaymentOnDelivery     PaymentStatus = "on_delivery"
        PaymentPendingPayment PaymentStatus = "pending_payment"
        PaymentDeclared       PaymentStatus = "declared"
        PaymentPaid           PaymentStatus = "paid"
        PaymentFailed         PaymentStatus = "failed"
        PaymentRefunded       PaymentStatus = "refunded"
)

type PaymentMode string

const (
        PayCash         PaymentMode = "cash"
        PayMobileMoney  PaymentMode = "mobile_money"
        PayWave         PaymentMode = "wave"
        PayOrangeMoney  PaymentMode = "orange_money"
        PayMTNMomo      PaymentMode = "mtn_momo"
)

type SubscriptionStatus string

const (
        SubTrial       SubscriptionStatus = "trial"
        SubActive      SubscriptionStatus = "active"
        SubLate        SubscriptionStatus = "late"
        SubGracePeriod SubscriptionStatus = "grace_period"
        SubSuspended   SubscriptionStatus = "suspended"
        SubTerminated  SubscriptionStatus = "terminated"
)

type ConversationState string // ai | human | closed

const (
        ConvAI     ConversationState = "ai"
        ConvHuman  ConversationState = "human"
        ConvClosed ConversationState = "closed"
)

type CustomerStatus string // prospect | client | recurring

const (
        CustomerProspect  CustomerStatus = "prospect"
        CustomerClient    CustomerStatus = "client"
        CustomerRecurring CustomerStatus = "recurring"
)

// --- Entities ---------------------------------------------------------------

// ShopStatus is the lifecycle state of a shop (ch. 4.1).
//   - draft      : newly created, onboarding not yet complete (no published
//                  products / no delivery zones / no hours). Cannot serve
//                  customers.
//   - active     : validated by the owner (POST /api/shops/{id}/activate) and
//                  open for business.
//   - suspended  : paused by a platform admin (impayé, abuse, ...). Data is
//                  preserved; the shop can be reactivated.
//   - terminated : permanently closed. Data is retained for the legal
//                  retention period then soft-deleted.
type ShopStatus string

const (
        ShopDraft      ShopStatus = "draft"
        ShopActive     ShopStatus = "active"
        ShopSuspended  ShopStatus = "suspended"
        ShopTerminated ShopStatus = "terminated"
)

// Shop represents a tenant business (ch. 10).
type Shop struct {
        ID                   uuid.UUID  `json:"id"                db:"id"`
        Name                 string     `json:"name"              db:"name"`
        Slug                 string     `json:"slug"              db:"slug"`
        Phone                *string    `json:"phone,omitempty"   db:"phone"`
        WhatsAppNumber       *string    `json:"whatsapp_number,omitempty" db:"whatsapp_number"`
        Address              *string    `json:"address,omitempty" db:"address"`
        Commune              *string    `json:"commune,omitempty" db:"commune"`
        Hours                []byte     `json:"hours"             db:"hours"` // jsonb
        Description          *string    `json:"description,omitempty" db:"description"`
        Categories           []string   `json:"categories"        db:"categories"`
        SaleConditions       *string    `json:"sale_conditions,omitempty" db:"sale_conditions"`
        AcceptedPaymentModes []string   `json:"accepted_payment_modes" db:"accepted_payment_modes"`
        AISettings           []byte     `json:"ai_settings"       db:"ai_settings"` // jsonb
        Status               ShopStatus `json:"status"            db:"status"`      // draft|active|suspended|terminated
        LogoURL              *string    `json:"logo_url,omitempty" db:"logo_url"`
        CreatedAt            time.Time  `json:"created_at"        db:"created_at"`
        UpdatedAt            time.Time  `json:"updated_at"        db:"updated_at"`
        DeletedAt            *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

// User is the global account (ch. 10). Role is the platform role; the
// per-shop role lives on ShopMember.
type User struct {
        ID                uuid.UUID  `json:"id"                 db:"id"`
        Email             string     `json:"email"              db:"email"`
        Phone             *string    `json:"phone,omitempty"    db:"phone"`
        PasswordHash      *string    `json:"-"                  db:"password_hash"` // never serialized
        FullName          string     `json:"full_name"          db:"full_name"`
        Role              UserRole   `json:"role"               db:"role"`
        TwoFactorSecret   *string    `json:"-"                  db:"two_factor_secret"`
        TwoFactorEnabled  bool       `json:"two_factor_enabled" db:"two_factor_enabled"`
        FailedLoginCount  int        `json:"-"                  db:"failed_login_count"`
        LockedUntil       *time.Time `json:"-"                  db:"locked_until"`
        LastLoginAt       *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
        CreatedAt         time.Time  `json:"created_at"         db:"created_at"`
        UpdatedAt         time.Time  `json:"updated_at"         db:"updated_at"`
        DeletedAt         *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

// ShopMember links a User to a Shop with a per-shop role.
type ShopMember struct {
        ID          uuid.UUID `json:"id"          db:"id"`
        ShopID      uuid.UUID `json:"shop_id"     db:"shop_id"`
        UserID      uuid.UUID `json:"user_id"     db:"user_id"`
        Role        UserRole  `json:"role"        db:"role"`
        Permissions []byte    `json:"permissions" db:"permissions"` // jsonb
        CreatedAt   time.Time `json:"created_at"  db:"created_at"`
        UpdatedAt   time.Time `json:"updated_at"  db:"updated_at"`
}

// Product is the parent of one or more ProductVariants.
type Product struct {
        ID          uuid.UUID  `json:"id"           db:"id"`
        ShopID      uuid.UUID  `json:"shop_id"      db:"shop_id"`
        Name        string     `json:"name"         db:"name"`
        Description *string    `json:"description,omitempty" db:"description"`
        Category    *string    `json:"category,omitempty"    db:"category"`
        Brand       *string    `json:"brand,omitempty"       db:"brand"`
        Status      ProductStatus `json:"status"      db:"status"`
	ImageURL     *string    `json:"image_url,omitempty" db:"image_url"`
        CreatedAt   time.Time  `json:"created_at"   db:"created_at"`
        UpdatedAt   time.Time  `json:"updated_at"   db:"updated_at"`
        DeletedAt   *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

// ProductVariant is the sellable SKU (size/color/price).
type ProductVariant struct {
        ID          uuid.UUID  `json:"id"           db:"id"`
        ProductID   uuid.UUID  `json:"product_id"   db:"product_id"`
        ShopID      uuid.UUID  `json:"shop_id"      db:"shop_id"`
        SKU         string     `json:"sku"          db:"sku"`
        Size        *string    `json:"size,omitempty" db:"size"`
        Color       *string    `json:"color,omitempty" db:"color"`
        Price       int64      `json:"price"        db:"price"`        // FCFA
        PromoPrice  *int64     `json:"promo_price,omitempty" db:"promo_price"`
        PromoStart  *time.Time `json:"promo_start,omitempty" db:"promo_start"`
        PromoEnd    *time.Time `json:"promo_end,omitempty"   db:"promo_end"`
        Active      bool       `json:"active"       db:"active"`
        CreatedAt   time.Time  `json:"created_at"   db:"created_at"`
        UpdatedAt   time.Time  `json:"updated_at"   db:"updated_at"`
}

// ProductImage is an ordered product photo.
type ProductImage struct {
        ID        uuid.UUID `json:"id"         db:"id"`
        ProductID uuid.UUID `json:"product_id" db:"product_id"`
        ShopID    uuid.UUID `json:"shop_id"    db:"shop_id"`
        URL       string    `json:"url"        db:"url"`
        Ord       int       `json:"ord"        db:"ord"`
        CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Inventory is the live stock row for a variant (with the GENERATED
// `available` column from migration 004).
type Inventory struct {
        VariantID      uuid.UUID `json:"variant_id"      db:"variant_id"`
        ShopID         uuid.UUID `json:"shop_id"         db:"shop_id"`
        OnHand         int       `json:"on_hand"         db:"on_hand"`
        Reserved       int       `json:"reserved"        db:"reserved"`
        Available      int       `json:"available"       db:"available"` // GENERATED ALWAYS AS (on_hand - reserved) STORED
        AlertThreshold int       `json:"alert_threshold" db:"alert_threshold"`
        StockMode      string    `json:"stock_mode"      db:"stock_mode"` // quantite | epuise | illimite (added v3, migration 017)
        UpdatedAt      time.Time `json:"updated_at"      db:"updated_at"`
}

// StockMovement is an immutable entry in the stock_movements journal
// (migration 004). Append-only (trigger + RLS block UPDATE/DELETE).
type StockMovement struct {
        ID        uuid.UUID  `json:"id"         db:"id"`
        VariantID uuid.UUID  `json:"variant_id" db:"variant_id"`
        ShopID    uuid.UUID  `json:"shop_id"    db:"shop_id"`
        Type      string     `json:"type"       db:"type"` // receipt|reservation|release|exit|return|adjustment
        Quantity  int        `json:"quantity"   db:"quantity"`
        Reason    *string    `json:"reason,omitempty" db:"reason"`
        OrderID   *uuid.UUID `json:"order_id,omitempty" db:"order_id"`
        AuthorID  *uuid.UUID `json:"author_id,omitempty" db:"author_id"`
        CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// Customer is a WhatsApp client of a shop.
type Customer struct {
        ID          uuid.UUID      `json:"id"          db:"id"`
        ShopID      uuid.UUID      `json:"shop_id"     db:"shop_id"`
        Phone       string         `json:"phone"       db:"phone"`
        Name        *string        `json:"name,omitempty" db:"name"`
        Status      CustomerStatus `json:"status"      db:"status"`
        Notes       *string        `json:"notes,omitempty" db:"notes"`
        CreatedAt   time.Time      `json:"created_at"  db:"created_at"`
        UpdatedAt   time.Time      `json:"updated_at"  db:"updated_at"`
        DeletedAt   *time.Time     `json:"deleted_at,omitempty" db:"deleted_at"`
}

// Order is the central commercial document.
type Order struct {
        ID                uuid.UUID      `json:"id"                 db:"id"`
        ShopID            uuid.UUID      `json:"shop_id"            db:"shop_id"`
        Number            string         `json:"number"             db:"number"`
        CustomerID        uuid.UUID      `json:"customer_id"        db:"customer_id"`
        CartID            *uuid.UUID     `json:"cart_id,omitempty"  db:"cart_id"`
        Status            OrderStatus    `json:"status"             db:"status"`
        PaymentStatus     PaymentStatus  `json:"payment_status"     db:"payment_status"`
        PaymentMode       PaymentMode    `json:"payment_mode"       db:"payment_mode"`
        PaymentReference  *string        `json:"payment_reference,omitempty" db:"payment_reference"`
        Subtotal          int64          `json:"subtotal"           db:"subtotal"` // FCFA
        DeliveryFee       int64          `json:"delivery_fee"       db:"delivery_fee"`
        Total             int64          `json:"total"              db:"total"`
        IdempotencyKey    *string        `json:"idempotency_key,omitempty" db:"idempotency_key"`
        DeliveryZoneID    *uuid.UUID     `json:"delivery_zone_id,omitempty" db:"delivery_zone_id"`
        DeliveryAddress   *string        `json:"delivery_address,omitempty" db:"delivery_address"`
        Note              *string        `json:"note,omitempty"     db:"note"`
        // NOVA v3 (migration 017) — payment deadline + revenue tracking.
        PaymentDeadline   *time.Time     `json:"payment_deadline,omitempty" db:"payment_deadline"`
        RevenueCounted    bool           `json:"revenue_counted"    db:"revenue_counted"`
        CreatedAt         time.Time      `json:"created_at"         db:"created_at"`
        ConfirmedAt       *time.Time     `json:"confirmed_at,omitempty" db:"confirmed_at"`
        UpdatedAt         time.Time      `json:"updated_at"         db:"updated_at"`
}

// OrderItem is a frozen snapshot line of an order.
type OrderItem struct {
        ID          uuid.UUID `json:"id"           db:"id"`
        OrderID     uuid.UUID `json:"order_id"     db:"order_id"`
        ShopID      uuid.UUID `json:"shop_id"      db:"shop_id"`
        VariantID   *uuid.UUID `json:"variant_id,omitempty" db:"variant_id"`
        ProductName string    `json:"product_name" db:"product_name"`
        VariantInfo *string   `json:"variant_info,omitempty" db:"variant_info"`
        UnitPrice   int64     `json:"unit_price"   db:"unit_price"` // FCFA
        Quantity    int       `json:"quantity"     db:"quantity"`
        LineTotal   int64     `json:"line_total"   db:"line_total"`
}

// DeliveryZone is a geographical area a shop delivers to.
type DeliveryZone struct {
        ID             uuid.UUID `json:"id"              db:"id"`
        ShopID         uuid.UUID `json:"shop_id"         db:"shop_id"`
        Name           string    `json:"name"            db:"name"`
        Aliases        []string  `json:"aliases"         db:"aliases"`
        Fee            int64     `json:"fee"             db:"fee"`               // FCFA
        EstimatedDelay *string   `json:"estimated_delay,omitempty" db:"estimated_delay"` // e.g. "2h", "Jour J+1"
        FreeFrom       *int64    `json:"free_from,omitempty" db:"free_from"`       // FCFA, NULL = never free
        MinOrderAmount *int64    `json:"min_order_amount,omitempty" db:"min_order_amount"` // FCFA, NULL = no minimum
        Active         bool      `json:"active"         db:"active"`
        CreatedAt      time.Time `json:"created_at"     db:"created_at"`
        UpdatedAt      time.Time `json:"updated_at"     db:"updated_at"`
}

// Plan is a global SaaS offer (no RLS — visible to all shops).
type Plan struct {
        ID            uuid.UUID `json:"id"             db:"id"`
        Name          string    `json:"name"           db:"name"`
        Price         int64     `json:"price"          db:"price"`         // monthly FCFA
        SetupFee      int64     `json:"setup_fee"      db:"setup_fee"`     // one-time FCFA
        MessageQuota  int       `json:"message_quota"  db:"message_quota"`
        ProductLimit  *int      `json:"product_limit,omitempty"  db:"product_limit"`  // NULL = unlimited
        EmployeeLimit *int      `json:"employee_limit,omitempty" db:"employee_limit"` // NULL = unlimited
        Features      []byte    `json:"features"       db:"features"` // jsonb
        Active        bool      `json:"active"         db:"active"`
        CreatedAt     time.Time `json:"created_at"     db:"created_at"`
        UpdatedAt     time.Time `json:"updated_at"     db:"updated_at"`
}

// Subscription is the active plan for a shop. Lifecycle per ch. 7:
//   trial → active → (late → grace_period → suspended | active) → terminated.
type Subscription struct {
        ID            uuid.UUID          `json:"id"             db:"id"`
        ShopID        uuid.UUID          `json:"shop_id"        db:"shop_id"`
        PlanID        uuid.UUID          `json:"plan_id"        db:"plan_id"`
        Status        SubscriptionStatus `json:"status"         db:"status"`
        StartedAt     time.Time          `json:"started_at"     db:"started_at"`
        NextBillingAt *time.Time         `json:"next_billing_at,omitempty" db:"next_billing_at"`
        GraceUntil    *time.Time         `json:"grace_until,omitempty"    db:"grace_until"`
        SuspendedAt   *time.Time         `json:"suspended_at,omitempty"   db:"suspended_at"`
        TerminatedAt  *time.Time         `json:"terminated_at,omitempty"  db:"terminated_at"`
        CreatedAt     time.Time          `json:"created_at"     db:"created_at"`
        UpdatedAt     time.Time          `json:"updated_at"     db:"updated_at"`
}

// SubscriptionPayment is a recorded payment for a subscription period.
type SubscriptionPayment struct {
        ID             uuid.UUID  `json:"id"              db:"id"`
        SubscriptionID uuid.UUID  `json:"subscription_id" db:"subscription_id"`
        ShopID         uuid.UUID  `json:"shop_id"         db:"shop_id"`
        Amount         int64      `json:"amount"          db:"amount"` // FCFA
        Mode           PaymentMode `json:"mode"           db:"mode"`
        Reference      *string    `json:"reference,omitempty" db:"reference"`
        PeriodStart    time.Time  `json:"period_start"    db:"period_start"` // date
        PeriodEnd      time.Time  `json:"period_end"      db:"period_end"`   // date
        RecordedBy     *uuid.UUID `json:"recorded_by,omitempty" db:"recorded_by"`
        RecordedAt     time.Time  `json:"recorded_at"     db:"recorded_at"`
}

// CartStatus is the lifecycle state of a cart.
type CartStatus string

const (
        CartActive    CartStatus = "active"
        CartAbandoned CartStatus = "abandoned"
        CartConverted CartStatus = "converted"
)

// Cart is a conversation-bound shopping cart (ch. 4.5).
type Cart struct {
        ID             uuid.UUID      `json:"id"              db:"id"`
        ShopID         uuid.UUID      `json:"shop_id"         db:"shop_id"`
        ConversationID *uuid.UUID     `json:"conversation_id,omitempty" db:"conversation_id"`
        CustomerID     *uuid.UUID     `json:"customer_id,omitempty"     db:"customer_id"`
        Status         CartStatus     `json:"status"          db:"status"`
        ExpiresAt      *time.Time     `json:"expires_at,omitempty"      db:"expires_at"`
        CreatedAt      time.Time      `json:"created_at"      db:"created_at"`
        UpdatedAt      time.Time      `json:"updated_at"      db:"updated_at"`
        Items          []CartItem     `json:"items,omitempty"`
}

// CartItem is a line in a cart. product_name + unit_price are snapshotted
// at insertion time so the cart display is stable between add and confirm.
type CartItem struct {
        ID          uuid.UUID  `json:"id"          db:"id"`
        CartID      uuid.UUID  `json:"cart_id"     db:"cart_id"`
        ShopID      uuid.UUID  `json:"shop_id"     db:"shop_id"`
        VariantID   *uuid.UUID `json:"variant_id,omitempty" db:"variant_id"`
        ProductName string     `json:"product_name" db:"product_name"`
        UnitPrice   int64      `json:"unit_price"   db:"unit_price"` // FCFA snapshot
        Quantity    int        `json:"quantity"     db:"quantity"`
        // Extra fields populated when reading (joined from variant/product):
        VariantInfo *string `json:"variant_info,omitempty" db:"variant_info"`
        SKU          *string `json:"sku,omitempty"          db:"sku"`
        Available    *int    `json:"available,omitempty"    db:"available"` // inventory.available at read time
}

// OrderEvent is an immutable status-change entry in the order_events journal
// (ch. 4.5 — the deterministic state machine).
type OrderEvent struct {
        ID         uuid.UUID   `json:"id"          db:"id"`
        OrderID    uuid.UUID   `json:"order_id"    db:"order_id"`
        ShopID     uuid.UUID   `json:"shop_id"     db:"shop_id"`
        FromStatus *OrderStatus `json:"from_status,omitempty" db:"from_status"`
        ToStatus   OrderStatus  `json:"to_status"   db:"to_status"`
        AuthorID   *uuid.UUID  `json:"author_id,omitempty" db:"author_id"`
        Reason     *string     `json:"reason,omitempty" db:"reason"`
        CreatedAt  time.Time   `json:"created_at"  db:"created_at"`
}
