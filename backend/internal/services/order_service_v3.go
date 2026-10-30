// NOVA v3 — Order service extensions for the new state machine.
//
// The v3 flow (spec section 1):
//
//   1. Client confirms cart → order created as `en_attente_confirmation`
//      (after collecting nom/prénom/téléphone/lieu) → stock RESERVED.
//   2. Merchant confirms:
//        - If paiement_livraison → `en_cours` (deduct stock + count revenue)
//        - If paiement_avance or paiement_integral → `en_attente_paiement`
//          (set payment_deadline = now + delay)
//   3. Client signals payment → `paiement_signalé`
//   4. Merchant confirms payment → `en_cours` (deduct stock + count revenue)
//   5. Merchant refuses payment → back to `en_attente_paiement` (retry)
//      OR `refusee` (cancel + release stock)
//   6. Cron: if `en_attente_paiement` and deadline passed → `annulee`
//      (release stock)
//   7. Merchant marks ready → `prete`
//   8. Merchant completes → `terminee`
//   9. Merchant cancels after en_cours → `annulee`
//      (REINSTATE stock + REMOVE revenue)
//
// These methods are SEPARATE from the v1/v2 methods in order_service.go.
// They use the same OrderService struct (extended with paymentCfgRepo).
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"nova-api/internal/models"
	"nova-api/internal/repository"
)

// ============================================================================
// V3 flow — ConfirmOrderV3 (creates order as en_attente_confirmation + reserves stock)
// ============================================================================

// ConfirmOrderV3Request is the input to ConfirmOrderV3. The client info
// (nom, prénom, téléphone, lieu) is REQUIRED — the AI must collect it
// before calling this method.
type ConfirmOrderV3Request struct {
	CartID          uuid.UUID
	ZoneID          uuid.UUID
	DeliveryAddress string
	PaymentMode     string
	IdempotencyKey  string
	CustomerID      *uuid.UUID
	Note            string
	// ClientInfo (REQUIRED per spec section 5)
	ClientNom       string
	ClientPrenom    string
	ClientTelephone string
	ClientLieu      string
}

