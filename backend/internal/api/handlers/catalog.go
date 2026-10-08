// Catalog handlers — product + variant + image endpoints for NOVA.
//
// All routes are shop-scoped: they mount under `/api/shops/{shopId}/products`.
// The {shopId} URL parameter is validated against the session's
// current_shop_id (defense in depth — the URL shopId MUST match the
// session shopId or we return 403). RLS provides a second layer of
// protection at the DB level.
//
// Each handler:
//  1. Extracts {shopId} from the URL and validates it against the session.
//  2. Decodes the JSON body into a models/catalog.go DTO.
//  3. Optionally runs go-playground/validator for cheap syntactic checks.
//  4. Calls into the catalog service for the business logic.
//  5. Translates the service result / sentinel error into an HTTP response.
package handlers

import (
        "errors"
        "log/slog"
        "net/http"

        "github.com/go-chi/chi/v5"
        "github.com/google/uuid"

        "nova-api/internal/api/middleware"
        "nova-api/internal/auth"
        "nova-api/internal/models"
        "nova-api/internal/services"
)

// CatalogHandler bundles the catalog HTTP handlers with their shared
// dependency: the catalog service.
type CatalogHandler struct {
        service *services.CatalogService
}

// NewCatalogHandler returns a CatalogHandler bound to the given service.
func NewCatalogHandler(service *services.CatalogService) *CatalogHandler {
        return &CatalogHandler{service: service}
}

// Router returns a chi.Router pre-wired with all product/variant/image
// routes. This is suitable for standalone mounting; for the shop-scoped
// route group, use Register() instead (the parent group applies the
// auth + shop context middleware once for all shop-scoped handlers).
func (h *CatalogHandler) Router() chi.Router {
        r := chi.NewRouter()
        r.Use(middleware.RequireAuth, middleware.ShopContext)
        h.Register(r)
        return r
}

// Register adds the catalog routes to the given chi.Router. The caller is
// responsible for applying the auth + shop context middleware on the
// parent router.
func (h *CatalogHandler) Register(r chi.Router) {
        // Products
        r.Post("/products", h.CreateProduct)
        r.Get("/products", h.ListProducts)
        r.Get("/products/stats", h.ProductStats)
        r.Get("/products/{id}", h.GetProduct)
        r.Patch("/products/{id}", h.UpdateProduct)
        r.Delete("/products/{id}", h.DeleteProduct)
        r.Post("/products/{id}/publish", h.PublishProduct)
        r.Post("/products/{id}/archive", h.ArchiveProduct)

        // Variants
        r.Post("/products/{id}/variants", h.CreateVariant)
        r.Patch("/products/{id}/variants/{variantId}", h.UpdateVariant)
        r.Delete("/products/{id}/variants/{variantId}", h.DeleteVariant)

        // Images
        r.Post("/products/{id}/images", h.AddImage)
        r.Delete("/products/{id}/images/{imageId}", h.RemoveImage)
}

// CreateProduct handles POST /api/shops/{shopId}/products.
func (h *CatalogHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        var req models.CreateProductRequest
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
        result, err := h.service.CreateProduct(r.Context(), s.UserID, s.Role, shopID, req, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        resp := models.ToProductResponse(&result.Product, result.Variants, result.Images)
        writeJSON(w, http.StatusCreated, resp)
}

// ListProducts handles GET /api/shops/{shopId}/products.
func (h *CatalogHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        params := models.ListProductsParams{
                Page:     atoiOr(r.URL.Query().Get("page"), 1),
                Limit:    atoiOr(r.URL.Query().Get("limit"), 20),
                Search:   r.URL.Query().Get("search"),
                Category: r.URL.Query().Get("category"),
                Status:   r.URL.Query().Get("status"),
        }
        s := mustSession(r)
        items, total, err := h.service.ListProducts(r.Context(), s.UserID, s.Role, shopID, params)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        out := make([]models.ProductListItemResponse, 0, len(items))
        for i := range items {
                out = append(out, toItemResponse(&items[i]))
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "products": out,
                "total":    total,
                "page":     params.Page,
                "limit":    params.Limit,
        })
}

// ProductStats is a placeholder — clients should use GET /inventory/stats.
func (h *CatalogHandler) ProductStats(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        if _, ok := requireShopMatch(w, r); !ok {
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":      true,
                "message": "Use GET /inventory/stats for stock dashboard stats.",
        })
}

// GetProduct handles GET /api/shops/{shopId}/products/{id}.
func (h *CatalogHandler) GetProduct(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        s := mustSession(r)
        result, err := h.service.GetProduct(r.Context(), s.UserID, s.Role, shopID, productID)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToProductResponse(&result.Product, result.Variants, result.Images))
}

