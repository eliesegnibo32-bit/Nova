// Order repository — handles the `orders`, `order_items`, and `order_events`
// tables (ch. 4.5).
//
// All queries go through db.WithTenantTx so the RLS policies from migration
// 011 are enforced:
//
//      orders / order_items  : shop_id = current_shop_id() OR is_platform_admin()
//      order_events          : SELECT + INSERT only (no UPDATE/DELETE policies)
//
// shopID comes from the authenticated session, NOT from the URL.
//
// CRITICAL: UpdateStatus implements the deterministic state machine. The
// transition map (orderTransitions in internal/services/order_service.go) is
// the source of truth for which transitions are allowed. The repository
// refuses any transition not allowed by the map. The LLM (Task 8) CANNOT
// modify a status directly — it MUST call this method (or the service wrapper
// AdvanceStatus).
//
// CRITICAL: Create uses idempotency_key to prevent duplicate orders. The
// orders table has UNIQUE (shop_id, idempotency_key) — if the same key is
// used twice, the second insert fails with a unique_violation. The service
// layer catches this and returns the original order (see GetByIdempotencyKey).
//
// CRITICAL: order_items.unit_price + line_total are FROZEN at confirmation.
// The DB has CHECK (line_total = unit_price * quantity) — we compute
// line_total in Go and pass it to the INSERT; the CHECK validates it.
package repository

