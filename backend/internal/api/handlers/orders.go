// Orders handlers — cart + order + state machine endpoints for NOVA.
//
// All routes are shop-scoped: they mount under
// /api/shops/{shopId}/carts and /api/shops/{shopId}/orders.
// The {shopId} URL parameter is validated against the session's
// current_shop_id (defense in depth — the URL shopId MUST match the
// session shopId or we return 403). RLS provides a second layer of
// protection at the DB level.
//
// Each handler:
//  1. Extracts {shopId} from the URL and validates it against the session.
//  2. Decodes the JSON body into a models/order.go DTO.
//  3. Optionally runs go-playground/validator for cheap syntactic checks.
//  4. Calls into the order service for the business logic.
//  5. Translates the service result / sentinel error into an HTTP response.
package handlers

import (
        "errors"
        "log/slog"
        "net/http"
        "strconv"
        "time"

        "github.com/go-chi/chi/v5"
        "github.com/google/uuid"

        "nova-api/internal/api/middleware"
        "nova-api/internal/models"
        "nova-api/internal/services"
)

// OrderHandler bundles the cart + order HTTP handlers with their shared
// dependency: the order service.
type OrderHandler struct {
        service *services.OrderService
}

// NewOrderHandler returns an OrderHandler bound to the given service.
func NewOrderHandler(service *services.OrderService) *OrderHandler {
        return &OrderHandler{service: service}
}

// Register adds the cart + order routes to the given chi.Router. The caller
// is responsible for applying the auth + shop context middleware on the
// parent router.
func (h *OrderHandler) Register(r chi.Router) {
        // Carts
        r.Post("/carts", h.GetOrCreateCart)
        r.Get("/carts/{cartId}", h.GetCart)
        r.Post("/carts/{cartId}/items", h.AddCartItem)
        r.Patch("/carts/{cartId}/items/{itemId}", h.UpdateCartItem)
        r.Delete("/carts/{cartId}/items/{itemId}", h.RemoveCartItem)
        r.Delete("/carts/{cartId}", h.ClearCart)
        r.Post("/carts/{cartId}/recap", h.GenerateRecap)

        // Orders
        r.Post("/orders", h.ConfirmOrder)
        r.Get("/orders", h.ListOrders)
        r.Get("/orders/stats", h.OrderStats)
        r.Get("/orders/{id}", h.GetOrder)
        r.Get("/orders/{id}/events", h.ListOrderEvents)
        r.Post("/orders/{id}/confirm", h.ConfirmPendingOrder)
        r.Post("/orders/{id}/cancel", h.CancelOrder)
        r.Post("/orders/{id}/advance", h.AdvanceOrder)
        r.Patch("/orders/{id}/payment", h.UpdatePayment)

        // NOVA v3 — new flow endpoints (spec section 1)
        r.Post("/orders/{id}/v3/create", h.ConfirmOrderV3)           // create as en_attente_confirmation + reserve stock
        r.Post("/orders/{id}/merchant-confirm", h.MerchantConfirmOrder) // → en_attente_paiement OR en_cours
        r.Post("/orders/{id}/refuse", h.MerchantRefuseOrder)         // → refusee + release stock
        r.Post("/orders/{id}/signal-payment", h.ClientSignalPayment) // → paiement_signalé
        r.Post("/orders/{id}/confirm-payment", h.MerchantConfirmPayment) // → en_cours (deduct stock + count revenue)
        r.Post("/orders/{id}/refuse-payment", h.MerchantRefusePayment)   // → en_attente_paiement OR refusee
        r.Post("/orders/{id}/ready", h.MerchantMarkReady)            // en_cours → prete
        r.Post("/orders/{id}/complete", h.MerchantComplete)          // prete → terminee
        r.Post("/orders/{id}/cancel-v3", h.CancelOrderV3)            // → annulee (reinstate/release stock + remove revenue)
}

// ============================================================================
// Carts
// ============================================================================

// GetOrCreateCart handles POST /api/shops/{shopId}/carts.
func (h *OrderHandler) GetOrCreateCart(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req models.GetOrCreateCartRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        var conversationID *uuid.UUID
        if req.ConversationID != "" {
                id, err := uuid.Parse(req.ConversationID)
                if err != nil {
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_conversation_id",
                                "conversation_id doit être un UUID.")
                        return
                }
                conversationID = &id
        }
        customerID, err := uuid.Parse(req.CustomerID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_customer_id",
                        "customer_id doit être un UUID.")
                return
        }
        s := mustSession(r)
        cart, err := h.service.GetOrCreateCart(r.Context(), s.UserID, s.Role, shopID, conversationID, &customerID)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToCartResponse(cart))
}