// ConfirmOrderV3 creates an order from a cart and sets its status to
// en_attente_confirmation. Stock is RESERVED atomically for each item.
// If ANY reservation fails, releases what was reserved and returns
// ErrInsufficientStockOrder.
//
// Idempotent: if the idempotency_key is already used, returns the existing order.
func (s *OrderService) ConfirmOrderV3(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, req ConfirmOrderV3Request, ip, userAgent string) (*models.Order, error) {
	// Validate required client info.
	if req.ClientNom == "" || req.ClientTelephone == "" || req.ClientLieu == "" {
		return nil, ErrClientInfoRequired
	}
	if !isValidPaymentMode(req.PaymentMode) {
		return nil, ErrInvalidPaymentModeOrder
	}
	if req.IdempotencyKey == "" {
		return nil, errors.New("idempotency_key is required")
	}
	// 1. Idempotency check.
	if existing, err := s.orderRepo.GetByIdempotencyKey(ctx, shopID, userID, userRole, req.IdempotencyKey); err == nil {
		return existing, nil
	} else if !errors.Is(err, repository.ErrOrderNotFound) {
		return nil, fmt.Errorf("confirm order v3: idempotency check: %w", err)
	}
	// 2. Get the cart.
	cart, err := s.cartRepo.GetByID(ctx, shopID, userID, userRole, req.CartID)
	if err != nil {
		return nil, err
	}
	if cart.Status != models.CartActive {
		return nil, ErrCartNotActive
	}
	if len(cart.Items) == 0 {
		return nil, ErrEmptyCart
	}
	// 3. Resolve customer_id.
	customerID := req.CustomerID
	if customerID == nil {
		customerID = cart.CustomerID
	}
	if customerID == nil {
		return nil, ErrCustomerRequired
	}
	// 4. Validate items + snapshot prices + compute subtotal.
	type itemPlan struct {
		VariantID   *uuid.UUID
		ProductName string
		VariantInfo string
		UnitPrice   int64
		Quantity    int
		LineTotal   int64
	}
	plans := make([]itemPlan, 0, len(cart.Items))
	var subtotal int64
	for i := range cart.Items {
		ci := &cart.Items[i]
		if ci.VariantID == nil {
			return nil, fmt.Errorf("cart item %s has no variant_id", ci.ID)
		}
		v, err := s.productRepo.GetVariantByID(ctx, shopID, userID, userRole, *ci.VariantID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, fmt.Errorf("variant %s no longer exists: %w", ci.VariantID, ErrVariantNotFound)
			}
			return nil, fmt.Errorf("confirm order v3: get variant: %w", err)
		}
		if !v.Active {
			return nil, fmt.Errorf("variant %s is no longer active: %w", ci.VariantID, ErrVariantNotActive)
		}
		unitPrice := models.CurrentVariantPrice(v)
		lineTotal := unitPrice * int64(ci.Quantity)
		variantInfo := buildVariantInfo(v)
		productName := ci.ProductName
		plans = append(plans, itemPlan{
			VariantID:   ci.VariantID,
			ProductName: productName,
			VariantInfo: variantInfo,
			UnitPrice:   unitPrice,
			Quantity:    ci.Quantity,
			LineTotal:   lineTotal,
		})
		subtotal += lineTotal
	}
	// 5. Calculate delivery fee.
	feeResult, err := s.zoneRepo.CalculateFee(ctx, shopID, userID, userRole, req.ZoneID, subtotal)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrZoneNotFound
		}
		if errors.Is(err, repository.ErrZoneInactive) {
			return nil, ErrZoneInactive
		}
		if errors.Is(err, repository.ErrOrderBelowMinimum) {
			return nil, ErrOrderBelowMin
		}
		return nil, fmt.Errorf("confirm order v3: calculate fee: %w", err)
	}
	deliveryFee := feeResult.Fee
	total := subtotal + deliveryFee
	// 6. Generate order number.
	var number string
	for attempt := 0; attempt < 3; attempt++ {
		n, err := s.orderRepo.GenerateOrderNumber(ctx, shopID, userID, userRole)
		if err != nil {
			return nil, fmt.Errorf("confirm order v3: generate number: %w", err)
		}
		number = n
		break
	}
	// 7. RESERVE stock atomically. If any reservation fails, release what we've
	//    reserved and return ErrInsufficientStockOrder.
	reserved := make([]reservedItem, 0, len(plans))
	var failedItem string
	var failedErr error
	for _, p := range plans {
		_, _, err := s.inventoryRepo.Reserve(ctx, shopID, userID, userRole, *p.VariantID, repository.ReserveInput{
			Quantity: p.Quantity,
			AuthorID: &userID,
		})
		if err != nil {
			failedItem = p.ProductName
			failedErr = err
			break
		}
		reserved = append(reserved, reservedItem{VariantID: *p.VariantID, Quantity: p.Quantity})
	}
	if failedErr != nil {
		for _, r := range reserved {
			_, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, r.VariantID, repository.ReleaseInput{
				Quantity: r.Quantity,
				AuthorID: &userID,
			})
		}
		if errors.Is(failedErr, repository.ErrInsufficientStock) || errors.Is(failedErr, repository.ErrNotFound) {
			return nil, fmt.Errorf("article « %s »: stock insuffisant: %w", failedItem, ErrInsufficientStockOrder)
		}
		return nil, fmt.Errorf("confirm order v3: reserve stock for %s: %w", failedItem, failedErr)
	}
	// 8. Create the order with status = en_attente_confirmation.
	itemInputs := make([]repository.CreateOrderItemInput, 0, len(plans))
	for _, p := range plans {
		itemInputs = append(itemInputs, repository.CreateOrderItemInput{
			VariantID:   p.VariantID,
			ProductName: p.ProductName,
			VariantInfo: p.VariantInfo,
			UnitPrice:   p.UnitPrice,
			Quantity:    p.Quantity,
			LineTotal:   p.LineTotal,
		})
	}
	var cartIDArg *uuid.UUID
	if req.CartID != uuid.Nil {
		cartIDArg = &req.CartID
	}
	zoneID := req.ZoneID
	order, err := s.orderRepo.Create(ctx, shopID, userID, userRole, repository.CreateOrderInput{
		Number:          number,
		CustomerID:      *customerID,
		CartID:          cartIDArg,
		Status:          string(models.OrderV3EnAttenteConfirmation),
		PaymentStatus:   "on_delivery",
		PaymentMode:     req.PaymentMode,
		Items:           itemInputs,
		Subtotal:        subtotal,
		DeliveryFee:     deliveryFee,
		Total:           total,
		IdempotencyKey:  req.IdempotencyKey,
		DeliveryZoneID:  &zoneID,
		DeliveryAddress: req.DeliveryAddress,
		Note:            req.Note,
	})
	if err != nil {
		// Release reserved stock on failure.
		for _, p := range plans {
			if p.VariantID != nil {
				_, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, *p.VariantID, repository.ReleaseInput{
					Quantity: p.Quantity,
					AuthorID: &userID,
				})
			}
		}
		if errors.Is(err, repository.ErrDuplicateIdempotency) {
			if existing, gerr := s.orderRepo.GetByIdempotencyKey(ctx, shopID, userID, userRole, req.IdempotencyKey); gerr == nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("confirm order v3: create: %w", err)
	}
	// 9. Mark cart as converted.
	_ = s.cartRepo.MarkConverted(ctx, shopID, userID, userRole, req.CartID, order.ID)
	// 10. Audit log.
	shopIDForLog := shopID
	orderIDForLog := order.ID
	_ = s.auditRepo.Log(ctx, repository.AuditEntry{
		ShopID:     &shopIDForLog,
		ActorID:    &userID,
		ActorRole:  userRole,
		Action:     "order.v3.create",
		ObjectType: "order",
		ObjectID:   &orderIDForLog,
		IPAddress:  ip,
		UserAgent:  userAgent,
		After:      orderSnapshotFrom(order),
	})
	return order, nil
}

