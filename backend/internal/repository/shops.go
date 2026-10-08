// Shop repository — handles the `shops` table and the creation-time
// orchestration of shop_members + subscriptions.
//
// All shop-scoped queries go through db.WithTenantTx so the RLS policies from
// migration 011 are enforced:
//   - SELECT on shops: id = current_shop_id() OR is_platform_admin()
//   - INSERT on shops: is_platform_admin()
//   - UPDATE on shops: is_platform_admin()
//   - DELETE on shops: is_platform_admin()
//
// The shop table is special because it IS the tenant — owners can read their
// own shop (RLS sees current_shop_id = shop.id) and update it (RLS only allows
// platform admins; the service layer enforces "owner can update their own
// shop" by running updates with role='super_admin' after the service has
// verified the requester is the owner).
package repository

import (
        "context"
        "encoding/json"
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

// CreateShopInput is the data needed to create a shop + owner membership +
// trial subscription in one transaction. The service resolves owner_email →
// owner_user_id before calling Create.
type CreateShopInput struct {
        Name                 string
        Slug                 string
        OwnerUserID          uuid.UUID
        Phone                string
        WhatsAppNumber       string
        Address              string
        Commune              string
        Hours                json.RawMessage // may be nil → defaults to '{}'
        Description          string
        Categories           []string
        SaleConditions       string
        AcceptedPaymentModes []string
        PlanID               uuid.UUID
}

// ShopRepository wraps the shops table.
type ShopRepository struct {
        pool *pgxpool.Pool
}

// NewShopRepository returns a ShopRepository bound to the given pool.
func NewShopRepository(pool *pgxpool.Pool) *ShopRepository {
        return &ShopRepository{pool: pool}
}

// Create inserts a shop, an owner shop_member, and a trial subscription in
// one transaction. The shop starts in 'draft' status (onboarding not yet
// complete). The subscription is 'trial' with next_billing_at = now+14days.
//
// The caller MUST have verified that the actor is a platform admin (RLS
// enforces this on INSERT).
//
// Slug uniqueness is enforced by the DB (shops.slug UNIQUE). On conflict the
// error wraps pgx's unique_violation; callers can detect it via
// isUniqueViolation(err) and map it to ErrSlugTaken.
func (r *ShopRepository) Create(ctx context.Context, actorID uuid.UUID, in CreateShopInput) (*models.Shop, error) {
        var shop models.Shop
        err := db.WithTenantTx(ctx, r.pool, nil, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // 1. Insert the shop with status='draft'.
                hoursArg := normalizeJSONB(in.Hours)
                catsArg := normalizeStringArray(in.Categories)
                modesArg := normalizeStringArray(in.AcceptedPaymentModes)

                const shopQ = `
                        INSERT INTO shops
                            (name, slug, phone, whatsapp_number, address, commune, hours,
                             description, categories, sale_conditions, accepted_payment_modes,
                             ai_settings, status)
                        VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11::payment_mode[], '{}'::jsonb, 'draft')
                        RETURNING
                            id, name, slug, phone, whatsapp_number, address, commune, hours,
                            description, categories, sale_conditions, accepted_payment_modes,
                            ai_settings, status, logo_url, created_at, updated_at, deleted_at
                `
                row := tx.QueryRow(ctx, shopQ,
                        in.Name, in.Slug,
                        nullableString(in.Phone),
                        nullableString(in.WhatsAppNumber),
                        nullableString(in.Address),
                        nullableString(in.Commune),
                        hoursArg,
                        nullableString(in.Description),
                        catsArg,
                        nullableString(in.SaleConditions),
                        modesArg,
                )
                if err := scanShop(row, &shop); err != nil {
                        // Detect slug unique_violation (23505) and surface a typed error.
                        if isUniqueViolation(err) || strings.Contains(err.Error(), "shops_slug_key") {
                                return ErrSlugTaken
                        }
                        return fmt.Errorf("insert shop: %w", err)
                }

                // 2. Insert the owner membership.
                const memberQ = `
                        INSERT INTO shop_members (shop_id, user_id, role, permissions)
                        VALUES ($1, $2, 'owner', '{"orders":{"refund":true,"cancel":true},"stock":{"adjust":true},"catalog":{"manage":true}}'::jsonb)
                `
                if _, err := tx.Exec(ctx, memberQ, shop.ID, in.OwnerUserID); err != nil {
                        return fmt.Errorf("insert owner shop_member: %w", err)
                }

                // 3. Insert the trial subscription (14 days).
                const subQ = `
                        INSERT INTO subscriptions (shop_id, plan_id, status, started_at, next_billing_at)
                        VALUES ($1, $2, 'trial', now(), now() + interval '14 days')
                `
                if _, err := tx.Exec(ctx, subQ, shop.ID, in.PlanID); err != nil {
                        return fmt.Errorf("insert trial subscription: %w", err)
                }

                return nil
        })
        if err != nil {
                return nil, err
        }
        return &shop, nil
}

