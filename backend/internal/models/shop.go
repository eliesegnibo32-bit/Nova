// Shop DTOs for the HTTP layer.
//
// These structs live in the models package (alongside the auth DTOs) so they
// can be imported by any consumer (handlers, tests, OpenAPI generators, SDK
// codegen). They carry JSON tags for wire serialization and `validate` tags
// per the go-playground/validator convention.
//
// Field semantics follow the cahier des charges (ch. 4.1 Onboarding, ch. 9.3
// Isolation, ch. 7 Abonnements):
//   - shop_id is NEVER a free parameter on shop-scoped requests — it comes
//     from the authenticated session (set by POST /api/shops/switch).
//   - hours is a JSON object: {day: [open, close]} e.g. {"mon": ["08:00","18:00"]}.
//   - categories is a string array (free-form text labels).
//   - accepted_payment_modes is an array of payment_mode enum values.
//   - plan_id is optional on create; defaults to "Essentiel".
package models

import (
        "encoding/json"
        "time"

        "github.com/google/uuid"
)

// CreateShopRequest is the body of POST /api/shops. Only platform admins
// (super_admin, admin) can create shops. The new shop starts in 'draft'
// status and a 'trial' subscription is created for it (14-day trial).
//
// The creating admin specifies the future owner by email — the owner must
// already have an account (registered via POST /api/auth/register). The
// owner is then linked to the shop via shop_members with role='owner'.
type CreateShopRequest struct {
        Name                 string     `json:"name"                  validate:"required,min=2,max=120"`
        Slug                 string     `json:"slug"                  validate:"required,min=2,max=80"`
        OwnerEmail           string     `json:"owner_email"           validate:"required,email"`
        Phone                string     `json:"phone"                 validate:"omitempty"`
        WhatsAppNumber       string     `json:"whatsapp_number"       validate:"omitempty"`
        Address              string     `json:"address"               validate:"omitempty,max=240"`
        Commune              string     `json:"commune"               validate:"omitempty,max=80"`
        Hours                json.RawMessage `json:"hours,omitempty"  validate:"omitempty"`
        Description          string     `json:"description"           validate:"omitempty,max=2000"`
        Categories           []string   `json:"categories,omitempty"`
        SaleConditions       string     `json:"sale_conditions"       validate:"omitempty,max=4000"`
        AcceptedPaymentModes []string   `json:"accepted_payment_modes,omitempty"`
        PlanID               *uuid.UUID `json:"plan_id,omitempty"`
}

// UpdateShopRequest is the body of PATCH /api/shops/{id}. Only the owner of
// the shop (or a platform admin) can update shop info. All fields are
// optional; the handler only updates the fields that are present in the
// request body.
type UpdateShopRequest struct {
        Name                 *string    `json:"name,omitempty"                  validate:"omitempty,min=2,max=120"`
        LogoURL              *string    `json:"logo_url,omitempty"              validate:"omitempty,max=2048"`
        Phone                *string    `json:"phone,omitempty"`
        WhatsAppNumber       *string    `json:"whatsapp_number,omitempty"`
        Address              *string    `json:"address,omitempty"               validate:"omitempty,max=240"`
        Commune              *string    `json:"commune,omitempty"               validate:"omitempty,max=80"`
        Hours                json.RawMessage `json:"hours,omitempty"`
        Description          *string    `json:"description,omitempty"           validate:"omitempty,max=2000"`
        Categories           *[]string  `json:"categories,omitempty"`
        SaleConditions       *string    `json:"sale_conditions,omitempty"       validate:"omitempty,max=4000"`
        AcceptedPaymentModes *[]string  `json:"accepted_payment_modes,omitempty"`
        AISettings           json.RawMessage `json:"ai_settings,omitempty"`
}

