// NOVA v3 — Order handlers for the new state machine (spec section 1).
//
// Each handler:
//  1. Extracts {shopId} from the URL and validates it against the session.
//  2. Decodes the JSON body (if any) into a models DTO.
//  3. Calls into the order service for the v3 business logic.
//  4. Translates the service result / sentinel error into an HTTP response.
package handlers

import (
        "errors"
        "net/http"

        "github.com/google/uuid"

        "nova-api/internal/models"
        "nova-api/internal/services"
)

// ============================================================================
// V3 — ConfirmOrderV3 (create as en_attente_confirmation + reserve stock)
// ============================================================================

// ConfirmOrderV3Request is the body of POST /api/shops/{shopId}/orders/{id}/v3/create.
// The cart_id comes from the URL (we use the {id} placeholder for it on this
// particular endpoint, since the order doesn't exist yet).
//
// Actually, to keep the API consistent, we accept the cart_id in the body
// (the {id} in the URL is ignored for this endpoint — we use it because chi
// requires a placeholder).
type ConfirmOrderV3RequestBody struct {
        CartID          string `json:"cart_id"          validate:"required,uuid"`
        ZoneID          string `json:"zone_id"          validate:"required,uuid"`
        DeliveryAddress string `json:"delivery_address" validate:"required,min=3,max=500"`
        PaymentMode     string `json:"payment_mode"     validate:"required,oneof=cash mobile_money wave orange_money mtn_momo"`
        IdempotencyKey  string `json:"idempotency_key"  validate:"required,min=1,max=200"`
        CustomerID      string `json:"customer_id,omitempty" validate:"omitempty,uuid"`
        Note            string `json:"note,omitempty"         validate:"omitempty,max=1000"`
        // Client info (REQUIRED per spec section 5)
        ClientNom       string `json:"client_nom"       validate:"required,min=1,max=200"`
        ClientPrenom    string `json:"client_prenom,omitempty" validate:"omitempty,max=200"`
        ClientTelephone string `json:"client_telephone" validate:"required,min=4,max=30"`
        ClientLieu      string `json:"client_lieu"      validate:"required,min=2,max=200"`
}

