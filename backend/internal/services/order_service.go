// Order service — orchestrates the cart + order + state machine lifecycle.
//
// This service is the single entry point for cart and order business logic.
// HTTP handlers in internal/api/handlers/orders.go call into this service.
//
// CRITICAL INVARIANTS (ch. 4.5):
//
// 1. State machine is DETERMINISTIC and lives in the code. The transition
//    map (orderTransitions in internal/repository/orders.go) is the source
//    of truth. The LLM (Task 8) CANNOT modify a status directly — it MUST
//    call AdvanceStatus which validates via this map.
//
// 2. IDEMPOTENCY: ConfirmOrder with the same idempotency_key returns the
//    existing order. The DB has UNIQUE (shop_id, idempotency_key); the
//    service catches ErrDuplicateIdempotency and returns the original order.
//
// 3. PRICE SNAPSHOT: order_items.unit_price + line_total are FROZEN at
//    confirmation. We compute them once (using CurrentVariantPrice at the
//    moment of confirmation) and never recompute. A subsequent catalog
//    change does NOT affect past orders.
//
// 4. STOCK RESERVATION ON CONFIRMATION: when pending → confirmed, we
//    reserve stock for each item (atomic UPDATE). If ANY reservation
//    fails, we release the previously-reserved quantities and return
//    ErrInsufficientStock with which item failed. The order stays 'pending'.
//
// 5. STOCK RELEASE ON CANCELLATION: when an order with reserved stock is
//    cancelled (status was confirmed/preparing/delivering/delivery_failed),
//    we release the reserved quantity for each item.
//
// 6. STOCK EXIT ON DELIVERY: when delivering → delivered, we call ExitStock
//    (on_hand -= q AND reserved -= q) for each item.
//
// 7. PAYMENT STATUS IS SEPARATE: payment_status has its own state machine
//    (paymentTransitions). 'déclaré' never counts as 'payé' without
//    explicit validation (ch. 13).
package services

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "strings"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// Order service sentinel errors.
var (
        ErrCartNotFound          = repository.ErrCartNotFound
        ErrCartItemNotFound      = repository.ErrCartItemNotFound
        ErrCartNotActive         = repository.ErrCartNotActive
        ErrOrderNotFound         = repository.ErrOrderNotFound
        ErrInvalidTransition     = repository.ErrInvalidTransition
        ErrInvalidPaymentTransition = repository.ErrInvalidPaymentTransition
        ErrInsufficientStockOrder = errors.New("insufficient stock for order")
        ErrEmptyCart              = errors.New("cart is empty")
        // ErrInvalidPaymentModeOrder is the order-specific variant of
        // "invalid payment mode" — the shop_service already defines
        // ErrInvalidPaymentMode for subscription payments, so we use a
        // distinct name here to avoid a redeclaration conflict.
        ErrInvalidPaymentModeOrder = errors.New("invalid payment mode")
        ErrInvalidPaymentStatus   = errors.New("invalid payment status")
        ErrInvalidOrderStatus     = errors.New("invalid order status")
        ErrCustomerRequired       = errors.New("customer_id is required (cart has no customer)")
        ErrVariantNotActive       = errors.New("variant is not active")

        // NOVA v3 — new sentinels for the v3 flow.
        ErrPaymentConfigMissing   = errors.New("payment config not found for shop")
        ErrInvalidPaymentCfgMode  = errors.New("invalid payment config mode")
        ErrClientInfoRequired     = errors.New("client info required (nom, prenom, telephone, lieu)")
)

// OrderService is the order business-logic layer.
type OrderService struct {
        orderRepo      *repository.OrderRepository
        cartRepo       *repository.CartRepository
        inventoryRepo  *repository.InventoryRepository
        productRepo    *repository.ProductRepository
        zoneRepo       *repository.DeliveryZoneRepository
        auditRepo      *repository.AuditRepository
        paymentCfgRepo *repository.PaymentConfigRepository
        pool           *pgxpool.Pool
}

// NewOrderService constructs an OrderService.
func NewOrderService(
        orderRepo *repository.OrderRepository,
        cartRepo *repository.CartRepository,
        inventoryRepo *repository.InventoryRepository,
        productRepo *repository.ProductRepository,
        zoneRepo *repository.DeliveryZoneRepository,
        auditRepo *repository.AuditRepository,
        pool *pgxpool.Pool,
) *OrderService {
        return &OrderService{
                orderRepo:     orderRepo,
                cartRepo:      cartRepo,
                inventoryRepo: inventoryRepo,
                productRepo:   productRepo,
                zoneRepo:      zoneRepo,
                auditRepo:     auditRepo,
                pool:          pool,
        }
}

