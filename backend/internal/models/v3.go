// Package models — NOVA v3 additions: ProductOption, PaymentConfig, StockMode,
// OrderV3 statuses, and the new DTOs for the v3 endpoints.
//
// These types supplement the existing models.go / order.go / catalog.go.
// We keep them in a separate file so the v3 changes are easy to review
// independently of the existing (still-working) v1/v2 code.
package models

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// Stock mode (NOVA v3 — spec section 2)
// ============================================================================

// StockMode is the lifecycle mode of an inventory row (or product_option).
//   - quantite : normal stock (on_hand/reserved, decremented)
//   - epuise   : explicitly out of stock (NOVA won't propose)
//   - illimite : infinite, never decremented
type StockMode string

const (
	StockModeQuantite StockMode = "quantite"
	StockModeEpuise   StockMode = "epuise"
	StockModeIllimite StockMode = "illimite"
)

// IsValidStockMode returns true if the given string is a valid stock_mode.
func IsValidStockMode(s string) bool {
	switch StockMode(s) {
	case StockModeQuantite, StockModeEpuise, StockModeIllimite:
		return true
	}
	return false
}

// ============================================================================
// Product options (NOVA v3 — spec section 3)
// ============================================================================

// ProductOptionType is the type of a product option (plat / accompagnement / boisson).
type ProductOptionType string

const (
	OptionTypePlat           ProductOptionType = "plat"
	OptionTypeAccompagnement ProductOptionType = "accompagnement"
	OptionTypeBoisson        ProductOptionType = "boisson"
)

// IsValidProductOptionType returns true if the given string is a valid option type.
func IsValidProductOptionType(s string) bool {
	switch ProductOptionType(s) {
	case OptionTypePlat, OptionTypeAccompagnement, OptionTypeBoisson:
		return true
	}
	return false
}

// ProductOption is a row in the product_options table.
type ProductOption struct {
	ID        uuid.UUID          `json:"id"          db:"id"`
	ShopID    uuid.UUID          `json:"shop_id"     db:"shop_id"`
	ProductID *uuid.UUID         `json:"product_id,omitempty" db:"product_id"`
	Type      ProductOptionType  `json:"type"        db:"type"`
	Name      string             `json:"name"        db:"name"`
	Price     int64              `json:"price"       db:"price"` // FCFA — 0 = "Offert"
	StockMode StockMode          `json:"stock_mode"  db:"stock_mode"`
	StockQty  *int               `json:"stock_qty,omitempty" db:"stock_qty"` // NULL if illimite
	Active    bool               `json:"active"      db:"active"`
	CreatedAt time.Time          `json:"created_at"  db:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"  db:"updated_at"`
}

// CreateProductOptionRequest is the body of POST /api/shops/{shopId}/products/{productId}/options
// or POST /api/shops/{shopId}/options (standalone).
type CreateProductOptionRequest struct {
	Type      string `json:"type"       validate:"required,oneof=plat accompagnement boisson"`
	Name      string `json:"name"       validate:"required,min=1,max=200"`
	Price     int64  `json:"price"      validate:"min=0"`
	StockMode string `json:"stock_mode" validate:"omitempty,oneof=quantite epuise illimite"`
	StockQty  *int   `json:"stock_qty,omitempty" validate:"omitempty,min=0"`
	Active    *bool  `json:"active,omitempty"`
}

// UpdateProductOptionRequest is the body of PATCH /api/shops/{shopId}/options/{id}.
type UpdateProductOptionRequest struct {
	Name      *string `json:"name,omitempty"       validate:"omitempty,min=1,max=200"`
	Price     *int64  `json:"price,omitempty"      validate:"omitempty,min=0"`
	StockMode *string `json:"stock_mode,omitempty" validate:"omitempty,oneof=quantite epuise illimite"`
	StockQty  *int    `json:"stock_qty,omitempty"  validate:"omitempty,min=0"`
	Active    *bool   `json:"active,omitempty"`
}