// ============================================================================
// V3 flow — MerchantConfirm (en_attente_confirmation → en_attente_paiement OR en_cours)
// ============================================================================

// MerchantConfirmOrder transitions an order from en_attente_confirmation to
// either en_attente_paiement (if payment is needed) or en_cours (if paiement_livraison).
//
// Behavior:
//   - If shop's payment config = paiement_livraison → en_cours (deduct stock + count revenue)
//   - If shop's payment config = paiement_avance or paiement_integral → en_attente_paiement
//     (set payment_deadline = now + delay_minutes)
//
// NOVA NEVER auto-confirms payment. The merchant must explicitly call
// ConfirmPayment after the client signals payment.
func (s *OrderService) MerchantConfirmOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, ip, userAgent string) (*models.Order, error) {
	if s.paymentCfgRepo == nil {
		return nil, ErrPaymentConfigMissing
	}
	// Get the order + items.
	order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderV3EnAttenteConfirmation {
		return nil, fmt.Errorf("order is not en_attente_confirmation (status=%s): %w", order.Status, ErrInvalidTransition)
	}
	// Get the shop's payment config.
	cfg, err := s.paymentCfgRepo.GetOrCreateByShop(ctx, shopID, userID, userRole)
	if err != nil {
		return nil, fmt.Errorf("merchant confirm: get payment config: %w", err)
	}
	// Branch on the mode.
	if cfg.Mode == models.PaymentModeLivraison {
		// → en_cours : deduct stock (ExitStock) + count revenue.
		return s.transitionToEnCours(ctx, userID, userRole, shopID, orderID, order, items, "merchant confirmed (paiement_livraison)", ip, userAgent)
	}
	// paiement_avance or paiement_integral → en_attente_paiement (set payment_deadline).
	deadline := time.Now().Add(time.Duration(cfg.DelayMinutes) * time.Minute)
	deadlinePtr := deadline
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus:       string(models.OrderV3EnAttentePaiement),
		AuthorID:        &userID,
		Reason:          "merchant confirmed — waiting for payment",
		PaymentDeadline: &deadlinePtr,
	})
	if err != nil {
		return nil, fmt.Errorf("merchant confirm: transition to en_attente_paiement: %w", err)
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.merchant_confirm", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — MerchantRefuse (en_attente_confirmation → refusee — RELEASE STOCK)
// ============================================================================

// MerchantRefuseOrder transitions an order from en_attente_confirmation (or
// en_attente_paiement / paiement_signalé) to refusee. Releases the reserved stock.
func (s *OrderService) MerchantRefuseOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, reason string, ip, userAgent string) (*models.Order, error) {
	if reason == "" {
		reason = "merchant refused the order"
	}
	order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	// The stock was RESERVED at en_attente_confirmation. We release it.
	hadReservedStock := order.Status == models.OrderV3EnAttenteConfirmation ||
		order.Status == models.OrderV3EnAttentePaiement ||
		order.Status == models.OrderV3PaiementSignale
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus: string(models.OrderV3Refusee),
		AuthorID:  &userID,
		Reason:    reason,
	})
	if err != nil {
		return nil, fmt.Errorf("merchant refuse: transition: %w", err)
	}
	if hadReservedStock {
		for _, it := range items {
			if it.VariantID == nil {
				continue
			}
			_, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, *it.VariantID, repository.ReleaseInput{
				Quantity: it.Quantity,
				OrderID:  &orderID,
				AuthorID: &userID,
			})
		}
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.merchant_refuse", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — ClientSignalPayment (en_attente_paiement → paiement_signalé)
// ============================================================================