// SetPaymentConfigRepo injects the payment config repository. This is
// called by main.go AFTER the OrderService is constructed (to avoid a
// circular init dependency).
func (s *OrderService) SetPaymentConfigRepo(repo *repository.PaymentConfigRepository) {
        s.paymentCfgRepo = repo
}

// OrderListItem is a re-export of repository.OrderListItem so handlers don't
// need to import the repository package directly.
type OrderListItem = repository.OrderListItem

// ============================================================================
// Carts
// ============================================================================

// GetOrCreateCart returns the active cart for the (conversationID, customerID)
// tuple, or creates a new one.
func (s *OrderService) GetOrCreateCart(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, conversationID, customerID *uuid.UUID) (*models.Cart, error) {
        return s.cartRepo.GetOrCreateActive(ctx, shopID, userID, userRole, conversationID, customerID)
}

// GetCart returns a cart with items.
func (s *OrderService) GetCart(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID uuid.UUID) (*models.Cart, error) {
        return s.cartRepo.GetByID(ctx, shopID, userID, userRole, cartID)
}

// AddToCart adds an item to the cart. Validates variant belongs to shop, is
// active. Uses CurrentVariantPrice for the unit_price snapshot.
func (s *OrderService) AddToCart(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, variantID uuid.UUID, quantity int) (*models.Cart, error) {
        if quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        // Fetch the variant.
        v, err := s.productRepo.GetVariantByID(ctx, shopID, userID, userRole, variantID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrVariantNotFound
                }
                return nil, fmt.Errorf("add to cart: get variant: %w", err)
        }
        if !v.Active {
                return nil, ErrVariantNotActive
        }
        // Fetch the product (for the name snapshot).
        prod, err := s.productRepo.GetByID(ctx, shopID, userID, userRole, v.ProductID)
        if err != nil {
                return nil, fmt.Errorf("add to cart: get product: %w", err)
        }
        // Compute the current price (promo if active).
        unitPrice := models.CurrentVariantPrice(v)
        // Build variant_info snapshot ("Taille M - Bleu" or "").
        variantInfo := buildVariantInfo(v)
        // Add the item.
        if _, err := s.cartRepo.AddItem(ctx, shopID, userID, userRole, cartID, repository.AddItemInput{
                VariantID:   variantID,
                ProductName: prod.Product.Name,
                VariantInfo: variantInfo,
                UnitPrice:   unitPrice,
                Quantity:    quantity,
        }); err != nil {
                return nil, err
        }
        // Audit log.
        shopIDForLog := shopID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "cart.item.add",
                ObjectType: "cart_item",
                ObjectID:   &variantID,
                IPAddress:  "",
                UserAgent:  "",
                After:      cartItemSnapshot{VariantID: variantID.String(), ProductName: prod.Product.Name, UnitPrice: unitPrice, Quantity: quantity},
        })
        return s.cartRepo.GetByID(ctx, shopID, userID, userRole, cartID)
}

// UpdateCartItem updates the quantity of a cart item. quantity=0 removes the item.
func (s *OrderService) UpdateCartItem(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, itemID uuid.UUID, quantity int) (*models.Cart, error) {
        if err := s.cartRepo.UpdateItemQuantity(ctx, shopID, userID, userRole, cartID, itemID, quantity); err != nil {
                return nil, err
        }
        return s.cartRepo.GetByID(ctx, shopID, userID, userRole, cartID)
}

// RemoveFromCart removes an item from the cart.
func (s *OrderService) RemoveFromCart(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, itemID uuid.UUID) (*models.Cart, error) {
        if err := s.cartRepo.RemoveItem(ctx, shopID, userID, userRole, cartID, itemID); err != nil {
                return nil, err
        }
        return s.cartRepo.GetByID(ctx, shopID, userID, userRole, cartID)
}

// ClearCart removes all items from a cart.
func (s *OrderService) ClearCart(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID uuid.UUID) error {
        return s.cartRepo.Clear(ctx, shopID, userID, userRole, cartID)
}

// ============================================================================
// Recap (pre-confirmation) — ch. 4.5 step 7
// ============================================================================

