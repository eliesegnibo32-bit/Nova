// NOVA v3 — Stock handler extension for setting stock_mode (spec section 2).
package handlers

import (
	"net/http"

	"nova-api/internal/models"
)

// SetStockMode handles PATCH /api/shops/{shopId}/inventory/{variantId}/mode.
// Sets the stock_mode of a variant (quantite | epuise | illimite).
func (h *StockHandler) SetStockMode(w http.ResponseWriter, r *http.Request) {
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
	var req models.SetStockModeRequest
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
	if err := h.service.SetStockMode(r.Context(), s.UserID, s.Role, shopID, variantID, req.StockMode, ip, ua); err != nil {
		writeStockServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.SetStockModeResponse{
		Ok:        true,
		StockMode: req.StockMode,
	})
}
