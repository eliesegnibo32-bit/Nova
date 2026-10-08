// Auth service — orchestrates all authentication flows for NOVA.
//
// The service is the single entry point for every auth-related business
// operation (register, login, 2FA, password reset, logout). HTTP handlers
// in internal/api/handlers/auth.go call into this service and translate the
// results into JSON responses.
//
// Responsibilities:
//   - Password strength / email format validation (Register, ChangePassword,
//     ResetPassword).
//   - Login rate limiting (delegates to auth.LoginRateLimiter).
//   - User account lock enforcement (delegates to UserRepository.IncrementFailedLogin).
//   - 2FA setup / verify / disable (delegates to auth.ValidateTOTP).
//   - Session cookie issuance (delegates to auth.SignSession).
//   - Audit logging (delegates to repository.AuditRepository).
//
// The service does NOT touch HTTP. It returns plain Go values and sentinel
// errors; the handler layer maps those to status codes and JSON envelopes.
package services

import (
        "context"
        "errors"
        "fmt"
        "regexp"
        "strings"
        "time"

        "github.com/google/uuid"

        "nova-api/internal/auth"
        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// Sentinel errors. Handlers map these to specific HTTP responses (401, 423,
// 400, 409, etc.). Callers should use errors.Is to detect them.
var (
        // ErrInvalidCredentials is returned when the email is unknown or the
        // password does not match. The error message is intentionally generic
        // ("Identifiants invalides") to prevent email enumeration.
        ErrInvalidCredentials = errors.New("invalid credentials")
        // ErrAccountLocked is returned when the account is currently locked
        // (either by the rate limiter or by the DB-side locked_until column).
        ErrAccountLocked = errors.New("account locked")
        // ErrEmailTaken is returned by Register when the email is already used.
        ErrEmailTaken = errors.New("email already registered")
        // ErrWeakPassword is returned when a password fails the strength check.
        ErrWeakPassword = errors.New("password does not meet strength requirements")
        // ErrInvalidEmail is returned when the email fails format validation.
        ErrInvalidEmail = errors.New("invalid email format")
        // ErrTwoFactorRequired is returned by Login when the user has 2FA
        // enabled — the caller should switch to the 2FA verify flow. The
        // associated temp token is in the LoginResult, not the error.
        ErrTwoFactorRequired = errors.New("two-factor authentication required")
        // ErrInvalidTwoFactorCode is returned when the TOTP code is wrong.
        ErrInvalidTwoFactorCode = errors.New("invalid two-factor code")
        // ErrTwoFactorNotEnabled is returned by DisableTwoFactor when the user
        // doesn't have 2FA on (and so can't disable it).
        ErrTwoFactorNotEnabled = errors.New("two-factor not enabled")
        // ErrInvalidResetToken is returned by ResetPassword when the token is
        // bad, expired, or has the wrong purpose.
        ErrInvalidResetToken = errors.New("invalid or expired reset token")
        // ErrTwoFactorAlreadyEnabled is returned by EnableTwoFactor when the
        // user already has 2FA on (the setup endpoint should still return the
        // current provisioning info if needed; here we just refuse to spin a
        // new secret).
        ErrTwoFactorAlreadyEnabled = errors.New("two-factor already enabled")
)

// emailRegex is a pragmatic email format check (not strictly RFC 5322, but
// sufficient for input validation). The DB UNIQUE constraint is the real
// guarantee against duplicates.
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// TwoFactorSetup is returned by EnableTwoFactor so the frontend can render
// a QR code and let the user type in the 6-digit code to confirm.
type TwoFactorSetup struct {
        Secret           string `json:"secret"`
        ProvisioningURI  string `json:"provisioning_uri"`
        QRDataURI        string `json:"qr_data_uri"`
}

// ShopMembership is the per-shop role info returned in LoginResult and
// UserWithShops. It's a subset of models.ShopMember + the shop's basic
// display fields (so the frontend can render a shop switcher without an
// extra round-trip).
type ShopMembership struct {
        ShopID   uuid.UUID `json:"shop_id"`
        ShopName string    `json:"shop_name"`
        ShopSlug string    `json:"shop_slug"`
        Role     string    `json:"role"`
}

// UserWithShops is the shape returned by GetMe — the user record (without
// password_hash / two_factor_secret) plus the list of shops they can
// access.
type UserWithShops struct {
        User  *models.User      `json:"user"`
        Shops []ShopMembership  `json:"shops"`
}

// LoginResult is what Login and VerifyTwoFactor return on success or
// pending-2FA. The handler inspects RequiresTwoFactor to decide between
// the two response shapes:
//   - RequiresTwoFactor == true: respond with {requires_two_factor, temp_token}
//   - RequiresTwoFactor == false: respond with {user, shops} + Set-Cookie
type LoginResult struct {
        RequiresTwoFactor bool             `json:"requires_two_factor,omitempty"`
        TempToken         string           `json:"temp_token,omitempty"`
        SessionCookie     string           `json:"-"`               // handler writes this via Set-Cookie
        CookieMaxAge      int              `json:"-"`               // seconds, for the Set-Cookie header
        User              *models.User     `json:"user,omitempty"`
        Shops             []ShopMembership `json:"shops,omitempty"`
}

// AuthService is the auth business-logic layer.
type AuthService struct {
        userRepo             *repository.UserRepository
        memberRepo           *repository.ShopMemberRepository
        auditRepo            *repository.AuditRepository
        rateLimiter          *auth.LoginRateLimiter
        sessionSecret        []byte
        sessionDuration      time.Duration // owners / employees
        adminSessionDuration time.Duration // super_admin / admin
        loginMaxAttempts     int
        loginLockDuration    time.Duration
}

// NewAuthService constructs an AuthService. The sessionSecret must be the
// same value used by the Auth middleware (auth.Auth(secret)) so cookies
// issued here are readable by the middleware.
func NewAuthService(
        userRepo *repository.UserRepository,
        memberRepo *repository.ShopMemberRepository,
        auditRepo *repository.AuditRepository,
        rateLimiter *auth.LoginRateLimiter,
        sessionSecret []byte,
        sessionDuration, adminSessionDuration time.Duration,
        loginMaxAttempts int,
        loginLockDuration time.Duration,
) *AuthService {
        if sessionDuration <= 0 {
                sessionDuration = 7 * 24 * time.Hour
        }
        if adminSessionDuration <= 0 {
                adminSessionDuration = 24 * time.Hour
        }
        if loginMaxAttempts <= 0 {
                loginMaxAttempts = 5
        }
        if loginLockDuration <= 0 {
                loginLockDuration = 15 * time.Minute
        }
        return &AuthService{
                userRepo:             userRepo,
                memberRepo:           memberRepo,
                auditRepo:            auditRepo,
                rateLimiter:          rateLimiter,
                sessionSecret:        sessionSecret,
                sessionDuration:      sessionDuration,
                adminSessionDuration: adminSessionDuration,
                loginMaxAttempts:     loginMaxAttempts,
                loginLockDuration:    loginLockDuration,
        }
}

// RegisterRequest is the input to Register. Email/Password/FullName are
// required; Phone is optional.
type RegisterRequest struct {
        Email    string
        Password string
        FullName string
        Phone    string
}

// Register creates a new owner account. The new user has no shops yet —
// the frontend should call POST /api/shops to create the first one. The
// caller is responsible for issuing the session cookie from the returned
// user; this method does not log the user in (returning the user lets the
// handler decide whether to auto-login or require an explicit login step).
//
// Idempotency: if the email is already taken, returns ErrEmailTaken. The
// caller should map this to 409 Conflict (not 422) so the frontend can
// prompt "login instead".
func (s *AuthService) Register(ctx context.Context, req RegisterRequest, ip, userAgent string) (*models.User, error) {
        email := normalizeEmail(req.Email)
        if !emailRegex.MatchString(email) {
                return nil, ErrInvalidEmail
        }
        if err := validatePassword(req.Password); err != nil {
                return nil, err
        }
        if strings.TrimSpace(req.FullName) == "" {
                return nil, errors.New("full_name is required")
        }

        hash, err := auth.HashPassword(req.Password)
        if err != nil {
                return nil, fmt.Errorf("hash password: %w", err)
        }

        user, err := s.userRepo.Create(ctx, email, hash, strings.TrimSpace(req.FullName), req.Phone, models.RoleOwner)
        if err != nil {
                // Detect Postgres unique_violation (code 23505). pgx wraps the
                // *pgconn.PgError; we check the error message for the constraint
                // name to avoid importing pgconn here (keeps the service layer
                // driver-agnostic). For a more robust check, callers can inspect
                // the underlying error themselves.
                if isUniqueViolation(err) {
                        return nil, ErrEmailTaken
                }
                return nil, fmt.Errorf("create user: %w", err)
        }

        // Audit log — register is a platform-level action (no shop yet).
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     nil,
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.register",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return user, nil
}

// Login validates credentials and either:
//   - Returns a LoginResult with User + Shops + SessionCookie (success, no 2FA).
//   - Returns a LoginResult with RequiresTwoFactor=true + TempToken (success, 2FA on).
//   - Returns an error (ErrInvalidCredentials, ErrAccountLocked).
//
// On ANY failure (unknown email, bad password, locked), the rate limiter is
// incremented to slow down brute-force attempts. The error returned is
// always ErrInvalidCredentials or ErrAccountLocked — never "user not found"
// — to prevent email enumeration.
func (s *AuthService) Login(ctx context.Context, email, password, ip, userAgent string) (*LoginResult, error) {
        email = normalizeEmail(email)
        identifier := auth.LoginIdentifier(email, ip)

        // 1. Rate limit check.
        if s.rateLimiter.IsLocked(identifier) {
                retry := s.rateLimiter.RetryAfter(identifier)
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ActorID:   nil,
                        Action:    "auth.login.locked",
                        ObjectType: "user",
                        IPAddress: ip,
                        UserAgent: userAgent,
                })
                _ = retry // unused; handler can compute from header
                return nil, ErrAccountLocked
        }

        // 2. Lookup user.
        user, err := s.userRepo.GetByEmail(ctx, email)
        if err != nil {
                // Unknown email — still record a failure so the attacker can't
                // distinguish "email unknown" from "wrong password" by timing.
                s.rateLimiter.RecordFailure(identifier)
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ActorID:    nil,
                        Action:     "auth.login.failed",
                        ObjectType: "user",
                        IPAddress:  ip,
                        UserAgent:  userAgent,
                })
                return nil, ErrInvalidCredentials
        }

        // 3. Account lock check (DB-side).
        if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
                s.rateLimiter.RecordFailure(identifier)
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ActorID:    &user.ID,
                        ActorRole:  string(user.Role),
                        Action:     "auth.login.locked",
                        ObjectType: "user",
                        ObjectID:   &user.ID,
                        IPAddress:  ip,
                        UserAgent:  userAgent,
                })
                return nil, ErrAccountLocked
        }

        // 4. Verify password.
        if user.PasswordHash == nil {
                // Should never happen for a real user — they always have a password.
                s.rateLimiter.RecordFailure(identifier)
                return nil, ErrInvalidCredentials
        }
        if err := auth.CheckPassword(*user.PasswordHash, password); err != nil {
                s.rateLimiter.RecordFailure(identifier)
                _ = s.userRepo.IncrementFailedLogin(ctx, user.ID, s.loginMaxAttempts, s.loginLockDuration)
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ActorID:    &user.ID,
                        ActorRole:  string(user.Role),
                        Action:     "auth.login.failed",
                        ObjectType: "user",
                        ObjectID:   &user.ID,
                        IPAddress:  ip,
                        UserAgent:  userAgent,
                })
                // If the rate limiter just locked the identifier, return AccountLocked
                // so the handler emits 423 with retry_after.
                if s.rateLimiter.IsLocked(identifier) {
                        return nil, ErrAccountLocked
                }
                return nil, ErrInvalidCredentials
        }

        // 5. Success path. Reset failure counters.
        s.rateLimiter.RecordSuccess(identifier)
        _ = s.userRepo.UpdateLastLogin(ctx, user.ID)

        // 6. 2FA check.
        if user.TwoFactorEnabled && user.TwoFactorSecret != nil && *user.TwoFactorSecret != "" {
                tempToken, err := IssueTempToken(user.ID, PurposeTwoFactor, s.sessionSecret, 5*time.Minute)
                if err != nil {
                        return nil, fmt.Errorf("issue 2fa temp token: %w", err)
                }
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ActorID:    &user.ID,
                        ActorRole:  string(user.Role),
                        Action:     "auth.login.2fa_required",
                        ObjectType: "user",
                        ObjectID:   &user.ID,
                        IPAddress:  ip,
                        UserAgent:  userAgent,
                })
                return &LoginResult{
                        RequiresTwoFactor: true,
                        TempToken:         tempToken,
                }, nil
        }

        // 7. No 2FA — issue session cookie and return.
        result, err := s.completeLogin(ctx, user, ip, userAgent)
        if err != nil {
                return nil, err
        }
        return result, nil
}

