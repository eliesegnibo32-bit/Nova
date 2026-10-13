// Customer repository (ch. 4.5 — migration 005).
//
// customers: phone-identified clients of a shop. status prospect → client → recurring.
// We never delete customers (soft delete via deleted_at) so the conversation
// history and accounting are preserved.
//
// All queries go through db.WithTenantTx so RLS is enforced.
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
)

// Customer repository sentinels.
var (
        ErrCustomerNotFound = errors.New("customer not found")
)

// CustomerRepository wraps the customers table.
type CustomerRepository struct {
        pool *pgxpool.Pool
}

// NewCustomerRepository returns a CustomerRepository bound to the pool.
func NewCustomerRepository(pool *pgxpool.Pool) *CustomerRepository {
        return &CustomerRepository{pool: pool}
}

// Customer is the persisted customer row. The DB table (migration 005) does
// NOT have a `notes` column — Notes here is in-memory only, never persisted.
type Customer struct {
        ID                uuid.UUID  `json:"id"`
        ShopID            uuid.UUID  `json:"shop_id"`
        Phone             string     `json:"phone"`
        Name              *string    `json:"name,omitempty"`
        Status            string     `json:"status"` // prospect | client | recurring
        ConsentMarketing  bool       `json:"consent_marketing"`
        ConsentSource     *string    `json:"consent_source,omitempty"`
        ConsentAt         *time.Time `json:"consent_at,omitempty"`
        LastInteractionAt *time.Time `json:"last_interaction_at,omitempty"`
        TotalOrders       int        `json:"total_orders"`
        TotalSpent        int64      `json:"total_spent"`     // FCFA
        Notes             *string    `json:"notes,omitempty"` // in-memory only (no DB column)
        CreatedAt         time.Time  `json:"created_at"`
        UpdatedAt         time.Time  `json:"updated_at"`
        DeletedAt         *time.Time `json:"deleted_at,omitempty"`
}

// CustomerListItem is the list-view shape.
type CustomerListItem struct {
        Customer
        OrdersCount int `json:"orders_count"`
}

// ListCustomersParams holds the query parameters for List.
type ListCustomersParams struct {
        Page   int
        Limit  int
        Search string
        Status string
}

// Normalize fills sane defaults.
func (p *ListCustomersParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 200 {
                p.Limit = 20
        }
}

// Offset returns the SQL OFFSET.
func (p *ListCustomersParams) Offset() int { return (p.Page - 1) * p.Limit }

// UpdateCustomerRequest is the partial-update input. Notes are not stored in
// the DB (the column doesn't exist) — only Name can be updated through this
// method for now.
type UpdateCustomerRequest struct {
        Name *string
}

