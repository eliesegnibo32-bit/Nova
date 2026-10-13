// Stock service — orchestrates inventory + stock movement lifecycle.
//
// The service is the single entry point for stock business logic. HTTP
// handlers in internal/api/handlers/stock.go call into this service.
//
// CRITICAL: the Reserve method delegates to inventoryRepo.Reserve which
// performs the atomic UPDATE — see internal/repository/inventory.go for the
// exact SQL. The service wraps it with audit logging and returns
// ErrInsufficientStock (a service-level sentinel) on failure.
package services

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// Stock service sentinel errors.
var (
        ErrInventoryNotFound = errors.New("inventory not found")
        // ErrInsufficientStock is returned when a reservation/exit/adjustment
        // cannot be performed because available stock is insufficient. This
        // is the application-level sentinel; the repository layer also
        // returns the same value via repository.ErrInsufficientStock (same
        // underlying error).
        ErrInsufficientStock = repository.ErrInsufficientStock
        ErrInvalidQuantity   = errors.New("quantity must be greater than zero")
        ErrReasonRequired    = errors.New("reason is required for stock adjustments")
)

// StockService is the stock business-logic layer.
type StockService struct {
        inventoryRepo *repository.InventoryRepository
        auditRepo     *repository.AuditRepository
        pool          *pgxpool.Pool
}

// NewStockService constructs a StockService.
func NewStockService(
        inventoryRepo *repository.InventoryRepository,
        auditRepo *repository.AuditRepository,
        pool *pgxpool.Pool,
) *StockService {
        return &StockService{
                inventoryRepo: inventoryRepo,
                auditRepo:     auditRepo,
                pool:          pool,
        }
}

// GetInventory returns the inventory for a variant.
func (s *StockService) GetInventory(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID) (*models.Inventory, error) {
        inv, err := s.inventoryRepo.GetByVariant(ctx, shopID, userID, userRole, variantID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                return nil, fmt.Errorf("get inventory: %w", err)
        }
        return inv, nil
}

// ListInventory returns paginated inventory for the shop.
func (s *StockService) ListInventory(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, params models.ListInventoryParams) ([]InventoryWithVariant, int64, error) {
        params.Normalize()
        items, total, err := s.inventoryRepo.ListByShop(ctx, shopID, userID, userRole, repository.ListInventoryRepoParams{
                Page:   params.Page,
                Limit:  params.Limit,
                Filter: params.Filter,
                Search: params.Search,
        })
        if err != nil {
                return nil, 0, fmt.Errorf("list inventory: %w", err)
        }
        out := make([]InventoryWithVariant, 0, len(items))
        for _, it := range items {
                out = append(out, InventoryWithVariant{
                        Inventory:   it.Inventory,
                        ProductID:   it.ProductID,
                        ProductName: it.ProductName,
                        SKU:         it.SKU,
                        Size:        it.Size,
                        Color:       it.Color,
                        Price:       it.Price,
                        Active:      it.Active,
                })
        }
        return out, total, nil
}

// InventoryWithVariant is the service-level list-view shape, mirroring
// repository.InventoryWithVariant.
type InventoryWithVariant struct {
        Inventory   models.Inventory
        ProductID   uuid.UUID
        ProductName string
        SKU         string
        Size        *string
        Color       *string
        Price       int64
        Active      bool
}