// GetByID returns the shop with the given UUID. RLS: only members of the shop
// OR platform admins can read it. The caller must set the tenant context
// (shopID=in.ID, role=actor's role) before calling — for cross-shop reads
// (admin dashboard), pass role='super_admin'.
func (r *ShopRepository) GetByID(ctx context.Context, requesterID uuid.UUID, role string, id uuid.UUID) (*models.Shop, error) {
        var shop models.Shop
        err := db.WithTenantTx(ctx, r.pool, &id, requesterID, role, func(tx pgx.Tx) error {
                const q = `
                        SELECT id, name, slug, phone, whatsapp_number, address, commune, hours,
                               description, categories, sale_conditions, accepted_payment_modes,
                               ai_settings, status, logo_url, created_at, updated_at, deleted_at
                          FROM shops
                         WHERE id = $1 AND deleted_at IS NULL
                `
                return scanShop(tx.QueryRow(ctx, q, id), &shop)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("shop repo: get by id: %w", err)
        }
        return &shop, nil
}

// GetBySlug returns the shop with the given slug. Used for public/SEO
// lookups. Admin only (the caller passes role='super_admin').
func (r *ShopRepository) GetBySlug(ctx context.Context, slug string) (*models.Shop, error) {
        var shop models.Shop
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT id, name, slug, phone, whatsapp_number, address, commune, hours,
                               description, categories, sale_conditions, accepted_payment_modes,
                               ai_settings, status, logo_url, created_at, updated_at, deleted_at
                          FROM shops
                         WHERE slug = $1 AND deleted_at IS NULL
                `
                return scanShop(tx.QueryRow(ctx, q, slug), &shop)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("shop repo: get by slug: %w", err)
        }
        return &shop, nil
}

// GetByWhatsAppNumber returns the shop whose whatsapp_number matches the given
// phone (E.164 with or without the leading "+"). Meta's webhook payload
// includes the display_phone_number of the receiving WhatsApp Business number;
// the webhook processor uses it to route the inbound message to the right
// shop. Pilot mode: when several shops share the NOVA number, the first match
// is returned (a future iteration could add a many-to-many mapping table).
//
// Returns ErrNotFound when no shop matches. Caller runs as super_admin so RLS
// sees all shops (this lookup is cross-tenant — Meta doesn't send a shop_id).
func (r *ShopRepository) GetByWhatsAppNumber(ctx context.Context, phone string) (*models.Shop, error) {
        // Normalize: strip leading "+", then strip any spaces/dashes.
        normalized := normalizePhone(phone)
        if normalized == "" {
                return nil, ErrNotFound
        }
        var shop models.Shop
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // Match on either the raw value, the value with a "+", or the
                // digits-only value. shops.whatsapp_number is text, so we
                // compare with a few candidate formats.
                const q = `
                        SELECT id, name, slug, phone, whatsapp_number, address, commune, hours,
                               description, categories, sale_conditions, accepted_payment_modes,
                               ai_settings, status, logo_url, created_at, updated_at, deleted_at
                          FROM shops
                         WHERE deleted_at IS NULL
                           AND (
                                 whatsapp_number = $1
                              OR whatsapp_number = $2
                              OR regexp_replace(coalesce(whatsapp_number, ''), '[^0-9]', '', 'g') = $3
                           )
                         ORDER BY created_at ASC
                         LIMIT 1
                `
                row := tx.QueryRow(ctx, q, normalized, "+"+normalized, normalized)
                return scanShop(row, &shop)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrNotFound
                }
                return nil, fmt.Errorf("shop repo: get by whatsapp number: %w", err)
        }
        return &shop, nil
}

// normalizePhone strips leading "+", spaces and dashes from a phone string.
// Returns the digits-only form. Empty input returns "".
func normalizePhone(s string) string {
        if s == "" {
                return ""
        }
        var b strings.Builder
        b.Grow(len(s))
        for _, r := range s {
                if r >= '0' && r <= '9' {
                        b.WriteRune(r)
                }
        }
        return b.String()
}

// ListByUser returns every shop a user is a member of. The caller passes the
// user's platform role (super_admin / admin / owner / employee) — platform
// admins can see all shops via RLS, owners/employees see only their shops
// (RLS joins shop_members).
func (r *ShopRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]models.Shop, error) {
        var out []models.Shop
        err := db.WithTenantTx(ctx, r.pool, nil, userID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                const q = `
                        SELECT s.id, s.name, s.slug, s.phone, s.whatsapp_number, s.address, s.commune, s.hours,
                               s.description, s.categories, s.sale_conditions, s.accepted_payment_modes,
                               s.ai_settings, s.status, s.logo_url, s.created_at, s.updated_at, s.deleted_at
                          FROM shops s
                          JOIN shop_members sm ON sm.shop_id = s.id
                         WHERE sm.user_id = $1
                           AND s.deleted_at IS NULL
                         ORDER BY s.created_at ASC
                `
                rows, err := tx.Query(ctx, q, userID)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var s models.Shop
                        if err := scanShop(rows, &s); err != nil {
                                return err
                        }
                        out = append(out, s)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("shop repo: list by user: %w", err)
        }
        return out, nil
}

// List returns a paginated list of all shops for the admin dashboard.
// Admin only (RLS: role='super_admin'). Filters: status, search (substring
// on name OR slug, case-insensitive). Returns the shops + total count (for
// pagination UI).
func (r *ShopRepository) List(ctx context.Context, params ListShopsParams) ([]models.Shop, int64, error) {
        var (
                out   []models.Shop
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // Build the WHERE clause dynamically.
                where := "WHERE deleted_at IS NULL"
                args := []any{}
                argIdx := 1
                if params.Status != "" {
                        where += fmt.Sprintf(" AND status = $%d", argIdx)
                        args = append(args, params.Status)
                        argIdx++
                }
                if params.Search != "" {
                        where += fmt.Sprintf(" AND (name ILIKE $%d OR slug ILIKE $%d)", argIdx, argIdx)
                        args = append(args, "%"+params.Search+"%")
                        argIdx++
                }

                // Count total.
                countQ := "SELECT COUNT(*) FROM shops " + where
                if err := tx.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
                        return fmt.Errorf("count shops: %w", err)
                }

                // Fetch page.
                listQ := `
                        SELECT id, name, slug, phone, whatsapp_number, address, commune, hours,
                               description, categories, sale_conditions, accepted_payment_modes,
                               ai_settings, status, logo_url, created_at, updated_at, deleted_at
                          FROM shops
                ` + where + `
                        ORDER BY created_at DESC
                        LIMIT $` + fmt.Sprintf("%d", argIdx) + ` OFFSET $` + fmt.Sprintf("%d", argIdx+1)
                args = append(args, params.Limit, params.Offset())

                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var s models.Shop
                        if err := scanShop(rows, &s); err != nil {
                                return err
                        }
                        out = append(out, s)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("shop repo: list: %w", err)
        }
        return out, total, nil
}

// ListShopsParams is the input to List (filters + pagination).
type ListShopsParams struct {
        Page   int
        Limit  int
        Search string
        Status string
}

// Offset returns the SQL OFFSET value for the current page/limit.
func (p ListShopsParams) Offset() int {
        if p.Page < 1 {
                p.Page = 1
        }
        return (p.Page - 1) * p.Limit
}

// Update applies a partial update to a shop. Only fields present in req are
// updated. RLS: only platform admins can UPDATE shops (the service verifies
// "owner can update their own shop" before calling, then runs as super_admin).
func (r *ShopRepository) Update(ctx context.Context, actorID uuid.UUID, id uuid.UUID, req UpdateShopInput) (*models.Shop, error) {
        var shop models.Shop
        err := db.WithTenantTx(ctx, r.pool, nil, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                // Build the SET clause dynamically from non-nil fields.
                sets := []string{}
                args := []any{}
                argIdx := 1

                addString := func(col string, v *string) {
                        if v != nil {
                                sets = append(sets, fmt.Sprintf("%s = $%d", col, argIdx))
                                args = append(args, *v)
                                argIdx++
                        }
                }
                addString("name", req.Name)
                addString("logo_url", req.LogoURL)
                addString("phone", req.Phone)
                addString("whatsapp_number", req.WhatsAppNumber)
                addString("address", req.Address)
                addString("commune", req.Commune)
                addString("description", req.Description)
                addString("sale_conditions", req.SaleConditions)

                if len(req.Hours) > 0 {
                        sets = append(sets, fmt.Sprintf("hours = $%d::jsonb", argIdx))
                        args = append(args, []byte(req.Hours))
                        argIdx++
                }
                if len(req.AISettings) > 0 {
                        sets = append(sets, fmt.Sprintf("ai_settings = $%d::jsonb", argIdx))
                        args = append(args, []byte(req.AISettings))
                        argIdx++
                }
                if req.Categories != nil {
                        sets = append(sets, fmt.Sprintf("categories = $%d", argIdx))
                        args = append(args, normalizeStringArray(*req.Categories))
                        argIdx++
                }
                if req.AcceptedPaymentModes != nil {
                        sets = append(sets, fmt.Sprintf("accepted_payment_modes = $%d::payment_mode[]", argIdx))
                        args = append(args, normalizeStringArray(*req.AcceptedPaymentModes))
                        argIdx++
                }

                if len(sets) == 0 {
                        // Nothing to update — just fetch and return.
                        const q = `
                                SELECT id, name, slug, phone, whatsapp_number, address, commune, hours,
                                       description, categories, sale_conditions, accepted_payment_modes,
                                       ai_settings, status, logo_url, created_at, updated_at, deleted_at
                                  FROM shops WHERE id = $1 AND deleted_at IS NULL
                        `
                        return scanShop(tx.QueryRow(ctx, q, id), &shop)
                }

                sets = append(sets, "updated_at = now()")

                // WHERE id = $N
                args = append(args, id)
                whereArg := argIdx

                q := fmt.Sprintf(`
                        UPDATE shops
                           SET %s
                         WHERE id = $%d AND deleted_at IS NULL
                        RETURNING id, name, slug, phone, whatsapp_number, address, commune, hours,
                                  description, categories, sale_conditions, accepted_payment_modes,
                                  ai_settings, status, logo_url, created_at, updated_at, deleted_at
                `, strings.Join(sets, ", "), whereArg)
                if err := scanShop(tx.QueryRow(ctx, q, args...), &shop); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("update shop: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &shop, nil
}

// UpdateShopInput mirrors models.UpdateShopRequest but with raw pointers
// (no JSON tags) so the repository doesn't depend on the HTTP DTO shape.
type UpdateShopInput struct {
        Name                 *string
        LogoURL              *string
        Phone                *string
        WhatsAppNumber       *string
        Address              *string
        Commune              *string
        Hours                json.RawMessage
        Description          *string
        Categories           *[]string
        SaleConditions       *string
        AcceptedPaymentModes *[]string
        AISettings           json.RawMessage
}

// Activate sets status to 'active'. The service MUST call
// ValidateActivation first and refuse if criteria aren't met.
func (r *ShopRepository) Activate(ctx context.Context, actorID uuid.UUID, id uuid.UUID) (*models.Shop, error) {
        return r.setStatus(ctx, actorID, id, models.ShopActive, false, "")
}

// Suspend sets status to 'suspended'. Admin only. The reason is recorded in
// the audit log by the service (not stored on the shop row — the schema has
// no suspend_reason column; the audit log is the system of record).
func (r *ShopRepository) Suspend(ctx context.Context, actorID uuid.UUID, id uuid.UUID, _ string) (*models.Shop, error) {
        return r.setStatus(ctx, actorID, id, models.ShopSuspended, true, "")
}

// Reactivate sets status back to 'active'. Admin only.
func (r *ShopRepository) Reactivate(ctx context.Context, actorID uuid.UUID, id uuid.UUID) (*models.Shop, error) {
        return r.setStatus(ctx, actorID, id, models.ShopActive, false, "")
}

// setStatus is the shared implementation for status transitions. When
// `clearSuspendedAt` is true we also clear any subscription.suspended_at —
// kept here so all status transitions go through one code path.
func (r *ShopRepository) setStatus(ctx context.Context, actorID uuid.UUID, id uuid.UUID, status models.ShopStatus, suspendSub bool, _ string) (*models.Shop, error) {
        var shop models.Shop
        err := db.WithTenantTx(ctx, r.pool, nil, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                q := `
                        UPDATE shops
                           SET status = $2,
                               updated_at = now()
                         WHERE id = $1 AND deleted_at IS NULL
                        RETURNING id, name, slug, phone, whatsapp_number, address, commune, hours,
                                  description, categories, sale_conditions, accepted_payment_modes,
                                  ai_settings, status, logo_url, created_at, updated_at, deleted_at
                `
                if err := scanShop(tx.QueryRow(ctx, q, id, string(status)), &shop); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrNotFound
                        }
                        return fmt.Errorf("update shop status: %w", err)
                }

                // Keep the subscription.suspended_at column in sync.
                if suspendSub {
                        _, _ = tx.Exec(ctx, `
                                UPDATE subscriptions
                                   SET suspended_at = now(),
                                       status = CASE WHEN status = 'trial' THEN 'suspended' ELSE status END,
                                       updated_at = now()
                                 WHERE shop_id = $1 AND terminated_at IS NULL
                        `, id)
                } else if status == models.ShopActive {
                        _, _ = tx.Exec(ctx, `
                                UPDATE subscriptions
                                   SET suspended_at = NULL,
                                       status = CASE WHEN status = 'suspended' THEN 'active' ELSE status END,
                                       updated_at = now()
                                 WHERE shop_id = $1 AND terminated_at IS NULL
                        `, id)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &shop, nil
}

// Delete soft-deletes a shop (sets deleted_at = now()). Admin only. The shop
// row is preserved for the retention period — RLS hides it from all future
// queries (the WHERE deleted_at IS NULL clauses in our queries).
func (r *ShopRepository) Delete(ctx context.Context, actorID uuid.UUID, id uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, nil, actorID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `
                        UPDATE shops SET deleted_at = now(), updated_at = now()
                         WHERE id = $1 AND deleted_at IS NULL
                `, id)
                if err != nil {
                        return fmt.Errorf("soft-delete shop: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrNotFound
                }
                return nil
        })
}

// Count returns the total number of non-deleted shops. Admin dashboard metric.
func (r *ShopRepository) Count(ctx context.Context) (int64, error) {
        var n int64
        err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `SELECT COUNT(*) FROM shops WHERE deleted_at IS NULL`).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("shop repo: count: %w", err)
        }
        return n, nil
}

// --- Activation validation helpers ------------------------------------------

// CountPublishedProducts returns the number of products WHERE shop_id=X AND
// status='published'. Used by ValidateActivation. The caller passes the
// shop's ID as the tenant context so RLS sees the shop's products.
func (r *ShopRepository) CountPublishedProducts(ctx context.Context, requesterID, shopID uuid.UUID) (int, error) {
        var n int
        err := db.WithTenantTx(ctx, r.pool, &shopID, requesterID, string(models.RoleOwner), func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM products
                         WHERE shop_id = $1 AND status = 'published' AND deleted_at IS NULL
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("shop repo: count published products: %w", err)
        }
        return n, nil
}

// CountActiveDeliveryZones returns the number of active delivery zones for
// the shop. Used by ValidateActivation.
func (r *ShopRepository) CountActiveDeliveryZones(ctx context.Context, requesterID, shopID uuid.UUID) (int, error) {
        var n int
        err := db.WithTenantTx(ctx, r.pool, &shopID, requesterID, string(models.RoleOwner), func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT COUNT(*) FROM delivery_zones
                         WHERE shop_id = $1 AND active = true
                `, shopID).Scan(&n)
        })
        if err != nil {
                return 0, fmt.Errorf("shop repo: count delivery zones: %w", err)
        }
        return n, nil
}