// VerifyTwoFactor completes a 2FA login: validate the temp token, validate
// the TOTP code, then issue the session cookie.
func (s *AuthService) VerifyTwoFactor(ctx context.Context, tempToken, code, ip, userAgent string) (*LoginResult, error) {
        userID, err := VerifyTempToken(tempToken, s.sessionSecret, PurposeTwoFactor)
        if err != nil {
                return nil, fmt.Errorf("%w: %v", ErrInvalidCredentials, err)
        }
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return nil, ErrInvalidCredentials
        }
        if !user.TwoFactorEnabled || user.TwoFactorSecret == nil || *user.TwoFactorSecret == "" {
                return nil, ErrTwoFactorNotEnabled
        }
        if !auth.ValidateTOTP(*user.TwoFactorSecret, code, 1) {
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ActorID:    &user.ID,
                        ActorRole:  string(user.Role),
                        Action:     "auth.login.2fa_failed",
                        ObjectType: "user",
                        ObjectID:   &user.ID,
                        IPAddress:  ip,
                        UserAgent:  userAgent,
                })
                return nil, ErrInvalidTwoFactorCode
        }

        result, err := s.completeLogin(ctx, user, ip, userAgent)
        if err != nil {
                return nil, err
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.login.2fa_success",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return result, nil
}