// AdjustStock applies a manual adjustment (delta can be negative). Reason
// is mandatory.
func (s *StockService) AdjustStock(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, req models.AdjustStockRequest, ip, userAgent string) (*models.Inventory, error) {
        if req.Reason == "" {
                return nil, ErrReasonRequired
        }
        if req.Delta == 0 {
                return nil, ErrInvalidQuantity
        }
        inv, mut, err := s.inventoryRepo.AdjustStock(ctx, shopID, userID, userRole, variantID, repository.AdjustStockInput{
                Delta:    req.Delta,
                Reason:   req.Reason,
                AuthorID: userID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                if errors.Is(err, repository.ErrInsufficientStock) {
                        return nil, ErrInsufficientStock
                }
                return nil, fmt.Errorf("adjust stock: %w", err)
        }
        s.logMovement(ctx, shopID, userID, userRole, "stock.adjust", mut, ip, userAgent)
        return inv, nil
}

// ReceiveStock adds a positive quantity to on_hand.
func (s *StockService) ReceiveStock(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, req models.ReceiveStockRequest, ip, userAgent string) (*models.Inventory, error) {
        if req.Quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        inv, mut, err := s.inventoryRepo.ReceiveStock(ctx, shopID, userID, userRole, variantID, repository.ReceiveStockInput{
                Quantity: req.Quantity,
                Reason:   req.Reason,
                AuthorID: userID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                return nil, fmt.Errorf("receive stock: %w", err)
        }
        s.logMovement(ctx, shopID, userID, userRole, "stock.receive", mut, ip, userAgent)
        return inv, nil
}

// ListMovements returns the movement history for a variant (or all
// variants if variantID is nil).
func (s *StockService) ListMovements(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, variantID *uuid.UUID, params models.ListMovementsParams) ([]models.StockMovement, int64, error) {
        params.Normalize()
        movements, total, err := s.inventoryRepo.ListMovements(ctx, shopID, userID, userRole, variantID, repository.ListMovementsRepoParams{
                Page:  params.Page,
                Limit: params.Limit,
                Type:  params.Type,
                From:  params.From,
                To:    params.To,
        })
        if err != nil {
                return nil, 0, fmt.Errorf("list movements: %w", err)
        }
        if movements == nil {
                movements = []models.StockMovement{}
        }
        return movements, total, nil
}

// SetAlertThreshold sets the low-stock alert threshold for a variant.
func (s *StockService) SetAlertThreshold(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, threshold int, ip, userAgent string) error {
        if err := s.inventoryRepo.SetAlertThreshold(ctx, shopID, userID, userRole, variantID, threshold); err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return ErrInventoryNotFound
                }
                return fmt.Errorf("set alert threshold: %w", err)
        }
        shopIDForLog := shopID
        varIDForLog := variantID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "stock.threshold.set",
                ObjectType: "inventory",
                ObjectID:   &varIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      inventorySnapshot{VariantID: variantID.String(), AlertThreshold: threshold},
        })
        return nil
}