// GenerateRecap calculates the subtotal + delivery fee + total for a cart +
// zone + payment mode. Does NOT create an order. Returns the recap for
// display.
func (s *OrderService) GenerateRecap(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, zoneID uuid.UUID, paymentMode string) (*models.OrderRecap, error) {
        // Validate payment mode.
        if !isValidPaymentMode(paymentMode) {
                return nil, ErrInvalidPaymentMode
        }
        // Get the cart.
        cart, err := s.cartRepo.GetByID(ctx, shopID, userID, userRole, cartID)
        if err != nil {
                return nil, err
        }
        if len(cart.Items) == 0 {
                return nil, ErrEmptyCart
        }
        // Compute subtotal.
        var subtotal int64
        items := make([]models.CartItemResponse, 0, len(cart.Items))
        for i := range cart.Items {
                it := &cart.Items[i]
                lineTotal := it.UnitPrice * int64(it.Quantity)
                subtotal += lineTotal
                items = append(items, models.CartItemResponse{
                        ID:          it.ID.String(),
                        VariantID:   uuidToStrP(it.VariantID),
                        ProductName: it.ProductName,
                        VariantInfo: it.VariantInfo,
                        SKU:         it.SKU,
                        UnitPrice:   it.UnitPrice,
                        Quantity:    it.Quantity,
                        LineTotal:   lineTotal,
                        Available:   it.Available,
                })
        }
        // Calculate delivery fee.
        feeResult, err := s.zoneRepo.CalculateFee(ctx, shopID, userID, userRole, zoneID, subtotal)
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
                return nil, fmt.Errorf("generate recap: calculate fee: %w", err)
        }
        zoneResp := models.ToDeliveryZoneResponse(&feeResult.Zone)
        return &models.OrderRecap{
                Items:        items,
                Subtotal:     subtotal,
                DeliveryFee:  feeResult.Fee,
                FreeDelivery: feeResult.FreeDelivery,
                Total:        subtotal + feeResult.Fee,
                Zone:         &zoneResp,
                PaymentMode:  paymentMode,
        }, nil
}

// ============================================================================
// ConfirmOrder — THE MAIN METHOD (ch. 4.5 step 8-9)
// ============================================================================

// ConfirmOrderRequest is the input to ConfirmOrder. AutoConfirm=true means
// the order should be created directly in 'confirmed' state with stock
// reserved (default false → order is created as 'pending' and the merchant
// must confirm via ConfirmPendingOrder).
type ConfirmOrderRequest struct {
        CartID          uuid.UUID
        ZoneID          uuid.UUID
        DeliveryAddress string
        PaymentMode     string
        PaymentStatus   string // optional, defaults to "on_delivery"
        IdempotencyKey  string
        CustomerID      *uuid.UUID // optional; falls back to cart.CustomerID
        Note            string
        AutoConfirm     bool
}