// ProductOptionResponse is the wire shape for a product option.
type ProductOptionResponse struct {
	ID          string  `json:"id"`
	ProductID   *string `json:"product_id,omitempty"`
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Price       int64   `json:"price"`
	PriceLabel  string  `json:"price_label"` // "Offert" if price == 0, else "5 000 FCFA"
	StockMode   string  `json:"stock_mode"`
	StockQty    *int    `json:"stock_qty,omitempty"`
	Active      bool    `json:"active"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ToProductOptionResponse converts a ProductOption into the wire shape.
// The PriceLabel field is computed (price == 0 → "Offert" — never "0 FCFA").
func ToProductOptionResponse(o *ProductOption) ProductOptionResponse {
	if o == nil {
		return ProductOptionResponse{}
	}
	resp := ProductOptionResponse{
		ID:        o.ID.String(),
		Type:      string(o.Type),
		Name:      o.Name,
		Price:     o.Price,
		PriceLabel: FormatPriceLabel(o.Price),
		StockMode: string(o.StockMode),
		StockQty:  o.StockQty,
		Active:    o.Active,
		CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: o.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if o.ProductID != nil {
		s := o.ProductID.String()
		resp.ProductID = &s
	}
	return resp
}

// FormatPriceLabel returns the human-readable price label.
// 0 → "Offert", otherwise "<formatted> FCFA".
func FormatPriceLabel(price int64) string {
	if price == 0 {
		return "Offert"
	}
	return FormatFCFA(price)
}

// ============================================================================
// Payment config (NOVA v3 — spec section 4)
// ============================================================================

// PaymentConfigMode is the mode of a shop's payment configuration.
type PaymentConfigMode string

const (
	PaymentModeLivraison  PaymentConfigMode = "paiement_livraison"
	PaymentModeAvance     PaymentConfigMode = "paiement_avance"
	PaymentModeIntegral   PaymentConfigMode = "paiement_integral"
)

// IsValidPaymentConfigMode returns true if the given string is a valid payment config mode.
func IsValidPaymentConfigMode(s string) bool {
	switch PaymentConfigMode(s) {
	case PaymentModeLivraison, PaymentModeAvance, PaymentModeIntegral:
		return true
	}
	return false
}

// PaymentConfig is a row in the payment_configs table.
type PaymentConfig struct {
	ShopID        uuid.UUID          `json:"shop_id"        db:"shop_id"`
	Mode          PaymentConfigMode  `json:"mode"           db:"mode"`
	AdvanceAmount *int64             `json:"advance_amount,omitempty" db:"advance_amount"`
	DelayMinutes  int                `json:"delay_minutes"  db:"delay_minutes"`
	WaveLink      *string            `json:"wave_link,omitempty"      db:"wave_link"`
	WaveNumber    *string            `json:"wave_number,omitempty"    db:"wave_number"`
	MoovNumber    *string            `json:"moov_number,omitempty"    db:"moov_number"`
	OrangeNumber  *string            `json:"orange_number,omitempty"  db:"orange_number"`
	MTNNumber     *string            `json:"mtn_number,omitempty"     db:"mtn_number"`
	ActiveMethods []string           `json:"active_methods" db:"active_methods"`
	Active        bool               `json:"active"         db:"active"`
	CreatedAt     time.Time          `json:"created_at"     db:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"     db:"updated_at"`
}

// UpdatePaymentConfigRequest is the body of PUT /api/shops/{shopId}/payment-config.
type UpdatePaymentConfigRequest struct {
	Mode          string   `json:"mode"            validate:"required,oneof=paiement_livraison paiement_avance paiement_integral"`
	AdvanceAmount *int64   `json:"advance_amount,omitempty" validate:"omitempty,min=0"`
	DelayMinutes  *int     `json:"delay_minutes,omitempty"  validate:"omitempty,min=1,max=10080"`
	WaveLink      *string  `json:"wave_link,omitempty"`
	WaveNumber    *string  `json:"wave_number,omitempty"`
	MoovNumber    *string  `json:"moov_number,omitempty"`
	OrangeNumber  *string  `json:"orange_number,omitempty"`
	MTNNumber     *string  `json:"mtn_number,omitempty"`
	ActiveMethods []string `json:"active_methods,omitempty"`
	Active        *bool    `json:"active,omitempty"`
}