import (
        "context"
        "errors"
        "fmt"
        "strings"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// --- Sentinels --------------------------------------------------------------

// ErrOrderNotFound is returned when an order doesn't exist.
var ErrOrderNotFound = errors.New("order not found")

// ErrInvalidTransition is returned by UpdateStatus when the requested
// transition is not allowed by the deterministic state machine.
var ErrInvalidTransition = errors.New("invalid order status transition")

// ErrInvalidPaymentTransition is returned by UpdatePaymentStatus when the
// requested payment transition is not allowed.
var ErrInvalidPaymentTransition = errors.New("invalid payment status transition")

// ErrDuplicateIdempotency is returned by Create when the (shopID, key) pair
// is already used by another order. The caller should fetch the existing order
// via GetByIdempotencyKey and return it (idempotent behavior).
var ErrDuplicateIdempotency = errors.New("idempotency key already used")

// ErrOrderNumberTaken is returned when the generated order number is already
// used (very unlikely given the sequence). The caller should retry.
var ErrOrderNumberTaken = errors.New("order number already taken")

// --- Inputs -----------------------------------------------------------------

// CreateOrderInput is the data needed to create an order + order_items in
// one transaction. All prices/subtotals/totals are pre-computed by the
// service (which calls the delivery zone + price helpers) — the repository
// just persists them as-is.
type CreateOrderInput struct {
        Number            string
        CustomerID        uuid.UUID
        CartID            *uuid.UUID
        Status            string // "pending" or "confirmed" (initial)
        PaymentStatus     string // "on_delivery" by default
        PaymentMode       string
        PaymentReference  string
        Items             []CreateOrderItemInput
        Subtotal          int64
        DeliveryFee       int64
        Total             int64
        IdempotencyKey    string
        DeliveryZoneID    *uuid.UUID
        DeliveryAddress   string
        Note              string
}

// CreateOrderItemInput is one frozen line of an order.
type CreateOrderItemInput struct {
        VariantID   *uuid.UUID
        ProductName string
        VariantInfo string
        UnitPrice   int64
        Quantity    int
        LineTotal   int64
}

// ListOrdersRepoParams is the input to List.
type ListOrdersRepoParams struct {
        Page          int
        Limit         int
        Status        string
        PaymentStatus string
        CustomerID    *uuid.UUID
        Search        string
        From          *time.Time
        To            *time.Time
}

// Offset returns the SQL OFFSET.
func (p ListOrdersRepoParams) Offset() int {
        if p.Page < 1 {
                p.Page = 1
        }
        return (p.Page - 1) * p.Limit
}

// OrderListItem is the list-view shape (with customer name + item count).
type OrderListItem struct {
        Order        models.Order
        CustomerName *string
        ItemsCount   int
}

// OrderRepository wraps the orders + order_items + order_events tables.
type OrderRepository struct {
        pool *pgxpool.Pool
}

// NewOrderRepository returns an OrderRepository bound to the pool.
func NewOrderRepository(pool *pgxpool.Pool) *OrderRepository {
        return &OrderRepository{pool: pool}
}

// --- Order state machine (transition map) -----------------------------------
//
// The map is the SOURCE OF TRUTH for which transitions are allowed. The LLM
// (Task 8) cannot bypass this — it must call AdvanceStatus which validates
// via this map.
//
// State diagram (ch. 4.5):
//
//      draft            → pending, cancelled
//      pending          → confirmed, cancelled
//      confirmed        → preparing, cancelled
//      preparing        → delivering, cancelled
//      delivering       → delivered, delivery_failed
//      delivery_failed  → delivering (retry), cancelled
//      delivered        → returned (V2)
//      cancelled        → (final)
//      returned         → (final)
//
// NOVA v3 (spec section 1) — NEW state machine (additive, the old statuses
// stay available for backward compatibility):
//
//      en_attente_confirmation → en_attente_paiement (merchant confirms, payment needed)
//      en_attente_confirmation → en_cours           (merchant confirms, payment_livraison)
//      en_attente_confirmation → refusee            (merchant refuses → release stock)
//      en_attente_paiement     → paiement_signalé   (client says paid)
//      paiement_signalé        → en_cours           (merchant CONFIRMS payment → deduct stock + revenue)
//      paiement_signalé        → en_attente_paiement(merchant REFUSES payment → ask client to retry)
//      paiement_signalé        → refusee            (merchant refuses → cancel)
//      en_attente_paiement     → refusee            (merchant cancels → release stock)
//      en_attente_paiement     → annulee            (delay exceeded → release stock)
//      en_cours                → prete              (merchant prepares)
//      en_cours                → annulee            (merchant cancels after en_cours → REINSTATE stock + REMOVE revenue)
//      prete                   → terminee           (merchant completes)
//      prete                   → annulee            (rare — cancel after prete → REINSTATE stock + REMOVE revenue)
//      refusee                 → (final)
//      terminee                → (final)
//      annulee                 → (final)

var orderTransitions = map[string]map[string]bool{
        "draft":           {"pending": true, "cancelled": true},
        "pending":         {"confirmed": true, "cancelled": true},
        "confirmed":       {"preparing": true, "cancelled": true},
        "preparing":       {"delivering": true, "cancelled": true},
        "delivering":      {"delivered": true, "delivery_failed": true},
        "delivery_failed": {"delivering": true, "cancelled": true},
        "delivered":       {"returned": true},
        "cancelled":       {},
        "returned":        {},
        // NOVA v3 statuses
        "en_attente_confirmation": {"en_attente_paiement": true, "en_cours": true, "refusee": true, "annulee": true},
        "en_attente_paiement":     {"paiement_signale": true, "refusee": true, "annulee": true},
        "paiement_signale":        {"en_cours": true, "en_attente_paiement": true, "refusee": true, "annulee": true},
        "en_cours":                {"prete": true, "annulee": true},
        "prete":                   {"terminee": true, "annulee": true},
        "terminee":                {},
        "refusee":                 {},
        "annulee":                 {},
}

// IsTransitionAllowed returns true if from → to is allowed by the state machine.
func IsTransitionAllowed(from, to string) bool {
        allowed, ok := orderTransitions[from]
        if !ok {
                return false
        }
        return allowed[to]
}

// --- Payment status transitions ---------------------------------------------
//
// Payment is a SEPARATE state machine from the order status (ch. 4.5).
//
//      on_delivery      → pending_payment, declared, paid, failed
//      pending_payment  → declared, paid, failed
//      declared         → paid, failed
//      paid             → refunded
//      failed           → pending_payment, declared
//      refunded         → (final)

var paymentTransitions = map[string]map[string]bool{
        "on_delivery":     {"pending_payment": true, "declared": true, "paid": true, "failed": true},
        "pending_payment": {"declared": true, "paid": true, "failed": true},
        "declared":        {"paid": true, "failed": true},
        "paid":            {"refunded": true},
        "failed":          {"pending_payment": true, "declared": true},
        "refunded":        {},
}

// IsPaymentTransitionAllowed returns true if from → to is allowed by the
// payment state machine.
func IsPaymentTransitionAllowed(from, to string) bool {
        allowed, ok := paymentTransitions[from]
        if !ok {
                return false
        }
        return allowed[to]
}

// --- Create -----------------------------------------------------------------

// Create inserts an order + order_items + initial order_event in one
// transaction. The initial event has from_status=NULL, to_status=$Status.
//
// Idempotency: if (shopID, idempotency_key) already exists, returns
// ErrDuplicateIdempotency. The service should catch this and return the
// existing order via GetByIdempotencyKey.
func (r *OrderRepository) Create(ctx context.Context, shopID, userID uuid.UUID, role string, in CreateOrderInput) (*models.Order, error) {
        var order models.Order
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // 1. Insert the order.
                var (
                        idempArg  any = nullableString(in.IdempotencyKey)
                        zoneArg   any
                        cartArg   any
                        addrArg   any = nullableString(in.DeliveryAddress)
                        noteArg   any = nullableString(in.Note)
                        refArg    any = nullableString(in.PaymentReference)
                )
                if in.DeliveryZoneID != nil {
                        zoneArg = *in.DeliveryZoneID
                }
                if in.CartID != nil {
                        cartArg = *in.CartID
                }
                const oq = `
                        INSERT INTO orders
                            (shop_id, number, customer_id, cart_id, status, payment_status, payment_mode,
                             payment_reference, subtotal, delivery_fee, total, idempotency_key,
                             delivery_zone_id, delivery_address, note, confirmed_at)
                        VALUES ($1, $2, $3, $4, $5::order_status, $6::payment_status, $7::payment_mode,
                                $8, $9, $10, $11, $12, $13, $14, $15,
                                CASE WHEN $5 IN ('confirmed','preparing','delivering','delivered','delivery_failed','returned')
                                     THEN now() ELSE NULL END)
                        RETURNING id, shop_id, number, customer_id, cart_id, status, payment_status,
                                  payment_mode, payment_reference, subtotal, delivery_fee, total,
                                  idempotency_key, delivery_zone_id, delivery_address, note,
                                  created_at, confirmed_at, updated_at,
                                  payment_deadline, revenue_counted
                `
                if err := scanOrder(tx.QueryRow(ctx, oq,
                        shopID, in.Number, in.CustomerID, cartArg,
                        in.Status, in.PaymentStatus, in.PaymentMode,
                        refArg, in.Subtotal, in.DeliveryFee, in.Total, idempArg,
                        zoneArg, addrArg, noteArg,
                ), &order); err != nil {
                        if isUniqueViolation(err) {
                                // Could be the idempotency_key OR the number.
                                if strings.Contains(err.Error(), "idempotency_key") || strings.Contains(err.Error(), "orders_shop_id_idempotency_key_key") {
                                        return ErrDuplicateIdempotency
                                }
                                if strings.Contains(err.Error(), "number") || strings.Contains(err.Error(), "orders_shop_id_number_key") {
                                        return ErrOrderNumberTaken
                                }
                                return ErrDuplicateIdempotency
                        }
                        return fmt.Errorf("order repo: create order: %w", err)
                }
                // 2. Insert order_items (frozen prices).
                const iq = `
                        INSERT INTO order_items (order_id, shop_id, variant_id, product_name, variant_info, unit_price, quantity, line_total)
                        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                `
                for _, it := range in.Items {
                        var variantArg any
                        if it.VariantID != nil {
                                variantArg = *it.VariantID
                        }
                        if _, err := tx.Exec(ctx, iq,
                                order.ID, shopID, variantArg,
                                it.ProductName, nullableString(it.VariantInfo),
                                it.UnitPrice, it.Quantity, it.LineTotal,
                        ); err != nil {
                                return fmt.Errorf("order repo: create order_items: %w", err)
                        }
                }
                // 3. Insert initial order_event (from=NULL, to=status).
                if _, err := tx.Exec(ctx, `
                        INSERT INTO order_events (order_id, shop_id, from_status, to_status, author_id, reason)
                        VALUES ($1, $2, NULL, $3::order_status, $4, $5)
                `, order.ID, shopID, in.Status, userID, nullableString("order created")); err != nil {
                        return fmt.Errorf("order repo: create initial event: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &order, nil
}

// --- Get --------------------------------------------------------------------

// GetByID returns an order with items + events.
func (r *OrderRepository) GetByID(ctx context.Context, shopID, userID uuid.UUID, role string, orderID uuid.UUID) (*models.Order, []models.OrderItem, []models.OrderEvent, error) {
        var (
                order  models.Order
                items  []models.OrderItem
                events []models.OrderEvent
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, number, customer_id, cart_id, status, payment_status,
                               payment_mode, payment_reference, subtotal, delivery_fee, total,
                               idempotency_key, delivery_zone_id, delivery_address, note,
                               created_at, confirmed_at, updated_at,
                               payment_deadline, revenue_counted
                          FROM orders WHERE id = $1
                `
                if err := scanOrder(tx.QueryRow(ctx, q, orderID), &order); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrOrderNotFound
                        }
                        return fmt.Errorf("order repo: get by id: %w", err)
                }
                // Items
                rows, err := tx.Query(ctx, `
                        SELECT id, order_id, shop_id, variant_id, product_name, variant_info, unit_price, quantity, line_total
                          FROM order_items WHERE order_id = $1 ORDER BY id
                `, orderID)
                if err != nil {
                        return fmt.Errorf("order repo: get items: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var it models.OrderItem
                        var variantID *uuid.UUID
                        var variantInfo *string
                        if err := rows.Scan(
                                &it.ID, &it.OrderID, &it.ShopID, &variantID, &it.ProductName, &variantInfo,
                                &it.UnitPrice, &it.Quantity, &it.LineTotal,
                        ); err != nil {
                                return err
                        }
                        it.VariantID = variantID
                        it.VariantInfo = variantInfo
                        items = append(items, it)
                }
                if err := rows.Err(); err != nil {
                        return err
                }
                // Events
                erows, err := tx.Query(ctx, `
                        SELECT id, order_id, shop_id, from_status, to_status, author_id, reason, created_at
                          FROM order_events WHERE order_id = $1 ORDER BY created_at
                `, orderID)
                if err != nil {
                        return fmt.Errorf("order repo: get events: %w", err)
                }
                defer erows.Close()
                for erows.Next() {
                        var ev models.OrderEvent
                        var fromStatus *string
                        var authorID *uuid.UUID
                        var reason *string
                        if err := erows.Scan(
                                &ev.ID, &ev.OrderID, &ev.ShopID, &fromStatus, &ev.ToStatus, &authorID, &reason, &ev.CreatedAt,
                        ); err != nil {
                                return err
                        }
                        if fromStatus != nil {
                                s := models.OrderStatus(*fromStatus)
                                ev.FromStatus = &s
                        }
                        ev.AuthorID = authorID
                        ev.Reason = reason
                        events = append(events, ev)
                }
                if err := erows.Err(); err != nil {
                        return err
                }
                return nil
        })
        if err != nil {
                return nil, nil, nil, err
        }
        if items == nil {
                items = []models.OrderItem{}
        }
        if events == nil {
                events = []models.OrderEvent{}
        }
        return &order, items, events, nil
}

// GetByNumber returns an order by its human-readable number (e.g. "#1048").
// The leading '#' is stripped if present.
func (r *OrderRepository) GetByNumber(ctx context.Context, shopID, userID uuid.UUID, role string, number string) (*models.Order, []models.OrderItem, []models.OrderEvent, error) {
        n := strings.TrimPrefix(number, "#")
        var order models.Order
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `SELECT id FROM orders WHERE shop_id = $1 AND number = $2`
                var id uuid.UUID
                if err := tx.QueryRow(ctx, q, shopID, n).Scan(&id); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrOrderNotFound
                        }
                        return fmt.Errorf("order repo: get by number: %w", err)
                }
                // Re-use GetByID via a sub-call.
                // We can't call r.GetByID from inside the tx (it would open
                // its own tx); just fetch the row here.
                order.ID = id
                return nil
        })
        if err != nil {
                return nil, nil, nil, err
        }
        return r.GetByID(ctx, shopID, userID, role, order.ID)
}

// GetByIdempotencyKey returns the order with the given idempotency_key for the
// shop, or ErrOrderNotFound if no such order exists.
func (r *OrderRepository) GetByIdempotencyKey(ctx context.Context, shopID, userID uuid.UUID, role string, key string) (*models.Order, error) {
        var order models.Order
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, number, customer_id, cart_id, status, payment_status,
                               payment_mode, payment_reference, subtotal, delivery_fee, total,
                               idempotency_key, delivery_zone_id, delivery_address, note,
                               created_at, confirmed_at, updated_at,
                               payment_deadline, revenue_counted
                          FROM orders WHERE shop_id = $1 AND idempotency_key = $2
                `
                if err := scanOrder(tx.QueryRow(ctx, q, shopID, key), &order); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrOrderNotFound
                        }
                        return fmt.Errorf("order repo: get by idempotency: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &order, nil
}

// --- List -------------------------------------------------------------------

// List returns paginated orders for the shop with optional filters.
func (r *OrderRepository) List(ctx context.Context, shopID, userID uuid.UUID, role string, params ListOrdersRepoParams) ([]OrderListItem, int64, error) {
        var (
                out   []OrderListItem
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                where := "WHERE o.shop_id = $1"
                args := []any{shopID}
                idx := 2
                if params.Status != "" {
                        where += fmt.Sprintf(" AND o.status = $%d::order_status", idx)
                        args = append(args, params.Status)
                        idx++
                }
                if params.PaymentStatus != "" {
                        where += fmt.Sprintf(" AND o.payment_status = $%d::payment_status", idx)
                        args = append(args, params.PaymentStatus)
                        idx++
                }
                if params.CustomerID != nil {
                        where += fmt.Sprintf(" AND o.customer_id = $%d", idx)
                        args = append(args, *params.CustomerID)
                        idx++
                }
                if params.Search != "" {
                        where += fmt.Sprintf(" AND o.number ILIKE $%d", idx)
                        args = append(args, "%"+params.Search+"%")
                        idx++
                }
                if params.From != nil {
                        where += fmt.Sprintf(" AND o.created_at >= $%d", idx)
                        args = append(args, *params.From)
                        idx++
                }
                if params.To != nil {
                        where += fmt.Sprintf(" AND o.created_at <= $%d", idx)
                        args = append(args, *params.To)
                        idx++
                }
                // Count.
                countQ := `SELECT COUNT(*) FROM orders o ` + where
                if err := tx.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
                        return fmt.Errorf("order repo: list count: %w", err)
                }
                // List.
                listQ := `
                        SELECT o.id, o.shop_id, o.number, o.customer_id, o.cart_id, o.status, o.payment_status,
                               o.payment_mode, o.payment_reference, o.subtotal, o.delivery_fee, o.total,
                               o.idempotency_key, o.delivery_zone_id, o.delivery_address, o.note,
                               o.created_at, o.confirmed_at, o.updated_at,
                               c.name AS customer_name,
                               (SELECT COUNT(*) FROM order_items oi WHERE oi.order_id = o.id) AS items_count
                          FROM orders o
                          LEFT JOIN customers c ON c.id = o.customer_id
                ` + where + `
                        ORDER BY o.created_at DESC
                        LIMIT $` + fmt.Sprintf("%d", idx) + ` OFFSET $` + fmt.Sprintf("%d", idx+1)
                args = append(args, params.Limit, params.Offset())
                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return fmt.Errorf("order repo: list: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var (
                                it       OrderListItem
                                customerName *string
                                status       string
                                paymentStatus string
                                paymentMode  string
                        )
                        if err := scanOrderWithExtras(rows, &it.Order, &customerName, &it.ItemsCount, &status, &paymentStatus, &paymentMode); err != nil {
                                return err
                        }
                        it.CustomerName = customerName
                        out = append(out, it)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, err
        }
        if out == nil {
                out = []OrderListItem{}
        }
        return out, total, nil
}

// ListByCustomer returns all orders for a customer (most recent first).
func (r *OrderRepository) ListByCustomer(ctx context.Context, shopID, userID uuid.UUID, role string, customerID uuid.UUID) ([]OrderListItem, error) {
        params := ListOrdersRepoParams{
                Page:       1,
                Limit:      500,
                CustomerID: &customerID,
        }
        items, _, err := r.List(ctx, shopID, userID, role, params)
        if err != nil {
                return nil, err
        }
        return items, nil
}

// --- Update status (STATE MACHINE) -----------------------------------------

// UpdateStatusInput is the input to UpdateStatus.
type UpdateStatusInput struct {
        NewStatus string
        AuthorID  *uuid.UUID
        Reason    string
}

// UpdateStatus applies a state machine transition. The transition MUST be
// allowed by orderTransitions; otherwise ErrInvalidTransition is returned.
//
// On success:
//   - The order's status is updated.
//   - confirmed_at is set (idempotently) if the new status implies the order
//     is past the 'pending' stage.
//   - An order_event row is inserted with from_status=old, to_status=new.
//
// The caller (service layer) is responsible for stock side-effects
// (Reserve/Release/ExitStock). The repository just persists the status change
// + the event.
func (r *OrderRepository) UpdateStatus(ctx context.Context, shopID, userID uuid.UUID, role string, orderID uuid.UUID, in UpdateStatusInput) (*models.Order, *models.OrderEvent, error) {
        var (
                order models.Order
                ev    models.OrderEvent
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                // 1. Get the current order.
                const sq = `
                        SELECT id, shop_id, number, customer_id, cart_id, status, payment_status,
                               payment_mode, payment_reference, subtotal, delivery_fee, total,
                               idempotency_key, delivery_zone_id, delivery_address, note,
                               created_at, confirmed_at, updated_at,
                               payment_deadline, revenue_counted
                          FROM orders WHERE id = $1
                          FOR UPDATE
                `
                if err := scanOrder(tx.QueryRow(ctx, sq, orderID), &order); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrOrderNotFound
                        }
                        return fmt.Errorf("order repo: update status: get: %w", err)
                }
                // 2. Validate transition.
                oldStatus := string(order.Status)
                if !IsTransitionAllowed(oldStatus, in.NewStatus) {
                        return ErrInvalidTransition
                }
                // 3. Update the order.
                var confirmedAt any
                if order.ConfirmedAt != nil {
                        confirmedAt = *order.ConfirmedAt
                } else if isPostPending(in.NewStatus) {
                        confirmedAt = time.Now()
                }
                const uq = `
                        UPDATE orders
                           SET status = $2::order_status,
                               confirmed_at = COALESCE($3, confirmed_at),
                               updated_at = now()
                         WHERE id = $1
                         RETURNING id, shop_id, number, customer_id, cart_id, status, payment_status,
                                   payment_mode, payment_reference, subtotal, delivery_fee, total,
                                   idempotency_key, delivery_zone_id, delivery_address, note,
                                   created_at, confirmed_at, updated_at,
                                   payment_deadline, revenue_counted
                `
                if err := scanOrder(tx.QueryRow(ctx, uq, orderID, in.NewStatus, confirmedAt), &order); err != nil {
                        return fmt.Errorf("order repo: update status: update: %w", err)
                }
                // 4. Insert the event (from_status = OLD, to_status = NEW).
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                const eq = `
                        INSERT INTO order_events (order_id, shop_id, from_status, to_status, author_id, reason)
                        VALUES ($1, $2, $3::order_status, $4::order_status, $5, $6)
                        RETURNING id, order_id, shop_id, from_status, to_status, author_id, reason, created_at
                `
                var (
                        fromStatus *string
                        authorID   *uuid.UUID
                        reason     *string
                )
                if err := tx.QueryRow(ctx, eq,
                        orderID, shopID,
                        oldStatus,
                        in.NewStatus,
                        authorArg,
                        nullableString(in.Reason),
                ).Scan(
                        &ev.ID, &ev.OrderID, &ev.ShopID, &fromStatus, &ev.ToStatus, &authorID, &reason, &ev.CreatedAt,
                ); err != nil {
                        return fmt.Errorf("order repo: update status: insert event: %w", err)
                }
                if fromStatus != nil {
                        s := models.OrderStatus(*fromStatus)
                        ev.FromStatus = &s
                }
                ev.AuthorID = authorID
                ev.Reason = reason
                return nil
        })
        if err != nil {
                return nil, nil, err
        }
        return &order, &ev, nil
}