// ConfirmOrder creates an order from a cart. The flow:
//  1. Check idempotency_key — if order exists, return it.
//  2. Get the cart, validate it's active and has items.
//  3. Resolve customer_id (request body OR cart.CustomerID).
//  4. Validate all items: variants exist, are active. Snapshot prices.
//  5. Calculate delivery fee via zone.CalculateFee.
//  6. Generate order number (retry on collision).
//  7. If AutoConfirm: reserve stock for each item; on failure, release +
//     return ErrInsufficientStockOrder. (NO order is created.)
//  8. Create the order (status='confirmed' if AutoConfirm, 'pending' otherwise)
//     + order_items (frozen prices) + initial order_event.
//  9. If Create fails AFTER reservation: release reserved stock, return error.
// 10. Mark cart as 'converted'.
// 11. Audit log.
func (s *OrderService) ConfirmOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, req ConfirmOrderRequest, ip, userAgent string) (*models.Order, error) {
        // Validate payment mode.
        if !isValidPaymentMode(req.PaymentMode) {
                return nil, ErrInvalidPaymentMode
        }
        paymentStatus := req.PaymentStatus
        if paymentStatus == "" {
                paymentStatus = "on_delivery"
        }
        if !isValidPaymentStatus(paymentStatus) {
                return nil, ErrInvalidPaymentStatus
        }
        if req.IdempotencyKey == "" {
                return nil, errors.New("idempotency_key is required")
        }
        // 1. Idempotency check.
        if existing, err := s.orderRepo.GetByIdempotencyKey(ctx, shopID, userID, userRole, req.IdempotencyKey); err == nil {
                return existing, nil
        } else if !errors.Is(err, repository.ErrOrderNotFound) {
                return nil, fmt.Errorf("confirm order: idempotency check: %w", err)
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
        // We re-fetch each variant to get the CURRENT price (the cart's
        // unit_price snapshot may be stale if the catalog changed since add).
        // The order_items will use the FRESH price.
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
                        return nil, fmt.Errorf("confirm order: get variant: %w", err)
                }
                if !v.Active {
                        return nil, fmt.Errorf("variant %s is no longer active: %w", ci.VariantID, ErrVariantNotActive)
                }
                unitPrice := models.CurrentVariantPrice(v)
                lineTotal := unitPrice * int64(ci.Quantity)
                // Use the FRESH variant_info (in case the variant was updated).
                variantInfo := buildVariantInfo(v)
                // Use the cart's product_name as fallback; the variant's
                // product could have been renamed.
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
                return nil, fmt.Errorf("confirm order: calculate fee: %w", err)
        }
        deliveryFee := feeResult.Fee
        total := subtotal + deliveryFee
        // 6. Generate order number (retry on collision).
        var number string
        for attempt := 0; attempt < 3; attempt++ {
                n, err := s.orderRepo.GenerateOrderNumber(ctx, shopID, userID, userRole)
                if err != nil {
                        return nil, fmt.Errorf("confirm order: generate number: %w", err)
                }
                number = n
                break
        }
        // 7. If AutoConfirm: reserve stock for each item BEFORE creating the
        // order. If any reservation fails, release what we've reserved and
        // return ErrInsufficientStockOrder with details.
        initialStatus := "pending"
        if req.AutoConfirm {
                initialStatus = "confirmed"
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
                        // Release what we've reserved.
                        for _, r := range reserved {
                                _, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, r.VariantID, repository.ReleaseInput{
                                        Quantity: r.Quantity,
                                        AuthorID: &userID,
                                })
                        }
                        if errors.Is(failedErr, repository.ErrInsufficientStock) || errors.Is(failedErr, repository.ErrNotFound) {
                                return nil, fmt.Errorf("article « %s »: stock insuffisant: %w", failedItem, ErrInsufficientStockOrder)
                        }
                        return nil, fmt.Errorf("confirm order: reserve stock for %s: %w", failedItem, failedErr)
                }
        }
        // 8. Create the order.
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
                Number:           number,
                CustomerID:       *customerID,
                CartID:           cartIDArg,
                Status:           initialStatus,
                PaymentStatus:    paymentStatus,
                PaymentMode:      req.PaymentMode,
                Items:            itemInputs,
                Subtotal:         subtotal,
                DeliveryFee:      deliveryFee,
                Total:            total,
                IdempotencyKey:   req.IdempotencyKey,
                DeliveryZoneID:   &zoneID,
                DeliveryAddress:  req.DeliveryAddress,
                Note:             req.Note,
        })
        if err != nil {
                // If we reserved stock (AutoConfirm) but the order creation
                // failed, release the reserved stock.
                if req.AutoConfirm {
                        for _, p := range plans {
                                if p.VariantID != nil {
                                        _, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, *p.VariantID, repository.ReleaseInput{
                                                Quantity: p.Quantity,
                                                AuthorID: &userID,
                                        })
                                }
                        }
                }
                if errors.Is(err, repository.ErrDuplicateIdempotency) {
                        // Race: another request created the order with the
                        // same key. Fetch + return it.
                        if existing, gerr := s.orderRepo.GetByIdempotencyKey(ctx, shopID, userID, userRole, req.IdempotencyKey); gerr == nil {
                                return existing, nil
                        }
                }
                return nil, fmt.Errorf("confirm order: create: %w", err)
        }
        // 10. Mark cart as converted.
        if err := s.cartRepo.MarkConverted(ctx, shopID, userID, userRole, req.CartID, order.ID); err != nil {
                // Non-fatal: the order is created, just log.
                // (The cart might already be converted by a race, or the
                // cart_id might be nil — both are OK.)
        }
        // 11. Audit log.
        shopIDForLog := shopID
        orderIDForLog := order.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "order.create",
                ObjectType: "order",
                ObjectID:   &orderIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      orderSnapshotFrom(order),
        })
        return order, nil
}

// ============================================================================
// ConfirmPendingOrder (pending → confirmed) — RESERVES STOCK ATOMICALLY
// ============================================================================

