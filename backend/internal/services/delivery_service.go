// Delivery service — orchestrates delivery zone lifecycle + fee calculation.
//
// Per cahier des charges (ch. 4.4):
//   - Each shop defines its own zones and tariffs. NOVA imposes no tariff.
//   - Each zone can have aliases (text[]) for AI matching.
//   - Options per zone: fee, estimated_delay, free_from threshold,
//     min_order_amount, active flag.
//   - Unknown/ambiguous zone: NOVA must ask for clarification; never
//     calculate a fee for a guess. MatchZone returns ErrZoneAmbiguous
//     when more than one zone matches.
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

// Delivery service sentinel errors.
var (
        ErrZoneNotFound     = errors.New("delivery zone not found")
        ErrZoneAmbiguous    = repository.ErrZoneAmbiguous
        ErrZoneInactive     = repository.ErrZoneInactive
        ErrOrderBelowMin    = repository.ErrOrderBelowMinimum
        ErrInvalidFee       = errors.New("fee must be >= 0")
        ErrInvalidFreeFrom  = errors.New("free_from must be >= 0")
)

// DeliveryService is the delivery business-logic layer.
type DeliveryService struct {
        zoneRepo  *repository.DeliveryZoneRepository
        auditRepo *repository.AuditRepository
        pool      *pgxpool.Pool
}

// NewDeliveryService constructs a DeliveryService.
func NewDeliveryService(
        zoneRepo *repository.DeliveryZoneRepository,
        auditRepo *repository.AuditRepository,
        pool *pgxpool.Pool,
) *DeliveryService {
        return &DeliveryService{
                zoneRepo:  zoneRepo,
                auditRepo: auditRepo,
                pool:      pool,
        }
}

// CreateZone creates a delivery zone.
func (s *DeliveryService) CreateZone(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, req models.CreateDeliveryZoneRequest, ip, userAgent string) (*models.DeliveryZone, error) {
        if req.Fee < 0 {
                return nil, ErrInvalidFee
        }
        active := true
        if req.Active != nil {
                active = *req.Active
        }
        z, err := s.zoneRepo.Create(ctx, shopID, userID, userRole, repository.CreateDeliveryZoneInput{
                Name:           req.Name,
                Aliases:        req.Aliases,
                Fee:            req.Fee,
                EstimatedDelay: req.EstimatedDelay,
                FreeFrom:       req.FreeFrom,
                MinOrderAmount: req.MinOrderAmount,
                Active:         active,
        })
        if err != nil {
                return nil, fmt.Errorf("create zone: %w", err)
        }
        shopIDForLog := shopID
        zoneIDForLog := z.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "delivery_zone.create",
                ObjectType: "delivery_zone",
                ObjectID:   &zoneIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      zoneSnapshotFrom(z),
        })
        return z, nil
}

// GetZone returns a single delivery zone.
func (s *DeliveryService) GetZone(ctx context.Context, userID uuid.UUID, userRole string, shopID, zoneID uuid.UUID) (*models.DeliveryZone, error) {
        z, err := s.zoneRepo.GetByID(ctx, shopID, userID, userRole, zoneID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrZoneNotFound
                }
                return nil, fmt.Errorf("get zone: %w", err)
        }
        return z, nil
}

// ListZones returns all delivery zones for the shop.
func (s *DeliveryService) ListZones(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, onlyActive bool) ([]models.DeliveryZone, error) {
        var (
                zones []models.DeliveryZone
                err   error
        )
        if onlyActive {
                zones, err = s.zoneRepo.ListActive(ctx, shopID, userID, userRole)
        } else {
                zones, err = s.zoneRepo.List(ctx, shopID, userID, userRole)
        }
        if err != nil {
                return nil, fmt.Errorf("list zones: %w", err)
        }
        if zones == nil {
                zones = []models.DeliveryZone{}
        }
        return zones, nil
}

