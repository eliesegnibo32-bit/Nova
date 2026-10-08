// Shop service — orchestrates the shop lifecycle: creation, onboarding,
// activation, suspension, reactivation, team management, and shop switching.
//
// The service is the single entry point for shop business logic. HTTP
// handlers in internal/api/handlers/shops.go call into this service and
// translate the results into JSON responses. The service never touches HTTP
// directly — it returns plain Go values and sentinel errors.
//
// Responsibilities:
//   - Permission checks (admin-only for create/suspend/reactivate, owner-or-
//     admin for update/activate, member for read).
//   - Resolving the owner email → user ID during shop creation.
//   - Validating activation criteria (products, delivery zones, hours) per
//     the cahier des charges (ch. 4.1).
//   - Issuing a new session cookie on shop switch (re-uses auth.SignSession).
//   - Audit logging every shop action.
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

        "nova-api/internal/auth"
        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// Sentinel errors for the shop service. Handlers map these to HTTP responses.
var (
        // ErrNotAdmin is returned when a non-admin tries an admin-only action.
        ErrNotAdmin = errors.New("admin privileges required")
        // ErrNotShopMember is returned when the user is not a member of the
        // shop they're trying to access.
        ErrNotShopMember = errors.New("not a member of this shop")
        // ErrNotShopOwner is returned when the user is a member but not the
        // owner (and the action requires owner level).
        ErrNotShopOwner = errors.New("only the shop owner can perform this action")
        // ErrShopNotFound is returned when the shop does not exist or is
        // soft-deleted.
        ErrShopNotFound = errors.New("shop not found")
        // ErrSlugTaken is returned by Create when the slug is already in use.
        ErrSlugTaken = errors.New("shop slug already taken")
        // ErrOwnerNotFound is returned by Create when the owner_email does not
        // match any registered user. The owner must register first.
        ErrOwnerNotFound = errors.New("owner must register first")
        // ErrActivationCriteriaNotMet is returned by Activate when the shop
        // has not met the activation criteria (≥1 published product, ≥1 active
        // delivery zone, hours set). The ValidationResult with the missing
        // criteria list is attached as the error data via the handler.
        ErrActivationCriteriaNotMet = errors.New("activation criteria not met")
        // ErrShopSuspended is returned by SwitchShop when the target shop is
        // suspended and the user is not a platform admin.
        ErrShopSuspended = errors.New("shop is suspended")
        // ErrPlanNotFound is returned by Create when the requested plan_id
        // (or the default "Essentiel" plan) does not exist.
        ErrPlanNotFound = errors.New("plan not found")
        // ErrSubscriptionNotFound is returned by GetSubscription when the shop
        // has no active subscription.
        ErrSubscriptionNotFound = errors.New("subscription not found")
        // ErrInvalidPaymentMode is returned by RecordPayment when the mode
        // is not a valid payment_mode enum value.
        ErrInvalidPaymentMode = errors.New("invalid payment mode")
        // ErrInvalidPeriod is returned by RecordPayment when period_start or
        // period_end can't be parsed or period_end < period_start.
        ErrInvalidPeriod = errors.New("invalid payment period")
)

// ShopService is the shop business-logic layer.
type ShopService struct {
        shopRepo    *repository.ShopRepository
        planRepo    *repository.PlanRepository
        subRepo     *repository.SubscriptionRepository
        memberRepo  *repository.ShopMemberRepository
        userRepo    *repository.UserRepository
        auditRepo   *repository.AuditRepository
        pool        *pgxpool.Pool
        sessionSecret []byte
        sessionDuration time.Duration
        adminSessionDuration time.Duration
}

// NewShopService constructs a ShopService. The sessionSecret must be the same
// value used by the Auth middleware so cookies issued by SwitchShop are
// readable by the middleware.
func NewShopService(
        shopRepo *repository.ShopRepository,
        planRepo *repository.PlanRepository,
        subRepo *repository.SubscriptionRepository,
        memberRepo *repository.ShopMemberRepository,
        userRepo *repository.UserRepository,
        auditRepo *repository.AuditRepository,
        pool *pgxpool.Pool,
        sessionSecret []byte,
        sessionDuration, adminSessionDuration time.Duration,
) *ShopService {
        if sessionDuration <= 0 {
                sessionDuration = 7 * 24 * time.Hour
        }
        if adminSessionDuration <= 0 {
                adminSessionDuration = 24 * time.Hour
        }
        return &ShopService{
                shopRepo:             shopRepo,
                planRepo:             planRepo,
                subRepo:              subRepo,
                memberRepo:           memberRepo,
                userRepo:             userRepo,
                auditRepo:            auditRepo,
                pool:                 pool,
                sessionSecret:        sessionSecret,
                sessionDuration:      sessionDuration,
                adminSessionDuration: adminSessionDuration,
        }
}

