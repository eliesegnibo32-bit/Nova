// NOVA v3 — Payment config handlers (spec section 4).
//
// Routes:
//   GET /api/shops/{shopId}/payment-config
//   PUT /api/shops/{shopId}/payment-config
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"nova-api/internal/models"
	"nova-api/internal/repository"
)

// PaymentConfigHandler bundles the payment config HTTP handlers.
type PaymentConfigHandler struct {
	repo *repository.PaymentConfigRepository
}

// NewPaymentConfigHandler returns a PaymentConfigHandler bound to the repo.
func NewPaymentConfigHandler(repo *repository.PaymentConfigRepository) *PaymentConfigHandler {
	return &PaymentConfigHandler{repo: repo}
}

// Register adds the payment config routes to the given chi.Router.
func (h *PaymentConfigHandler) Register(r chi.Router) {
	r.Get("/payment-config", h.Get)
	r.Put("/payment-config", h.Update)
}

// Get handles GET /api/shops/{shopId}/payment-config.
// Returns the shop's payment config (creates a default one if it doesn't exist).
func (h *PaymentConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	s := mustSession(r)
	cfg, err := h.repo.GetOrCreateByShop(r.Context(), shopID, s.UserID, s.Role)
	if err != nil {
		writeErrorWithCode(w, http.StatusInternalServerError, "internal",
			"Une erreur est survenue. Réessayez.")
		return
	}
	writeJSON(w, http.StatusOK, models.ToPaymentConfigResponse(cfg))
}

// Update handles PUT /api/shops/{shopId}/payment-config.
func (h *PaymentConfigHandler) Update(w http.ResponseWriter, r *http.Request) {
	if h.serviceUnavailable(w) {
		return
	}
	shopID, ok := requireShopMatch(w, r)
	if !ok {
		return
	}
	var req models.UpdatePaymentConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorWithCode(w, http.StatusBadRequest, "invalid_body",
			"Le corps de la requête est invalide: "+err.Error())
		return
	}
	if err := validateStruct(req); err != nil {
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
		return
	}
	if !models.IsValidPaymentConfigMode(req.Mode) {
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_mode",
			"Le mode doit être paiement_livraison, paiement_avance ou paiement_integral.")
		return
	}
	// Validate advance_amount when mode = paiement_avance.
	if req.Mode == string(models.PaymentModeAvance) && (req.AdvanceAmount == nil || *req.AdvanceAmount <= 0) {
		writeErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_advance_amount",
			"Le montant de l'acompte est requis et doit être > 0 pour le mode paiement_avance.")
		return
	}
	s := mustSession(r)
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	methods := req.ActiveMethods
	if methods == nil {
		methods = []string{}
	}
	cfg, err := h.repo.Upsert(r.Context(), shopID, s.UserID, s.Role, repository.UpsertPaymentConfigInput{
		Mode:          req.Mode,
		AdvanceAmount: req.AdvanceAmount,
		DelayMinutes:  firstNonZeroInt(req.DelayMinutes, 120),
		WaveLink:      req.WaveLink,
		WaveNumber:    req.WaveNumber,
		MoovNumber:    req.MoovNumber,
		OrangeNumber:  req.OrangeNumber,
		MTNNumber:     req.MTNNumber,
		ActiveMethods: methods,
		Active:        active,
	})
	if err != nil {
		writeErrorWithCode(w, http.StatusInternalServerError, "internal",
			"Une erreur est survenue. Réessayez.")
		return
	}
	writeJSON(w, http.StatusOK, models.ToPaymentConfigResponse(cfg))
}

// --- helpers ----------------------------------------------------------------

func (h *PaymentConfigHandler) serviceUnavailable(w http.ResponseWriter) bool {
	if h.repo != nil {
		return false
	}
	writeErrorWithCode(w, http.StatusServiceUnavailable, "service_unavailable",
		"Le service de configuration de paiement n'est pas disponible (base de données injoignable).")
	return true
}

// firstNonZeroInt returns the first non-zero int from a *int + fallback.
func firstNonZeroInt(p *int, fallback int) int {
	if p != nil && *p > 0 {
		return *p
	}
	return fallback
}