// isPostPending returns true if the given status implies the order is past
// the 'pending' stage (i.e. confirmed_at should be set).
func isPostPending(status string) bool {
        switch status {
        case "confirmed", "preparing", "delivering", "delivered", "delivery_failed", "returned":
                return true
        }
        return false
}

// --- Update payment status --------------------------------------------------

// UpdatePaymentStatusInput is the input to UpdatePaymentStatus.
type UpdatePaymentStatusInput struct {
        NewPaymentStatus string
        PaymentReference string
        AuthorID         *uuid.UUID
        Reason           string
}

// UpdatePaymentStatus updates the payment_status. Validates the transition
// via paymentTransitions. Inserts an order_event with to_status=current order
// status (payment changes don't change order status, but they're tracked).
//
// We store the payment transition as an order_event with from_status=
// to_status=current order status, and the reason includes the payment
// transition (e.g. "payment_status: declared → paid, ref=XYZ").
func (r *OrderRepository) UpdatePaymentStatus(ctx context.Context, shopID, userID uuid.UUID, role string, orderID uuid.UUID, in UpdatePaymentStatusInput) (*models.Order, error) {
        var order models.Order
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                const sq = `
                        SELECT id, shop_id, number, customer_id, cart_id, status, payment_status,
                               payment_mode, payment_reference, subtotal, delivery_fee, total,
                               idempotency_key, delivery_zone_id, delivery_address, note,
                               created_at, confirmed_at, updated_at,
                               payment_deadline, revenue_counted
                          FROM orders WHERE id = $1
                          FOR UPDATE
                `
                if err := scanOrder(tx.QueryRow(ctx, sq, orderID), &order); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrOrderNotFound
                        }
                        return fmt.Errorf("order repo: update payment: get: %w", err)
                }
                if !IsPaymentTransitionAllowed(string(order.PaymentStatus), in.NewPaymentStatus) {
                        return ErrInvalidPaymentTransition
                }
                var refArg any = order.PaymentReference
                if in.PaymentReference != "" {
                        s := in.PaymentReference
                        refArg = s
                }
                const uq = `
                        UPDATE orders
                           SET payment_status = $2::payment_status,
                               payment_reference = $3,
                               updated_at = now()
                         WHERE id = $1
                         RETURNING id, shop_id, number, customer_id, cart_id, status, payment_status,
                                   payment_mode, payment_reference, subtotal, delivery_fee, total,
                                   idempotency_key, delivery_zone_id, delivery_address, note,
                                   created_at, confirmed_at, updated_at,
                                   payment_deadline, revenue_counted
                `
                if err := scanOrder(tx.QueryRow(ctx, uq, orderID, in.NewPaymentStatus, refArg), &order); err != nil {
                        return fmt.Errorf("order repo: update payment: update: %w", err)
                }
                // Insert an order_event tracking the payment transition. The
                // from_status = to_status = current order status (no order
                // status change); the reason captures the payment transition.
                var authorArg any
                if in.AuthorID != nil {
                        authorArg = *in.AuthorID
                }
                reason := fmt.Sprintf("payment_status: %s → %s", order.PaymentStatus, in.NewPaymentStatus)
                if in.PaymentReference != "" {
                        reason += ", ref=" + in.PaymentReference
                }
                if in.Reason != "" {
                        reason += " — " + in.Reason
                }
                if _, err := tx.Exec(ctx, `
                        INSERT INTO order_events (order_id, shop_id, from_status, to_status, author_id, reason)
                        VALUES ($1, $2, $3::order_status, $4::order_status, $5, $6)
                `, orderID, shopID, string(order.Status), string(order.Status), authorArg, reason); err != nil {
                        return fmt.Errorf("order repo: update payment: insert event: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &order, nil
}

// --- Counters / dashboard ---------------------------------------------------

// CountByStatus returns a map of status → count for the shop's orders.
func (r *OrderRepository) CountByStatus(ctx context.Context, shopID, userID uuid.UUID, role string) (map[string]int64, error) {
        out := make(map[string]int64)
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                rows, err := tx.Query(ctx, `
                        SELECT status::text, COUNT(*) FROM orders WHERE shop_id = $1 GROUP BY status
                `, shopID)
                if err != nil {
                        return fmt.Errorf("order repo: count by status: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var s string
                        var n int64
                        if err := rows.Scan(&s, &n); err != nil {
                                return err
                        }
                        out[s] = n
                }
                return rows.Err()
        })
        if err != nil {
                return nil, err
        }
        return out, nil
}