// GetOrCreateByPhone returns the customer for (shopID, phone), or creates a new
// one in status='prospect'. If name is non-empty and the existing customer has
// no name, the name is updated.
func (r *CustomerRepository) GetOrCreateByPhone(ctx context.Context, shopID uuid.UUID, phone, name string) (*Customer, error) {
        var cust Customer
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                // Try fetch.
                const selQ = `
                        SELECT id, shop_id, phone, name, status, consent_marketing, consent_source, consent_at,
                               last_interaction_at, total_orders, total_spent, created_at, updated_at, deleted_at
                          FROM customers
                         WHERE shop_id = $1 AND phone = $2 AND deleted_at IS NULL
                `
                err := scanCustomer(tx.QueryRow(ctx, selQ, shopID, phone), &cust)
                if err == nil {
                        // If name is provided and customer has no name, update.
                        if name != "" && cust.Name == nil {
                                _, _ = tx.Exec(ctx, `UPDATE customers SET name = $3, last_interaction_at = now(), updated_at = now() WHERE shop_id = $1 AND id = $2`, shopID, cust.ID, name)
                                cust.Name = &name
                        } else {
                                _, _ = tx.Exec(ctx, `UPDATE customers SET last_interaction_at = now(), updated_at = now() WHERE shop_id = $1 AND id = $2`, shopID, cust.ID)
                        }
                        return nil
                }
                if !errors.Is(err, pgx.ErrNoRows) {
                        return fmt.Errorf("customer repo: get or create: select: %w", err)
                }
                // Insert.
                var nameArg any
                if name != "" {
                        nameArg = name
                }
                const insQ = `
                        INSERT INTO customers (shop_id, phone, name, status, last_interaction_at)
                        VALUES ($1, $2, $3, 'prospect', now())
                        RETURNING id, shop_id, phone, name, status, consent_marketing, consent_source, consent_at,
                                  last_interaction_at, total_orders, total_spent, created_at, updated_at, deleted_at
                `
                if err := scanCustomer(tx.QueryRow(ctx, insQ, shopID, phone, nameArg), &cust); err != nil {
                        return fmt.Errorf("customer repo: get or create: insert: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &cust, nil
}

// GetByID returns the customer for the given shop+id.
func (r *CustomerRepository) GetByID(ctx context.Context, shopID, id uuid.UUID) (*Customer, error) {
        var cust Customer
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, phone, name, status, consent_marketing, consent_source, consent_at,
                               last_interaction_at, total_orders, total_spent, created_at, updated_at, deleted_at
                          FROM customers
                         WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL
                `
                return scanCustomer(tx.QueryRow(ctx, q, shopID, id), &cust)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrCustomerNotFound
                }
                return nil, fmt.Errorf("customer repo: get by id: %w", err)
        }
        return &cust, nil
}

// Update applies a partial update to a customer.
func (r *CustomerRepository) Update(ctx context.Context, shopID, id uuid.UUID, req UpdateCustomerRequest) (*Customer, error) {
        var cust Customer
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                sets := []string{"updated_at = now()"}
                args := []any{shopID, id}
                idx := 3
                if req.Name != nil {
                        sets = append(sets, fmt.Sprintf("name = $%d", idx))
                        args = append(args, *req.Name)
                        idx++
                }
                q := fmt.Sprintf(`UPDATE customers SET %s WHERE shop_id = $1 AND id = $2 AND deleted_at IS NULL RETURNING id, shop_id, phone, name, status, consent_marketing, consent_source, consent_at, last_interaction_at, total_orders, total_spent, created_at, updated_at, deleted_at`, strings.Join(sets, ", "))
                if err := scanCustomer(tx.QueryRow(ctx, q, args...), &cust); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return ErrCustomerNotFound
                        }
                        return fmt.Errorf("customer repo: update: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &cust, nil
}

// List returns a paginated list of customers for the shop.
func (r *CustomerRepository) List(ctx context.Context, shopID uuid.UUID, params ListCustomersParams) ([]CustomerListItem, int64, error) {
        params.Normalize()
        var (
                out   []CustomerListItem
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                where := "WHERE shop_id = $1 AND deleted_at IS NULL"
                args := []any{shopID}
                idx := 2
                if params.Status != "" {
                        where += fmt.Sprintf(" AND status = $%d", idx)
                        args = append(args, params.Status)
                        idx++
                }
                if params.Search != "" {
                        where += fmt.Sprintf(" AND (coalesce(name, '') ILIKE $%d OR phone ILIKE $%d)", idx, idx)
                        args = append(args, "%"+params.Search+"%")
                        idx++
                }
                // Count.
                if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM customers "+where, args...).Scan(&total); err != nil {
                        return fmt.Errorf("customer repo: list: count: %w", err)
                }
                // List.
                listQ := fmt.Sprintf(`
                        SELECT id, shop_id, phone, name, status, consent_marketing, consent_source, consent_at,
                               last_interaction_at, total_orders, total_spent, created_at, updated_at, deleted_at,
                               (SELECT COUNT(*) FROM orders o WHERE o.customer_id = c.id) AS orders_count
                          FROM customers c
                          %s
                          ORDER BY last_interaction_at DESC NULLS LAST, created_at DESC
                          LIMIT $%d OFFSET $%d
                `, where, idx, idx+1)
                args = append(args, params.Limit, params.Offset())
                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var item CustomerListItem
                        if err := scanCustomerWithOrders(rows, &item); err != nil {
                                return err
                        }
                        out = append(out, item)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("customer repo: list: %w", err)
        }
        return out, total, nil
}

// UpdateStats updates the denormalized counters after a delivered order. Used
// by the order state machine when an order transitions to 'delivered'.
func (r *CustomerRepository) UpdateStats(ctx context.Context, shopID, id uuid.UUID, totalOrders int, totalSpent int64) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `UPDATE customers SET total_orders = $3, total_spent = $4, status = CASE WHEN $3 >= 5 THEN 'recurring' WHEN $3 >= 1 THEN 'client' ELSE status END, updated_at = now() WHERE shop_id = $1 AND id = $2`
                ct, err := tx.Exec(ctx, q, shopID, id, totalOrders, totalSpent)
                if err != nil {
                        return fmt.Errorf("customer repo: update stats: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCustomerNotFound
                }
                return nil
        })
}

// SetConsent records a marketing consent opt-in/opt-out.
func (r *CustomerRepository) SetConsent(ctx context.Context, shopID, id uuid.UUID, marketing bool, source string) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                var sourceArg any
                if source != "" {
                        sourceArg = source
                }
                var consentAt any
                if marketing {
                        consentAt = time.Now().UTC()
                }
                const q = `UPDATE customers SET consent_marketing = $3, consent_source = $4, consent_at = $5, updated_at = now() WHERE shop_id = $1 AND id = $2`
                ct, err := tx.Exec(ctx, q, shopID, id, marketing, sourceArg, consentAt)
                if err != nil {
                        return fmt.Errorf("customer repo: set consent: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCustomerNotFound
                }
                return nil
        })
}

// UpdateStatus sets the customer's commercial status (prospect → client → recurring).
func (r *CustomerRepository) UpdateStatus(ctx context.Context, shopID, id uuid.UUID, status string) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `UPDATE customers SET status = $3, updated_at = now() WHERE shop_id = $1 AND id = $2`
                ct, err := tx.Exec(ctx, q, shopID, id, status)
                if err != nil {
                        return fmt.Errorf("customer repo: update status: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCustomerNotFound
                }
                return nil
        })
}

// RegisterProspect ensures a customer is in 'prospect' status and bumps
// last_interaction_at. Used by the AI engine's enregistrer_prospect tool
// (ch. 5.2) when the customer expresses buying intent without placing an
// order. If the customer is already 'client' or 'recurring', we do NOT
// downgrade them — they keep their higher status.
//
// productID is optional (uuid.Nil = no specific product linked).
// intention is a free-text tag (e.g. "intéressé", "rappel souhaité").
func (r *CustomerRepository) RegisterProspect(ctx context.Context, shopID, customerID uuid.UUID, productID *uuid.UUID, intention string) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                // Update status to 'prospect' ONLY IF the customer is not yet a client
                // or recurring. We use a CASE expression so we never downgrade.
                const q = `
                        UPDATE customers
                           SET status = CASE WHEN status = 'prospect' THEN 'prospect' ELSE status END,
                               last_interaction_at = now(),
                               updated_at = now()
                         WHERE shop_id = $1 AND id = $2
                `
                ct, err := tx.Exec(ctx, q, shopID, customerID)
                if err != nil {
                        return fmt.Errorf("customer repo: register prospect: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrCustomerNotFound
                }
                // intention is logged via audit_logs at a higher level (the AI engine
                // audits the tool call). We don't have a dedicated column for it yet
                // (would be a V2 schema addition — prospect_intentions table).
                // For now, we just acknowledge via the row update above.
                _ = intention
                _ = productID // future: link prospect to a product in a join table
                return nil
        })
}

// --- scanners ---------------------------------------------------------------

type custScanner interface {
        Scan(dest ...any) error
}

func scanCustomer(s custScanner, c *Customer) error {
        var (
                name            *string
                consentSource   *string
                consentAt       *time.Time
                lastInteraction *time.Time
                deletedAt       *time.Time
        )
        err := s.Scan(
                &c.ID, &c.ShopID, &c.Phone, &name, &c.Status, &c.ConsentMarketing, &consentSource, &consentAt,
                &lastInteraction, &c.TotalOrders, &c.TotalSpent, &c.CreatedAt, &c.UpdatedAt, &deletedAt,
        )
        if err != nil {
                return err
        }
        c.Name = name
        c.ConsentSource = consentSource
        c.ConsentAt = consentAt
        c.LastInteractionAt = lastInteraction
        c.DeletedAt = deletedAt
        return nil
}

func scanCustomerWithOrders(s custScanner, item *CustomerListItem) error {
        var (
                name            *string
                consentSource   *string
                consentAt       *time.Time
                lastInteraction *time.Time
                deletedAt       *time.Time
        )
        err := s.Scan(
                &item.ID, &item.ShopID, &item.Phone, &name, &item.Status, &item.ConsentMarketing, &consentSource, &consentAt,
                &lastInteraction, &item.TotalOrders, &item.TotalSpent, &item.CreatedAt, &item.UpdatedAt, &deletedAt,
                &item.OrdersCount,
        )
        if err != nil {
                return err
        }
        item.Name = name
        item.ConsentSource = consentSource
        item.ConsentAt = consentAt
        item.LastInteractionAt = lastInteraction
        item.DeletedAt = deletedAt
        return nil
}