// HasHours returns true if the shop has non-empty hours jsonb. Used by
// ValidateActivation. "{}" (empty object) counts as "not set".
func (r *ShopRepository) HasHours(ctx context.Context, requesterID, shopID uuid.UUID) (bool, error) {
        var has bool
        err := db.WithTenantTx(ctx, r.pool, &shopID, requesterID, string(models.RoleOwner), func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `
                        SELECT hours IS NOT NULL AND hours::text != '{}'::text AND hours::text != 'null'::text
                          FROM shops WHERE id = $1 AND deleted_at IS NULL
                `, shopID).Scan(&has)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return false, ErrNotFound
                }
                return false, fmt.Errorf("shop repo: has hours: %w", err)
        }
        return has, nil
}

// --- Sentinels --------------------------------------------------------------

// ErrSlugTaken is returned by Create when the slug is already in use.
var ErrSlugTaken = errors.New("shop slug already taken")

// --- helpers ----------------------------------------------------------------

func scanShop(s scanner, shop *models.Shop) error {
        var (
                phone       *string
                whatsapp    *string
                address     *string
                commune     *string
                hours       []byte
                description *string
                categories  []string
                saleCond    *string
                payModes    []string
                aiSettings  []byte
                status      string
                logoURL     *string
                deletedAt   *time.Time
        )
        err := s.Scan(
                &shop.ID,
                &shop.Name,
                &shop.Slug,
                &phone,
                &whatsapp,
                &address,
                &commune,
                &hours,
                &description,
                &categories,
                &saleCond,
                &payModes,
                &aiSettings,
                &status,
                &logoURL,
                &shop.CreatedAt,
                &shop.UpdatedAt,
                &deletedAt,
        )
        if err != nil {
                return err
        }
        shop.Phone = phone
        shop.WhatsAppNumber = whatsapp
        shop.Address = address
        shop.Commune = commune
        shop.Hours = hours
        shop.Description = description
        shop.Categories = categories
        shop.SaleConditions = saleCond
        shop.AcceptedPaymentModes = payModes
        shop.AISettings = aiSettings
        shop.Status = models.ShopStatus(status)
        shop.LogoURL = logoURL
        shop.DeletedAt = deletedAt
        return nil
}

// normalizeJSONB returns a non-nil []byte for the jsonb column. Empty input
// becomes "{}" so the column default isn't bypassed with NULL.
func normalizeJSONB(b json.RawMessage) []byte {
        if len(b) == 0 {
                return []byte("{}")
        }
        return []byte(b)
}

// normalizeStringArray returns a non-nil []string for the text[] / payment_mode[]
// column. Empty input becomes "{}" via the SQL `DEFAULT '{}'::text[]` path
// (we still pass an empty slice here; pgx encodes it as `{}`).
func normalizeStringArray(s []string) []string {
        if s == nil {
                return []string{}
        }
        return s
}

// isUniqueViolation returns true if err is a Postgres unique_violation
// (SQLSTATE 23505). We check the error string for both the code and common
// constraint name patterns to avoid importing pgconn (keeps the repository
// layer driver-agnostic).
func isUniqueViolation(err error) bool {
        if err == nil {
                return false
        }
        msg := err.Error()
        return strings.Contains(msg, "23505")
}