// CountByPaymentStatus returns a map of payment_status → count for the shop.
func (r *OrderRepository) CountByPaymentStatus(ctx context.Context, shopID, userID uuid.UUID, role string) (map[string]int64, error) {
        out := make(map[string]int64)
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                rows, err := tx.Query(ctx, `
                        SELECT payment_status::text, COUNT(*) FROM orders WHERE shop_id = $1 GROUP BY payment_status
                `, shopID)
                if err != nil {
                        return fmt.Errorf("order repo: count by payment: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var s string
                        var n int64
                        if err := rows.Scan(&s, &n); err != nil {
                                return err
                        }
                        out[s] = n
                }
                return rows.Err()
        })
        if err != nil {
                return nil, err
        }
        return out, nil
}

// CountPending returns the number of orders with status='pending' (waiting
// for merchant confirmation).
func (r *OrderRepository) CountPending(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE shop_id = $1 AND status = 'pending'`, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("order repo: count pending: %w", err)
        }
        return n, nil
}

// CountToday returns the number of orders created today.
func (r *OrderRepository) CountToday(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM orders
                         WHERE shop_id = $1 AND created_at >= date_trunc('day', now())
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("order repo: count today: %w", err)
        }
        return n, nil
}

// CountTotal returns the total number of orders for the shop.
func (r *OrderRepository) CountTotal(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE shop_id = $1`, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("order repo: count total: %w", err)
        }
        return n, nil
}