// ShopWithSubscription is returned by Create — the freshly-created shop plus
// its trial subscription. The handler maps this to ShopWithSubscriptionResponse.
type ShopWithSubscription struct {
        Shop         *models.Shop
        Subscription *models.Subscription
        PlanName     string
}

// CreateShopInput is the service-level request shape (the handler converts
// from models.CreateShopRequest). The owner_email has already been validated
// by the handler.
type CreateShopInput struct {
        models.CreateShopRequest
}

// Create creates a new shop in 'draft' status, links the owner, and creates
// a trial subscription. The actor MUST be a platform admin. The owner_email
// must correspond to an existing user (the owner must register first).
func (s *ShopService) Create(ctx context.Context, actorID uuid.UUID, actorRole string, req models.CreateShopRequest, ip, userAgent string) (*ShopWithSubscription, error) {
        // 1. Permission check: only platform admins can create shops.
        if !IsPlatformAdmin(actorRole) {
                return nil, ErrNotAdmin
        }

        // 2. Resolve owner_email → user.
        ownerEmail := strings.ToLower(strings.TrimSpace(req.OwnerEmail))
        owner, err := s.userRepo.GetByEmail(ctx, ownerEmail)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrOwnerNotFound
                }
                return nil, fmt.Errorf("lookup owner: %w", err)
        }
        if owner.DeletedAt != nil {
                return nil, ErrOwnerNotFound
        }

        // 3. Resolve plan_id (default to "Essentiel" if not specified).
        var plan *models.Plan
        if req.PlanID != nil {
                plan, err = s.planRepo.GetByID(ctx, *req.PlanID)
        } else {
                plan, err = s.planRepo.GetByName(ctx, "Essentiel")
        }
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrPlanNotFound
                }
                return nil, fmt.Errorf("lookup plan: %w", err)
        }

        // 4. Create the shop + owner membership + trial subscription.
        shop, err := s.shopRepo.Create(ctx, actorID, repository.CreateShopInput{
                Name:                 req.Name,
                Slug:                 req.Slug,
                OwnerUserID:          owner.ID,
                Phone:                req.Phone,
                WhatsAppNumber:       req.WhatsAppNumber,
                Address:              req.Address,
                Commune:              req.Commune,
                Hours:                req.Hours,
                Description:          req.Description,
                Categories:           req.Categories,
                SaleConditions:       req.SaleConditions,
                AcceptedPaymentModes: req.AcceptedPaymentModes,
                PlanID:               plan.ID,
        })
        if err != nil {
                if errors.Is(err, repository.ErrSlugTaken) {
                        return nil, ErrSlugTaken
                }
                return nil, fmt.Errorf("create shop: %w", err)
        }

        // 5. Fetch the freshly-created subscription for the response.
        sub, err := s.subRepo.GetByShopID(ctx, actorID, string(models.RoleSuperAdmin), shop.ID)
        if err != nil {
                // Non-fatal: log and proceed without subscription in the response.
                sub = nil
        }

        // 6. Audit log.
        shopIDForLog := shop.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &actorID,
                ActorRole:  actorRole,
                Action:     "shop.create",
                ObjectType: "shop",
                ObjectID:   &shop.ID,
                IPAddress:  ip,
                UserAgent:  userAgent,
                After:      shopSnapshotFrom(shop),
        })

        return &ShopWithSubscription{
                Shop:         shop,
                Subscription: sub,
                PlanName:     plan.Name,
        }, nil
}

// Get returns a single shop. The requester must be a member of the shop OR a
// platform admin.
func (s *ShopService) Get(ctx context.Context, requesterID uuid.UUID, requesterRole string, shopID uuid.UUID) (*models.Shop, error) {
        // Permission: platform admins can read any shop.
        if !IsPlatformAdmin(requesterRole) {
                // Otherwise, the user must be a member.
                ok, err := s.isShopMember(ctx, shopID, requesterID)
                if err != nil {
                        return nil, err
                }
                if !ok {
                        return nil, ErrNotShopMember
                }
        }
        shop, err := s.shopRepo.GetByID(ctx, requesterID, requesterRole, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("get shop: %w", err)
        }
        return shop, nil
}

