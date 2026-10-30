// WhatsApp admin endpoints (ch. 6 — Canal WhatsApp, ops management).
//
// Shop-scoped endpoints (require auth + owner/admin role):
//
//      POST /api/shops/{shopId}/whatsapp/send
//           Send a message to a customer (merchant-initiated). Body:
//           {to, type, content}. type defaults to "text".
//      GET  /api/shops/{shopId}/whatsapp/templates
//           List all message templates (global — not shop-specific).
//      POST /api/shops/{shopId}/whatsapp/templates/sync
//           Sync template approval statuses from Meta.
//      POST /api/shops/{shopId}/whatsapp/test
//           Send a test message to the merchant's own WhatsApp number
//           (cfg shop.whatsapp_number). Useful for verifying the setup.
//
// These endpoints are mounted under the shop-scoped route group in
// router.go, so they inherit RequireAuth + ShopContext middleware.
package handlers

import (
        "net/http"

        "github.com/go-chi/chi/v5"

        "nova-api/internal/api/middleware"
        "nova-api/internal/repository"
        "nova-api/internal/services"
        "nova-api/internal/whatsapp"
)

// WhatsAppAdminHandler bundles the WhatsApp management endpoints.
type WhatsAppAdminHandler struct {
        client      *whatsapp.Client
        templateMgr *whatsapp.TemplateManager
        aiSvc       *services.AIService
        shopSvc     *services.ShopService
        waSvc       *services.WhatsAppService
}

// NewWhatsAppAdminHandler constructs a WhatsAppAdminHandler. aiSvc and
// shopSvc are used to resolve the conversation/customer/shop when sending
// merchant-initiated messages. waSvc is used for the simulate-inbound
// endpoint (test pipeline without a real Meta connection).
func NewWhatsAppAdminHandler(
        client *whatsapp.Client,
        templateMgr *whatsapp.TemplateManager,
        aiSvc *services.AIService,
        shopSvc *services.ShopService,
        waSvc *services.WhatsAppService,
) *WhatsAppAdminHandler {
        return &WhatsAppAdminHandler{
                client:      client,
                templateMgr: templateMgr,
                aiSvc:       aiSvc,
                shopSvc:     shopSvc,
                waSvc:       waSvc,
        }
}

// Register adds the WhatsApp admin routes to the shop-scoped router.
func (h *WhatsAppAdminHandler) Register(r chi.Router) {
        r.Post("/whatsapp/send", h.SendMessage)
        r.Get("/whatsapp/templates", h.ListTemplates)
        r.Post("/whatsapp/templates/sync", h.SyncTemplates)
        r.Post("/whatsapp/test", h.SendTest)
        r.Post("/whatsapp/simulate-inbound", h.SimulateInbound)
}

// serviceUnavailable returns true (and writes a 503) when the WhatsApp client
// is nil (degraded mode — no DB pool at startup).
func (h *WhatsAppAdminHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.client == nil {
                writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                        "WhatsApp client is not initialized.")
                return true
        }
        return false
}

// ============================================================================
// POST /api/shops/{shopId}/whatsapp/send
// ============================================================================

// WhatsAppSendRequest is the body of POST /whatsapp/send.
type WhatsAppSendRequest struct {
        To      string `json:"to"      validate:"required,min=4,max=40"`
        Type    string `json:"type,omitempty" validate:"omitempty,oneof=text"`
        Content string `json:"content" validate:"required,min=1,max=4000"`
}