// GetCart handles GET /api/shops/{shopId}/carts/{cartId}.
func (h *OrderHandler) GetCart(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        cartID, ok := parseUUIDParam(w, r, "cartId", "cart_id")
        if !ok {
                return
        }
        s := mustSession(r)
        cart, err := h.service.GetCart(r.Context(), s.UserID, s.Role, shopID, cartID)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToCartResponse(cart))
}

// AddCartItem handles POST /api/shops/{shopId}/carts/{cartId}/items.
func (h *OrderHandler) AddCartItem(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        cartID, ok := parseUUIDParam(w, r, "cartId", "cart_id")
        if !ok {
                return
        }
        var req models.AddCartItemRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        variantID, err := uuid.Parse(req.VariantID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_variant_id", "variant_id doit être un UUID.")
                return
        }
        s := mustSession(r)
        cart, err := h.service.AddToCart(r.Context(), s.UserID, s.Role, shopID, cartID, variantID, req.Quantity)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusCreated, models.ToCartResponse(cart))
}

// UpdateCartItem handles PATCH /api/shops/{shopId}/carts/{cartId}/items/{itemId}.
func (h *OrderHandler) UpdateCartItem(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        cartID, ok := parseUUIDParam(w, r, "cartId", "cart_id")
        if !ok {
                return
        }
        itemID, ok := parseUUIDParam(w, r, "itemId", "item_id")
        if !ok {
                return
        }
        var req models.UpdateCartItemRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        s := mustSession(r)
        cart, err := h.service.UpdateCartItem(r.Context(), s.UserID, s.Role, shopID, cartID, itemID, req.Quantity)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToCartResponse(cart))
}

// RemoveCartItem handles DELETE /api/shops/{shopId}/carts/{cartId}/items/{itemId}.
func (h *OrderHandler) RemoveCartItem(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        cartID, ok := parseUUIDParam(w, r, "cartId", "cart_id")
        if !ok {
                return
        }
        itemID, ok := parseUUIDParam(w, r, "itemId", "item_id")
        if !ok {
                return
        }
        s := mustSession(r)
        cart, err := h.service.RemoveFromCart(r.Context(), s.UserID, s.Role, shopID, cartID, itemID)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToCartResponse(cart))
}

// ClearCart handles DELETE /api/shops/{shopId}/carts/{cartId}.
func (h *OrderHandler) ClearCart(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        cartID, ok := parseUUIDParam(w, r, "cartId", "cart_id")
        if !ok {
                return
        }
        s := mustSession(r)
        if err := h.service.ClearCart(r.Context(), s.UserID, s.Role, shopID, cartID); err != nil {
                writeOrderServiceError(w, err)
                return
        }
        w.WriteHeader(http.StatusNoContent)
}

// GenerateRecap handles POST /api/shops/{shopId}/carts/{cartId}/recap.
func (h *OrderHandler) GenerateRecap(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        cartID, ok := parseUUIDParam(w, r, "cartId", "cart_id")
        if !ok {
                return
        }
        var req models.RecapRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        zoneID, err := uuid.Parse(req.ZoneID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_zone_id", "zone_id doit être un UUID.")
                return
        }
        s := mustSession(r)
        recap, err := h.service.GenerateRecap(r.Context(), s.UserID, s.Role, shopID, cartID, zoneID, req.PaymentMode)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, recap)
}

// ============================================================================
// Orders
// ============================================================================

// ConfirmOrder handles POST /api/shops/{shopId}/orders.
func (h *OrderHandler) ConfirmOrder(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        // We accept the standard ConfirmOrderRequest + an optional auto_confirm
        // flag in the same body. We decode into a wrapper struct so we get
        // both in a single pass.
        var body struct {
                models.ConfirmOrderRequest
                AutoConfirm bool `json:"auto_confirm,omitempty"`
        }
        if err := decodeJSON(r, &body); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        req := body.ConfirmOrderRequest
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        cartID, err := uuid.Parse(req.CartID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_cart_id", "cart_id doit être un UUID.")
                return
        }
        zoneID, err := uuid.Parse(req.ZoneID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_zone_id", "zone_id doit être un UUID.")
                return
        }
        var customerID *uuid.UUID
        if req.CustomerID != "" {
                id, err := uuid.Parse(req.CustomerID)
                if err != nil {
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_customer_id",
                                "customer_id doit être un UUID.")
                        return
                }
                customerID = &id
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        order, err := h.service.ConfirmOrder(r.Context(), s.UserID, s.Role, shopID, services.ConfirmOrderRequest{
                CartID:          cartID,
                ZoneID:          zoneID,
                DeliveryAddress: req.DeliveryAddress,
                PaymentMode:     req.PaymentMode,
                PaymentStatus:   req.PaymentStatus,
                IdempotencyKey:  req.IdempotencyKey,
                CustomerID:      customerID,
                Note:            req.Note,
                AutoConfirm:     body.AutoConfirm,
        }, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        // Fetch items + events for the response.
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, order.ID)
        writeJSON(w, http.StatusCreated, models.ToOrderResponse(order, items, events))
}