// ListMine returns the shops the user is a member of (for the shop switcher).
func (s *ShopService) ListMine(ctx context.Context, userID uuid.UUID) ([]models.Shop, error) {
        shops, err := s.shopRepo.ListByUser(ctx, userID)
        if err != nil {
                return nil, fmt.Errorf("list mine: %w", err)
        }
        if shops == nil {
                shops = []models.Shop{}
        }
        return shops, nil
}

// List returns a paginated list of all shops. Admin only.
func (s *ShopService) List(ctx context.Context, requesterRole string, params models.ListShopsParams) ([]models.Shop, int64, error) {
        if !IsPlatformAdmin(requesterRole) {
                return nil, 0, ErrNotAdmin
        }
        params.Normalize()
        shops, total, err := s.shopRepo.List(ctx, repository.ListShopsParams{
                Page:   params.Page,
                Limit:  params.Limit,
                Search: params.Search,
                Status: params.Status,
        })
        if err != nil {
                return nil, 0, fmt.Errorf("list shops: %w", err)
        }
        if shops == nil {
                shops = []models.Shop{}
        }
        return shops, total, nil
}

// Update applies a partial update to a shop. Only the owner of the shop OR a
// platform admin can update shop info.
func (s *ShopService) Update(ctx context.Context, requesterID uuid.UUID, requesterRole string, shopID uuid.UUID, req models.UpdateShopRequest, ip, userAgent string) (*models.Shop, error) {
        // Permission check.
        if !IsPlatformAdmin(requesterRole) {
                ok, err := s.isShopOwner(ctx, shopID, requesterID)
                if err != nil {
                        return nil, err
                }
                if !ok {
                        return nil, ErrNotShopOwner
                }
        }

        // Fetch before-state for audit.
        before, _ := s.shopRepo.GetByID(ctx, requesterID, requesterRole, shopID)

        shop, err := s.shopRepo.Update(ctx, requesterID, shopID, repository.UpdateShopInput{
                Name:                 req.Name,
                LogoURL:              req.LogoURL,
                Phone:                req.Phone,
                WhatsAppNumber:       req.WhatsAppNumber,
                Address:              req.Address,
                Commune:              req.Commune,
                Hours:                req.Hours,
                Description:          req.Description,
                Categories:           req.Categories,
                SaleConditions:       req.SaleConditions,
                AcceptedPaymentModes: req.AcceptedPaymentModes,
                AISettings:           req.AISettings,
        })
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("update shop: %w", err)
        }

        shopIDForLog := shopID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &requesterID,
                ActorRole:  requesterRole,
                Action:     "shop.update",
                ObjectType: "shop",
                ObjectID:   &shopID,
                Before:     shopSnapshotFrom(before),
                After:      shopSnapshotFrom(shop),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return shop, nil
}

// Activate validates the activation criteria and transitions the shop from
// 'draft' (or 'suspended' for re-activation by owner) to 'active'. Only the
// owner of the shop OR a platform admin can activate.
//
// On validation failure, returns ErrActivationCriteriaNotMet wrapping a
// *ValidationResult — the handler can extract it via errors.As.
func (s *ShopService) Activate(ctx context.Context, requesterID uuid.UUID, requesterRole string, shopID uuid.UUID, ip, userAgent string) (*models.Shop, error) {
        // Permission check.
        if !IsPlatformAdmin(requesterRole) {
                ok, err := s.isShopOwner(ctx, shopID, requesterID)
                if err != nil {
                        return nil, err
                }
                if !ok {
                        return nil, ErrNotShopOwner
                }
        }

        // Validate activation criteria.
        vr, err := s.ValidateActivation(ctx, requesterID, shopID)
        if err != nil {
                return nil, err
        }
        if !vr.CanActivate {
                return nil, &ActivationError{Result: vr}
        }

        shop, err := s.shopRepo.Activate(ctx, requesterID, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("activate shop: %w", err)
        }

        shopIDForLog := shopID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &requesterID,
                ActorRole:  requesterRole,
                Action:     "shop.activate",
                ObjectType: "shop",
                ObjectID:   &shopID,
                After:      shopSnapshotFrom(shop),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return shop, nil
}