// ConfirmPendingOrder transitions a pending order to confirmed. Reserves
// stock for each item atomically. If ANY reservation fails, releases what
// was reserved and returns ErrInsufficientStockOrder. The order stays
// 'pending' so the merchant can decide (cancel, contact client, etc.).
func (s *OrderService) ConfirmPendingOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, ip, userAgent string) (*models.Order, error) {
        // Get the order with items.
        order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
        if err != nil {
                return nil, err
        }
        if order.Status != models.OrderPending {
                return nil, fmt.Errorf("order is not pending (status=%s): %w", order.Status, ErrInvalidTransition)
        }
        // Reserve stock for each item.
        reserved := make([]reservedItem, 0, len(items))
        var failedItem string
        var failedErr error
        for _, it := range items {
                if it.VariantID == nil {
                        continue
                }
                _, _, err := s.inventoryRepo.Reserve(ctx, shopID, userID, userRole, *it.VariantID, repository.ReserveInput{
                        Quantity: it.Quantity,
                        OrderID:  &orderID,
                        AuthorID: &userID,
                })
                if err != nil {
                        failedItem = it.ProductName
                        failedErr = err
                        break
                }
                reserved = append(reserved, reservedItem{VariantID: *it.VariantID, Quantity: it.Quantity})
        }
        if failedErr != nil {
                // Release what we've reserved.
                for _, r := range reserved {
                        _, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, r.VariantID, repository.ReleaseInput{
                                Quantity: r.Quantity,
                                OrderID:  &orderID,
                                AuthorID: &userID,
                        })
                }
                if errors.Is(failedErr, repository.ErrInsufficientStock) || errors.Is(failedErr, repository.ErrNotFound) {
                        return nil, fmt.Errorf("article « %s »: stock insuffisant: %w", failedItem, ErrInsufficientStockOrder)
                }
                return nil, fmt.Errorf("confirm pending order: reserve %s: %w", failedItem, failedErr)
        }
        // Transition pending → confirmed (no stock change in this transition;
        // the reservation already happened above).
        updated, _, err := s.orderRepo.UpdateStatus(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusInput{
                NewStatus: "confirmed",
                AuthorID:  &userID,
                Reason:    "merchant confirmed order",
        })
        if err != nil {
                // Release the reserved stock — the transition failed.
                for _, r := range reserved {
                        _, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, r.VariantID, repository.ReleaseInput{
                                Quantity: r.Quantity,
                                OrderID:  &orderID,
                                AuthorID: &userID,
                        })
                }
                return nil, fmt.Errorf("confirm pending order: transition: %w", err)
        }
        // Audit log.
        shopIDForLog := shopID
        orderIDForLog := orderID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "order.confirm",
                ObjectType: "order",
                ObjectID:   &orderIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      orderSnapshotFrom(updated),
        })
        return updated, nil
}

// ============================================================================
// CancelOrder — RELEASES RESERVED STOCK
// ============================================================================

// CancelOrder transitions an order to 'cancelled'. If the order had reserved
// stock (status was confirmed/preparing/delivering/delivery_failed), releases
// the reserved quantity for each item. 'pending' orders have no reserved
// stock to release. 'cancelled'/'returned' orders cannot be cancelled.
func (s *OrderService) CancelOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, reason string, ip, userAgent string) (*models.Order, error) {
        if reason == "" {
                return nil, errors.New("cancel reason is required")
        }
        // Get the order with items.
        order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
        if err != nil {
                return nil, err
        }
        // Check if stock was reserved (order is past 'pending').
        hadReservedStock := order.Status == models.OrderConfirmed ||
                order.Status == models.OrderPreparing ||
                order.Status == models.OrderDelivering ||
                order.Status == models.OrderDeliveryFailed
        // If the order was DELIVERED, the stock was already exited — we
        // can't release it. (Cancelling a delivered order is not allowed by
        // the state machine anyway.)
        // Transition to 'cancelled'.
        updated, _, err := s.orderRepo.UpdateStatus(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusInput{
                NewStatus: "cancelled",
                AuthorID:  &userID,
                Reason:    reason,
        })
        if err != nil {
                return nil, fmt.Errorf("cancel order: transition: %w", err)
        }
        // Release reserved stock if applicable.
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
        // Audit log.
        shopIDForLog := shopID
        orderIDForLog := orderID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "order.cancel",
                ObjectType: "order",
                ObjectID:   &orderIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      orderSnapshotFrom(updated),
        })
        return updated, nil
}