// MonthRevenue returns the sum of totals of orders delivered or in delivery
// this month + the count of those orders.
func (r *OrderRepository) MonthRevenue(ctx context.Context, shopID, userID uuid.UUID, role string) (int64, int64, error) {
        var (
                revenue int64
                count   int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COALESCE(SUM(total), 0), COUNT(*)
                          FROM orders
                         WHERE shop_id = $1
                           AND created_at >= date_trunc('month', now())
                           AND status IN ('confirmed','preparing','delivering','delivered')
                `, shopID).Scan(&revenue, &count)
        })
        if err != nil {
                return 0, 0, fmt.Errorf("order repo: month revenue: %w", err)
        }
        return revenue, count, nil
}

// --- Order number generation -----------------------------------------------

// GenerateOrderNumber returns the next sequential order number for the shop,
// formatted as "#NNNN" (starting at #1001). Uses COUNT + 1 within a
// transaction; the UNIQUE (shop_id, number) constraint protects against
// races (the caller retries on ErrOrderNumberTaken).
func (r *OrderRepository) GenerateOrderNumber(ctx context.Context, shopID, userID uuid.UUID, role string) (string, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE shop_id = $1`, shopID).Scan(&n); err != nil {
                        return fmt.Errorf("order repo: generate number: count: %w", err)
                }
                return nil
        })
        if err != nil {
                return "", err
        }
        return fmt.Sprintf("#%d", 1001+n), nil
}

