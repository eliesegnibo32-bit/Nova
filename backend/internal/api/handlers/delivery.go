// Delivery handlers — delivery zone endpoints for NOVA.
//
// All routes are shop-scoped: they mount under
// `/api/shops/{shopId}/delivery-zones`. The {shopId} URL parameter is
// validated against the session's current_shop_id (defense in depth).
package handlers

import (
        "errors"
        "log/slog"
        "net/http"

        "github.com/go-chi/chi/v5"
        "github.com/google/uuid"

        "nova-api/internal/api/middleware"
        "nova-api/internal/models"
        "nova-api/internal/services"
)

// DeliveryHandler bundles the delivery HTTP handlers with their shared
// dependency.
type DeliveryHandler struct {
        service *services.DeliveryService
}

// NewDeliveryHandler returns a DeliveryHandler bound to the given service.
func NewDeliveryHandler(service *services.DeliveryService) *DeliveryHandler {
        return &DeliveryHandler{service: service}
}

// Router returns a chi.Router pre-wired with all delivery-zone routes.
// For the shop-scoped route group, use Register() instead.
func (h *DeliveryHandler) Router() chi.Router {
        r := chi.NewRouter()
        r.Use(middleware.RequireAuth, middleware.ShopContext)
        h.Register(r)
        return r
}

// Register adds the delivery routes to the given chi.Router.
func (h *DeliveryHandler) Register(r chi.Router) {
        r.Post("/delivery-zones", h.CreateZone)
        r.Get("/delivery-zones", h.ListZones)
        r.Post("/delivery-zones/match", h.MatchZone) // placed before /{id}
        r.Get("/delivery-zones/{id}", h.GetZone)
        r.Patch("/delivery-zones/{id}", h.UpdateZone)
        r.Delete("/delivery-zones/{id}", h.DeleteZone)
        r.Post("/delivery-zones/{id}/calculate", h.CalculateFee)
}

// CreateZone handles POST /api/shops/{shopId}/delivery-zones.
func (h *DeliveryHandler) CreateZone(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req models.CreateDeliveryZoneRequest
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
        z, err := h.service.CreateZone(r.Context(), s.UserID, s.Role, shopID, req, ip, ua)
        if err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusCreated, models.ToDeliveryZoneResponse(z))
}

// ListZones handles GET /api/shops/{shopId}/delivery-zones.
func (h *DeliveryHandler) ListZones(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        onlyActive := r.URL.Query().Get("active") == "true"
        s := mustSession(r)
        zones, err := h.service.ListZones(r.Context(), s.UserID, s.Role, shopID, onlyActive)
        if err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        out := make([]models.DeliveryZoneResponse, 0, len(zones))
        for i := range zones {
                out = append(out, models.ToDeliveryZoneResponse(&zones[i]))
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "delivery_zones": out,
                "total":          len(out),
        })
}

// GetZone handles GET /api/shops/{shopId}/delivery-zones/{id}.
func (h *DeliveryHandler) GetZone(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        zoneID, ok := parseUUIDParam(w, r, "id", "zone_id")
        if !ok {
                return
        }
        s := mustSession(r)
        z, err := h.service.GetZone(r.Context(), s.UserID, s.Role, shopID, zoneID)
        if err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToDeliveryZoneResponse(z))
}

// UpdateZone handles PATCH /api/shops/{shopId}/delivery-zones/{id}.
func (h *DeliveryHandler) UpdateZone(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        zoneID, ok := parseUUIDParam(w, r, "id", "zone_id")
        if !ok {
                return
        }
        var req models.UpdateDeliveryZoneRequest
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
        z, err := h.service.UpdateZone(r.Context(), s.UserID, s.Role, shopID, zoneID, req, ip, ua)
        if err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToDeliveryZoneResponse(z))
}

// DeleteZone handles DELETE /api/shops/{shopId}/delivery-zones/{id}.
func (h *DeliveryHandler) DeleteZone(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        zoneID, ok := parseUUIDParam(w, r, "id", "zone_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        if err := h.service.DeleteZone(r.Context(), s.UserID, s.Role, shopID, zoneID, ip, ua); err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        w.WriteHeader(http.StatusNoContent)
}

// MatchZone handles POST /api/shops/{shopId}/delivery-zones/match.
//
// Used by the AI to find the right zone for a customer query like
// "Je suis à Cocody Angré". Returns 200 + zone if exactly one match,
// 404 if no match, 409 if ambiguous (the AI must ask for clarification).
func (h *DeliveryHandler) MatchZone(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req models.MatchZoneRequest
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
        z, err := h.service.MatchZone(r.Context(), s.UserID, s.Role, shopID, req.Query)
        if err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToDeliveryZoneResponse(z))
}

// CalculateFee handles POST /api/shops/{shopId}/delivery-zones/{id}/calculate.
func (h *DeliveryHandler) CalculateFee(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        zoneID, ok := parseUUIDParam(w, r, "id", "zone_id")
        if !ok {
                return
        }
        var req models.CalculateFeeRequest
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
        result, err := h.service.CalculateFee(r.Context(), s.UserID, s.Role, shopID, zoneID, req.OrderAmount)
        if err != nil {
                writeDeliveryServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.DeliveryFeeResult{
                ZoneID:       result.Zone.ID.String(),
                ZoneName:     result.Zone.Name,
                Fee:          result.Fee,
                FreeDelivery: result.FreeDelivery,
                OrderAmount:  result.OrderAmount,
                TotalPayable: result.TotalPayable,
        })
}

// --- helpers ----------------------------------------------------------------

func (h *DeliveryHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service de livraison n'est pas disponible (base de données injoignable).")
        return true
}

// writeDeliveryServiceError maps service sentinel errors to HTTP responses.
func writeDeliveryServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrZoneNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "zone_not_found",
                        "Zone de livraison introuvable.")
        case errors.Is(err, services.ErrZoneAmbiguous):
                writeErrorWithCode(w, http.StatusConflict, "zone_ambiguous",
                        "Plusieurs zones correspondent à cette requête — précisez le quartier.")
        case errors.Is(err, services.ErrZoneInactive):
                writeErrorWithCode(w, http.StatusConflict, "zone_inactive",
                        "Cette zone de livraison est désactivée.")
        case errors.Is(err, services.ErrOrderBelowMin):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "order_below_minimum",
                        "Le montant de la commande est inférieur au minimum requis pour cette zone.")
        case errors.Is(err, services.ErrInvalidFee):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_fee",
                        "Le tarif doit être supérieur ou égal à 0.")
        case errors.Is(err, services.ErrInvalidFreeFrom):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_free_from",
                        "Le seuil de gratuité doit être supérieur ou égal à 0.")
        default:
                slog.Error("delivery service error", "error", err)
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}

// Unused import shim — uuid is used via parseUUIDParam.
var _ = uuid.Nil