// completeLogin is the shared tail of Login (no 2FA) and VerifyTwoFactor:
// fetch the user's shops, sign the session cookie, audit the success, and
// return the LoginResult.
func (s *AuthService) completeLogin(ctx context.Context, user *models.User, ip, userAgent string) (*LoginResult, error) {
        shops, err := s.fetchShops(ctx, user.ID)
        if err != nil {
                // Non-fatal: log and proceed with an empty shop list. The user can
                // still operate; they'll just have to create a shop.
                shops = nil
        }

        // Pick session duration by role.
        ttl := s.sessionDuration
        if user.Role == models.RoleSuperAdmin || user.Role == models.RoleAdmin {
                ttl = s.adminSessionDuration
        }
        expiresAt := time.Now().Add(ttl)

        // Default role for the session cookie is the user's platform role.
        // For owners/employees without an active shop, the cookie carries
        // the user's role; once they activate a shop (POST /api/shops/{id}/activate),
        // the cookie is reissued with the per-shop role. The current implementation
        // uses the platform role here — the per-shop role override happens in
        // the shop activation handler (Task 5).
        sess := auth.Session{
                UserID:    user.ID,
                Role:      string(user.Role),
                ExpiresAt: expiresAt,
        }
        cookie, err := auth.SignSession(sess, s.sessionSecret)
        if err != nil {
                return nil, fmt.Errorf("sign session: %w", err)
        }

        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.login.success",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return &LoginResult{
                User:          user,
                Shops:         shops,
                SessionCookie: cookie,
                CookieMaxAge:  int(ttl.Seconds()),
        }, nil
}

