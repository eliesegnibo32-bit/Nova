// Stock handlers — inventory + stock movement endpoints for NOVA.
//
// All routes are shop-scoped: they mount under
// `/api/shops/{shopId}/inventory`. The {shopId} URL parameter is
// validated against the session's current_shop_id (defense in depth).
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

// StockHandler bundles the stock HTTP handlers with their shared dependency.
type StockHandler struct {
        service *services.StockService
}

// NewStockHandler returns a StockHandler bound to the given service.
func NewStockHandler(service *services.StockService) *StockHandler {
        return &StockHandler{service: service}
}

// Router returns a chi.Router pre-wired with all inventory/movement
// routes. For the shop-scoped route group, use Register() instead.
func (h *StockHandler) Router() chi.Router {
        r := chi.NewRouter()
        r.Use(middleware.RequireAuth, middleware.ShopContext)
        h.Register(r)
        return r
}

// Register adds the stock routes to the given chi.Router.
func (h *StockHandler) Register(r chi.Router) {
        // Stats first so it doesn't get shadowed by /inventory/{variantId}.
        r.Get("/inventory/stats", h.Stats)

        r.Get("/inventory", h.ListInventory)
        r.Get("/inventory/{variantId}", h.GetInventory)
        r.Post("/inventory/{variantId}/adjust", h.AdjustStock)
        r.Post("/inventory/{variantId}/receive", h.ReceiveStock)
        r.Patch("/inventory/{variantId}/threshold", h.SetThreshold)
        r.Patch("/inventory/{variantId}/mode", h.SetStockMode) // NOVA v3 — set stock_mode
        r.Get("/inventory/{variantId}/movements", h.ListMovements)
}

// ListInventory handles GET /api/shops/{shopId}/inventory.
func (h *StockHandler) ListInventory(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        params := models.ListInventoryParams{
                Page:   atoiOr(r.URL.Query().Get("page"), 1),
                Limit:  atoiOr(r.URL.Query().Get("limit"), 50),
                Filter: r.URL.Query().Get("filter"),
                Search: r.URL.Query().Get("search"),
        }
        s := mustSession(r)
        items, total, err := h.service.ListInventory(r.Context(), s.UserID, s.Role, shopID, params)
        if err != nil {
                writeStockServiceError(w, err)
                return
        }
        out := make([]models.InventoryWithVariantResponse, 0, len(items))
        for _, it := range items {
                out = append(out, toInventoryWithVariantResponse(it))
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "inventory": out,
                "total":     total,
                "page":      params.Page,
                "limit":     params.Limit,
        })
}

// GetInventory handles GET /api/shops/{shopId}/inventory/{variantId}.
func (h *StockHandler) GetInventory(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        s := mustSession(r)
        inv, err := h.service.GetInventory(r.Context(), s.UserID, s.Role, shopID, variantID)
        if err != nil {
                writeStockServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToInventoryResponse(inv))
}

// AdjustStock handles POST /api/shops/{shopId}/inventory/{variantId}/adjust.
func (h *StockHandler) AdjustStock(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        var req models.AdjustStockRequest
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
        inv, err := h.service.AdjustStock(r.Context(), s.UserID, s.Role, shopID, variantID, req, ip, ua)
        if err != nil {
                writeStockServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToInventoryResponse(inv))
}

// ReceiveStock handles POST /api/shops/{shopId}/inventory/{variantId}/receive.
func (h *StockHandler) ReceiveStock(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        var req models.ReceiveStockRequest
        if err := decodeJSON(r, &req); err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
                        "Le corps de la requête est invalide: "+err.Error())
                return
        }
        if err := validateStruct(req); err != nil {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
                return
        }
        if req.Quantity <= 0 {
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_quantity",
                        "La quantité doit être strictement positive.")
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        inv, err := h.service.ReceiveStock(r.Context(), s.UserID, s.Role, shopID, variantID, req, ip, ua)
        if err != nil {
                writeStockServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToInventoryResponse(inv))
}

// SetThreshold handles PATCH /api/shops/{shopId}/inventory/{variantId}/threshold.
func (h *StockHandler) SetThreshold(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        var req models.SetAlertThresholdRequest
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
        if err := h.service.SetAlertThreshold(r.Context(), s.UserID, s.Role, shopID, variantID, req.Threshold, ip, ua); err != nil {
                writeStockServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":        true,
                "threshold": req.Threshold,
        })
}

// ListMovements handles GET /api/shops/{shopId}/inventory/{variantId}/movements.
func (h *StockHandler) ListMovements(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        params := models.ListMovementsParams{
                Page:  atoiOr(r.URL.Query().Get("page"), 1),
                Limit: atoiOr(r.URL.Query().Get("limit"), 50),
                Type:  r.URL.Query().Get("type"),
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
        var vid *uuid.UUID
        // variantID is always provided in this route — pass it through.
        vid = &variantID
        movements, total, err := h.service.ListMovements(r.Context(), s.UserID, s.Role, shopID, vid, params)
        if err != nil {
                writeStockServiceError(w, err)
                return
        }
        out := make([]models.StockMovementResponse, 0, len(movements))
        for i := range movements {
                out = append(out, models.ToStockMovementResponse(&movements[i]))
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "movements": out,
                "total":     total,
                "page":      params.Page,
                "limit":     params.Limit,
        })
}

// Stats handles GET /api/shops/{shopId}/inventory/stats.
func (h *StockHandler) Stats(w http.ResponseWriter, r *http.Request) {
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
                writeStockServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, stats)
}

// --- helpers ----------------------------------------------------------------

func (h *StockHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service de stock n'est pas disponible (base de données injoignable).")
        return true
}

// writeStockServiceError maps service sentinel errors to HTTP responses.
func writeStockServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrInventoryNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "inventory_not_found",
                        "Inventaire introuvable pour cette variante.")
        case errors.Is(err, services.ErrInsufficientStock):
                writeErrorWithCode(w, http.StatusConflict, "insufficient_stock",
                        "Stock insuffisant pour cette opération.")
        case errors.Is(err, services.ErrInvalidQuantity):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_quantity",
                        "La quantité doit être strictement positive.")
        case errors.Is(err, services.ErrReasonRequired):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "reason_required",
                        "Un motif est obligatoire pour un ajustement de stock.")
        default:
                slog.Error("stock service error", "error", err)
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}

// toInventoryWithVariantResponse converts a services.InventoryWithVariant
// (mirrored from repository) into the wire shape.
func toInventoryWithVariantResponse(it services.InventoryWithVariant) models.InventoryWithVariantResponse {
        inv := models.ToInventoryResponse(&it.Inventory)
        return models.InventoryWithVariantResponse{
                InventoryResponse: inv,
                ProductID:         it.ProductID.String(),
                ProductName:       it.ProductName,
                SKU:               it.SKU,
                Size:              it.Size,
                Color:             it.Color,
                Price:             it.Price,
                Active:            it.Active,
        }
}

// strconv import shim — used via atoiOr from shops.go.
var _ = strconv.Atoi