// --- List events ------------------------------------------------------------

// ListEvents returns the full event history for an order.
func (r *OrderRepository) ListEvents(ctx context.Context, shopID, userID uuid.UUID, role string, orderID uuid.UUID) ([]models.OrderEvent, error) {
        var out []models.OrderEvent
        err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
                rows, err := tx.Query(ctx, `
                        SELECT id, order_id, shop_id, from_status, to_status, author_id, reason, created_at
                          FROM order_events WHERE order_id = $1 ORDER BY created_at
                `, orderID)
                if err != nil {
                        return fmt.Errorf("order repo: list events: %w", err)
                }
                defer rows.Close()
                for rows.Next() {
                        var ev models.OrderEvent
                        var fromStatus *string
                        var authorID *uuid.UUID
                        var reason *string
                        if err := rows.Scan(
                                &ev.ID, &ev.OrderID, &ev.ShopID, &fromStatus, &ev.ToStatus, &authorID, &reason, &ev.CreatedAt,
                        ); err != nil {
                                return err
                        }
                        if fromStatus != nil {
                                s := models.OrderStatus(*fromStatus)
                                ev.FromStatus = &s
                        }
                        ev.AuthorID = authorID
                        ev.Reason = reason
                        out = append(out, ev)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, err
        }
        if out == nil {
                out = []models.OrderEvent{}
        }
        return out, nil
}