// fetchShops returns the user's shop memberships enriched with the shop's
// display fields. It does a second query for shop names to avoid a JOIN in
// the repository (keeps shop_members.go focused on shop_members).
func (s *AuthService) fetchShops(ctx context.Context, userID uuid.UUID) ([]ShopMembership, error) {
        members, err := s.memberRepo.GetByUserID(ctx, userID)
        if err != nil {
                return nil, err
        }
        out := make([]ShopMembership, 0, len(members))
        for _, m := range members {
                // We don't have the shop name/slug cached on the ShopMember row.
                // For now we expose the IDs only — the frontend will call
                // GET /api/shops to get full details. This keeps the auth path
                // fast and avoids depending on the shops repository here.
                out = append(out, ShopMembership{
                        ShopID: m.ShopID,
                        Role:   string(m.Role),
                })
        }
        return out, nil
}

// EnableTwoFactor generates a new TOTP secret for the user, stores it (but
// does NOT enable it yet), and returns the provisioning URI + QR code so
// the frontend can render the setup screen. The user must then call
// ConfirmTwoFactor with a valid 6-digit code to activate 2FA.
//
// If the user already has 2FA enabled, returns ErrTwoFactorAlreadyEnabled
// — the frontend should redirect to a "disable 2FA" flow instead.
func (s *AuthService) EnableTwoFactor(ctx context.Context, userID uuid.UUID, ip, userAgent string) (*TwoFactorSetup, error) {
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return nil, ErrInvalidCredentials
        }
        if user.TwoFactorEnabled {
                return nil, ErrTwoFactorAlreadyEnabled
        }
        secret, err := auth.GenerateTOTPSecret()
        if err != nil {
                return nil, fmt.Errorf("generate totp secret: %w", err)
        }
        // Store the secret provisionally (enabled=false). The user must confirm
        // with a code to flip enabled to true.
        if err := s.userRepo.SetTwoFactor(ctx, userID, secret, false); err != nil {
                return nil, fmt.Errorf("store totp secret: %w", err)
        }

        uri := auth.TOTPProvisioningURI(user.Email, "NOVA", secret)
        qr, err := auth.GenerateQRCodePNG(uri)
        if err != nil {
                return nil, fmt.Errorf("generate qr: %w", err)
        }

        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.2fa.setup",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return &TwoFactorSetup{
                Secret:          secret,
                ProvisioningURI: uri,
                QRDataURI:       qr,
        }, nil
}