// SendMessage lets a merchant send a message directly to a customer. The
// message is stored in the conversation history (direction='outbound') and
// sent via the WhatsApp Cloud API. The merchant must provide the customer's
// phone (E.164); the handler resolves the active conversation for that phone
// (or creates one if needed).
func (h *WhatsAppAdminHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req WhatsAppSendRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        if req.Type == "" {
                req.Type = "text"
        }

        // Resolve the customer + conversation for this phone (get-or-create
        // both, so the merchant can send to a brand-new number).
        normalizedPhone := normalizePhoneE164(req.To)
        cust, err := h.aiSvc.GetOrCreateCustomerByPhone(r.Context(), shopID, normalizedPhone, "")
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "send_failed",
                        "Erreur lors de la résolution du client: "+err.Error())
                return
        }
        conv, err := h.aiSvc.GetOrCreateConversation(r.Context(), shopID, cust.ID, "whatsapp")
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "send_failed",
                        "Erreur lors de la résolution de la conversation: "+err.Error())
                return
        }

        // Send + store. We store the outbound row first (with wamid=""), then
        // send via WhatsApp, then fill in the wamid. This way the row exists
        // even if the HTTP send fails.
        stored, err := h.aiSvc.PersistOutboundWithWAMID(r.Context(), shopID, conv.ID, req.Type, req.Content, "")
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "send_failed",
                        "Erreur lors du stockage du message: "+err.Error())
                return
        }
        result, err := h.client.SendText(r.Context(), normalizedPhone, req.Content)
        if err != nil || result == nil {
                writeErrorWithCode(w, http.StatusBadGateway, "whatsapp_send_failed",
                        "Erreur lors de l'envoi du message WhatsApp: "+errOrEmpty(err))
                return
        }
        if result.Status == "failed" {
                writeErrorWithCode(w, http.StatusBadGateway, "whatsapp_send_failed",
                        "Meta a refusé le message: "+result.ErrorMessage+" (code "+result.ErrorCode+")")
                return
        }
        // Fill in the wamid on the stored row (best-effort).
        if err := h.aiSvc.UpdateMessageWAMID(r.Context(), shopID, stored.ID, result.MessageID); err != nil {
                writeJSON(w, http.StatusOK, map[string]any{
                        "ok":         true,
                        "message":    stored,
                        "message_id": result.MessageID,
                        "warning":    "message envoyé mais wamid non stocké: " + err.Error(),
                })
                return
        }
        stored.WAMID = strPtr(result.MessageID)
        stored.Status = result.Status
        writeJSON(w, http.StatusCreated, stored)
}

// strPtr returns a pointer to s. Helper for the rare cases where we need to
// fill a *string field with a non-const value.
func strPtr(s string) *string { return &s }

// ============================================================================
// GET /api/shops/{shopId}/whatsapp/templates
// ============================================================================

// ListTemplates returns all message templates (optionally filtered by status
// via ?status=). Templates are GLOBAL (not shop-specific) — they're managed
// by NOVA ops + Meta, not per shop.
func (h *WhatsAppAdminHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        // We still require a valid shop session (defense in depth) even
        // though templates aren't shop-scoped.
        if _, ok := requireShopMatch(w, r); !ok {
                return
        }
        status := r.URL.Query().Get("status")
        templates, err := h.templateMgr.List(r.Context(), status)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "templates_list_failed",
                        "Erreur lors de la récupération des modèles: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "items":     templates,
                "total":     len(templates),
                "filtered":  status != "",
        })
}

// ============================================================================
// POST /api/shops/{shopId}/whatsapp/templates/sync
// ============================================================================

// SyncTemplates fetches the latest approval statuses from Meta and updates
// the DB. Returns the list of templates after sync. In mock mode the call is
// a no-op (returns the current DB list).
func (h *WhatsAppAdminHandler) SyncTemplates(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        if _, ok := requireShopMatch(w, r); !ok {
                return
        }
        if err := h.templateMgr.SyncFromMeta(r.Context()); err != nil {
                writeErrorWithCode(w, http.StatusBadGateway, "templates_sync_failed",
                        "Erreur lors de la synchronisation avec Meta: "+err.Error())
                return
        }
        templates, err := h.templateMgr.List(r.Context(), "")
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "templates_list_failed",
                        "Erreur lors de la récupération des modèles après sync: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":        true,
                "synced":    true,
                "items":     templates,
                "total":     len(templates),
        })
}

// ============================================================================
// POST /api/shops/{shopId}/whatsapp/test
// ============================================================================