// ConfirmOrderV3 handles POST /api/shops/{shopId}/orders/{id}/v3/create.
// Creates an order from a cart as en_attente_confirmation + reserves stock.
func (h *OrderHandler) ConfirmOrderV3(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var body ConfirmOrderV3RequestBody
        if err := decodeJSON(r, &body); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(body); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        cartID, err := uuid.Parse(body.CartID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_cart_id", "cart_id doit être un UUID.")
                return
        }
        zoneID, err := uuid.Parse(body.ZoneID)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_zone_id", "zone_id doit être un UUID.")
                return
        }
        var customerID *uuid.UUID
        if body.CustomerID != "" {
                id, err := uuid.Parse(body.CustomerID)
                if err != nil {
                        writeErrorWithCode(w, http.StatusBadRequest, "invalid_customer_id",
                                "customer_id doit être un UUID.")
                        return
                }
                customerID = &id
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        order, err := h.service.ConfirmOrderV3(r.Context(), s.UserID, s.Role, shopID, services.ConfirmOrderV3Request{
                CartID:          cartID,
                ZoneID:          zoneID,
                DeliveryAddress: body.DeliveryAddress,
                PaymentMode:     body.PaymentMode,
                IdempotencyKey:  body.IdempotencyKey,
                CustomerID:      customerID,
                Note:            body.Note,
                ClientNom:       body.ClientNom,
                ClientPrenom:    body.ClientPrenom,
                ClientTelephone: body.ClientTelephone,
                ClientLieu:      body.ClientLieu,
        }, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, order.ID)
        writeJSON(w, http.StatusCreated, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — MerchantConfirmOrder (en_attente_confirmation → en_attente_paiement OR en_cours)
// ============================================================================

// MerchantConfirmOrder handles POST /api/shops/{shopId}/orders/{id}/merchant-confirm.
func (h *OrderHandler) MerchantConfirmOrder(w http.ResponseWriter, r *http.Request) {
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
        order, err := h.service.MerchantConfirmOrder(r.Context(), s.UserID, s.Role, shopID, orderID, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — MerchantRefuseOrder (en_attente_confirmation → refusee — release stock)
// ============================================================================

// MerchantRefuseOrderRequest is the body of POST /api/shops/{shopId}/orders/{id}/refuse.
type MerchantRefuseOrderRequest struct {
        Reason string `json:"reason,omitempty" validate:"omitempty,max=500"`
}

// MerchantRefuseOrder handles POST /api/shops/{shopId}/orders/{id}/refuse.
func (h *OrderHandler) MerchantRefuseOrder(w http.ResponseWriter, r *http.Request) {
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
        var req MerchantRefuseOrderRequest
        _ = decodeJSON(r, &req) // optional body
        s := mustSession(r)
        ip, ua := clientInfo(r)
        order, err := h.service.MerchantRefuseOrder(r.Context(), s.UserID, s.Role, shopID, orderID, req.Reason, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — ClientSignalPayment (en_attente_paiement → paiement_signalé)
// ============================================================================

// ClientSignalPaymentRequest is the body of POST /api/shops/{shopId}/orders/{id}/signal-payment.
type ClientSignalPaymentRequest struct {
        PaymentReference string `json:"payment_reference,omitempty" validate:"omitempty,max=200"`
}

// ClientSignalPayment handles POST /api/shops/{shopId}/orders/{id}/signal-payment.
func (h *OrderHandler) ClientSignalPayment(w http.ResponseWriter, r *http.Request) {
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
        var req ClientSignalPaymentRequest
        _ = decodeJSON(r, &req) // optional body
        s := mustSession(r)
        ip, ua := clientInfo(r)
        order, err := h.service.ClientSignalPayment(r.Context(), s.UserID, s.Role, shopID, orderID, req.PaymentReference, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — MerchantConfirmPayment (paiement_signalé → en_cours)
// ============================================================================

// MerchantConfirmPayment handles POST /api/shops/{shopId}/orders/{id}/confirm-payment.
// CRITICAL: only the merchant can confirm a payment. NOVA never auto-confirms.
func (h *OrderHandler) MerchantConfirmPayment(w http.ResponseWriter, r *http.Request) {
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
        order, err := h.service.MerchantConfirmPayment(r.Context(), s.UserID, s.Role, shopID, orderID, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — MerchantRefusePayment (paiement_signalé → en_attente_paiement OR refusee)
// ============================================================================

// MerchantRefusePaymentRequest is the body of POST /api/shops/{shopId}/orders/{id}/refuse-payment.
type MerchantRefusePaymentRequest struct {
        Cancel bool   `json:"cancel,omitempty"` // true → refusee + release stock; false → back to en_attente_paiement
        Reason string `json:"reason,omitempty" validate:"omitempty,max=500"`
}

// MerchantRefusePayment handles POST /api/shops/{shopId}/orders/{id}/refuse-payment.
func (h *OrderHandler) MerchantRefusePayment(w http.ResponseWriter, r *http.Request) {
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
        var req MerchantRefusePaymentRequest
        _ = decodeJSON(r, &req) // optional body
        s := mustSession(r)
        ip, ua := clientInfo(r)
        order, err := h.service.MerchantRefusePayment(r.Context(), s.UserID, s.Role, shopID, orderID, req.Cancel, req.Reason, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — MerchantMarkReady (en_cours → prete)
// ============================================================================

// MerchantMarkReady handles POST /api/shops/{shopId}/orders/{id}/ready.
func (h *OrderHandler) MerchantMarkReady(w http.ResponseWriter, r *http.Request) {
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
        order, err := h.service.MerchantMarkReady(r.Context(), s.UserID, s.Role, shopID, orderID, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — MerchantComplete (prete → terminee)
// ============================================================================

// MerchantComplete handles POST /api/shops/{shopId}/orders/{id}/complete.
func (h *OrderHandler) MerchantComplete(w http.ResponseWriter, r *http.Request) {
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
        order, err := h.service.MerchantComplete(r.Context(), s.UserID, s.Role, shopID, orderID, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// ============================================================================
// V3 — CancelOrderV3 (any v3 status → annulee — REINSTATE/RELEASE stock + REMOVE revenue)
// ============================================================================

// CancelOrderV3Request is the body of POST /api/shops/{shopId}/orders/{id}/cancel-v3.
type CancelOrderV3Request struct {
        Reason string `json:"reason" validate:"required,min=3,max=500"`
}

// CancelOrderV3 handles POST /api/shops/{shopId}/orders/{id}/cancel-v3.
func (h *OrderHandler) CancelOrderV3(w http.ResponseWriter, r *http.Request) {
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
        var req CancelOrderV3Request
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
        order, err := h.service.CancelOrderV3(r.Context(), s.UserID, s.Role, shopID, orderID, req.Reason, ip, ua)
        if err != nil {
                writeOrderServiceError(w, err)
                return
        }
        _, items, events, _ := h.service.GetOrder(r.Context(), s.UserID, s.Role, shopID, orderID)
        writeJSON(w, http.StatusOK, models.ToOrderResponse(order, items, events))
}

// unused-import guard
var _ = errors.New
