// Subscriptions + quotas HTTP handlers (Task 10 — ch. 7 Abonnements, ch. 5.6
// quotas / mode dégradé).
//
// Shop-scoped endpoints (require auth + ShopContext middleware):
//
//      GET    /api/shops/{shopId}/subscription
//             Current subscription + plan + status + next billing date.
//      GET    /api/shops/{shopId}/subscription/payments
//             Payment history for the shop.
//      POST   /api/shops/{shopId}/subscription/payment
//             Admin records a payment (subscription → 'active').
//             Body: {amount, mode, reference?, period_start, period_end}.
//      POST   /api/shops/{shopId}/subscription/suspend
//             Admin manually suspends. Body: {reason}.
//      POST   /api/shops/{shopId}/subscription/reactivate
//             Admin reactivates (records a synthetic payment + bumps next_billing_at).
//      POST   /api/shops/{shopId}/subscription/terminate
//             Admin terminates. Body: {reason}.
//      GET    /api/shops/{shopId}/quota
//             Current quota status (used vs quota, degraded mode flag, alerts).
//      GET    /api/shops/{shopId}/usage
//             AI usage stats (?from, ?to, ?daily=true for charts).
//      GET    /api/shops/{shopId}/usage/top-conversations
//             Top conversations by cost.
//
// Platform-admin endpoints (require auth + RequireRole super_admin|admin):
//
//      GET    /api/admin/subscriptions
//             List all subscriptions (?status, ?page, ?limit).
//      GET    /api/admin/subscriptions/late
//             Subscriptions in 'late' or 'grace_period'.
//      GET    /api/admin/subscriptions/revenue
//             Revenue stats (MRR, total, by plan).
//      POST   /api/admin/cron/run
//             Manually trigger cron jobs (testing).
package handlers

import (
        "errors"
        "fmt"
        "log/slog"
        "net/http"
        "strconv"
        "time"

        "github.com/go-chi/chi/v5"

        "nova-api/internal/api/middleware"
        "nova-api/internal/models"
        "nova-api/internal/repository"
        "nova-api/internal/services"
)

// SubscriptionHandler bundles the subscription + quota HTTP handlers with
// their shared dependencies: the SubscriptionService + QuotaService.
type SubscriptionHandler struct {
        service  *services.SubscriptionService
        quotaSvc *services.QuotaService
        cronSvc  *services.CronService
}

// NewSubscriptionHandler constructs a SubscriptionHandler. quotaSvc and
// cronSvc may be nil in degraded mode (the corresponding endpoints return
// 503).
func NewSubscriptionHandler(
        service *services.SubscriptionService,
        quotaSvc *services.QuotaService,
        cronSvc *services.CronService,
) *SubscriptionHandler {
        return &SubscriptionHandler{
                service:  service,
                quotaSvc: quotaSvc,
                cronSvc:  cronSvc,
        }
}

// Register adds the shop-scoped subscription + quota routes to the given
// chi.Router. The caller is responsible for applying the auth + shop context
// middleware on the parent router.
func (h *SubscriptionHandler) Register(r chi.Router) {
        r.Get("/subscription", h.GetSubscription)
        r.Get("/subscription/payments", h.ListPayments)
        r.Post("/subscription/payment", h.RecordPayment)
        r.Post("/subscription/suspend", h.Suspend)
        r.Post("/subscription/reactivate", h.Reactivate)
        r.Post("/subscription/terminate", h.Terminate)
        r.Get("/quota", h.GetQuota)
        r.Get("/usage", h.GetUsage)
        r.Get("/usage/top-conversations", h.GetTopConversations)
}

// RegisterAdmin adds the platform-admin routes (already under RequireRole).
func (h *SubscriptionHandler) RegisterAdmin(r chi.Router) {
        r.Get("/subscriptions", h.AdminListSubscriptions)
        r.Get("/subscriptions/late", h.AdminListLateSubscriptions)
        r.Get("/subscriptions/revenue", h.AdminGetRevenue)
        r.Post("/cron/run", h.AdminRunCron)
}

// serviceUnavailable returns true (and writes a 503) when the subscription
// service is nil — degraded mode (no DB pool at startup).
func (h *SubscriptionHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service d'abonnements n'est pas disponible (base de données injoignable).")
        return true
}

