// Authentication handlers — full implementation of NOVA's auth flow.
//
// Each handler:
//  1. Decodes the JSON request body into a models/auth.go DTO.
//  2. Optionally runs go-playground/validator for cheap syntactic checks.
//  3. Calls into the auth service (services.AuthService) for the actual
//     business logic.
//  4. Translates the service result / sentinel error into an HTTP response
//     (status code + JSON envelope + Set-Cookie when applicable).
//
// The handlers never touch the database directly — they go through the
// service. This keeps the SQL confined to the repository layer and makes
// the handler trivially testable with a mock service.
package handlers

import (
        "errors"
        "fmt"
        "log/slog"
        "net/http"
        "strconv"
        "strings"
        "time"

        "github.com/go-chi/chi/v5"
        "github.com/google/uuid"

        "nova-api/internal/api/middleware"
        "nova-api/internal/auth"
        "nova-api/internal/models"
        "nova-api/internal/services"
)

// AuthHandler bundles the auth-related HTTP handlers with their shared
// dependency: the auth service. The service encapsulates all DB access
// (via the repository layer) and business rules (rate limiting, 2FA,
// password strength, audit logging).
type AuthHandler struct {
        service       *services.AuthService
        sessionSecret []byte
        cookieSecure  bool
        isDev         bool
}

// NewAuthHandler returns an AuthHandler bound to the given service. The
// sessionSecret and cookieSecure flag are used to issue Set-Cookie headers
// for login / 2FA-verify / register / logout.
func NewAuthHandler(service *services.AuthService, sessionSecret []byte, cookieSecure, isDev bool) *AuthHandler {
        return &AuthHandler{
                service:       service,
                sessionSecret: sessionSecret,
                cookieSecure:  cookieSecure,
                isDev:         isDev,
        }
}

// Router returns a chi.Router pre-wired with all /api/auth/* routes.
// Routes that require authentication are wrapped in middleware.RequireAuth
// inline; the rest are public.
func (h *AuthHandler) Router() chi.Router {
        r := chi.NewRouter()
        r.Post("/register", h.Register)
        r.Post("/login", h.Login)
        r.Post("/logout", h.Logout)
        r.Post("/2fa/verify", h.Verify2FA)
        r.Post("/password-reset/request", h.RequestPasswordReset)
        r.Post("/password-reset/confirm", h.ConfirmPasswordReset)

        // Authenticated routes.
        r.Group(func(r chi.Router) {
                r.Use(middleware.RequireAuth)
                r.Get("/me", h.Me)
                r.Post("/change-password", h.ChangePassword)
                r.Post("/2fa/setup", h.Setup2FA)
                r.Post("/2fa/confirm", h.Confirm2FA)
                r.Post("/2fa/disable", h.Disable2FA)
        })
        return r
}

// Register handles POST /api/auth/register.
//
//      Request:  models.RegisterRequest
//      Response: 201 + models.UserResponse (no password_hash)
//                + Set-Cookie: nova_session=...
//      Errors:   400 invalid_body / validation_failed
//                409 email_taken
//                422 weak_password / invalid_email
//                500 internal
//
// After registration the user is auto-logged-in (a session cookie is set)
// so the frontend can go straight to the dashboard without an extra /login
// round-trip.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.RegisterRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }

        ip, ua := clientInfo(r)
        user, err := h.service.Register(r.Context(), services.RegisterRequest{
                Email:    req.Email,
                Password: req.Password,
                FullName: req.FullName,
                Phone:    req.Phone,
        }, ip, ua)
        if err != nil {
                writeAuthServiceError(w, err)
                return
        }

        // Auto-login: issue a session cookie so the frontend can skip /login.
        // We sign a fresh session with the user's role and a default expiry.
        sess := auth.Session{
                UserID:    user.ID,
                Role:      string(user.Role),
                ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
        }
        if err := auth.SetSessionCookie(w, sess, h.sessionSecret, h.cookieSecure); err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "session_sign_failed",
                        "Compte créé mais impossible d'émettre la session.")
                return
        }

        writeJSON(w, http.StatusCreated, toUserResponse(user))
}