// ============================================================================
// AdvanceStatus — generic state machine transition with stock side-effects
// ============================================================================

// AdvanceStatus applies a generic state machine transition. Validates via
// orderTransitions. Special handling:
//   - pending → confirmed: reserve stock (like ConfirmPendingOrder)
//   - confirmed → preparing: no stock change
//   - preparing → delivering: no stock change
//   - delivering → delivered: EXIT stock (on_hand -= q, reserved -= q)
//   - delivering → delivery_failed: no stock change (stock still reserved)
//   - delivery_failed → delivering (retry): no stock change
//   - any → cancelled: if stock was reserved, RELEASE it
//
// For the pending → confirmed transition, callers should prefer
// ConfirmPendingOrder (which gives better error messages on stock failure).
func (s *OrderService) AdvanceStatus(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, newStatus string, reason string, ip, userAgent string) (*models.Order, error) {
        if !isValidOrderStatus(newStatus) {
                return nil, ErrInvalidOrderStatus
        }
        // Get the order with items.
        order, items, _, err := s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
        if err != nil {
                return nil, err
        }
        oldStatus := string(order.Status)
        // Validate transition.
        if !repository.IsTransitionAllowed(oldStatus, newStatus) {
                return nil, fmt.Errorf("transition %s → %s not allowed: %w", oldStatus, newStatus, ErrInvalidTransition)
        }
        // Special handling.
        switch {
        case oldStatus == "pending" && newStatus == "confirmed":
                // Reserve stock. If any fails, release + return error.
                return s.confirmPendingWithReserved(ctx, userID, userRole, shopID, orderID, items, ip, userAgent)
        case newStatus == "cancelled":
                // Release reserved stock if applicable.
                hadReservedStock := oldStatus == "confirmed" || oldStatus == "preparing" ||
                        oldStatus == "delivering" || oldStatus == "delivery_failed"
                updated, _, err := s.orderRepo.UpdateStatus(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusInput{
                        NewStatus: newStatus,
                        AuthorID:  &userID,
                        Reason:    reason,
                })
                if err != nil {
                        return nil, fmt.Errorf("advance status: %w", err)
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
                s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.advance", updated, ip, userAgent)
                return updated, nil
        case oldStatus == "delivering" && newStatus == "delivered":
                // EXIT stock (on_hand -= q, reserved -= q) for each item.
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
                                return nil, fmt.Errorf("advance status: exit stock for %s: %w", it.ProductName, err)
                        }
                }
                updated, _, err := s.orderRepo.UpdateStatus(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusInput{
                        NewStatus: newStatus,
                        AuthorID:  &userID,
                        Reason:    reason,
                })
                if err != nil {
                        return nil, fmt.Errorf("advance status: %w", err)
                }
                s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.advance", updated, ip, userAgent)
                return updated, nil
        default:
                // No stock side-effect.
                updated, _, err := s.orderRepo.UpdateStatus(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusInput{
                        NewStatus: newStatus,
                        AuthorID:  &userID,
                        Reason:    reason,
                })
                if err != nil {
                        return nil, fmt.Errorf("advance status: %w", err)
                }
                s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.advance", updated, ip, userAgent)
                return updated, nil
        }
}

