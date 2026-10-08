// Shop handlers — full implementation of NOVA's shop endpoints (Task 5).
//
// Each handler:
//  1. Decodes the JSON request body into a models/shop.go DTO.
//  2. Optionally runs go-playground/validator for cheap syntactic checks.
//  3. Calls into the shop service (services.ShopService) for the actual
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

        "github.com/go-chi/chi/v5"
        "github.com/google/uuid"

        "nova-api/internal/api/middleware"
        "nova-api/internal/auth"
        "nova-api/internal/models"
        "nova-api/internal/services"
)

// ShopHandler bundles the shop-related HTTP handlers with their shared
// dependency: the shop service.
type ShopHandler struct {
        service       *services.ShopService
        sessionSecret []byte
        cookieSecure  bool
        isDev         bool
}

// NewShopHandler returns a ShopHandler bound to the given service. The
// sessionSecret and cookieSecure flag are used to issue Set-Cookie headers
// for /api/shops/switch.
func NewShopHandler(service *services.ShopService, sessionSecret []byte, cookieSecure, isDev bool) *ShopHandler {
        return &ShopHandler{
                service:       service,
                sessionSecret: sessionSecret,
                cookieSecure:  cookieSecure,
                isDev:         isDev,
        }
}

// Router returns a chi.Router pre-wired with the non-shop-scoped routes
// (list, create, switch). The shop-scoped routes (/{id}/*) are registered
// separately via RegisterShopScopedRoutes inside the unified {shopId}
// route group in internal/api/router.go — this avoids chi routing conflicts
// between the {id} parameter and the {shopId} regex-constrained parameter.
//
// All routes require an authenticated session (middleware.RequireAuth is
// applied here). Role checks happen in the service layer.
func (h *ShopHandler) Router() chi.Router {
        r := chi.NewRouter()
        r.Use(middleware.RequireAuth)

        r.Get("/", h.List)           // GET /api/shops
        r.Post("/", h.Create)        // POST /api/shops
        r.Post("/switch", h.Switch)  // POST /api/shops/switch
        return r
}

// Create handles POST /api/shops.
//
//      Requires: authenticated session + platform admin role.
//      Request:  models.CreateShopRequest
//      Response: 201 + models.ShopWithSubscriptionResponse
//      Errors:   400 invalid_body / validation_failed
//                403 forbidden (not admin)
//                404 owner_not_found / plan_not_found
//                409 slug_taken
//                500 internal
func (h *ShopHandler) Create(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.CreateShopRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }

        s := mustSession(r)
        ip, ua := clientInfo(r)
        result, err := h.service.Create(r.Context(), s.UserID, s.Role, req, ip, ua)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }

        // Build the response.
        resp := models.ShopWithSubscriptionResponse{
                Shop:         models.ToShopResponse(result.Shop),
                Subscription: models.ToSubscriptionResponse(result.Subscription, result.PlanName),
        }
        writeJSON(w, http.StatusCreated, resp)
}

// List handles GET /api/shops.
//
//      - Platform admins see all shops (paginated). Query params: page, limit, search, status.
//      - Owners / employees see only their shops (paginated params ignored).
func (h *ShopHandler) List(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        s := mustSession(r)

        // If the user is a platform admin and didn't ask for "mine", return
        // the global paginated list. Otherwise, return their shops.
        // We expose ?mine=1 to force "my shops" even for admins.
        if services.IsPlatformAdmin(s.Role) && r.URL.Query().Get("mine") != "1" {
                params := models.ListShopsParams{
                        Page:   atoiOr(r.URL.Query().Get("page"), 1),
                        Limit:  atoiOr(r.URL.Query().Get("limit"), 20),
                        Search: r.URL.Query().Get("search"),
                        Status: r.URL.Query().Get("status"),
                }
                shops, total, err := h.service.List(r.Context(), s.Role, params)
                if err != nil {
                        writeShopServiceError(w, err)
                        return
                }
                resp := map[string]any{
                        "shops":     toShopResponses(shops),
                        "total":     total,
                        "page":      params.Page,
                        "limit":     params.Limit,
                }
                writeJSON(w, http.StatusOK, resp)
                return
        }

        // Owner/employee view: only their shops.
        shops, err := h.service.ListMine(r.Context(), s.UserID)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "shops": toShopResponses(shops),
                "total": len(shops),
        })
}