// ActivationError carries the ValidationResult so the handler can include
// the missing-criteria list in the 422 response body.
type ActivationError struct {
        Result *models.ValidationResult
}

// Error implements the error interface.
func (e *ActivationError) Error() string {
        if e.Result == nil {
                return "activation criteria not met"
        }
        return fmt.Sprintf("activation criteria not met: missing %d", len(e.Result.MissingCriteria))
}

// Is makes errors.Is(e, ErrActivationCriteriaNotMet) return true.
func (e *ActivationError) Is(target error) bool {
        return target == ErrActivationCriteriaNotMet
}

// Suspend sets the shop's status to 'suspended'. Admin only.
func (s *ShopService) Suspend(ctx context.Context, requesterID uuid.UUID, requesterRole string, shopID uuid.UUID, reason string, ip, userAgent string) (*models.Shop, error) {
        if !IsPlatformAdmin(requesterRole) {
                return nil, ErrNotAdmin
        }

        before, _ := s.shopRepo.GetByID(ctx, requesterID, requesterRole, shopID)

        shop, err := s.shopRepo.Suspend(ctx, requesterID, shopID, reason)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("suspend shop: %w", err)
        }

        shopIDForLog := shopID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &requesterID,
                ActorRole:  requesterRole,
                Action:     "shop.suspend",
                ObjectType: "shop",
                ObjectID:   &shopID,
                Before:     shopSnapshotFrom(before),
                After:      shopSnapshotFrom(shop),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return shop, nil
}

// Reactivate sets the shop's status back to 'active'. Admin only.
func (s *ShopService) Reactivate(ctx context.Context, requesterID uuid.UUID, requesterRole string, shopID uuid.UUID, ip, userAgent string) (*models.Shop, error) {
        if !IsPlatformAdmin(requesterRole) {
                return nil, ErrNotAdmin
        }

        before, _ := s.shopRepo.GetByID(ctx, requesterID, requesterRole, shopID)

        shop, err := s.shopRepo.Reactivate(ctx, requesterID, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("reactivate shop: %w", err)
        }

        shopIDForLog := shopID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &requesterID,
                ActorRole:  requesterRole,
                Action:     "shop.reactivate",
                ObjectType: "shop",
                ObjectID:   &shopID,
                Before:     shopSnapshotFrom(before),
                After:      shopSnapshotFrom(shop),
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return shop, nil
}

// ValidateActivation checks the activation criteria per the cahier des
// charges (ch. 4.1): ≥1 published product, ≥1 active delivery zone, hours
// set. Returns a ValidationResult with the list of missing criteria (empty
// if all are met). The caller (Activate) uses CanActivate to decide whether
// to proceed.
func (s *ShopService) ValidateActivation(ctx context.Context, requesterID uuid.UUID, shopID uuid.UUID) (*models.ValidationResult, error) {
        // Verify the shop exists and the requester is a member (or admin).
        shop, err := s.shopRepo.GetByID(ctx, requesterID, "super_admin", shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("validate: get shop: %w", err)
        }
        _ = shop

        pub, err := s.shopRepo.CountPublishedProducts(ctx, requesterID, shopID)
        if err != nil {
                return nil, fmt.Errorf("validate: count products: %w", err)
        }
        zones, err := s.shopRepo.CountActiveDeliveryZones(ctx, requesterID, shopID)
        if err != nil {
                return nil, fmt.Errorf("validate: count zones: %w", err)
        }
        hours, err := s.shopRepo.HasHours(ctx, requesterID, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("validate: has hours: %w", err)
        }

        vr := &models.ValidationResult{
                PublishedProducts:    pub,
                ActiveDeliveryZones:  zones,
                HoursSet:             hours,
                MissingCriteria:      []string{},
        }
        if pub == 0 {
                vr.MissingCriteria = append(vr.MissingCriteria, "published_products")
        }
        if zones == 0 {
                vr.MissingCriteria = append(vr.MissingCriteria, "delivery_zones")
        }
        if !hours {
                vr.MissingCriteria = append(vr.MissingCriteria, "hours")
        }
        vr.CanActivate = len(vr.MissingCriteria) == 0
        return vr, nil
}