// ListOrders handles GET /api/shops/{shopId}/orders.
func (h *OrderHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        params := models.ListOrdersParams{
                Page:          atoiOr(r.URL.Query().Get("page"), 1),
                Limit:         atoiOr(r.URL.Query().Get("limit"), 20),
                Status:        r.URL.Query().Get("status"),
                PaymentStatus: r.URL.Query().Get("payment_status"),
                CustomerID:    r.URL.Query().Get("customer_id"),
                Search:        r.URL.Query().Get("search"),
        }
        if from := r.URL.Query().Get("from"); from != "" {
                if t, err := time.Parse(time.RFC3339, from); err == nil {
                        params.From = &t
                }
        }
        if to := r.URL.Query().Get("to"); to != "" {
                if t, err := time.Parse(time.RFC3339, to); err == nil {
                        params.To = &t
                }
        }
        s := mustSession(r)
        items, total, err := h.service.ListOrders(r.Context(), s.UserID, s.Role, shopID, params)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        out := make([]models.OrderListItemResponse, 0, len(items))
        for _, it := range items {
                out = append(out, toOrderListItemResponse(&it))
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "orders": out,
                "total":  total,
                "page":   params.Page,
                "limit":  params.Limit,
        })
}

// OrderStats handles GET /api/shops/{shopId}/orders/stats.
func (h *OrderHandler) OrderStats(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        s := mustSession(r)
        stats, err := h.service.DashboardStats(r.Context(), s.UserID, s.Role, shopID)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, stats)
}

// GetOrder handles GET /api/shops/{shopId}/orders/{id}.
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        orderID, ok := parseUUIDParam(w, r, "id", "order_id")
        if !ok {
                return
        }
        s := mustSession(r)
        order, items, events, err := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ListOrderEvents handles GET /api/shops/{shopId}/orders/{id}/events.
func (h *OrderHandler) ListOrderEvents(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        orderID, ok := parseUUIDParam(w, r, "id", "order_id")
        if !ok {
                return
        }
        s := mustSession(r)
        events, err := h.service.ListEvents(r.Context(), s.UserID, s.Role, shopID, orderID)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        out := make([]models.OrderEventResponse, 0, len(events))
        for i := range events {
                ev := &events[i]
                var fromS *string
                if ev.FromStatus != nil {
                        str := string(*ev.FromStatus)
                        fromS = &str
                }
                var authorID *string
                if ev.AuthorID != nil {
                        str := ev.AuthorID.String()
                        authorID = &str
                }
                out = append(out, models.OrderEventResponse{
                        ID:         ev.ID.String(),
                        FromStatus: fromS,
                        ToStatus:   string(ev.ToStatus),
                        AuthorID:   authorID,
                        Reason:     ev.Reason,
                        CreatedAt:  ev.CreatedAt.UTC().Format(time.RFC3339),
                })
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "events": out,
                "total":  len(out),
        })
}