// Reserve performs an atomic reservation. Used by the order module when a
// cart is confirmed. Returns ErrInsufficientStock on failure.
func (s *StockService) Reserve(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, quantity int, orderID *uuid.UUID, ip, userAgent string) (*models.Inventory, error) {
        if quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        inv, mut, err := s.inventoryRepo.Reserve(ctx, shopID, userID, userRole, variantID, repository.ReserveInput{
                Quantity: quantity,
                OrderID:  orderID,
                AuthorID: &userID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                if errors.Is(err, repository.ErrInsufficientStock) {
                        return nil, ErrInsufficientStock
                }
                return nil, fmt.Errorf("reserve stock: %w", err)
        }
        s.logMovement(ctx, shopID, userID, userRole, "stock.reserve", mut, ip, userAgent)
        return inv, nil
}

// Release releases a previously-reserved quantity. Used on order
// cancellation / delivery failure.
func (s *StockService) Release(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, quantity int, orderID *uuid.UUID, ip, userAgent string) (*models.Inventory, error) {
        if quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        inv, mut, err := s.inventoryRepo.Release(ctx, shopID, userID, userRole, variantID, repository.ReleaseInput{
                Quantity: quantity,
                OrderID:  orderID,
                AuthorID: &userID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                return nil, fmt.Errorf("release stock: %w", err)
        }
        s.logMovement(ctx, shopID, userID, userRole, "stock.release", mut, ip, userAgent)
        return inv, nil
}

// ExitStock physically exits stock on delivery (on_hand -= q AND reserved -= q).
func (s *StockService) ExitStock(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, quantity int, orderID *uuid.UUID, ip, userAgent string) (*models.Inventory, error) {
        if quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        inv, mut, err := s.inventoryRepo.ExitStock(ctx, shopID, userID, userRole, variantID, repository.ExitStockInput{
                Quantity: quantity,
                OrderID:  orderID,
                AuthorID: &userID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                if errors.Is(err, repository.ErrInsufficientStock) {
                        return nil, ErrInsufficientStock
                }
                return nil, fmt.Errorf("exit stock: %w", err)
        }
        s.logMovement(ctx, shopID, userID, userRole, "stock.exit", mut, ip, userAgent)
        return inv, nil
}

// ReturnStock re-enters returned goods (on_hand += q).
func (s *StockService) ReturnStock(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID, quantity int, orderID *uuid.UUID, ip, userAgent string) (*models.Inventory, error) {
        if quantity <= 0 {
                return nil, ErrInvalidQuantity
        }
        inv, mut, err := s.inventoryRepo.ReturnStock(ctx, shopID, userID, userRole, variantID, repository.ReturnStockInput{
                Quantity: quantity,
                OrderID:  orderID,
                AuthorID: &userID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrInventoryNotFound
                }
                return nil, fmt.Errorf("return stock: %w", err)
        }
        s.logMovement(ctx, shopID, userID, userRole, "stock.return", mut, ip, userAgent)
        return inv, nil
}

// DashboardStats returns the stock dashboard stats for the shop.
func (s *StockService) DashboardStats(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID) (*models.StockDashboardStats, error) {
        totalVariants, err := s.inventoryRepo.CountVariants(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, fmt.Errorf("count variants: %w", err)
        }
        lowStock, err := s.inventoryRepo.CountLowStock(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, fmt.Errorf("count low stock: %w", err)
        }
        outOfStock, err := s.inventoryRepo.CountOutOfStock(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, fmt.Errorf("count out of stock: %w", err)
        }
        totalValue, reservedValue, err := s.inventoryRepo.TotalValue(ctx, shopID, userID, userRole)
        if err != nil {
                return nil, fmt.Errorf("total value: %w", err)
        }
        return &models.StockDashboardStats{
                TotalVariants:    totalVariants,
                LowStockCount:    lowStock,
                OutOfStockCount:  outOfStock,
                TotalValueCFA:    totalValue,
                ReservedValueCFA: reservedValue,
        }, nil
}

// --- helpers ----------------------------------------------------------------

// logMovement is a best-effort audit log for a stock movement. It doesn't
// return an error — the movement has already been recorded in
// stock_movements (the immutable journal); the audit log is a secondary
// record for the activity feed.
func (s *StockService) logMovement(ctx context.Context, shopID, userID uuid.UUID, userRole, action string, mut *models.StockMovement, ip, userAgent string) {
        if mut == nil {
                return
        }
        shopIDForLog := shopID
        mutIDForLog := mut.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     action,
                ObjectType: "stock_movement",
                ObjectID:   &mutIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      movementSnapshotFrom(mut),
        })
}

// inventorySnapshot for audit logs.
type inventorySnapshot struct {
        VariantID      string `json:"variant_id"`
        AlertThreshold int    `json:"alert_threshold"`
}

func (i inventorySnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                VariantID      string `json:"variant_id"`
                AlertThreshold int    `json:"alert_threshold"`
        }{VariantID: i.VariantID, AlertThreshold: i.AlertThreshold})
}

// movementSnapshot for audit logs.
type movementSnapshot struct {
        ID       string `json:"id"`
        Type     string `json:"type"`
        Quantity int    `json:"quantity"`
}

func (m movementSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                ID       string `json:"id"`
                Type     string `json:"type"`
                Quantity int    `json:"quantity"`
        }{ID: m.ID, Type: m.Type, Quantity: m.Quantity})
}

func movementSnapshotFrom(m *models.StockMovement) movementSnapshot {
        if m == nil {
                return movementSnapshot{}
        }
        return movementSnapshot{
                ID:       m.ID.String(),
                Type:     m.Type,
                Quantity: m.Quantity,
        }
}