// --- helpers ----------------------------------------------------------------

// scanOrder maps an orders row into a models.Order.
func scanOrder(s scanner, o *models.Order) error {
        var (
                cartID           *uuid.UUID
                status           string
                paymentStatus    string
                paymentMode      string
                paymentReference *string
                idempotencyKey   *string
                deliveryZoneID   *uuid.UUID
                deliveryAddress  *string
                note             *string
                confirmedAt      *time.Time
                paymentDeadline  *time.Time
        )
        err := s.Scan(
                &o.ID, &o.ShopID, &o.Number, &o.CustomerID, &cartID, &status, &paymentStatus,
                &paymentMode, &paymentReference, &o.Subtotal, &o.DeliveryFee, &o.Total,
                &idempotencyKey, &deliveryZoneID, &deliveryAddress, &note,
                &o.CreatedAt, &confirmedAt, &o.UpdatedAt,
                &paymentDeadline, &o.RevenueCounted,
        )
        if err != nil {
                return err
        }
        o.CartID = cartID
        o.Status = models.OrderStatus(status)
        o.PaymentStatus = models.PaymentStatus(paymentStatus)
        o.PaymentMode = models.PaymentMode(paymentMode)
        o.PaymentReference = paymentReference
        o.IdempotencyKey = idempotencyKey
        o.DeliveryZoneID = deliveryZoneID
        o.DeliveryAddress = deliveryAddress
        o.Note = note
        o.ConfirmedAt = confirmedAt
        o.PaymentDeadline = paymentDeadline
        return nil
}