// UpdateProduct handles PATCH /api/shops/{shopId}/products/{id}.
func (h *CatalogHandler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        var req models.UpdateProductRequest
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
        p, err := h.service.UpdateProduct(r.Context(), s.UserID, s.Role, shopID, productID, req, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        // Fetch variants + images so the response is consistent with GetProduct.
        result, _ := h.service.GetProduct(r.Context(), s.UserID, s.Role, shopID, productID)
        if result != nil {
                writeJSON(w, http.StatusOK, models.ToProductResponse(p, result.Variants, result.Images))
                return
        }
        writeJSON(w, http.StatusOK, models.ToProductResponse(p, nil, nil))
}

// DeleteProduct handles DELETE /api/shops/{shopId}/products/{id}.
func (h *CatalogHandler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        if err := h.service.DeleteProduct(r.Context(), s.UserID, s.Role, shopID, productID, ip, ua); err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        w.WriteHeader(http.StatusNoContent)
}

// PublishProduct handles POST /api/shops/{shopId}/products/{id}/publish.
func (h *CatalogHandler) PublishProduct(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        p, err := h.service.PublishProduct(r.Context(), s.UserID, s.Role, shopID, productID, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        result, _ := h.service.GetProduct(r.Context(), s.UserID, s.Role, shopID, productID)
        if result != nil {
                writeJSON(w, http.StatusOK, models.ToProductResponse(p, result.Variants, result.Images))
                return
        }
        writeJSON(w, http.StatusOK, models.ToProductResponse(p, nil, nil))
}

// ArchiveProduct handles POST /api/shops/{shopId}/products/{id}/archive.
func (h *CatalogHandler) ArchiveProduct(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        p, err := h.service.ArchiveProduct(r.Context(), s.UserID, s.Role, shopID, productID, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        result, _ := h.service.GetProduct(r.Context(), s.UserID, s.Role, shopID, productID)
        if result != nil {
                writeJSON(w, http.StatusOK, models.ToProductResponse(p, result.Variants, result.Images))
                return
        }
        writeJSON(w, http.StatusOK, models.ToProductResponse(p, nil, nil))
}

// CreateVariant handles POST /api/shops/{shopId}/products/{id}/variants.
func (h *CatalogHandler) CreateVariant(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        var req models.CreateVariantRequest
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
        v, err := h.service.CreateVariant(r.Context(), s.UserID, s.Role, shopID, productID, req, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusCreated, models.ToVariantResponse(v))
}

// UpdateVariant handles PATCH /api/shops/{shopId}/products/{id}/variants/{variantId}.
func (h *CatalogHandler) UpdateVariant(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        // The {id} (product ID) isn't actually needed — variants are
        // globally addressed by their own UUID under RLS.
        _, ok = parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        var req models.UpdateVariantRequest
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
        v, err := h.service.UpdateVariant(r.Context(), s.UserID, s.Role, shopID, variantID, req, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusOK, models.ToVariantResponse(v))
}

// DeleteVariant handles DELETE /api/shops/{shopId}/products/{id}/variants/{variantId}.
func (h *CatalogHandler) DeleteVariant(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        _, ok = parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        variantID, ok := parseUUIDParam(w, r, "variantId", "variant_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        if err := h.service.DeleteVariant(r.Context(), s.UserID, s.Role, shopID, variantID, ip, ua); err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        w.WriteHeader(http.StatusNoContent)
}

// AddImage handles POST /api/shops/{shopId}/products/{id}/images.
func (h *CatalogHandler) AddImage(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        productID, ok := parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        var req struct {
                URL string `json:"url" validate:"required,max=2048"`
                Ord int    `json:"ord,omitempty"`
        }
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
        img, err := h.service.AddImage(r.Context(), s.UserID, s.Role, shopID, productID, req.URL, req.Ord, ip, ua)
        if err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        writeJSON(w, http.StatusCreated, img)
}

// RemoveImage handles DELETE /api/shops/{shopId}/products/{id}/images/{imageId}.
func (h *CatalogHandler) RemoveImage(w http.ResponseWriter, r *http.Request) {
        if h.serviceUnavailable(w) {
                return
        }
        shopID, ok := requireShopMatch(w, r)
        if !ok {
                return
        }
        _, ok = parseUUIDParam(w, r, "id", "product_id")
        if !ok {
                return
        }
        imageID, ok := parseUUIDParam(w, r, "imageId", "image_id")
        if !ok {
                return
        }
        s := mustSession(r)
        ip, ua := clientInfo(r)
        if err := h.service.RemoveImage(r.Context(), s.UserID, s.Role, shopID, imageID, ip, ua); err != nil {
                writeCatalogServiceError(w, err)
                return
        }
        w.WriteHeader(http.StatusNoContent)
}

// --- helpers ----------------------------------------------------------------

// serviceUnavailable returns true (and writes a 503) when the catalog
// service is nil — this happens when the server boots in degraded mode.
func (h *CatalogHandler) serviceUnavailable(w http.ResponseWriter) bool {
        if h.service != nil {
                return false
        }
        writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
                "Le service catalogue n'est pas disponible (base de données injoignable).")
        return true
}

// writeCatalogServiceError maps service sentinel errors to HTTP responses.
func writeCatalogServiceError(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, services.ErrProductNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "product_not_found",
                        "Produit introuvable.")
        case errors.Is(err, services.ErrVariantNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "variant_not_found",
                        "Variante introuvable.")
        case errors.Is(err, services.ErrImageNotFound):
                writeErrorWithCode(w, http.StatusNotFound, "image_not_found",
                        "Image introuvable.")
        case errors.Is(err, services.ErrSKUTaken):
                writeErrorWithCode(w, http.StatusConflict, "sku_taken",
                        "Ce SKU est déjà utilisé par une autre variante de la boutique.")
        case errors.Is(err, services.ErrInvalidPromoPrice):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_promo_price",
                        "Le prix promo doit être strictement inférieur au prix normal.")
        case errors.Is(err, services.ErrInvalidPromoDates):
                writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_promo_dates",
                        "promo_start et promo_end doivent être tous les deux renseignés (et start < end).")
        case errors.Is(err, services.ErrCannotDeleteVariant):
                writeErrorWithCode(w, http.StatusConflict, "variant_has_reserved_stock",
                        "Impossible de supprimer une variante avec du stock réservé.")
        default:
                slog.Error("catalog service error", "error", err, "error_type", errTypeName(err))
                writeErrorWithCode(w, http.StatusInternalServerError, "internal",
                        "Une erreur est survenue. Réessayez.")
        }
}