// Login handles POST /api/auth/login.
//
//      Request:  models.LoginRequest
//      Response (success, no 2FA):    200 + models.LoginResponse + Set-Cookie
//      Response (2FA required):       200 + models.LoginTwoFactorRequiredResponse (no cookie)
//      Errors:   400 invalid_body
//                401 invalid_credentials
//                423 account_locked (+ retry_after in body + Retry-After header)
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.LoginRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }

        ip, ua := clientInfo(r)
        result, err := h.service.Login(r.Context(), req.Email, req.Password, ip, ua)
        if err != nil {
                // Map sentinel errors to HTTP responses.
                switch {
                case errors.Is(err, services.ErrAccountLocked):
                        retry := h.retryAfter(req.Email, ip)
                        w.Header().Set("Retry-After", retryInSeconds(retry))
                        writeJSON(w, http.StatusLocked, map[string]any{
                                "error":       "account_locked",
                                "message":     "Trop de tentatives échouées. Réessayez plus tard.",
                                "retry_after": int(retry.Seconds()),
                        })
                case errors.Is(err, services.ErrInvalidCredentials):
                        writeErrorWithCode(w, http.StatusUnauthorized, "invalid_credentials",
                                "Identifiants invalides.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Une erreur est survenue. Réessayez.")
                }
                return
        }

        if result.RequiresTwoFactor {
                writeJSON(w, http.StatusOK, models.LoginTwoFactorRequiredResponse{
                        RequiresTwoFactor: true,
                        TempToken:         result.TempToken,
                })
                return
        }

        // Success — set the cookie and return the user + shops.
        h.writeSessionCookie(w, result)
        writeJSON(w, http.StatusOK, h.toLoginResponse(result))
}

// Verify2FA handles POST /api/auth/2fa/verify.
//
//      Request:  models.Verify2FARequest
//      Response: 200 + models.LoginResponse + Set-Cookie
//      Errors:   400 invalid_body
//                401 invalid_credentials (bad temp token)
//                401 invalid_two_factor_code
func (h *AuthHandler) Verify2FA(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.Verify2FARequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }

        ip, ua := clientInfo(r)
        result, err := h.service.VerifyTwoFactor(r.Context(), req.TempToken, req.Code, ip, ua)
        if err != nil {
                switch {
                case errors.Is(err, services.ErrInvalidTwoFactorCode):
                        writeErrorWithCode(w, http.StatusUnauthorized, "invalid_two_factor_code",
                                "Code 2FA invalide.")
                case errors.Is(err, services.ErrTwoFactorNotEnabled):
                        writeErrorWithCode(w, http.StatusBadRequest, "two_factor_not_enabled",
                                "La 2FA n'est pas activée pour ce compte.")
                case errors.Is(err, services.ErrInvalidCredentials):
                        writeErrorWithCode(w, http.StatusUnauthorized, "invalid_credentials",
                                "Token invalide ou expiré.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Une erreur est survenue.")
                }
                return
        }

        h.writeSessionCookie(w, result)
        writeJSON(w, http.StatusOK, h.toLoginResponse(result))
}

// Setup2FA handles POST /api/auth/2fa/setup.
//
//      Requires: authenticated session
//      Response: 200 + models.Setup2FAResponse (secret + provisioning URI + QR data URI)
//      Errors:   409 two_factor_already_enabled
//                500 internal
//
// The returned QR data URI can be rendered directly in an <img> tag. The
// secret is shown in plain text so the user can manually enter it in an
// authenticator app if QR scanning isn't available.
func (h *AuthHandler) Setup2FA(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        userID := mustUserID(r)
        ip, ua := clientInfo(r)
        setup, err := h.service.EnableTwoFactor(r.Context(), userID, ip, ua)
        if err != nil {
                switch {
                case errors.Is(err, services.ErrTwoFactorAlreadyEnabled):
                        writeErrorWithCode(w, http.StatusConflict, "two_factor_already_enabled",
                                "La 2FA est déjà activée.")
                case errors.Is(err, services.ErrInvalidCredentials):
                        writeErrorWithCode(w, http.StatusUnauthorized, "invalid_credentials",
                                "Utilisateur introuvable.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Impossible de configurer la 2FA.")
                }
                return
        }
        writeJSON(w, http.StatusOK, models.Setup2FAResponse{
                Secret:          setup.Secret,
                ProvisioningURI: setup.ProvisioningURI,
                QRDataURI:       setup.QRDataURI,
        })
}