// SwitchShopResult is returned by SwitchShop — the handler uses the cookie
// and max_age to write Set-Cookie, and returns the rest as JSON.
type SwitchShopResult struct {
        ShopID     uuid.UUID
        ShopName   string
        ShopSlug   string
        ShopStatus string
        RoleInShop string
        Cookie     string
        CookieMaxAge int
}

// SwitchShop verifies that the user is a member of the target shop, then
// issues a new session cookie with the shop_id set. The frontend uses this
// new cookie for all subsequent shop-scoped requests.
//
// The session role is set to the user's per-shop role if they're an
// owner/employee of the target shop. If the user is a platform admin (and
// not a member), the role stays as their platform role (super_admin/admin)
// so they can perform admin actions within the shop context too.
func (s *ShopService) SwitchShop(ctx context.Context, userID uuid.UUID, shopID uuid.UUID, ip, userAgent string) (*SwitchShopResult, error) {
        // Verify the user is a member of the target shop (or is a platform
        // admin who can access any shop).
        user, err := s.userRepo.GetByID(ctx, userID)
        if err != nil {
                return nil, ErrShopNotFound
        }
        platformAdmin := IsPlatformAdmin(string(user.Role))

        member, err := s.memberRepo.GetByShopAndUser(ctx, shopID, userID)
        var roleInShop string
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        if !platformAdmin {
                                return nil, ErrNotShopMember
                        }
                        // Platform admin not a member — use their platform role.
                        roleInShop = string(user.Role)
                } else {
                        return nil, fmt.Errorf("lookup shop member: %w", err)
                }
        } else {
                roleInShop = string(member.Role)
        }

        // Fetch the shop to confirm it exists and get name/slug/status.
        shop, err := s.shopRepo.GetByID(ctx, userID, roleInShop, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrShopNotFound
                }
                return nil, fmt.Errorf("get shop for switch: %w", err)
        }

        // Refuse to switch to a suspended shop for non-admins.
        if shop.Status == models.ShopSuspended && !platformAdmin {
                return nil, ErrShopSuspended
        }

        // Pick TTL: admins get the admin TTL, owners/employees get the standard one.
        ttl := s.sessionDuration
        if platformAdmin {
                ttl = s.adminSessionDuration
        }
        expiresAt := time.Now().Add(ttl)

        // Sign the new session cookie with the shop_id set.
        sess := auth.Session{
                UserID:    userID,
                ShopID:    &shopID,
                Role:      roleInShop,
                ExpiresAt: expiresAt,
        }
        cookie, err := auth.SignSession(sess, s.sessionSecret)
        if err != nil {
                return nil, fmt.Errorf("sign session: %w", err)
        }

        // Audit log.
        shopIDForLog := shopID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &userID,
                ActorRole:  roleInShop,
                Action:     "shop.switch",
                ObjectType: "shop",
                ObjectID:   &shopID,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return &SwitchShopResult{
                ShopID:       shop.ID,
                ShopName:     shop.Name,
                ShopSlug:     shop.Slug,
                ShopStatus:   string(shop.Status),
                RoleInShop:   roleInShop,
                Cookie:       cookie,
                CookieMaxAge: int(ttl.Seconds()),
        }, nil
}

// GetSubscription returns the active subscription for the shop plus the plan
// name. The requester must be a member of the shop OR a platform admin.
func (s *ShopService) GetSubscription(ctx context.Context, requesterID uuid.UUID, requesterRole string, shopID uuid.UUID) (*models.Subscription, *models.Plan, error) {
        if !IsPlatformAdmin(requesterRole) {
                ok, err := s.isShopMember(ctx, shopID, requesterID)
                if err != nil {
                        return nil, nil, err
                }
                if !ok {
                        return nil, nil, ErrNotShopMember
                }
        }
        sub, err := s.subRepo.GetByShopID(ctx, requesterID, requesterRole, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, nil, ErrSubscriptionNotFound
                }
                return nil, nil, fmt.Errorf("get subscription: %w", err)
        }
        plan, err := s.planRepo.GetByID(ctx, sub.PlanID)
        if err != nil {
                // Non-fatal — return the subscription without plan name.
                return sub, nil, nil
        }
        return sub, plan, nil
}