// Get handles GET /api/shops/{id}.
//
//      Requires: authenticated session + member of the shop OR platform admin.
//      Response: 200 + models.ShopResponse
//      Errors:   400 invalid_id
//                403 not_shop_member
//                404 shop_not_found
func (h *ShopHandler) Get(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        s := mustSession(r)
        shop, err := h.service.Get(r.Context(), s.UserID, s.Role, shopID)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToShopResponse(shop))
}

// Update handles PATCH /api/shops/{id}.
//
//      Requires: authenticated session + owner of the shop OR platform admin.
//      Request:  models.UpdateShopRequest
//      Response: 200 + models.ShopResponse
//      Errors:   400 invalid_body / validation_failed / invalid_id
//                403 forbidden
//                404 shop_not_found
func (h *ShopHandler) Update(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        var req models.UpdateShopRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        shop, err := h.service.Update(r.Context(), s.UserID, s.Role, shopID, req, ip, ua)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToShopResponse(shop))
}

// Activate handles POST /api/shops/{id}/activate.
//
//      Requires: authenticated session + owner of the shop OR platform admin.
//      Response: 200 + models.ShopResponse (status='active')
//      Errors:   403 forbidden
//                404 shop_not_found
//                422 activation_criteria_not_met (+ missing_criteria in body)
func (h *ShopHandler) Activate(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        shop, err := h.service.Activate(r.Context(), s.UserID, s.Role, shopID, ip, ua)
        if err != nil {
                // If the error carries a ValidationResult, surface it as 422 with
                // the missing-criteria list in the body.
                var ae *services.ActivationError
                if errors.As(err, &ae) && ae.Result != nil {
                        writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
                                "error":            "activation_criteria_not_met",
                                "message":          "L'activation nécessite au moins un produit publié, une zone de livraison active et des horaires renseignés.",
                                "validation":       ae.Result,
                                "missing_criteria": ae.Result.MissingCriteria,
                        })
                        return
                }
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToShopResponse(shop))
}

// Suspend handles POST /api/shops/{id}/suspend. Admin only.
//
//      Request:  models.SuspendShopRequest
//      Response: 200 + models.ShopResponse (status='suspended')
func (h *ShopHandler) Suspend(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        var req models.SuspendShopRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        shop, err := h.service.Suspend(r.Context(), s.UserID, s.Role, shopID, req.Reason, ip, ua)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToShopResponse(shop))
}

// Reactivate handles POST /api/shops/{id}/reactivate. Admin only.
func (h *ShopHandler) Reactivate(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        shop, err := h.service.Reactivate(r.Context(), s.UserID, s.Role, shopID, ip, ua)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToShopResponse(shop))
}

// Validation handles GET /api/shops/{id}/validation.
//
// Returns the activation readiness state — useful for the onboarding UI to
// show a checklist of what's still missing before the shop can be activated.
//
//      Requires: authenticated session + member of the shop OR platform admin.
//      Response: 200 + models.ValidationResult
func (h *ShopHandler) Validation(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        s := mustSession(r)
        // Permission: must be a member or admin. The service.ValidateActivation
        // doesn't do this check (it assumes the caller has verified) so we do
        // it here via the Get call.
        if _, err := h.service.Get(r.Context(), s.UserID, s.Role, shopID); err != nil {
                writeShopServiceError(w, err)
                return
        }
        vr, err := h.service.ValidateActivation(r.Context(), s.UserID, shopID)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, vr)
}