// ConfirmPendingOrder handles POST /api/shops/{shopId}/orders/{id}/confirm.
func (h *OrderHandler) ConfirmPendingOrder(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        orderID, ok := parseUUIDParam(w, r, "id", "order_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        order, err := h.service.ConfirmPendingOrder(r.Context(), s.UserID, s.Role, shopID, orderID, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// CancelOrder handles POST /api/shops/{shopId}/orders/{id}/cancel.
func (h *OrderHandler) CancelOrder(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        orderID, ok := parseUUIDParam(w, r, "id", "order_id")
        if !ok {
                return
        }
        var req models.CancelOrderRequest
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
        order, err := h.service.CancelOrder(r.Context(), s.UserID, s.Role, shopID, orderID, req.Reason, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// AdvanceOrder handles POST /api/shops/{shopId}/orders/{id}/advance.
func (h *OrderHandler) AdvanceOrder(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        orderID, ok := parseUUIDParam(w, r, "id", "order_id")
        if !ok {
                return
        }
        var req models.UpdateOrderStatusRequest
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
        order, err := h.service.AdvanceStatus(r.Context(), s.UserID, s.Role, shopID, orderID, req.Status, req.Reason, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// UpdatePayment handles PATCH /api/shops/{shopId}/orders/{id}/payment.
func (h *OrderHandler) UpdatePayment(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        orderID, ok := parseUUIDParam(w, r, "id", "order_id")
        if !ok {
                return
        }
        var req models.UpdatePaymentStatusRequest
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
        order, err := h.service.UpdatePaymentStatus(r.Context(), s.UserID, s.Role, shopID, orderID, req.PaymentStatus, req.PaymentReference, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// Helpers
// ============================================================================

// serviceUnavailable returns true (and writes a 503) when the order service
// is nil — this happens when the server boots in degraded mode.
func (h *OrderHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service commandes n'est pas disponible (base de données injoignable).")
        return true
}

// writeOrderServiceError maps service sentinel errors to HTTP responses.
func writeOrderServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrCartNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "cart_not_found", "Panier introuvable.")
        case errors.Is(err, services.ErrCartItemNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "cart_item_not_found", "Ligne de panier introuvable.")
        case errors.Is(err, services.ErrCartNotActive):
                writeErrorWithCode(w, http.StatusConflict, "cart_not_active",
                        "Le panier n'est plus actif (abandonné ou converti en commande).")
        case errors.Is(err, services.ErrOrderNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "order_not_found", "Commande introuvable.")
        case errors.Is(err, services.ErrInvalidTransition):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_transition",
                        "Cette transition de statut n'est pas autorisée par la machine à états.")
        case errors.Is(err, services.ErrInvalidPaymentTransition):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_payment_transition",
                        "Cette transition de statut de paiement n'est pas autorisée.")
        case errors.Is(err, services.ErrInsufficientStockOrder):
                writeErrorWithCode(w, http.StatusConflict, "insufficient_stock",
                        err.Error())
        case errors.Is(err, services.ErrEmptyCart):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "empty_cart",
                        "Le panier est vide — impossible de créer la commande.")
        case errors.Is(err, services.ErrInvalidPaymentModeOrder):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_payment_mode",
                        "Mode de paiement invalide.")
        case errors.Is(err, services.ErrInvalidPaymentStatus):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_payment_status",
                        "Statut de paiement invalide.")
        case errors.Is(err, services.ErrInvalidOrderStatus):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_order_status",
                        "Statut de commande invalide.")
        case errors.Is(err, services.ErrCustomerRequired):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "customer_required",
                        "L'identifiant client est requis (le panier n'a pas de client).")
        case errors.Is(err, services.ErrVariantNotActive):
                writeErrorWithCode(w, http.StatusConflict, "variant_not_active",
                        "La variante n'est plus active.")
        case errors.Is(err, services.ErrVariantNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "variant_not_found", "Variante introuvable.")
        case errors.Is(err, services.ErrZoneNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "zone_not_found", "Zone de livraison introuvable.")
        case errors.Is(err, services.ErrZoneInactive):
                writeErrorWithCode(w, http.StatusConflict, "zone_inactive",
                        "La zone de livraison est inactive.")
        case errors.Is(err, services.ErrOrderBelowMin):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "order_below_minimum",
                        "Le montant de la commande est inférieur au minimum de la zone.")
        case errors.Is(err, services.ErrInvalidQuantity):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_quantity",
                        "La quantité doit être supérieure à zéro.")
        default:
                slog.Error("order service error", "error", err, "error_type", err.Error())
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}

// toOrderListItemResponse converts a repository.OrderListItem into the wire
// shape.
func toOrderListItemResponse(it *services.OrderListItem) models.OrderListItemResponse {
        // services.OrderListItem is a type alias for repository.OrderListItem
        // — we accept the pointer here and convert to the wire shape.
        if it == nil {
                return models.OrderListItemResponse{}
        }
        o := it.Order
        var customerID string
        if o.CustomerID != uuid.Nil {
                customerID = o.CustomerID.String()
        }
        return models.OrderListItemResponse{
                ID:            o.ID.String(),
                Number:        o.Number,
                Status:        string(o.Status),
                PaymentStatus: string(o.PaymentStatus),
                PaymentMode:   string(o.PaymentMode),
                CustomerID:    customerID,
                CustomerName:  it.CustomerName,
                ItemsCount:    it.ItemsCount,
                Total:         o.Total,
                CreatedAt:     o.CreatedAt.UTC().Format(time.RFC3339),
                UpdatedAt:     o.UpdatedAt.UTC().Format(time.RFC3339),
        }
}

// Re-export the OrderListItem type from services so the handler doesn't need
// to import repository directly.
//
// (services.OrderListItem is already a type alias declared in the services
// package via `type OrderListItem = repository.OrderListItem` — we don't
// re-declare it here. The toOrderListItemResponse signature just uses the
// alias.)

// unused-import guard
var _ = middleware.RequireAuth
var _ = strconv.Atoi
