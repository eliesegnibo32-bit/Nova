// NOVA v3 — Product options handlers (spec section 3).
//
// CRUD for product_options (plats/accompagnements/boissons).
// Routes:
//   POST   /api/shops/{shopId}/products/{productId}/options
//   POST   /api/shops/{shopId}/options                     (standalone — no product_id)
//   GET    /api/shops/{shopId}/options?type=plat|accompagnement|boisson
//   GET    /api/shops/{shopId}/options/{id}
//   PATCH  /api/shops/{shopId}/options/{id}
//   DELETE /api/shops/{shopId}/options/{id}
package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"nova-api/internal/models"
	"nova-api/internal/repository"
	"nova-api/internal/services"
)

// ProductOptionHandler bundles the product option HTTP handlers.
type ProductOptionHandler struct {
	repo *repository.ProductOptionRepository
}

// NewProductOptionHandler returns a ProductOptionHandler bound to the repo.
func NewProductOptionHandler(repo *repository.ProductOptionRepository) *ProductOptionHandler {
	return &ProductOptionHandler{repo: repo}
}

// Register adds the product option routes to the given chi.Router.
func (h *ProductOptionHandler) Register(r chi.Router) {
	// Per-product options (create under a product).
	r.Post("/products/{productId}/options", h.CreateForProduct)
	// Shop-level options (standalone or list).
	r.Post("/options", h.CreateStandalone)
	r.Get("/options", h.List)
	r.Get("/options/{id}", h.Get)
	r.Patch("/options/{id}", h.Update)
	r.Delete("/options/{id}", h.Delete)
}

// CreateForProduct handles POST /api/shops/{shopId}/products/{productId}/options.
func (h *ProductOptionHandler) CreateForProduct(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, true)
}

// CreateStandalone handles POST /api/shops/{shopId}/options.
func (h *ProductOptionHandler) CreateStandalone(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, false)
}

func (h *ProductOptionHandler) create(w http.ResponseWriter, r *http.Request, withProduct bool) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	var req models.CreateProductOptionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
			"Le corps de la requête est invalide: "+err.Error())
		return
	}
	if err := validateStruct(req); err != nil {
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
		return
	}
	var productID *uuid.UUID
	if withProduct {
		pid, ok := parseUUIDParam(w, r, "productId", "product_id")
		if !ok {
			return
		}
		productID = &pid
	}
	s := mustSession(r)
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	stockMode := req.StockMode
	if stockMode == "" {
		stockMode = string(models.StockModeQuantite)
	}
	opt, err := h.repo.Create(r.Context(), shopID, s.UserID, s.Role, repository.CreateProductOptionInput{
		ProductID: productID,
		Type:      req.Type,
		Name:      req.Name,
		Price:     req.Price,
		StockMode: stockMode,
		StockQty:  req.StockQty,
		Active:    active,
	})
	if err != nil {
		writeProductOptionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, models.ToProductOptionResponse(opt))
}

// List handles GET /api/shops/{shopId}/options?type=plat|accompagnement|boisson.
func (h *ProductOptionHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	optType := r.URL.Query().Get("type")
	if optType != "" && !models.IsValidProductOptionType(optType) {
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_type",
			"Le type doit être plat, accompagnement ou boisson.")
		return
	}
	onlyActive := r.URL.Query().Get("active") == "true"
	s := mustSession(r)
	opts, err := h.repo.ListByShop(r.Context(), shopID, s.UserID, s.Role, optType, onlyActive)
	if err != nil {
		writeProductOptionError(w, err)
		return
	}
	out := make([]models.ProductOptionResponse, 0, len(opts))
	for i := range opts {
		out = append(out, models.ToProductOptionResponse(&opts[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"options": out,
		"total":   len(out),
	})
}

// Get handles GET /api/shops/{shopId}/options/{id}.
func (h *ProductOptionHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "option_id")
	if !ok {
		return
	}
	s := mustSession(r)
	opt, err := h.repo.GetByID(r.Context(), shopID, s.UserID, s.Role, id)
	if err != nil {
		writeProductOptionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ToProductOptionResponse(opt))
}

// Update handles PATCH /api/shops/{shopId}/options/{id}.
func (h *ProductOptionHandler) Update(w http.ResponseWriter, r *http.Request) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "option_id")
	if !ok {
		return
	}
	var req models.UpdateProductOptionRequest
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
	opt, err := h.repo.Update(r.Context(), shopID, s.UserID, s.Role, id, repository.UpdateProductOptionInput{
		Name:      req.Name,
		Price:     req.Price,
		StockMode: req.StockMode,
		StockQty:  req.StockQty,
		Active:    req.Active,
	})
	if err != nil {
		writeProductOptionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ToProductOptionResponse(opt))
}

// Delete handles DELETE /api/shops/{shopId}/options/{id}.
func (h *ProductOptionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id", "option_id")
	if !ok {
		return
	}
	s := mustSession(r)
	if err := h.repo.Delete(r.Context(), shopID, s.UserID, s.Role, id); err != nil {
		writeProductOptionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ----------------------------------------------------------------

func (h *ProductOptionHandler) serviceUnavailable(w http.ResponseWriter) bool {
	if h.repo != nil {
		return false
	}
	writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
		"Le service options n'est pas disponible (base de données injoignable).")
	return true
}

func writeProductOptionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrProductOptionNotFound):
		writeErrorWithCode(w, http.StatusNotFound, "option_not_found", "Option introuvable.")
	case errors.Is(err, repository.ErrInsufficientStock):
		writeErrorWithCode(w, http.StatusConflict, "insufficient_stock",
			"Stock insuffisant pour cette option.")
	default:
		slog.Error("product option error", "error", err)
		writeErrorWithCode(w, http.StatusInternalServerError, "internal",
			"Une erreur est survenue. Réessayez.")
	}
}

// unused-import guard
var _ = services.ErrCartNotFound