// Switch handles POST /api/shops/switch.
//
//      Request:  models.SwitchShopRequest
//      Response: 200 + models.SwitchShopResponse + Set-Cookie: nova_session=...
//      Errors:   400 invalid_body / validation_failed
//                403 not_shop_member / shop_suspended
//                404 shop_not_found
func (h *ShopHandler) Switch(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        var req models.SwitchShopRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        shopID, err := uuid.Parse(req.ShopID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_shop_id",
                        "shop_id doit être un UUID valide.")
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        result, err := h.service.SwitchShop(r.Context(), s.UserID, shopID, ip, ua)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }

        // Set the new session cookie.
        maxAge := result.CookieMaxAge
        if maxAge <= 0 {
                maxAge = int((7 * 24 * 60 * 60))
        }
        sameSite := http.SameSiteLaxMode
        if h.cookieSecure {
                sameSite = http.SameSiteNoneMode
        }
        http.SetCookie(w, &http.Cookie{
                Name:     auth.CookieName,
                Value:    result.Cookie,
                Path:     "/",
                MaxAge:   maxAge,
                HttpOnly: true,
                Secure:   h.cookieSecure,
                SameSite: sameSite,
        })

        writeJSON(w, http.StatusOK, models.SwitchShopResponse{
                OK:         true,
                ShopID:     result.ShopID.String(),
                ShopName:   result.ShopName,
                ShopSlug:   result.ShopSlug,
                ShopStatus: result.ShopStatus,
                RoleInShop: result.RoleInShop,
        })
}

// GetSubscription handles GET /api/shops/{id}/subscription.
//
//      Requires: authenticated session + member of the shop OR platform admin.
//      Response: 200 + models.SubscriptionResponse
func (h *ShopHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        s := mustSession(r)
        sub, plan, err := h.service.GetSubscription(r.Context(), s.UserID, s.Role, shopID)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        planName := ""
        if plan != nil {
                planName = plan.Name
        }
        writeJSON(w, http.StatusOK, models.ToSubscriptionResponse(sub, planName))
}

// RecordPayment handles POST /api/shops/{id}/subscription/payment. Admin only.
//
//      Request:  models.RecordPaymentRequest
//      Response: 200 {ok: true, recorded_at: ...}
func (h *ShopHandler) RecordPayment(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := parseShopID(w, r)
        if !ok {
                return
        }
        var req models.RecordPaymentRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        pay, err := h.service.RecordPayment(r.Context(), s.UserID, s.Role, shopID, req, ip, ua)
        if err != nil {
                writeShopServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":          true,
                "payment_id":  pay.ID.String(),
                "recorded_at": pay.RecordedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
        })
}

// --- helpers ----------------------------------------------------------------

// serviceUnavailable returns true (and writes a 503) when the shop service
// is nil — this happens when the server boots in degraded mode without a DB.
func (h *ShopHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service de boutiques n'est pas disponible (base de données injoignable).")
        return true
}