// ============================================================================
// GET /api/shops/{shopId}/subscription
// ============================================================================

// GetSubscription returns the current subscription + plan + status.
func (h *SubscriptionHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        sub, plan, err := h.service.GetSubscription(r.Context(), shopID)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        planName := ""
        if plan != nil {
                planName = plan.Name
        }
        resp := models.ToSubscriptionResponse(sub, planName)
        // Augment with plan details when available.
        if plan != nil {
                resp2 := map[string]any{
                        "id":              resp.ID,
                        "shop_id":         resp.ShopID,
                        "plan_id":         resp.PlanID,
                        "plan_name":       resp.PlanName,
                        "status":          resp.Status,
                        "started_at":      resp.StartedAt,
                        "next_billing_at": resp.NextBillingAt,
                        "grace_until":     resp.GraceUntil,
                        "suspended_at":    resp.SuspendedAt,
                        "terminated_at":   resp.TerminatedAt,
                        "created_at":      resp.CreatedAt,
                        "updated_at":      resp.UpdatedAt,
                        "plan": map[string]any{
                                "id":             plan.ID.String(),
                                "name":           plan.Name,
                                "price":          plan.Price,
                                "setup_fee":      plan.SetupFee,
                                "message_quota":  plan.MessageQuota,
                                "product_limit":  plan.ProductLimit,
                                "employee_limit": plan.EmployeeLimit,
                                "active":         plan.Active,
                        },
                }
                writeJSON(w, http.StatusOK, resp2)
                return
        }
        writeJSON(w, http.StatusOK, resp)
}

// ============================================================================
// GET /api/shops/{shopId}/subscription/payments
// ============================================================================

// ListPayments returns the payment history for the shop.
func (h *SubscriptionHandler) ListPayments(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        payments, err := h.service.ListPayments(r.Context(), shopID)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "items": payments,
                "total": len(payments),
        })
}

// ============================================================================
// POST /api/shops/{shopId}/subscription/payment — admin records payment
// ============================================================================

// RecordPayment handles the admin payment-recording endpoint.
func (h *SubscriptionHandler) RecordPayment(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if !services.IsPlatformAdmin(s.Role) {
                writeErrorWithCode(w, http.StatusForbidden, "forbidden",
                        "Seul un administrateur plateforme peut enregistrer un paiement.")
                return
        }
        var req services.RecordPaymentRequest
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
        pay, err := h.service.RecordPayment(r.Context(), s.UserID, shopID, req, ip, ua)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":          true,
                "payment_id":  pay.ID.String(),
                "amount":      pay.Amount,
                "mode":        string(pay.Mode),
                "reference":   pay.Reference,
                "recorded_at": pay.RecordedAt.UTC().Format(time.RFC3339),
        })
}

// ============================================================================
// POST /api/shops/{shopId}/subscription/suspend
// ============================================================================

// SuspendRequest is the body of POST /subscription/suspend.
type SuspendRequest struct {
        Reason string `json:"reason" validate:"required,min=3,max=1000"`
}

// Suspend handles the admin manual-suspend endpoint.
func (h *SubscriptionHandler) Suspend(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if !services.IsPlatformAdmin(s.Role) {
                writeErrorWithCode(w, http.StatusForbidden, "forbidden",
                        "Seul un administrateur plateforme peut suspendre un abonnement.")
                return
        }
        var req SuspendRequest
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
        sub, err := h.service.Suspend(r.Context(), s.UserID, shopID, req.Reason, ip, ua)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":     true,
                "id":     sub.ID.String(),
                "status": string(sub.Status),
        })
}

// ============================================================================
// POST /api/shops/{shopId}/subscription/reactivate
// ============================================================================

// Reactivate handles the admin manual-reactivate endpoint.
func (h *SubscriptionHandler) Reactivate(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if !services.IsPlatformAdmin(s.Role) {
                writeErrorWithCode(w, http.StatusForbidden, "forbidden",
                        "Seul un administrateur plateforme peut réactiver un abonnement.")
                return
        }
        ip, ua := clientInfo(r)
        sub, err := h.service.Reactivate(r.Context(), s.UserID, shopID, ip, ua)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":     true,
                "id":     sub.ID.String(),
                "status": string(sub.Status),
        })
}

