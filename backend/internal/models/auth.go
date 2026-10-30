// Auth request/response DTOs for the HTTP layer.
//
// These structs live in the models package (not handlers) so they can be
// imported by any future consumer (tests, OpenAPI generators, SDK codegen).
// They carry JSON tags for wire serialization and `validate` tags per the
// go-playground/validator convention so the handler can short-circuit
// malformed requests before invoking the service layer.
//
// Validation rules mirror the cahier des charges (ch. 11.1):
//   - email: required, RFC-ish format
//   - password: min 8 chars, ≥1 uppercase, ≥1 digit (service also enforces)
//   - full_name: required, non-empty after trim
//   - phone: optional, E.164-ish (we don't enforce strict format — the
//     real validation happens when we send the first WhatsApp message)
package models

// RegisterRequest is the body of POST /api/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"     validate:"required,email"`
	Password string `json:"password"  validate:"required,min=8"`
	FullName string `json:"full_name" validate:"required,min=1"`
	Phone    string `json:"phone"     validate:"omitempty"`
}

// LoginRequest is the body of POST /api/auth/login.
type LoginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// Verify2FARequest is the body of POST /api/auth/2fa/verify.
type Verify2FARequest struct {
	TempToken string `json:"temp_token" validate:"required"`
	Code      string `json:"code"       validate:"required,len=6"`
}

// Confirm2FARequest is the body of POST /api/auth/2fa/confirm and
// /api/auth/2fa/disable.
type Confirm2FARequest struct {
	Code string `json:"code" validate:"required,len=6"`
}

// ChangePasswordRequest is the body of POST /api/auth/change-password.
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

// PasswordResetRequest is the body of POST /api/auth/password-reset/request.
type PasswordResetRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// PasswordResetConfirmRequest is the body of POST /api/auth/password-reset/confirm.
type PasswordResetConfirmRequest struct {
	Token       string `json:"token"       validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

// --- Response shapes --------------------------------------------------------

// UserResponse is the public user shape (no password_hash, no 2FA secret).
// The models.User struct already excludes those fields via json:"-" tags,
// so we can return *models.User directly — but we define a named response
// type here so the OpenAPI schema is stable even if models.User grows
// internal-only fields later.
type UserResponse struct {
	ID               string  `json:"id"`
	Email            string  `json:"email"`
	FullName         string  `json:"full_name"`
	Phone            *string `json:"phone,omitempty"`
	Role             string  `json:"role"`
	TwoFactorEnabled bool    `json:"two_factor_enabled"`
	LastLoginAt      *string `json:"last_login_at,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

// ShopMembershipResponse is the per-shop role info returned in LoginResponse
// and MeResponse.
type ShopMembershipResponse struct {
	ShopID   string `json:"shop_id"`
	ShopName string `json:"shop_name,omitempty"`
	ShopSlug string `json:"shop_slug,omitempty"`
	Role     string `json:"role"`
}

// LoginResponse is the 200 body of POST /api/auth/login when no 2FA is
// required, or POST /api/auth/2fa/verify on success.
type LoginResponse struct {
	User           UserResponse           `json:"user"`
	Shops          []ShopMembershipResponse `json:"shops"`
	CurrentShopID  *string                `json:"current_shop_id,omitempty"`
}

// LoginTwoFactorRequiredResponse is the 200 body of POST /api/auth/login
// when 2FA is enabled for the user.
type LoginTwoFactorRequiredResponse struct {
	RequiresTwoFactor bool   `json:"requires_two_factor"`
	TempToken         string `json:"temp_token"`
}

// MeResponse is the 200 body of GET /api/auth/me.
type MeResponse struct {
	User          UserResponse           `json:"user"`
	Shops         []ShopMembershipResponse `json:"shops"`
	CurrentShopID *string                `json:"current_shop_id,omitempty"`
}

// Setup2FAResponse is the 200 body of POST /api/auth/2fa/setup.
type Setup2FAResponse struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
	QRDataURI       string `json:"qr_data_uri"`
}

// PasswordResetRequestResponse is the 200 body of POST /api/auth/password-reset/request.
// Always returns ok=true so an attacker can't enumerate emails by response.
type PasswordResetRequestResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// DevToken is only populated when the server runs in development
	// environment — it lets the developer paste the reset token into the
	// /password-reset/confirm endpoint without email infrastructure.
	DevToken string `json:"dev_token,omitempty"`
}