// Confirm2FA handles POST /api/auth/2fa/confirm.
//
//      Requires: authenticated session
//      Request:  models.Confirm2FARequest
//      Response: 200 {"ok": true}
//      Errors:   400 invalid_two_factor_code
//                400 two_factor_not_enabled (must call /setup first)
func (h *AuthHandler) Confirm2FA(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.Confirm2FARequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        userID := mustUserID(r)
        ip, ua := clientInfo(r)
        if err := h.service.ConfirmTwoFactor(r.Context(), userID, req.Code, ip, ua); err != nil {
                switch {
                case errors.Is(err, services.ErrInvalidTwoFactorCode):
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_two_factor_code",
                                "Code 2FA invalide.")
                case errors.Is(err, services.ErrTwoFactorNotEnabled):
                        writeErrorWithCode(w, http.StatusBadRequest, "two_factor_not_setup",
                                "Appelez /2fa/setup avant /2fa/confirm.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Impossible d'activer la 2FA.")
                }
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Disable2FA handles POST /api/auth/2fa/disable.
//
//      Requires: authenticated session
//      Request:  models.Confirm2FARequest (reuses the {code} shape)
//      Response: 200 {"ok": true}
//      Errors:   400 invalid_two_factor_code / two_factor_not_enabled
func (h *AuthHandler) Disable2FA(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.Confirm2FARequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        userID := mustUserID(r)
        ip, ua := clientInfo(r)
        if err := h.service.DisableTwoFactor(r.Context(), userID, req.Code, ip, ua); err != nil {
                switch {
                case errors.Is(err, services.ErrInvalidTwoFactorCode):
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_two_factor_code",
                                "Code 2FA invalide.")
                case errors.Is(err, services.ErrTwoFactorNotEnabled):
                        writeErrorWithCode(w, http.StatusBadRequest, "two_factor_not_enabled",
                                "La 2FA n'est pas activée.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Impossible de désactiver la 2FA.")
                }
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Logout handles POST /api/auth/logout. Clears the session cookie and
// records an audit event. Idempotent: calling logout without a session
// cookie is a no-op (still 200).
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
        s, _ := middleware.SessionFromContext(r.Context())
        ip, ua := clientInfo(r)
        if s != nil && h.service != nil {
                // Best-effort audit log — a failure here shouldn't block logout.
                _ = h.service.Logout(r.Context(), s, ip, ua)
        }
        auth.ClearSessionCookie(w)
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Me handles GET /api/auth/me. Returns the user record + shop memberships +
// the active shop_id (if any) from the session cookie.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized",
                        "Authentification requise.")
                return
        }
        uws, err := h.service.GetMe(r.Context(), s.UserID)
        if err != nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized",
                        "Session invalide.")
                return
        }
        resp := models.MeResponse{
                User:  toUserResponse(uws.User),
                Shops: toShopMemberships(uws.Shops),
        }
        if s.ShopID != nil {
                id := s.ShopID.String()
                resp.CurrentShopID = &id
        }
        writeJSON(w, http.StatusOK, resp)
}