// ============================================================================
// POST /api/shops/{shopId}/subscription/terminate
// ============================================================================

// Terminate handles the admin manual-terminate endpoint.
func (h *SubscriptionHandler) Terminate(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if !services.IsPlatformAdmin(s.Role) {
                writeErrorWithCode(w, http.StatusForbidden, "forbidden",
                        "Seul un administrateur plateforme peut résilier un abonnement.")
                return
        }
        var req SuspendRequest // same shape: {reason}
        if r.ContentLength > 0 {
                if err := decodeJSON(r, &req); err != nil {
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                                "Le corps de la requête est invalide: "+err.Error())
                        return
                }
                if err := validateStruct(req); err != nil {
                        writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                        return
                }
        }
        if req.Reason == "" {
                req.Reason = "Résiliation manuelle par l'administrateur."
        }
        ip, ua := clientInfo(r)
        sub, err := h.service.Terminate(r.Context(), s.UserID, shopID, req.Reason, ip, ua)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":     true,
                "id":     sub.ID.String(),
                "status": string(sub.Status),
        })
}

// ============================================================================
// GET /api/shops/{shopId}/quota
// ============================================================================

// GetQuota returns the current quota status for the shop.
func (h *SubscriptionHandler) GetQuota(w http.ResponseWriter, r *http.Request) {
        if h.quotaSvc == nil {
                writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                        "Le service de quotas n'est pas disponible.")
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        status, err := h.quotaSvc.CheckQuota(r.Context(), shopID)
        if err != nil {
                writeSubServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, status)
}

// ============================================================================
// GET /api/shops/{shopId}/usage
// ============================================================================

// GetUsage returns the AI usage stats for the shop. Supports:
//   - ?from=YYYY-MM-DD&to=YYYY-MM-DD  → custom range aggregate
//   - ?daily=true                      → daily breakdown (for charts)
//   - (no params)                      → current-month aggregate
func (h *SubscriptionHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        q := r.URL.Query()
        now := time.Now().UTC()
        fromStr := q.Get("from")
        toStr := q.Get("to")
        daily := q.Get("daily") == "true" || q.Get("daily") == "1"

        // Default range = current month.
        if fromStr == "" {
                fromStr = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
        }
        if toStr == "" {
                toStr = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
        }
        from, err := time.Parse("2006-01-02", fromStr)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_from",
                        "Paramètre 'from' invalide (format attendu YYYY-MM-DD).")
                return
        }
        to, err := time.Parse("2006-01-02", toStr)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_to",
                        "Paramètre 'to' invalide (format attendu YYYY-MM-DD).")
                return
        }
        if daily {
                daily, err := h.service.UsageRepoGetDailyUsage(r.Context(), shopID, from, to)
                if err != nil {
                        writeErrorWithCode(w, http.StatusInternalServerError, "usage_failed",
                                "Erreur lors de la récupération des statistiques: "+err.Error())
                        return
                }
                writeJSON(w, http.StatusOK, map[string]any{
                        "from":  from.Format("2006-01-02"),
                        "to":    to.Format("2006-01-02"),
                        "daily": daily,
                })
                return
        }
        summary, err := h.service.UsageRepoGetUsageByShop(r.Context(), shopID, from, to)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "usage_failed",
                        "Erreur lors de la récupération des statistiques: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, summary)
}

// ============================================================================
// GET /api/shops/{shopId}/usage/top-conversations
// ============================================================================

// GetTopConversations returns the top conversations by cost.
func (h *SubscriptionHandler) GetTopConversations(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        limit := 10
        if l := r.URL.Query().Get("limit"); l != "" {
                if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
                        limit = n
                }
        }
        items, err := h.service.UsageRepoGetTopConversationsByCost(r.Context(), shopID, limit)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "top_conversations_failed",
                        "Erreur lors de la récupération des conversations: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "items": items,
                "total": len(items),
        })
}

// ============================================================================
// Platform-admin endpoints
// ============================================================================