// ClientSignalPayment transitions an order from en_attente_paiement to
// paiement_signalé. The client says "I paid" — the merchant must verify.
// NOVA NEVER auto-confirms payment.
func (s *OrderService) ClientSignalPayment(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, paymentReference string, ip, userAgent string) (*models.Order, error) {
	order, _, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderV3EnAttentePaiement {
		return nil, fmt.Errorf("order is not en_attente_paiement (status=%s): %w", order.Status, ErrInvalidTransition)
	}
	// We use UpdateStatusV3 (which doesn't change payment_reference — we'd need
	// a separate method for that). For now, the payment_reference is stored in
	// the order_event reason.
	reason := "client signalled payment"
	if paymentReference != "" {
		reason = fmt.Sprintf("client signalled payment (ref=%s)", paymentReference)
	}
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus: string(models.OrderV3PaiementSignale),
		AuthorID:  &userID,
		Reason:    reason,
	})
	if err != nil {
		return nil, fmt.Errorf("client signal payment: transition: %w", err)
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.payment_signalled", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — MerchantConfirmPayment (paiement_signalé → en_cours — DEDUCT STOCK + COUNT REVENUE)
// ============================================================================

// MerchantConfirmPayment transitions an order from paiement_signalé to en_cours.
// This is the DEFINITIVE stock deduction: on_hand -= q, reserved -= q, and
// revenue_counted = true.
//
// CRITICAL: Only the merchant can confirm a payment. NOVA never auto-confirms.
func (s *OrderService) MerchantConfirmPayment(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, ip, userAgent string) (*models.Order, error) {
	order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderV3PaiementSignale {
		return nil, fmt.Errorf("order is not paiement_signale (status=%s): %w", order.Status, ErrInvalidTransition)
	}
	return s.transitionToEnCours(ctx, userID, userRole, shopID, orderID, order, items, "merchant confirmed payment", ip, userAgent)
}

// ============================================================================
// V3 flow — MerchantRefusePayment (paiement_signalé → en_attente_paiement OR refusee)
// ============================================================================

// MerchantRefusePayment transitions an order from paiement_signalé back to
// en_attente_paiement (client can retry) OR refusee (cancel + release stock).
// The `cancel` flag controls which branch is taken.
func (s *OrderService) MerchantRefusePayment(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, cancel bool, reason string, ip, userAgent string) (*models.Order, error) {
	if reason == "" {
		if cancel {
			reason = "merchant refused payment and cancelled the order"
		} else {
			reason = "merchant refused payment — client must retry"
		}
	}
	order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderV3PaiementSignale {
		return nil, fmt.Errorf("order is not paiement_signale (status=%s): %w", order.Status, ErrInvalidTransition)
	}
	if cancel {
		// → refusee + release reserved stock.
		updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
			NewStatus: string(models.OrderV3Refusee),
			AuthorID:  &userID,
			Reason:    reason,
		})
		if err != nil {
			return nil, fmt.Errorf("merchant refuse payment (cancel): transition: %w", err)
		}
		for _, it := range items {
			if it.VariantID == nil {
				continue
			}
			_, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, *it.VariantID, repository.ReleaseInput{
				Quantity: it.Quantity,
				OrderID:  &orderID,
				AuthorID: &userID,
			})
		}
		s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.payment_refused_cancelled", updated, ip, userAgent)
		return updated, nil
	}
	// → back to en_attente_paiement (set a new payment_deadline).
	if s.paymentCfgRepo != nil {
		cfg, err := s.paymentCfgRepo.GetOrCreateByShop(ctx, shopID, userID, userRole)
		if err == nil && cfg != nil {
			deadline := time.Now().Add(time.Duration(cfg.DelayMinutes) * time.Minute)
			updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
				NewStatus:       string(models.OrderV3EnAttentePaiement),
				AuthorID:        &userID,
				Reason:          reason,
				PaymentDeadline: &deadline,
			})
			if err != nil {
				return nil, fmt.Errorf("merchant refuse payment (retry): transition: %w", err)
			}
			s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.payment_refused_retry", updated, ip, userAgent)
			return updated, nil
		}
	}
	// Fallback (no config) — no deadline.
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus: string(models.OrderV3EnAttentePaiement),
		AuthorID:  &userID,
		Reason:    reason,
	})
	if err != nil {
		return nil, fmt.Errorf("merchant refuse payment (retry): transition: %w", err)
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.payment_refused_retry", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — MerchantMarkReady (en_cours → prete)
// ============================================================================

// MerchantMarkReady transitions an order from en_cours to prete.
func (s *OrderService) MerchantMarkReady(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, ip, userAgent string) (*models.Order, error) {
	order, _, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderV3EnCours {
		return nil, fmt.Errorf("order is not en_cours (status=%s): %w", order.Status, ErrInvalidTransition)
	}
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus: string(models.OrderV3Prete),
		AuthorID:  &userID,
		Reason:    "merchant marked order as ready",
	})
	if err != nil {
		return nil, fmt.Errorf("merchant mark ready: transition: %w", err)
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.mark_ready", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — MerchantComplete (prete → terminee)
// ============================================================================

// MerchantComplete transitions an order from prete to terminee.
func (s *OrderService) MerchantComplete(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, ip, userAgent string) (*models.Order, error) {
	order, _, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != models.OrderV3Prete {
		return nil, fmt.Errorf("order is not prete (status=%s): %w", order.Status, ErrInvalidTransition)
	}
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus: string(models.OrderV3Terminee),
		AuthorID:  &userID,
		Reason:    "merchant completed the order",
	})
	if err != nil {
		return nil, fmt.Errorf("merchant complete: transition: %w", err)
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.complete", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — CancelOrderV3 (any v3 status → annulee — REINSTATE/RELEASE stock + REMOVE revenue)
// ============================================================================

// CancelOrderV3 transitions an order to annulee. The stock handling depends
// on the current status:
//   - en_attente_confirmation / en_attente_paiement / paiement_signalé → RELEASE reserved stock
//   - en_cours / prete → REINSTATE definitively-deducted stock + set revenue_counted=false
//
// The cron job also calls this method when a payment_deadline is exceeded.
func (s *OrderService) CancelOrderV3(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, reason string, ip, userAgent string) (*models.Order, error) {
	if reason == "" {
		reason = "order cancelled"
	}
	order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
	if err != nil {
		return nil, err
	}
	// Determine the stock action based on the current status.
	wasEnCoursOrLater := order.Status == models.OrderV3EnCours || order.Status == models.OrderV3Prete
	hadReservedStock := order.Status == models.OrderV3EnAttenteConfirmation ||
		order.Status == models.OrderV3EnAttentePaiement ||
		order.Status == models.OrderV3PaiementSignale
	// If the order was en_cours or prete, we need to REMOVE revenue (set
	// revenue_counted=false) AND reinstate the stock.
	var revenueOverride *bool
	if wasEnCoursOrLater {
		f := false
		revenueOverride = &f
	}
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus:      string(models.OrderV3Annulee),
		AuthorID:       &userID,
		Reason:         reason,
		RevenueCounted: revenueOverride,
	})
	if err != nil {
		return nil, fmt.Errorf("cancel v3: transition: %w", err)
	}
	// Stock side-effects.
	if wasEnCoursOrLater {
		// REINSTATE the definitively-deducted stock (on_hand += q).
		for _, it := range items {
			if it.VariantID == nil {
				continue
			}
			_, _, _ = s.inventoryRepo.Reinstate(ctx, shopID, userID, userRole, *it.VariantID, repository.ExitStockInput{
				Quantity: it.Quantity,
				OrderID:  &orderID,
				AuthorID: &userID,
			})
		}
	} else if hadReservedStock {
		// RELEASE the reserved stock (reserved -= q).
		for _, it := range items {
			if it.VariantID == nil {
				continue
			}
			_, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, *it.VariantID, repository.ReleaseInput{
				Quantity: it.Quantity,
				OrderID:  &orderID,
				AuthorID: &userID,
			})
		}
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.cancel", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — transitionToEnCours (shared helper for MerchantConfirmOrder + MerchantConfirmPayment)
// ============================================================================