// ChangePassword handles POST /api/auth/change-password.
//
//      Requires: authenticated session
//      Request:  models.ChangePasswordRequest
//      Response: 200 {"ok": true}
//      Errors:   401 invalid_credentials (old password wrong)
//                422 weak_password
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.ChangePasswordRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        userID := mustUserID(r)
        ip, ua := clientInfo(r)
        err := h.service.ChangePassword(r.Context(), userID, req.OldPassword, req.NewPassword, ip, ua)
        if err != nil {
                switch {
                case errors.Is(err, services.ErrInvalidCredentials):
                        writeErrorWithCode(w, http.StatusUnauthorized, "invalid_credentials",
                                "Ancien mot de passe incorrect.")
                case errors.Is(err, services.ErrWeakPassword):
                        writeErrorWithCode(w, http.StatusUnprocessableEntity, "weak_password",
                                "Le nouveau mot de passe doit contenir ≥8 caractères, 1 majuscule, 1 chiffre.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Impossible de changer le mot de passe.")
                }
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RequestPasswordReset handles POST /api/auth/password-reset/request.
//
//      Request:  models.PasswordResetRequest
//      Response: 200 models.PasswordResetRequestResponse (ALWAYS 200 — no enumeration)
//
// In development, the response carries a `dev_token` field so the developer
// can paste it into /password-reset/confirm without email infrastructure.
// In production the token is emailed and dev_token is omitted.
func (h *AuthHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.PasswordResetRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        ip, ua := clientInfo(r)
        token, err := h.service.RequestPasswordReset(r.Context(), req.Email, ip, ua)
        if err != nil {
                // Even on internal errors we return 200 to avoid leaking state.
                writeJSON(w, http.StatusOK, models.PasswordResetRequestResponse{
                        OK:      true,
                        Message: "Si cette adresse email existe, un lien de réinitialisation a été envoyé.",
                })
                return
        }
        resp := models.PasswordResetRequestResponse{
                OK:      true,
                Message: "Si cette adresse email existe, un lien de réinitialisation a été envoyé.",
        }
        if h.isDev {
                resp.DevToken = token
        }
        writeJSON(w, http.StatusOK, resp)
}

// ConfirmPasswordReset handles POST /api/auth/password-reset/confirm.
//
//      Request:  models.PasswordResetConfirmRequest
//      Response: 200 {"ok": true}
//      Errors:   400 invalid_reset_token / weak_password
func (h *AuthHandler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.PasswordResetConfirmRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        ip, ua := clientInfo(r)
        err := h.service.ResetPassword(r.Context(), req.Token, req.NewPassword, ip, ua)
        if err != nil {
                switch {
                case errors.Is(err, services.ErrInvalidResetToken):
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_reset_token",
                                "Token invalide ou expiré.")
                case errors.Is(err, services.ErrWeakPassword):
                        writeErrorWithCode(w, http.StatusUnprocessableEntity, "weak_password",
                                "Le mot de passe doit contenir ≥8 caractères, 1 majuscule, 1 chiffre.")
                default:
                        writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                                "Impossible de réinitialiser le mot de passe.")
                }
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- helpers ----------------------------------------------------------------

// serviceUnavailable returns true (and writes a 503) when the auth service
// is nil — this happens when the server boots in degraded mode without a
// DB. The handler can't do anything useful in that state, so we fail fast
// with a clear error rather than panicking on a nil pointer.
func (h *AuthHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service d'authentification n'est pas disponible (base de données injoignable).")
        return true
}

// writeAuthServiceError maps non-sentinel service errors to a 500. The
// sentinel errors are handled by the caller (each handler has a switch).
func writeAuthServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrEmailTaken):
                writeErrorWithCode(w, http.StatusConflict, "email_taken",
                        "Cette adresse email est déjà enregistrée.")
        case errors.Is(err, services.ErrWeakPassword):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "weak_password",
                        "Le mot de passe doit contenir ≥8 caractères, 1 majuscule, 1 chiffre.")
        case errors.Is(err, services.ErrInvalidEmail):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_email",
                        "Adresse email invalide.")
        default:
                slog.Error("auth service error", "error", err, "error_type", fmt.Sprintf("%T", err))
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}

// writeSessionCookie sets the nova_session cookie from a LoginResult. The
// cookie value is computed by the service (signed HMAC) — we just emit it.
// MaxAge is taken from the result so the cookie expires at the same time
// as the payload's ExpiresAt.
func (h *AuthHandler) writeSessionCookie(w http.ResponseWriter, result *services.LoginResult) {
        maxAge := result.CookieMaxAge
        if maxAge <= 0 {
                maxAge = int((7 * 24 * time.Hour).Seconds())
        }
        sameSite := http.SameSiteLaxMode
        if h.cookieSecure {
                sameSite = http.SameSiteNoneMode
        }
        http.SetCookie(w, &http.Cookie{
                Name:     auth.CookieName,
                Value:    result.SessionCookie,
                Path:     "/",
                MaxAge:   maxAge,
                HttpOnly: true,
                Secure:   h.cookieSecure,
                SameSite: sameSite,
        })
}

// toLoginResponse converts a services.LoginResult into the wire shape
// returned by /login and /2fa/verify on success.
func (h *AuthHandler) toLoginResponse(result *services.LoginResult) models.LoginResponse {
        resp := models.LoginResponse{
                User:  toUserResponse(result.User),
                Shops: toShopMemberships(result.Shops),
        }
        return resp
}