// scanOrderWithExtras scans an order row + customer name + items count.
// status/paymentStatus/paymentMode are read as text and converted.
func scanOrderWithExtras(s scanner, o *models.Order, customerName **string, itemsCount *int, status, paymentStatus, paymentMode *string) error {
        var (
                cartID           *uuid.UUID
                paymentReference *string
                idempotencyKey   *string
                deliveryZoneID   *uuid.UUID
                deliveryAddress  *string
                note             *string
                confirmedAt      *time.Time
                paymentDeadline  *time.Time
        )
        err := s.Scan(
                &o.ID, &o.ShopID, &o.Number, &o.CustomerID, &cartID, status, paymentStatus,
                paymentMode, &paymentReference, &o.Subtotal, &o.DeliveryFee, &o.Total,
                &idempotencyKey, &deliveryZoneID, &deliveryAddress, &note,
                &o.CreatedAt, &confirmedAt, &o.UpdatedAt,
                &paymentDeadline, &o.RevenueCounted,
                customerName, itemsCount,
        )
        if err != nil {
                return err
        }
        o.CartID = cartID
        o.Status = models.OrderStatus(*status)
        o.PaymentStatus = models.PaymentStatus(*paymentStatus)
        o.PaymentMode = models.PaymentMode(*paymentMode)
        o.PaymentReference = paymentReference
        o.IdempotencyKey = idempotencyKey
        o.DeliveryZoneID = deliveryZoneID
        o.DeliveryAddress = deliveryAddress
        o.Note = note
        o.ConfirmedAt = confirmedAt
        o.PaymentDeadline = paymentDeadline
        return nil
}