// PaymentConfigResponse is the wire shape for a payment config.
type PaymentConfigResponse struct {
	ShopID        string   `json:"shop_id"`
	Mode          string   `json:"mode"`
	AdvanceAmount *int64   `json:"advance_amount,omitempty"`
	DelayMinutes  int      `json:"delay_minutes"`
	WaveLink      *string  `json:"wave_link,omitempty"`
	WaveNumber    *string  `json:"wave_number,omitempty"`
	MoovNumber    *string  `json:"moov_number,omitempty"`
	OrangeNumber  *string  `json:"orange_number,omitempty"`
	MTNNumber     *string  `json:"mtn_number,omitempty"`
	ActiveMethods []string `json:"active_methods"`
	Active        bool     `json:"active"`
}

// ToPaymentConfigResponse converts a PaymentConfig into the wire shape.
func ToPaymentConfigResponse(c *PaymentConfig) PaymentConfigResponse {
	if c == nil {
		return PaymentConfigResponse{ActiveMethods: []string{}}
	}
	methods := c.ActiveMethods
	if methods == nil {
		methods = []string{}
	}
	return PaymentConfigResponse{
		ShopID:        c.ShopID.String(),
		Mode:          string(c.Mode),
		AdvanceAmount: c.AdvanceAmount,
		DelayMinutes:  c.DelayMinutes,
		WaveLink:      c.WaveLink,
		WaveNumber:    c.WaveNumber,
		MoovNumber:    c.MoovNumber,
		OrangeNumber:  c.OrangeNumber,
		MTNNumber:     c.MTNNumber,
		ActiveMethods: methods,
		Active:        c.Active,
	}
}

// ============================================================================
// NOVA v3 order statuses (additive — old ones stay in models.go)
// ============================================================================

const (
	// OrderV3 statuses — new in v3.
	OrderV3EnAttenteConfirmation OrderStatus = "en_attente_confirmation"
	OrderV3EnAttentePaiement     OrderStatus = "en_attente_paiement"
	OrderV3PaiementSignale       OrderStatus = "paiement_signale"
	OrderV3EnCours               OrderStatus = "en_cours"
	OrderV3Prete                 OrderStatus = "prete"
	OrderV3Terminee              OrderStatus = "terminee"
	OrderV3Refusee               OrderStatus = "refusee"
	OrderV3Annulee               OrderStatus = "annulee"
)

// IsV3OrderStatus returns true if the given string is one of the v3 statuses.
func IsV3OrderStatus(s string) bool {
	switch OrderStatus(s) {
	case OrderV3EnAttenteConfirmation, OrderV3EnAttentePaiement, OrderV3PaiementSignale,
		OrderV3EnCours, OrderV3Prete, OrderV3Terminee, OrderV3Refusee, OrderV3Annulee:
		return true
	}
	return false
}

// IsV3RevenueCounted returns true if the given status implies the order's
// revenue is counted (status = en_cours or later, and not cancelled).
func IsV3RevenueCounted(s string) bool {
	switch OrderStatus(s) {
	case OrderV3EnCours, OrderV3Prete, OrderV3Terminee:
		return true
	}
	return false
}

// ============================================================================
// Order v3 DTOs — for the new flow endpoints
// ============================================================================

// SetStockModeRequest is the body of PATCH /api/shops/{shopId}/inventory/{variantId}/mode.
type SetStockModeRequest struct {
	StockMode string `json:"stock_mode" validate:"required,oneof=quantite epuise illimite"`
}

// SetStockModeResponse is the wire shape for the response.
type SetStockModeResponse struct {
	Ok        bool   `json:"ok"`
	StockMode string `json:"stock_mode"`
}

// ============================================================================
// Helpers
// ============================================================================

// FormatFCFA formats a FCFA amount with thousand separators. Kept here for
// the FormatPriceLabel helper. We don't import the frontend's format util —
// we just format with the standard library.
func FormatFCFA(n int64) string {
	// Use simple grouping with non-breaking space (U+00A0).
	// e.g. 286000 → "286 000 FCFA"
	if n < 0 {
		return "-" + FormatFCFA(-n)
	}
	s := []byte{}
	str := []byte(fmtInt(n))
	// Group by 3 from the right.
	nb := len(str)
	for i, c := range str {
		// Add a separator before each group of 3 except the first.
		if i > 0 && (nb-i)%3 == 0 {
			s = append(s, '\u00A0') // non-breaking space
		}
		s = append(s, c)
	}
	s = append(s, []byte(" FCFA")...)
	return string(s)
}

// fmtInt is a tiny itoa helper (avoids strconv import for this file).
func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