// ListShopsParams holds the query parameters for GET /api/shops (admin mode).
// Pagination is page/limit (1-indexed page). Search is a case-insensitive
// substring match on name or slug. Status filters by shop status.
type ListShopsParams struct {
        Page   int    `json:"page"`
        Limit  int    `json:"limit"`
        Search string `json:"search,omitempty"`
        Status string `json:"status,omitempty"`
}

// Normalize fills in sane defaults for missing pagination fields.
func (p *ListShopsParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 200 {
                p.Limit = 20
        }
}

// Offset returns the SQL OFFSET value for the current page/limit.
func (p *ListShopsParams) Offset() int {
        return (p.Page - 1) * p.Limit
}

// ShopResponse is the public shop shape returned by GET / POST / PATCH
// /api/shops. Sensitive internal columns (deleted_at) are omitted.
type ShopResponse struct {
        ID                   string    `json:"id"`
        Name                 string    `json:"name"`
        Slug                 string    `json:"slug"`
        Phone                *string   `json:"phone,omitempty"`
        WhatsAppNumber       *string   `json:"whatsapp_number,omitempty"`
        Address              *string   `json:"address,omitempty"`
        Commune              *string   `json:"commune,omitempty"`
        Hours                json.RawMessage `json:"hours"`
        Description          *string   `json:"description,omitempty"`
        Categories           []string  `json:"categories"`
        SaleConditions       *string   `json:"sale_conditions,omitempty"`
        AcceptedPaymentModes []string  `json:"accepted_payment_modes"`
        AISettings           json.RawMessage `json:"ai_settings"`
        Status               string    `json:"status"`
        LogoURL              *string   `json:"logo_url,omitempty"`
        CreatedAt            string    `json:"created_at"`
        UpdatedAt            string    `json:"updated_at"`
}

// SubscriptionResponse is the public shape for a shop's subscription.
type SubscriptionResponse struct {
        ID            string     `json:"id"`
        ShopID        string     `json:"shop_id"`
        PlanID        string     `json:"plan_id"`
        PlanName      string     `json:"plan_name,omitempty"`
        Status        string     `json:"status"`
        StartedAt     string     `json:"started_at"`
        NextBillingAt *string    `json:"next_billing_at,omitempty"`
        GraceUntil    *string    `json:"grace_until,omitempty"`
        SuspendedAt   *string    `json:"suspended_at,omitempty"`
        TerminatedAt  *string    `json:"terminated_at,omitempty"`
        CreatedAt     string     `json:"created_at"`
        UpdatedAt     string     `json:"updated_at"`
}

// ShopWithSubscriptionResponse is returned by POST /api/shops on success —
// the freshly-created shop plus its trial subscription.
type ShopWithSubscriptionResponse struct {
        Shop         ShopResponse         `json:"shop"`
        Subscription SubscriptionResponse `json:"subscription"`
}

// ValidationResult describes the activation readiness of a shop. Used by
// GET /api/shops/{id}/validation and returned as the error details body of
// POST /api/shops/{id}/activate when criteria aren't met.
type ValidationResult struct {
        CanActivate      bool     `json:"can_activate"`
        MissingCriteria  []string `json:"missing_criteria,omitempty"`
        PublishedProducts int     `json:"published_products"`
        ActiveDeliveryZones int   `json:"active_delivery_zones"`
        HoursSet         bool     `json:"hours_set"`
}

// SwitchShopRequest is the body of POST /api/shops/switch. The shop_id must
// correspond to a shop the user is a member of (the service verifies).
type SwitchShopRequest struct {
        ShopID string `json:"shop_id" validate:"required,uuid"`
}

// SwitchShopResponse is returned on success — the new session cookie has
// already been set by the handler; this body just confirms the active shop.
type SwitchShopResponse struct {
        OK            bool   `json:"ok"`
        ShopID        string `json:"shop_id"`
        ShopName      string `json:"shop_name"`
        ShopSlug      string `json:"shop_slug"`
        ShopStatus    string `json:"shop_status"`
        RoleInShop    string `json:"role_in_shop"`
        SessionCookie string `json:"-"` // written via Set-Cookie by the handler
        CookieMaxAge  int    `json:"-"` // seconds, for Set-Cookie
}