// ConfirmTwoFactor validates the TOTP code against the user's stored secret
// and, on success, sets two_factor_enabled = true. The user must have
// called EnableTwoFactor first (which stores the secret provisionally).
func (s *AuthService) ConfirmTwoFactor(ctx context.Context, userID uuid.UUID, code, ip, userAgent string) error {
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return ErrInvalidCredentials
        }
        if user.TwoFactorSecret == nil || *user.TwoFactorSecret == "" {
                return ErrTwoFactorNotEnabled
        }
        if !auth.ValidateTOTP(*user.TwoFactorSecret, code, 1) {
                return ErrInvalidTwoFactorCode
        }
        if err := s.userRepo.SetTwoFactor(ctx, userID, *user.TwoFactorSecret, true); err != nil {
                return fmt.Errorf("enable two_factor: %w", err)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.2fa.enable",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// DisableTwoFactor validates the TOTP code first (proof the attacker didn't
// steal the session and lock the user out), then clears the secret and the
// enabled flag. Returns ErrTwoFactorNotEnabled if 2FA isn't on.
func (s *AuthService) DisableTwoFactor(ctx context.Context, userID uuid.UUID, code, ip, userAgent string) error {
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return ErrInvalidCredentials
        }
        if !user.TwoFactorEnabled || user.TwoFactorSecret == nil || *user.TwoFactorSecret == "" {
                return ErrTwoFactorNotEnabled
        }
        if !auth.ValidateTOTP(*user.TwoFactorSecret, code, 1) {
                return ErrInvalidTwoFactorCode
        }
        if err := s.userRepo.SetTwoFactor(ctx, userID, "", false); err != nil {
                return fmt.Errorf("disable two_factor: %w", err)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.2fa.disable",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// Logout logs the event to audit_logs. The session cookie itself is
// stateless (HMAC-signed), so there is no server-side session store to
// invalidate; the handler clears the cookie and that's enough to log the
// user out from this browser. For full revocation across devices, a
// session-nonce table would be needed (TODO production hardening).
func (s *AuthService) Logout(ctx context.Context, sess *auth.Session, ip, userAgent string) error {
        if sess == nil {
                return nil
        }
        return s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &sess.UserID,
                ActorRole:  sess.Role,
                Action:     "auth.logout",
                ObjectType: "user",
                ObjectID:   &sess.UserID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
}

// GetMe returns the user record (without password_hash / 2FA secret) plus
// the list of shops they can access.
func (s *AuthService) GetMe(ctx context.Context, userID uuid.UUID) (*UserWithShops, error) {
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return nil, ErrInvalidCredentials
        }
        shops, err := s.fetchShops(ctx, userID)
        if err != nil {
                shops = nil
        }
        return &UserWithShops{User: user, Shops: shops}, nil
}

// RetryAfter returns the remaining lock duration for the (email, ip)
// identifier, or 0 if not locked. Used by the login handler to populate
// the `Retry-After` HTTP header and the `retry_after` JSON field on a 423
// Locked response.
func (s *AuthService) RetryAfter(email, ip string) time.Duration {
        if s.rateLimiter == nil {
                return 0
        }
        return s.rateLimiter.RetryAfter(auth.LoginIdentifier(email, ip))
}

// ChangePassword verifies the old password, hashes the new one, and writes
// it. The user is NOT logged out — they keep their current session. For
// higher security, callers can clear the cookie after this and force a
// re-login; we don't do that here to match the UX expectation that
// changing your password doesn't kick you out.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword, ip, userAgent string) error {
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return ErrInvalidCredentials
        }
        if user.PasswordHash == nil {
                return ErrInvalidCredentials
        }
        if err := auth.CheckPassword(*user.PasswordHash, oldPassword); err != nil {
                return ErrInvalidCredentials
        }
        if err := validatePassword(newPassword); err != nil {
                return err
        }
        hash, err := auth.HashPassword(newPassword)
        if err != nil {
                return fmt.Errorf("hash new password: %w", err)
        }
        if err := s.userRepo.UpdatePassword(ctx, userID, hash); err != nil {
                return fmt.Errorf("update password: %w", err)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.password.change",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// RequestPasswordReset generates a signed reset token and returns it. In
// production, the token would be emailed to the user; here we return it so
// the developer can paste it into the /password-reset/confirm endpoint
// during testing.
//
// Idempotency: if the email is unknown, the method returns a freshly
// generated (random, unsigned) UUID-shaped token but does NOT persist
// anything. The caller (handler) always returns 200 OK to prevent email
// enumeration. The returned token is only meaningful if the email exists.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email, ip, userAgent string) (string, error) {
        email = normalizeEmail(email)
        user, err := s.userRepo.GetByEmail(ctx, email)
        if err != nil {
                // Unknown email — return a random UUID-shaped string so the response
                // shape is identical to the known-email case. The caller discards
                // it (no email is sent in dev either way).
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        Action:     "auth.password_reset.request_unknown",
                        ObjectType: "user",
                        IPAddress:  ip,
                        UserAgent:  userAgent,
                })
                return uuid.NewString(), nil
        }

        token, err := IssueTempToken(user.ID, PurposePasswordReset, s.sessionSecret, 1*time.Hour)
        if err != nil {
                return "", fmt.Errorf("issue reset token: %w", err)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.password_reset.request",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return token, nil
}