// requireShopMatch extracts {shopId} from the URL, parses it as a UUID,
// and verifies it matches the session's current_shop_id. Returns the
// matched shopID + true on success, or (zero, false) after writing a 403.
//
// Defense in depth: even if a malicious client tampers with the URL to
// point at another shop, RLS would still block access at the DB layer.
// This check just gives us a clean 403 instead of a noisy RLS violation.
func requireShopMatch(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
        raw := chi.URLParam(r, "shopId")
        if raw == "" {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_shop_id",
                        "ID de boutique manquant dans l'URL.")
                return uuid.Nil, false
        }
        urlShopID, err := uuid.Parse(raw)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_shop_id",
                        "ID de boutique invalide (UUID attendu).")
                return uuid.Nil, false
        }
        s, ok := middleware.SessionFromContext(r.Context())
        if !ok || s == nil || s.ShopID == nil {
                writeErrorWithCode(w, http.StatusForbidden, "no_active_shop",
                        "Aucune boutique active dans la session.")
                return uuid.Nil, false
        }
        if *s.ShopID != urlShopID {
                writeErrorWithCode(w, http.StatusForbidden, "shop_mismatch",
                        "L'ID de boutique dans l'URL ne correspond pas à la boutique active de la session.")
                return uuid.Nil, false
        }
        return urlShopID, true
}

// parseUUIDParam extracts a named URL parameter and parses it as a UUID.
// `code` is the suffix of the error code returned on failure
// (e.g. "product_id" → "invalid_product_id").
func parseUUIDParam(w http.ResponseWriter, r *http.Request, param, code string) (uuid.UUID, bool) {
        raw := chi.URLParam(r, param)
        if raw == "" {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_"+code,
                        "Paramètre "+param+" manquant.")
                return uuid.Nil, false
        }
        id, err := uuid.Parse(raw)
        if err != nil {
                writeErrorWithCode(w, http.StatusBadRequest, "invalid_"+code,
                        "Paramètre "+param+" invalide (UUID attendu).")
                return uuid.Nil, false
        }
        return id, true
}

// errTypeName returns the type name of err for logging.
func errTypeName(err error) string {
        if err == nil {
                return ""
        }
        // Cheap %T on the error.
        return fmtErrType(err)
}

// fmtErrType isolates fmt.Sprint so this file doesn't need to import fmt
// just for one call.
func fmtErrType(err error) string { return err.Error() }

// toItemResponse converts a repository.ProductListItem into the wire shape.
// It accepts the struct by value because the slice element type is the
// concrete repository.ProductListItem — we use a structural alias here so
// the handlers package doesn't import repository (which would create a
// cycle: repository imports models; handlers imports models; we want to
// keep the handler → repository dependency flowing through services).
func toItemResponse(it *services.ProductListItem) models.ProductListItemResponse {
        if it == nil {
                return models.ProductListItemResponse{}
        }
        p := it.Product
        return models.ProductListItemResponse{
                ID:           p.ID.String(),
                Name:         p.Name,
                Category:     p.Category,
                Brand:        p.Brand,
                Status:       string(p.Status),
                VariantCount: it.VariantCount,
                FirstImage:   it.FirstImage,
                CreatedAt:    p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
                UpdatedAt:    p.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
        }
}

// auth import is used by mustSession which is defined in shops.go; keep
// the unused-import linter happy by referencing the package symbol here.
var _ = auth.CookieName