// SuspendShopRequest is the body of POST /api/shops/{id}/suspend (admin only).
type SuspendShopRequest struct {
        Reason string `json:"reason" validate:"required,min=3,max=1000"`
}

// RecordPaymentRequest is the body of POST /api/shops/{id}/subscription/payment
// (admin only). The amount is in FCFA. Mode must be one of payment_mode enum.
// Period start/end are dates (YYYY-MM-DD); end must be >= start.
type RecordPaymentRequest struct {
        Amount      int64  `json:"amount"       validate:"required,min=1"`
        Mode        string `json:"mode"         validate:"required,oneof=cash mobile_money wave orange_money mtn_momo"`
        Reference   string `json:"reference"    validate:"omitempty,max=200"`
        PeriodStart string `json:"period_start" validate:"required"` // YYYY-MM-DD
        PeriodEnd   string `json:"period_end"   validate:"required"` // YYYY-MM-DD
}

// --- Conversion helpers -----------------------------------------------------

// ToShopResponse converts a models.Shop into the wire shape. Times are
// formatted as RFC3339 UTC strings; jsonb columns are passed through as
// json.RawMessage (they're already valid JSON from the DB).
func ToShopResponse(s *Shop) ShopResponse {
        if s == nil {
                return ShopResponse{}
        }
        var hours json.RawMessage
        if len(s.Hours) > 0 {
                hours = json.RawMessage(s.Hours)
        } else {
                hours = json.RawMessage("{}")
        }
        var ai json.RawMessage
        if len(s.AISettings) > 0 {
                ai = json.RawMessage(s.AISettings)
        } else {
                ai = json.RawMessage("{}")
        }
        cats := s.Categories
        if cats == nil {
                cats = []string{}
        }
        modes := s.AcceptedPaymentModes
        if modes == nil {
                modes = []string{}
        }
        return ShopResponse{
                ID:                   s.ID.String(),
                Name:                 s.Name,
                Slug:                 s.Slug,
                Phone:                s.Phone,
                WhatsAppNumber:       s.WhatsAppNumber,
                Address:              s.Address,
                Commune:              s.Commune,
                Hours:                hours,
                Description:          s.Description,
                Categories:           cats,
                SaleConditions:       s.SaleConditions,
                AcceptedPaymentModes: modes,
                AISettings:           ai,
                Status:               string(s.Status),
                LogoURL:              s.LogoURL,
                CreatedAt:            s.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:            s.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// ToSubscriptionResponse converts a models.Subscription into the wire shape.
// The plan name is optional (only populated when the service has joined it in).
func ToSubscriptionResponse(s *Subscription, planName string) SubscriptionResponse {
        if s == nil {
                return SubscriptionResponse{}
        }
        var nb, gu, su, te *string
        if s.NextBillingAt != nil {
                v := s.NextBillingAt.UTC().Format(time.RFC3339)
                nb = &v
        }
        if s.GraceUntil != nil {
                v := s.GraceUntil.UTC().Format(time.RFC3339)
                gu = &v
        }
        if s.SuspendedAt != nil {
                v := s.SuspendedAt.UTC().Format(time.RFC3339)
                su = &v
        }
        if s.TerminatedAt != nil {
                v := s.TerminatedAt.UTC().Format(time.RFC3339)
                te = &v
        }
        return SubscriptionResponse{
                ID:            s.ID.String(),
                ShopID:        s.ShopID.String(),
                PlanID:        s.PlanID.String(),
                PlanName:      planName,
                Status:        string(s.Status),
                StartedAt:     s.StartedAt.UTC().Format(time.RFC3339),
                NextBillingAt: nb,
                GraceUntil:    gu,
                SuspendedAt:   su,
                TerminatedAt:  te,
                CreatedAt:     s.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:     s.UpdatedAt.UTC().Format(time.RFC3339),
        }
}