// writeShopServiceError maps service sentinel errors to HTTP responses.
func writeShopServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrNotAdmin):
                writeErrorWithCode(w, http.StatusForbidden, "forbidden",
                        "Cette action nécessite les droits administrateur.")
        case errors.Is(err, services.ErrNotShopMember):
                writeErrorWithCode(w, http.StatusForbidden, "not_shop_member",
                        "Vous n'êtes pas membre de cette boutique.")
        case errors.Is(err, services.ErrNotShopOwner):
                writeErrorWithCode(w, http.StatusForbidden, "not_shop_owner",
                        "Seul le propriétaire de la boutique peut effectuer cette action.")
        case errors.Is(err, services.ErrShopNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "shop_not_found",
                        "Boutique introuvable.")
        case errors.Is(err, services.ErrSubscriptionNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "subscription_not_found",
                        "Abonnement introuvable pour cette boutique.")
        case errors.Is(err, services.ErrSlugTaken):
                writeErrorWithCode(w, http.StatusConflict, "slug_taken",
                        "Ce slug est déjà utilisé par une autre boutique.")
        case errors.Is(err, services.ErrOwnerNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "owner_not_found",
                        "Le propriétaire doit d'abord créer un compte (email inconnu).")
        case errors.Is(err, services.ErrPlanNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "plan_not_found",
                        "Plan d'abonnement introuvable.")
        case errors.Is(err, services.ErrShopSuspended):
                writeErrorWithCode(w, http.StatusForbidden, "shop_suspended",
                        "Cette boutique est suspendue.")
        case errors.Is(err, services.ErrInvalidPaymentMode):
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_payment_mode",
                        "Mode de paiement invalide.")
        case errors.Is(err, services.ErrInvalidPeriod):
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_period",
                        "Période de paiement invalide.")
        case errors.Is(err, services.ErrActivationCriteriaNotMet):
                // Bare error (no ActivationError attached) — generic 422.
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "activation_criteria_not_met",
                        "Critères d'activation non remplis.")
        default:
                slog.Error("shop service error", "error", err, "error_type", fmt.Sprintf("%T", err))
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}

// parseShopID extracts the {id} or {shopId} route param and parses it as
// a UUID. Writes a 400 response and returns false if the ID is missing or
// malformed. The handler accepts both parameter names so the same handler
// can be registered at /api/shops/{id} (in the standalone shop router) or
// at /api/shops/{shopId} (in the unified shop-scoped route group).
func parseShopID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
        raw := chi.URLParam(r, "shopId")
        if raw == "" {
                raw = chi.URLParam(r, "id")
        }
        if raw == "" {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_id",
                        "ID de boutique manquant.")
                return uuid.Nil, false
        }
        id, err := uuid.Parse(raw)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_id",
                        "ID de boutique invalide (UUID attendu).")
                return uuid.Nil, false
        }
        return id, true
}

// RegisterShopScopedRoutes adds the shop-specific routes (Get, Update,
// Activate, Suspend, etc.) to the given router. The router is expected to
// be the {shopId} route group — i.e. the URL pattern is /api/shops/{shopId}/...
// and chi.URLParam(r, "shopId") returns the shop UUID.
//
// Routes registered here:
//   GET    /                  → Get
//   PATCH  /                  → Update
//   POST   /activate          → Activate
//   POST   /suspend           → Suspend
//   POST   /reactivate        → Reactivate
//   GET    /validation        → Validation
//   GET    /subscription      → GetSubscription
//   POST   /subscription/payment → RecordPayment
//
// The "list / create / switch" routes stay on the standalone Router()
// (those don't have a shopId in the URL).
func (h *ShopHandler) RegisterShopScopedRoutes(r chi.Router) {
        r.Get("/", h.Get)
        r.Patch("/", h.Update)
        r.Post("/activate", h.Activate)
        r.Post("/suspend", h.Suspend)
        r.Post("/reactivate", h.Reactivate)
        r.Get("/validation", h.Validation)
        r.Get("/subscription", h.GetSubscription)
        r.Post("/subscription/payment", h.RecordPayment)
}

// mustSession extracts the verified session from the request context. The
// RequireAuth middleware runs before this handler so the session is always
// present; if not (programmer error), we return a zero session.
func mustSession(r *http.Request) *auth.Session {
        s, _ := middleware.SessionFromContext(r.Context())
        if s == nil {
                return &auth.Session{}
        }
        return s
}

// toShopResponses converts a slice of models.Shop into the wire shape.
func toShopResponses(in []models.Shop) []models.ShopResponse {
        out := make([]models.ShopResponse, 0, len(in))
        for i := range in {
                out = append(out, models.ToShopResponse(&in[i]))
        }
        return out
}

// atoiOr parses s as an int, returning def on error or empty input.
func atoiOr(s string, def int) int {
        if s == "" {
                return def
        }
        n, err := strconv.Atoi(s)
        if err != nil {
                return def
        }
        return n
}