// toUserResponse converts a models.User (with sensitive fields) into the
// public wire shape. PasswordHash and TwoFactorSecret are already excluded
// from JSON via `json:"-"` tags on models.User, but we also explicitly
// rebuild a clean response so the wire contract is stable.
func toUserResponse(u *models.User) models.UserResponse {
        if u == nil {
                return models.UserResponse{}
        }
        var lastLogin *string
        if u.LastLoginAt != nil {
                s := u.LastLoginAt.UTC().Format(time.RFC3339)
                lastLogin = &s
        }
        return models.UserResponse{
                ID:               u.ID.String(),
                Email:            u.Email,
                FullName:         u.FullName,
                Phone:            u.Phone,
                Role:             string(u.Role),
                TwoFactorEnabled: u.TwoFactorEnabled,
                LastLoginAt:      lastLogin,
                CreatedAt:        u.CreatedAt.UTC().Format(time.RFC3339),
        }
}

// toShopMemberships converts the service-level ShopMembership slice into
// the wire shape.
func toShopMemberships(in []services.ShopMembership) []models.ShopMembershipResponse {
        if len(in) == 0 {
                return []models.ShopMembershipResponse{}
        }
        out := make([]models.ShopMembershipResponse, 0, len(in))
        for _, m := range in {
                out = append(out, models.ShopMembershipResponse{
                        ShopID:   m.ShopID.String(),
                        ShopName: m.ShopName,
                        ShopSlug: m.ShopSlug,
                        Role:     m.Role,
                })
        }
        return out
}

// mustUserID extracts the user ID from the session context. Panics if no
// session is present — but RequireAuth middleware already guarantees a
// session, so this is safe on all authenticated routes.
func mustUserID(r *http.Request) uuid.UUID {
        s, _ := middleware.SessionFromContext(r.Context())
        if s == nil {
                // Should never happen — RequireAuth runs before this. Defensive
                // default keeps the handler from panicking if middleware is removed.
                return uuid.Nil
        }
        return s.UserID
}

// clientInfo extracts the client IP and User-Agent from the request. The
// IP honors X-Forwarded-For / X-Real-IP so it works behind Caddy / Neon's
// edge. The User-Agent is truncated to 512 bytes to fit in the audit_logs
// column without blowing up the row size.
func clientInfo(r *http.Request) (ip, userAgent string) {
        if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
                if i := strings.Index(xff, ","); i > 0 {
                        ip = strings.TrimSpace(xff[:i])
                } else {
                        ip = strings.TrimSpace(xff)
                }
        }
        if ip == "" {
                if xri := r.Header.Get("X-Real-IP"); xri != "" {
                        ip = strings.TrimSpace(xri)
                }
        }
        if ip == "" {
                // Strip the port if present (RemoteAddr is "host:port").
                if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
                        ip = r.RemoteAddr[:i]
                } else {
                        ip = r.RemoteAddr
                }
        }
        ua := r.UserAgent()
        if len(ua) > 512 {
                ua = ua[:512]
        }
        return ip, ua
}

// retryAfter asks the auth service for the remaining lock duration on the
// (email, ip) identifier. The service owns the rate limiter; we delegate to
// keep the limiter's internals out of the handler layer.
func (h *AuthHandler) retryAfter(email, ip string) time.Duration {
        if h.service == nil {
                return 15 * time.Minute
        }
        d := h.service.RetryAfter(email, ip)
        if d <= 0 {
                // Identifier isn't locked at the limiter level — the lock is
                // DB-side (locked_until). Fall back to the configured lock
                // duration as a conservative hint.
                return 15 * time.Minute
        }
        return d
}

// retryInSeconds formats a duration as a whole-second string for the
// Retry-After HTTP header (RFC 7231 §7.1.3).
func retryInSeconds(d time.Duration) string {
        s := int(d.Seconds())
        if s < 1 {
                s = 1
        }
        return strconv.Itoa(s)
}

// validateStruct is a thin wrapper around go-playground/validator that
// returns the first validation error as a plain string. The handler
// surfaces it as a 422 with the error message; field-level details are
// not exposed (TODO: surface via writeErrorWithDetails).
func validateStruct(v any) error {
        return validate.Struct(v)
}