// transitionToEnCours is the shared implementation for the transition to
// en_cours (the definitive stock deduction + revenue count).
//
// Pre-condition: the order is in en_attente_confirmation (paiement_livraison)
// OR paiement_signalé (merchant confirmed payment).
//
// Post-condition:
//   - status = en_cours
//   - revenue_counted = true
//   - stock definitively deducted (ExitStock: on_hand -= q, reserved -= q)
//   - payment_deadline cleared
func (s *OrderService) transitionToEnCours(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, order *models.Order, items []models.OrderItem, reason string, ip, userAgent string) (*models.Order, error) {
	// 1. Definitively deduct stock (ExitStock) for each item.
	for _, it := range items {
		if it.VariantID == nil {
			continue
		}
		_, _, err := s.inventoryRepo.ExitStock(ctx, shopID, userID, userRole, *it.VariantID, repository.ExitStockInput{
			Quantity: it.Quantity,
			OrderID:  &orderID,
			AuthorID: &userID,
		})
		if err != nil {
			return nil, fmt.Errorf("transition to en_cours: exit stock for %s: %w", it.ProductName, err)
		}
	}
	// 2. Transition to en_cours + set revenue_counted = true.
	t := true
	updated, _, err := s.orderRepo.UpdateStatusV3(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusV3Input{
		NewStatus:      string(models.OrderV3EnCours),
		AuthorID:       &userID,
		Reason:         reason,
		RevenueCounted: &t,
	})
	if err != nil {
		// Rollback: reinstate the stock we just exited.
		for _, it := range items {
			if it.VariantID == nil {
				continue
			}
			_, _, _ = s.inventoryRepo.Reinstate(ctx, shopID, userID, userRole, *it.VariantID, repository.ExitStockInput{
				Quantity: it.Quantity,
				OrderID:  &orderID,
				AuthorID: &userID,
			})
		}
		return nil, fmt.Errorf("transition to en_cours: %w", err)
	}
	s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.v3.en_cours", updated, ip, userAgent)
	return updated, nil
}

