// AI + simulation console + conversations HTTP handlers (ch. 5.6).
//
// Endpoints:
//
//      POST /api/shops/{shopId}/ai/simulate
//           THE SIMULATION CONSOLE — processes a message through the full AI
//           engine (tools + guardrail) without sending anything to WhatsApp.
//           Body: {customer_phone, customer_name?, message, message_type?}
//      GET  /api/shops/{shopId}/conversations
//           List conversations (?state, ?page, ?limit, ?search)
//      GET  /api/shops/{shopId}/conversations/{id}
//           Get a conversation with its messages + customer
//      POST /api/shops/{shopId}/conversations/{id}/takeover
//           Merchant takes over (state → human)
//      POST /api/shops/{shopId}/conversations/{id}/handback
//           "Rendre à NOVA" (state → ai) — also exposed as /return-to-ai
//      POST /api/shops/{shopId}/conversations/{id}/return-to-ai
//           Alias of /handback (spec naming).
//      POST /api/shops/{shopId}/conversations/{id}/close
//           Close the conversation (state → closed)
//      POST /api/shops/{shopId}/conversations/{id}/messages
//           Merchant sends a message in human mode
//      GET  /api/shops/{shopId}/ai/usage
//           AI usage stats this month (tokens, cost, by model)
//
// WhatsApp webhook (NOT shop-scoped — Meta calls it with a phone number):
//
//      GET  /webhooks/whatsapp           Meta verify handshake (hub.mode=subscribe, hub.verify_token)
//      POST /webhooks/whatsapp           Inbound message / status update (stub for Task 9)
package handlers

import (
        "net/http"
        "strconv"

        "github.com/go-chi/chi/v5"

        "nova-api/internal/api/middleware"
        "nova-api/internal/repository"
        "nova-api/internal/services"
)

// AIHandler bundles the AI + conversations HTTP handlers with their shared
// dependency: the AI service.
type AIHandler struct {
        service *services.AIService
}

// NewAIHandler returns an AIHandler bound to the given service.
func NewAIHandler(service *services.AIService) *AIHandler {
        return &AIHandler{service: service}
}

// Register adds the AI + conversation routes to the given chi.Router.
func (h *AIHandler) Register(r chi.Router) {
        // AI simulation console + usage stats.
        r.Post("/ai/simulate", h.Simulate)
        r.Get("/ai/usage", h.GetAIUsage)

        // Conversations.
        r.Get("/conversations", h.ListConversations)
        r.Get("/conversations/{id}", h.GetConversation)
        r.Post("/conversations/{id}/takeover", h.TakeOver)
        r.Post("/conversations/{id}/handback", h.HandBack)
        r.Post("/conversations/{id}/return-to-ai", h.HandBack) // alias (spec naming)
        r.Post("/conversations/{id}/close", h.CloseConversation)
        r.Post("/conversations/{id}/messages", h.SendMessage)
}

// serviceUnavailable returns true (and writes a 503) when the AI service is nil.
func (h *AIHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service == nil {
                writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                        "AI service is not initialized.")
                return true
        }
        return false
}

// ============================================================================
// POST /api/shops/{shopId}/ai/simulate — THE SIMULATION CONSOLE
// ============================================================================

// SimulateRequest is the body of POST /ai/simulate.
type SimulateRequest struct {
        CustomerPhone string `json:"customer_phone" validate:"required,min=4,max=40"`
        CustomerName  string `json:"customer_name,omitempty" validate:"omitempty,max=120"`
        Message       string `json:"message"         validate:"required,min=1,max=4000"`
        MessageType   string `json:"message_type,omitempty" validate:"omitempty,oneof=text image audio video document"`
}

// Simulate processes a message through the full AI engine (tools + guardrail)
// WITHOUT sending anything to WhatsApp. Used by the merchant to test NOVA
// before going live (ch. 5.6 — mode test).
func (h *AIHandler) Simulate(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req SimulateRequest
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

        // Get the user ID from the session (used for RLS on DB writes).
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        userRole := s.Role
        if userRole == "" {
                userRole = "owner"
        }

        // Process the message synchronously (bypassDebounce=true — the simulation
        // console wants immediate responses, not buffered ones).
        result, err := h.service.ProcessIncomingMessage(r.Context(), shopID, req.CustomerPhone, req.CustomerName, req.Message, req.MessageType, true, s.UserID, userRole)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "ai_simulate_failed",
                        "Erreur lors du traitement du message: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, result)
}