// ResetPassword verifies the reset token, hashes the new password, and
// writes it. After this, all future logins must use the new password; the
// user is NOT auto-logged-in here (they must call /login).
func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword, ip, userAgent string) error {
        userID, err := VerifyTempToken(token, s.sessionSecret, PurposePasswordReset)
        if err != nil {
                return ErrInvalidResetToken
        }
        if err := validatePassword(newPassword); err != nil {
                return err
        }
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return ErrInvalidResetToken
        }
        hash, err := auth.HashPassword(newPassword)
        if err != nil {
                return fmt.Errorf("hash password: %w", err)
        }
        if err := s.userRepo.UpdatePassword(ctx, userID, hash); err != nil {
                return fmt.Errorf("update password: %w", err)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ActorID:    &user.ID,
                ActorRole:  string(user.Role),
                Action:     "auth.password_reset.confirm",
                ObjectType: "user",
                ObjectID:   &user.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// --- helpers ----------------------------------------------------------------

// normalizeEmail lowercases and trims the email. The DB stores emails
// verbatim (case-sensitive UNIQUE) — by normalizing at every entry point
// we ensure "Foo@Bar.com" and "foo@bar.com" always hit the same row.
func normalizeEmail(s string) string {
        return strings.ToLower(strings.TrimSpace(s))
}

// validatePassword enforces the policy from the cahier des charges
// (ch. 11.1): minimum 8 characters, at least one uppercase letter, at
// least one digit. Returns ErrWeakPassword on failure.
func validatePassword(p string) error {
        if len(p) < 8 {
                return ErrWeakPassword
        }
        var hasUpper, hasDigit bool
        for _, r := range p {
                switch {
                case r >= 'A' && r <= 'Z':
                        hasUpper = true
                case r >= '0' && r <= '9':
                        hasDigit = true
                }
        }
        if !hasUpper || !hasDigit {
                return ErrWeakPassword
        }
        return nil
}

// isUniqueViolation returns true if err is a Postgres unique_violation
// (SQLSTATE 23505). We use a string check on the error message to avoid
// importing pgconn (keeps the service layer driver-agnostic; a future
// switch to pgx-only error inspection can refine this).
func isUniqueViolation(err error) bool {
        if err == nil {
                return false
        }
        msg := err.Error()
        // pgx v5 wraps *pgconn.PgError and includes the SQLSTATE in the error
        // string. We check for both the code and the constraint name pattern.
        return strings.Contains(msg, "23505") || strings.Contains(msg, "users_email_key")
}