// SendTest sends a test message to the merchant's own WhatsApp number
// (shop.whatsapp_number). Useful for verifying the setup after onboarding.
// Body: {content?} — defaults to "Message de test NOVA".
func (h *WhatsAppAdminHandler) SendTest(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        // Load the shop to get its whatsapp_number. We use the shop service's
        // Get method which requires a requester ID + role — for the admin
        // endpoints we know the session has been authenticated by RequireAuth
        // middleware, so we use the session's user ID + role.
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        shop, err := h.shopSvc.Get(r.Context(), s.UserID, s.Role, shopID)
        if err != nil {
                writeErrorWithCode(w, http.StatusNotFound, "shop_not_found",
                        "Boutique introuvable: "+err.Error())
                return
        }
        if shop.WhatsAppNumber == nil || *shop.WhatsAppNumber == "" {
                writeErrorWithCode(w, http.StatusBadRequest, "no_whatsapp_number",
                        "Cette boutique n'a pas de numéro WhatsApp configuré.")
                return
        }
        var req struct {
                Content string `json:"content,omitempty"`
        }
        // Body is optional — only decode if present.
        if r.ContentLength > 0 {
                _ = decodeJSON(r, &req)
        }
        if req.Content == "" {
                req.Content = "✅ Message de test NOVA — votre intégration WhatsApp fonctionne."
        }

        to := normalizePhoneE164(*shop.WhatsAppNumber)
        result, err := h.client.SendText(r.Context(), to, req.Content)
        if err != nil || result == nil {
                writeErrorWithCode(w, http.StatusBadGateway, "test_send_failed",
                        "Erreur lors de l'envoi du test: "+errOrEmpty(err))
                return
        }
        if result.Status == "failed" {
                writeErrorWithCode(w, http.StatusBadGateway, "test_send_failed",
                        "Meta a refusé le test: "+result.ErrorMessage+" (code "+result.ErrorCode+")")
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":         true,
                "message_id": result.MessageID,
                "to":         to,
                "mock":       h.client.IsMock(),
        })
}

// --- helpers ----------------------------------------------------------------

// errOrEmpty returns err.Error() or "" when err is nil. Used for building
// user-friendly error messages without fmt.Sprintf-ing nil errors.
func errOrEmpty(err error) string {
        if err == nil {
                return ""
        }
        return err.Error()
}

// normalizePhoneE164 strips spaces/dashes and ensures the leading "+" is
// present (Meta wants E.164 without "+", but our internal customer table
// uses "+"). Returns "" for empty input.
func normalizePhoneE164(s string) string {
        if s == "" {
                return ""
        }
        var b []byte
        for i := 0; i < len(s); i++ {
                c := s[i]
                if c >= '0' && c <= '9' {
                        b = append(b, c)
                }
        }
        if len(b) == 0 {
                return ""
        }
        return "+" + string(b)
}

// Ensure the repository import is referenced (used by services helpers).
var _ = repository.ErrConversationNotFound

// ============================================================================
// POST /api/shops/{shopId}/whatsapp/simulate-inbound
// ============================================================================

// SimulateInboundRequest is the body of POST /whatsapp/simulate-inbound.
// It mimics the shape of an inbound WhatsApp message: customer phone, name
// (optional), message text, and message type (defaults to "text").
type SimulateInboundRequest struct {
        CustomerPhone string `json:"customer_phone" validate:"required,min=4,max=40"`
        CustomerName  string `json:"customer_name,omitempty" validate:"omitempty,max=120"`
        Message       string `json:"message"         validate:"required,min=1,max=4000"`
        MessageType   string `json:"message_type,omitempty" validate:"omitempty,oneof=text image audio video document"`
}

// SimulateInbound simulates an inbound WhatsApp message and processes it
// through the full pipeline (customer get-or-create, conversation, AI
// engine, outbound message store) WITHOUT sending anything to Meta. Returns
// the AI reply + metadata.
//
// Use case (cahier des charges ch. 5.6 — mode test): lets the operator
// verify the end-to-end WhatsApp pipeline (webhook → processor → AI →
// reply → status update) before going live with a real Meta Business
// number. In mock mode the WhatsApp client returns synthetic wamids
// (wamid.mock.*) so the message rows in the DB look just like real ones.
func (h *WhatsAppAdminHandler) SimulateInbound(w http.ResponseWriter, r *http.Request) {
        if h.waSvc == nil {
                writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                        "WhatsApp service is not initialized.")
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req SimulateInboundRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        if req.MessageType == "" {
                req.MessageType = "text"
        }
        // Resolve the user from the session (used for RLS on DB writes).
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        userRole := s.Role
        if userRole == "" {
                userRole = "owner"
        }
        result, err := h.waSvc.SimulateInbound(r.Context(), shopID, req.CustomerPhone, req.CustomerName, req.Message, req.MessageType, s.UserID, userRole)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "simulate_inbound_failed",
                        "Erreur lors de la simulation: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, result)
}