// ============================================================================
// GET /api/shops/{shopId}/conversations
// ============================================================================

func (h *AIHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        page, _ := strconv.Atoi(r.URL.Query().Get("page"))
        limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
        state := r.URL.Query().Get("state")
        search := r.URL.Query().Get("search")
        params := repository.ListConversationsParams{Page: page, Limit: limit, State: state, Search: search}
        items, total, err := h.service.ListConversations(r.Context(), shopID, params)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "list_conversations_failed",
                        "Erreur lors de la récupération des conversations: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "items": items,
                "total": total,
                "page":  params.Page,
                "limit": params.Limit,
        })
}

// ============================================================================
// GET /api/shops/{shopId}/conversations/{id}
// ============================================================================

func (h *AIHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        convID, ok := parseUUIDParam(w, r, "id", "conversation_id")
        if !ok {
                return
        }
        conv, err := h.service.GetConversation(r.Context(), shopID, convID)
        if err != nil {
                if err == repository.ErrConversationNotFound {
                        writeErrorWithCode(w, http.StatusNotFound, "conversation_not_found",
                                "Conversation introuvable.")
                        return
                }
                writeErrorWithCode(w, http.StatusInternalServerError, "get_conversation_failed",
                        "Erreur lors de la récupération de la conversation: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, conv)
}

// ============================================================================
// POST /api/shops/{shopId}/conversations/{id}/takeover
// ============================================================================

func (h *AIHandler) TakeOver(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        convID, ok := parseUUIDParam(w, r, "id", "conversation_id")
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if err := h.service.TakeOver(r.Context(), shopID, convID, s.UserID); err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "takeover_failed",
                        "Erreur lors de la reprise: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "human"})
}

// ============================================================================
// POST /api/shops/{shopId}/conversations/{id}/handback
// (also serves /return-to-ai — same handler, alias registered in Register)
// ============================================================================

func (h *AIHandler) HandBack(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        convID, ok := parseUUIDParam(w, r, "id", "conversation_id")
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if err := h.service.HandBack(r.Context(), shopID, convID, s.UserID); err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "handback_failed",
                        "Erreur lors du retour à NOVA: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "ai"})
}

// ============================================================================
// POST /api/shops/{shopId}/conversations/{id}/close
// ============================================================================

func (h *AIHandler) CloseConversation(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        convID, ok := parseUUIDParam(w, r, "id", "conversation_id")
        if !ok {
                return
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil {
                writeErrorWithCode(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                return
        }
        if err := h.service.Close(r.Context(), shopID, convID, s.UserID); err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "close_failed",
                        "Erreur lors de la fermeture: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "closed"})
}

// ============================================================================
// POST /api/shops/{shopId}/conversations/{id}/messages
// ============================================================================

// SendMessageRequest is the body of POST /conversations/{id}/messages.
type SendMessageRequest struct {
        Content string `json:"content" validate:"required,min=1,max=4000"`
}

func (h *AIHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        convID, ok := parseUUIDParam(w, r, "id", "conversation_id")
        if !ok {
                return
        }
        var req SendMessageRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        msg, err := h.service.SendMessage(r.Context(), shopID, convID, req.Content)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "send_message_failed",
                        "Erreur lors de l'envoi du message: "+err.Error())
                return
        }
        writeJSON(w, http.StatusCreated, msg)
}

// ============================================================================
// GET /api/shops/{shopId}/ai/usage
// ============================================================================

func (h *AIHandler) GetAIUsage(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        stats, err := h.service.GetAIUsage(r.Context(), shopID)
        if err != nil {
                writeErrorWithCode(w, http.StatusInternalServerError, "ai_usage_failed",
                        "Erreur lors de la récupération des statistiques IA: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, stats)
}