// AdminListSubscriptions handles GET /api/admin/subscriptions.
func (h *SubscriptionHandler) AdminListSubscriptions(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        params := repository.ListAllParams{
                Status: r.URL.Query().Get("status"),
                Page:   atoiOr(r.URL.Query().Get("page"), 1),
                Limit:  atoiOr(r.URL.Query().Get("limit"), 50),
        }
        items, total, err := h.service.ListAllSubscriptions(r.Context(), params)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "list_failed",
                        "Erreur lors de la récupération des abonnements: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "items": items,
                "total": total,
                "page":  params.Page,
                "limit": params.Limit,
        })
}

// AdminListLateSubscriptions handles GET /api/admin/subscriptions/late.
func (h *SubscriptionHandler) AdminListLateSubscriptions(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        // ListAll with status filter — "late" by default; allow ?status=grace_period too.
        status := r.URL.Query().Get("status")
        if status == "" {
                // Late + grace_period combined — we fetch both via two calls.
                lateItems, _, err := h.service.ListAllSubscriptions(r.Context(), repository.ListAllParams{Status: "late", Page: 1, Limit: 200})
                if err != nil {
                        writeErrorWithCode(w, http.StatusInternalServerError, "list_failed",
                                "Erreur: "+err.Error())
                        return
                }
                graceItems, _, err := h.service.ListAllSubscriptions(r.Context(), repository.ListAllParams{Status: "grace_period", Page: 1, Limit: 200})
                if err != nil {
                        writeErrorWithCode(w, http.StatusInternalServerError, "list_failed",
                                "Erreur: "+err.Error())
                        return
                }
                items := append(lateItems, graceItems...)
                writeJSON(w, http.StatusOK, map[string]any{
                        "items": items,
                        "total": len(items),
                })
                return
        }
        items, total, err := h.service.ListAllSubscriptions(r.Context(), repository.ListAllParams{
                Status: status, Page: 1, Limit: 200,
        })
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "list_failed",
                        "Erreur: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "items": items,
                "total": total,
        })
}

// AdminGetRevenue handles GET /api/admin/subscriptions/revenue.
func (h *SubscriptionHandler) AdminGetRevenue(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        stats, err := h.service.GetRevenueStats(r.Context())
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "revenue_failed",
                        "Erreur lors du calcul du revenu: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, stats)
}

// AdminRunCron handles POST /api/admin/cron/run.
func (h *SubscriptionHandler) AdminRunCron(w http.ResponseWriter, r *http.Request) {
        if h.cronSvc == nil {
                writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                        "Le service cron n'est pas disponible.")
                return
        }
        results := h.cronSvc.RunAllNow(r.Context())
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":      true,
                "results": results,
        })
}

// --- helpers ----------------------------------------------------------------

// writeSubServiceError maps SubscriptionService sentinel errors to HTTP responses.
func writeSubServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrSubServiceNotInitialized):
                writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                        "Le service d'abonnements n'est pas initialisé.")
        case errors.Is(err, services.ErrSubscriptionNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "subscription_not_found",
                        "Abonnement introuvable pour cette boutique.")
        case errors.Is(err, services.ErrPlanNotFoundForQuota):
                writeErrorWithCode(w, http.StatusNotFound, "plan_not_found",
                        "Plan d'abonnement introuvable pour le calcul du quota.")
        case errors.Is(err, services.ErrInvalidPaymentMode):
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_payment_mode",
                        "Mode de paiement invalide.")
        case errors.Is(err, services.ErrInvalidPeriod):
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_period",
                        "Période de paiement invalide.")
        case errors.Is(err, services.ErrCannotSuspend):
                writeErrorWithCode(w, http.StatusConflict, "cannot_suspend",
                        "L'abonnement ne peut pas être suspendu depuis son état actuel.")
        case errors.Is(err, services.ErrCannotReactivate):
                writeErrorWithCode(w, http.StatusConflict, "cannot_reactivate",
                        "L'abonnement ne peut pas être réactivé (déjà résilié).")
        case errors.Is(err, services.ErrCannotTerminate):
                writeErrorWithCode(w, http.StatusConflict, "cannot_terminate",
                        "L'abonnement est déjà résilié.")
        default:
                slog.Error("subscription service error", "error", err, "error_type", fmt.Sprintf("%T", err))
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}