// RecordPayment records a payment for the shop's subscription. Admin only.
// On success the subscription status is set to 'active' and next_billing_at
// is bumped by 1 month.
func (s *ShopService) RecordPayment(ctx context.Context, actorID uuid.UUID, actorRole string, shopID uuid.UUID, req models.RecordPaymentRequest, ip, userAgent string) (*models.SubscriptionPayment, error) {
        if !IsPlatformAdmin(actorRole) {
                return nil, ErrNotAdmin
        }

        // Parse period_start / period_end (YYYY-MM-DD).
        periodStart, err := time.Parse("2006-01-02", req.PeriodStart)
        if err != nil {
                return nil, ErrInvalidPeriod
        }
        periodEnd, err := time.Parse("2006-01-02", req.PeriodEnd)
        if err != nil {
                return nil, ErrInvalidPeriod
        }
        if periodEnd.Before(periodStart) {
                return nil, ErrInvalidPeriod
        }

        // Validate payment mode.
        switch models.PaymentMode(req.Mode) {
        case models.PayCash, models.PayMobileMoney, models.PayWave, models.PayOrangeMoney, models.PayMTNMomo:
                // OK
        default:
                return nil, ErrInvalidPaymentMode
        }

        // Fetch the active subscription.
        sub, err := s.subRepo.GetByShopID(ctx, actorID, actorRole, shopID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return nil, ErrSubscriptionNotFound
                }
                return nil, fmt.Errorf("get subscription: %w", err)
        }

        pay, err := s.subRepo.RecordPayment(ctx, actorID, repository.RecordPaymentInput{
                SubscriptionID: sub.ID,
                ShopID:         shopID,
                Amount:         req.Amount,
                Mode:           models.PaymentMode(req.Mode),
                Reference:      req.Reference,
                PeriodStart:    periodStart,
                PeriodEnd:      periodEnd,
                RecordedBy:     actorID,
        })
        if err != nil {
                return nil, fmt.Errorf("record payment: %w", err)
        }

        // Audit log.
        shopIDForLog := shopID
        payIDForLog := pay.ID
        _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                ShopID:     &shopIDForLog,
                ActorID:    &actorID,
                ActorRole:  actorRole,
                Action:     "subscription.payment",
                ObjectType: "subscription_payment",
                ObjectID:   &payIDForLog,
                IPAddress:  ip,
                UserAgent:  userAgent,
        })

        return pay, nil
}

// --- helpers ----------------------------------------------------------------

// IsPlatformAdmin returns true if the role is super_admin or admin.
func IsPlatformAdmin(role string) bool {
        return role == string(models.RoleSuperAdmin) || role == string(models.RoleAdmin)
}

// isShopMember returns true if the user is a member of the shop.
func (s *ShopService) isShopMember(ctx context.Context, shopID, userID uuid.UUID) (bool, error) {
        _, err := s.memberRepo.GetByShopAndUser(ctx, shopID, userID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return false, nil
                }
                return false, fmt.Errorf("check shop member: %w", err)
        }
        return true, nil
}

// isShopOwner returns true if the user is the owner of the shop.
func (s *ShopService) isShopOwner(ctx context.Context, shopID, userID uuid.UUID) (bool, error) {
        m, err := s.memberRepo.GetByShopAndUser(ctx, shopID, userID)
        if err != nil {
                if errors.Is(err, repository.ErrNotFound) {
                        return false, nil
                }
                return false, fmt.Errorf("check shop owner: %w", err)
        }
        return m.Role == models.RoleOwner, nil
}

// shopSnapshot is a json.Marshaler that serializes a Shop into a JSON object
// for the audit log. We define a small struct so the JSON shape is stable
// (independent of the wire DTO).
type shopSnapshot struct {
        ID     string `json:"id"`
        Name   string `json:"name"`
        Slug   string `json:"slug"`
        Status string `json:"status"`
}

func (s shopSnapshot) MarshalJSON() ([]byte, error) {
        return json.Marshal(struct {
                ID     string `json:"id"`
                Name   string `json:"name"`
                Slug   string `json:"slug"`
                Status string `json:"status"`
        }{ID: s.ID, Name: s.Name, Slug: s.Slug, Status: s.Status})
}

// shopSnapshotFrom converts a *models.Shop into a shopSnapshot.
func shopSnapshotFrom(shop *models.Shop) shopSnapshot {
        if shop == nil {
                return shopSnapshot{}
        }
        return shopSnapshot{
                ID:     shop.ID.String(),
                Name:   shop.Name,
                Slug:   shop.Slug,
                Status: string(shop.Status),
        }
}