// UpdateZone updates a delivery zone.
func (s *DeliveryService) UpdateZone(ctx context.Context, userID uuid.UUID, userRole string, shopID, zoneID uuid.UUID, req models.UpdateDeliveryZoneRequest, ip, userAgent string) (*models.DeliveryZone, error) {
        if req.Fee != nil && *req.Fee < 0 {
                return nil, ErrInvalidFee
        }
        before, _ := s.zoneRepo.GetByID(ctx, shopID, userID, userRole, zoneID)
        z, err := s.zoneRepo.Update(ctx, shopID, userID, userRole, zoneID, repository.UpdateDeliveryZoneInput{
                Name:               req.Name,
                Aliases:            req.Aliases,
                Fee:                req.Fee,
                EstimatedDelay:     req.EstimatedDelay,
                FreeFrom:           req.FreeFrom,
                MinOrderAmount:     req.MinOrderAmount,
                Active:             req.Active,
                FreeFromClear:      req.FreeFrom == nil && req.MinOrderAmount == nil && false, // not exposing clear endpoints in MVP
                MinOrderAmountClear: false,
                EstimatedDelayClear: false,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrZoneNotFound
                }
                return nil, fmt.Errorf("update zone: %w", err)
        }
        shopIDForLog := shopID
        zoneIDForLog := zoneID
        var beforeSnap json.Marshaler
        if before != nil {
                beforeSnap = zoneSnapshotFrom(before)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "delivery_zone.update",
                ObjectType: "delivery_zone",
                ObjectID:   &zoneIDForLog,
                Before:     beforeSnap,
                After:      zoneSnapshotFrom(z),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return z, nil
}

// DeleteZone deletes a delivery zone.
func (s *DeliveryService) DeleteZone(ctx context.Context, userID uuid.UUID, userRole string, shopID, zoneID uuid.UUID, ip, userAgent string) error {
        before, _ := s.zoneRepo.GetByID(ctx, shopID, userID, userRole, zoneID)
        if err := s.zoneRepo.Delete(ctx, shopID, userID, userRole, zoneID); err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return ErrZoneNotFound
                }
                return fmt.Errorf("delete zone: %w", err)
        }
        shopIDForLog := shopID
        zoneIDForLog := zoneID
        var beforeSnap json.Marshaler
        if before != nil {
                beforeSnap = zoneSnapshotFrom(before)
        }
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  userRole,
                Action:     "delivery_zone.delete",
                ObjectType: "delivery_zone",
                ObjectID:   &zoneIDForLog,
                Before:     beforeSnap,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })
        return nil
}

// MatchZone finds a zone whose name OR one of its aliases matches the
// query (case-insensitive). Returns ErrZoneAmbiguous if multiple zones
// match (the AI must ask for clarification).
func (s *DeliveryService) MatchZone(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, query string) (*models.DeliveryZone, error) {
        z, err := s.zoneRepo.MatchByQuery(ctx, shopID, userID, userRole, query)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrZoneNotFound
                }
                return nil, err // ErrZoneAmbiguous passes through.
        }
        return z, nil
}

// CalculateFee computes the delivery fee for the given zone and order amount.
func (s *DeliveryService) CalculateFee(ctx context.Context, userID uuid.UUID, userRole string, shopID, zoneID uuid.UUID, orderAmount int64) (*repository.CalculateFeeResult, error) {
        result, err := s.zoneRepo.CalculateFee(ctx, shopID, userID, userRole, zoneID, orderAmount)
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
                return nil, fmt.Errorf("calculate fee: %w", err)
        }
        return result, nil
}

// --- helpers ----------------------------------------------------------------

// zoneSnapshot for audit logs.
type zoneSnapshot struct {
        ID     string `json:"id"`
        Name   string `json:"name"`
        Fee    int64  `json:"fee"`
        Active bool   `json:"active"`
}

func (z zoneSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                ID     string `json:"id"`
                Name   string `json:"name"`
                Fee    int64  `json:"fee"`
                Active bool   `json:"active"`
        }{ID: z.ID, Name: z.Name, Fee: z.Fee, Active: z.Active})
}

func zoneSnapshotFrom(z *models.DeliveryZone) zoneSnapshot {
        if z == nil {
                return zoneSnapshot{}
        }
        return zoneSnapshot{
                ID:     z.ID.String(),
                Name:   z.Name,
                Fee:    z.Fee,
                Active: z.Active,
        }
}