// confirmPendingWithReserved is the shared implementation for the
// pending → confirmed transition with stock reservation. Used by both
// ConfirmPendingOrder and AdvanceStatus.
func (s *OrderService) confirmPendingWithReserved(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, items []models.OrderItem, ip, userAgent string) (*models.Order, error) {
        reserved := make([]reservedItem, 0, len(items))
        var failedItem string
        var failedErr error
        for _, it := range items {
                if it.VariantID == nil {
                        continue
                }
                _, _, err := s.inventoryRepo.Reserve(ctx, shopID, userID, userRole, *it.VariantID, repository.ReserveInput{
                        Quantity: it.Quantity,
                        OrderID:  &orderID,
                        AuthorID: &userID,
                })
                if err != nil {
                        failedItem = it.ProductName
                        failedErr = err
                        break
                }
                reserved = append(reserved, reservedItem{VariantID: *it.VariantID, Quantity: it.Quantity})
        }
        if failedErr != nil {
                for _, r := range reserved {
                        _, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, r.VariantID, repository.ReleaseInput{
                                Quantity: r.Quantity,
                                OrderID:  &orderID,
                                AuthorID: &userID,
                        })
                }
                if errors.Is(failedErr, repository.ErrInsufficientStock) || errors.Is(failedErr, repository.ErrNotFound) {
                        return nil, fmt.Errorf("article « %s »: stock insuffisant: %w", failedItem, ErrInsufficientStockOrder)
                }
                return nil, fmt.Errorf("confirm: reserve %s: %w", failedItem, failedErr)
        }
        updated, _, err := s.orderRepo.UpdateStatus(ctx, shopID, userID, userRole, orderID, repository.UpdateStatusInput{
                NewStatus: "confirmed",
                AuthorID:  &userID,
                Reason:    "status advanced to confirmed",
        })
        if err != nil {
                for _, r := range reserved {
                        _, _, _ = s.inventoryRepo.Release(ctx, shopID, userID, userRole, r.VariantID, repository.ReleaseInput{
                                Quantity: r.Quantity,
                                OrderID:  &orderID,
                                AuthorID: &userID,
                        })
                }
                return nil, fmt.Errorf("confirm: transition: %w", err)
        }
        s.auditOrderAction(ctx, shopID, userID, userRole, orderID, "order.confirm", updated, ip, userAgent)
        return updated, nil
}

// ============================================================================
// Payment status — SEPARATE state machine (ch. 4.5)
// ============================================================================

// UpdatePaymentStatus updates the payment_status. Validates via
// paymentTransitions. 'déclaré' → 'payé' requires explicit merchant
// validation (ch. 13).
func (s *OrderService) UpdatePaymentStatus(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID, newStatus string, reference string, ip, userAgent string) (*models.Order, error) {
        if !isValidPaymentStatus(newStatus) {
                return nil, ErrInvalidPaymentStatus
        }
        updated, err := s.orderRepo.UpdatePaymentStatus(ctx, shopID, userID, userRole, orderID, repository.UpdatePaymentStatusInput{
                NewPaymentStatus: newStatus,
                PaymentReference: reference,
                AuthorID:         &userID,
        })
        if err != nil {
                return nil, fmt.Errorf("update payment status: %w", err)
        }
        // Audit log.
        shopIDForLog := shopID
        orderIDForLog := orderID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "order.payment",
                ObjectType: "order",
                ObjectID:   &orderIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      orderSnapshotFrom(updated),
        })
        return updated, nil
}

// ============================================================================
// Read methods
// ============================================================================

// GetOrder returns an order with items + events.
func (s *OrderService) GetOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID) (*models.Order, []models.OrderItem, []models.OrderEvent, error) {
        return s.orderRepo.GetByID(ctx, shopID, userID, userRole, orderID)
}

// ListOrders returns paginated orders for the shop.
func (s *OrderService) ListOrders(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, params models.ListOrdersParams) ([]repository.OrderListItem, int64, error) {
        params.Normalize()
        var customerID *uuid.UUID
        if params.CustomerID != "" {
                id, err := uuid.Parse(params.CustomerID)
                if err == nil {
                        customerID = &id
                }
        }
        repoParams := repository.ListOrdersRepoParams{
                Page:          params.Page,
                Limit:         params.Limit,
                Status:        params.Status,
                PaymentStatus: params.PaymentStatus,
                CustomerID:    customerID,
                Search:        params.Search,
                From:          params.From,
                To:            params.To,
        }
        return s.orderRepo.List(ctx, shopID, userID, userRole, repoParams)
}

// ListCustomerOrders returns all orders for a customer.
func (s *OrderService) ListCustomerOrders(ctx context.Context, userID uuid.UUID, userRole string, shopID, customerID uuid.UUID) ([]repository.OrderListItem, error) {
        return s.orderRepo.ListByCustomer(ctx, shopID, userID, userRole, customerID)
}

// ListEvents returns the order event history.
func (s *OrderService) ListEvents(ctx context.Context, userID uuid.UUID, userRole string, shopID, orderID uuid.UUID) ([]models.OrderEvent, error) {
        return s.orderRepo.ListEvents(ctx, shopID, userID, userRole, orderID)
}