// ============================================================================
// V3 flow — CronExpirePaymentDeadline (called by cron_service.go)
// ============================================================================

// CronExpirePaymentDeadline cancels all orders in en_attente_paiement whose
// payment_deadline is past. The cron service calls this periodically.
//
// Returns the number of orders cancelled + any error.
func (s *OrderService) CronExpirePaymentDeadline(ctx context.Context) (int, error) {
	orderIDs, shopIDs, err := s.orderRepo.ListExpiredPaymentOrders(ctx)
	if err != nil {
		return 0, fmt.Errorf("cron expire payment: list: %w", err)
	}
	cancelled := 0
	for i, orderID := range orderIDs {
		shopID := shopIDs[i]
		// Use a super_admin role for the cron (it's a server-side operation).
		_, err := s.CancelOrderV3(ctx, uuid.Nil, "super_admin", shopID, orderID, "payment deadline exceeded — auto-cancelled by cron", "", "")
		if err != nil {
			// Log + continue — don't fail the whole batch on one error.
			continue
		}
		cancelled++
	}
	return cancelled, nil
}

// ============================================================================
// V3 — DashboardStatsV3
// ============================================================================

// DashboardStatsV3 returns the v3 dashboard stats: counts by v3 status +
// month revenue computed from revenue_counted=true (instead of by status).
func (s *OrderService) DashboardStatsV3(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID) (*models.OrderDashboardStats, error) {
	byStatus, err := s.orderRepo.CountByStatus(ctx, shopID, userID, userRole)
	if err != nil {
		return nil, err
	}
	byPayment, err := s.orderRepo.CountByPaymentStatus(ctx, shopID, userID, userRole)
	if err != nil {
		return nil, err
	}
	// Pending = en_attente_confirmation count.
	pending := byStatus[string(models.OrderV3EnAttenteConfirmation)]
	today, err := s.orderRepo.CountToday(ctx, shopID, userID, userRole)
	if err != nil {
		return nil, err
	}
	total, err := s.orderRepo.CountTotal(ctx, shopID, userID, userRole)
	if err != nil {
		return nil, err
	}
	revenue, monthCount, err := s.orderRepo.MonthRevenueV3(ctx, shopID, userID, userRole)
	if err != nil {
		return nil, err
	}
	return &models.OrderDashboardStats{
		ByStatus:        byStatus,
		ByPaymentStatus: byPayment,
		PendingCount:    pending,
		TodayCount:      today,
		MonthRevenue:    revenue,
		MonthOrderCount: monthCount,
		TotalOrders:     total,
	}, nil
}
