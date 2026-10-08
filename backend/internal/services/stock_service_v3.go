// NOVA v3 — Stock service extensions (spec section 2).
//
// Adds:
//   - SetStockMode: set the stock_mode of a variant (quantite/epuise/illimite)
//   - Reinstate: reverse a definitive ExitStock (for annulee after en_cours)
//
// The repository already enforces stock_mode in Reserve/ExitStock/Release
// (illimite = no-op, epuise = error on Reserve).
package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"nova-api/internal/models"
	"nova-api/internal/repository"
)

// SetStockMode updates the stock_mode of a variant's inventory row.
// mode must be one of: quantite, epuise, illimite.
func (s *StockService) SetStockMode(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, mode string, ip, userAgent string) error {
	if !models.IsValidStockMode(mode) {
		return fmt.Errorf("invalid stock_mode: %s", mode)
	}
	if err := s.inventoryRepo.SetStockMode(ctx, shopID, userID, userRole, variantID, mode); err != nil {
		if err == repository.ErrNotFound {
			return ErrInventoryNotFound
		}
		return fmt.Errorf("set stock mode: %w", err)
	}
	shopIDForLog := shopID
	varIDForLog := variantID
	_ = s.auditRepo.Log(ctx, repository.AuditEntry{
		ShopID:     &shopIDForLog,
		ActorID:    &userID,
		ActorRole:  userRole,
		Action:     "stock.mode.set",
		ObjectType: "inventory",
		ObjectID:   &varIDForLog,
		IPAddress:  ip,
		UserAgent:  userAgent,
		After:      stockModeSnapshot{VariantID: variantID.String(), StockMode: mode},
	})
	return nil
}

// Reinstate reverses a definitive ExitStock (on_hand += q). Used when an
// en_cours order is cancelled (status → annulee): the stock that was
// definitively deducted is reinstated. No-op for illimite.
func (s *StockService) Reinstate(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, quantity int, orderID *uuid.UUID, ip, userAgent string) (*models.Inventory, error) {
	if quantity <= 0 {
		return nil, ErrInvalidQuantity
	}
	inv, mut, err := s.inventoryRepo.Reinstate(ctx, shopID, userID, userRole, variantID, repository.ExitStockInput{
		Quantity: quantity,
		OrderID:  orderID,
		AuthorID: &userID,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, ErrInventoryNotFound
		}
		return nil, fmt.Errorf("reinstate stock: %w", err)
	}
	s.logMovement(ctx, shopID, userID, userRole, "stock.reinstate", mut, ip, userAgent)
	return inv, nil
}

// stockModeSnapshot for audit logs.
type stockModeSnapshot struct {
	VariantID string `json:"variant_id"`
	StockMode string `json:"stock_mode"`
}

func (m stockModeSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		VariantID string `json:"variant_id"`
		StockMode string `json:"stock_mode"`
	}{VariantID: m.VariantID, StockMode: m.StockMode})
}