// DashboardStats returns the orders dashboard stats for the shop.
func (s *OrderService) DashboardStats(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID) (*models.OrderDashboardStats, error) {
        byStatus, err := s.orderRepo.CountByStatus(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, err
        }
        byPayment, err := s.orderRepo.CountByPaymentStatus(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, err
        }
        pending, err := s.orderRepo.CountPending(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, err
        }
        today, err := s.orderRepo.CountToday(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, err
        }
        total, err := s.orderRepo.CountTotal(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, err
        }
        revenue, monthCount, err := s.orderRepo.MonthRevenue(ctx, shopID, userID, userRole)
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

// ============================================================================
// Helpers
// ============================================================================

// reservedItem tracks an item we've reserved so we can release it on failure.
type reservedItem struct {
        VariantID uuid.UUID
        Quantity  int
}

// buildVariantInfo builds a human-readable variant snapshot string from a
// ProductVariant (e.g. "Taille M - Bleu"). Returns "" if neither size nor
// color is set.
func buildVariantInfo(v *models.ProductVariant) string {
        var parts []string
        if v.Size != nil && *v.Size != "" {
                parts = append(parts, "Taille "+*v.Size)
        }
        if v.Color != nil && *v.Color != "" {
                parts = append(parts, *v.Color)
        }
        return strings.Join(parts, " - ")
}

// isValidPaymentMode returns true if the given string is a valid payment_mode.
func isValidPaymentMode(s string) bool {
        switch models.PaymentMode(s) {
        case models.PayCash, models.PayMobileMoney, models.PayWave, models.PayOrangeMoney, models.PayMTNMomo:
                return true
        }
        return false
}

// isValidPaymentStatus returns true if the given string is a valid payment_status.
func isValidPaymentStatus(s string) bool {
        switch models.PaymentStatus(s) {
        case models.PaymentOnDelivery, models.PaymentPendingPayment, models.PaymentDeclared,
                models.PaymentPaid, models.PaymentFailed, models.PaymentRefunded:
                return true
        }
        return false
}

// isValidOrderStatus returns true if the given string is a valid order_status.
func isValidOrderStatus(s string) bool {
        switch models.OrderStatus(s) {
        case models.OrderDraft, models.OrderPending, models.OrderConfirmed, models.OrderPreparing,
                models.OrderDelivering, models.OrderDelivered, models.OrderDeliveryFailed,
                models.OrderReturned, models.OrderCancelled:
                return true
        }
        return false
}

// uuidToStrP converts a *uuid.UUID to a *string.
func uuidToStrP(u *uuid.UUID) *string {
        if u == nil {
                return nil
        }
        s := u.String()
        return &s
}

// auditOrderAction is a best-effort audit log for an order action.
func (s *OrderService) auditOrderAction(ctx context.Context, shopID, userID uuid.UUID, userRole string, orderID uuid.UUID, action string, order *models.Order, ip, userAgent string) {
        if order == nil {
                return
        }
        shopIDForLog := shopID
        orderIDForLog := orderID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     action,
                ObjectType: "order",
                ObjectID:   &orderIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      orderSnapshotFrom(order),
        })
}

// orderSnapshot for audit logs.
type orderSnapshot struct {
        ID            string `json:"id"`
        Number        string `json:"number"`
        Status        string `json:"status"`
        PaymentStatus string `json:"payment_status"`
        Total         int64  `json:"total"`
}

func (o orderSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                ID            string `json:"id"`
                Number        string `json:"number"`
                Status        string `json:"status"`
                PaymentStatus string `json:"payment_status"`
                Total         int64  `json:"total"`
        }{ID: o.ID, Number: o.Number, Status: o.Status, PaymentStatus: o.PaymentStatus, Total: o.Total})
}

func orderSnapshotFrom(o *models.Order) orderSnapshot {
        if o == nil {
                return orderSnapshot{}
        }
        return orderSnapshot{
                ID:            o.ID.String(),
                Number:        o.Number,
                Status:        string(o.Status),
                PaymentStatus: string(o.PaymentStatus),
                Total:         o.Total,
        }
}

// cartItemSnapshot for audit logs.
type cartItemSnapshot struct {
        VariantID   string `json:"variant_id"`
        ProductName string `json:"product_name"`
        UnitPrice   int64  `json:"unit_price"`
        Quantity    int    `json:"quantity"`
}

func (c cartItemSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                VariantID   string `json:"variant_id"`
                ProductName string `json:"product_name"`
                UnitPrice   int64  `json:"unit_price"`
                Quantity    int    `json:"quantity"`
        }{VariantID: c.VariantID, ProductName: c.ProductName, UnitPrice: c.UnitPrice, Quantity: c.Quantity})
}

// unused-import guards (time is used in case we add date filters later)
var _ = time.Now
