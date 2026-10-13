// Message template repository (ch. 6 — Modèles WhatsApp, migration 009).
//
// message_templates is a GLOBAL table (no shop_id, no RLS) — the templates
// are approved by Meta at the WhatsApp Business Account level, not per shop.
// In production the NOVA ops team manages the template catalog; merchants
// cannot create their own (controlled variables, ch. 6).
//
// The repository is read-mostly. Create/Update/Delete are used by the ops
// admin tooling (POST /admin/whatsapp/templates/... — future task).
package repository

import (
        "context"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"
)

// Message template repository sentinels.
var (
        ErrTemplateNotFound = errors.New("message template not found")
        ErrTemplateTaken    = errors.New("message template name already taken")
)

// MessageTemplate is a row of message_templates (migration 009).
type MessageTemplate struct {
        ID        uuid.UUID  `json:"id"`
        Name      string     `json:"name"`      // Meta template name (UNIQUE)
        Category  string     `json:"category"`  // marketing | utility | authentication
        Language  string     `json:"language"`  // fr | en | ...
        Status    string     `json:"status"`    // pending | approved | rejected
        Body      string     `json:"body"`      // template body with {{1}}, {{2}}, ...
        Variables []byte     `json:"variables"` // jsonb metadata (array of {key,label,type})
        CreatedAt time.Time  `json:"created_at"`
        UpdatedAt time.Time  `json:"updated_at"`
}

// MessageTemplateRepository wraps the message_templates table.
type MessageTemplateRepository struct {
        pool *pgxpool.Pool
}

// NewMessageTemplateRepository returns a MessageTemplateRepository bound to the
// pool. The pool's RLS context is NOT used: message_templates is global, so
// queries go through the pool directly (no WithTenantTx).
func NewMessageTemplateRepository(pool *pgxpool.Pool) *MessageTemplateRepository {
        return &MessageTemplateRepository{pool: pool}
}

// Create inserts a new template. Returns ErrTemplateTaken if the name is
// already in use (UNIQUE constraint).
func (r *MessageTemplateRepository) Create(ctx context.Context, t *MessageTemplate) (*MessageTemplate, error) {
        if t == nil {
                return nil, errors.New("template is nil")
        }
        if t.Variables == nil {
                t.Variables = []byte("[]")
        }
        const q = `
                INSERT INTO message_templates (name, category, language, status, body, variables)
                VALUES ($1, $2, $3, $4, $5, $6::jsonb)
                RETURNING id, name, category, language, status, body, variables, created_at, updated_at
        `
        if err := scanMessageTemplate(r.pool.QueryRow(ctx, q,
                t.Name, t.Category, defaultStr(t.Language, "fr"), defaultStr(t.Status, "pending"),
                t.Body, []byte(t.Variables)), t); err != nil {
                if isUniqueViolation(err) {
                        return nil, ErrTemplateTaken
                }
                return nil, fmt.Errorf("template repo: create: %w", err)
        }
        return t, nil
}

// GetByName returns the template with the given Meta name (UNIQUE).
func (r *MessageTemplateRepository) GetByName(ctx context.Context, name string) (*MessageTemplate, error) {
        var t MessageTemplate
        const q = `
                SELECT id, name, category, language, status, body, variables, created_at, updated_at
                  FROM message_templates
                 WHERE name = $1
        `
        if err := scanMessageTemplate(r.pool.QueryRow(ctx, q, name), &t); err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrTemplateNotFound
                }
                return nil, fmt.Errorf("template repo: get by name: %w", err)
        }
        return &t, nil
}

// GetByID returns the template with the given UUID.
func (r *MessageTemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*MessageTemplate, error) {
        var t MessageTemplate
        const q = `
                SELECT id, name, category, language, status, body, variables, created_at, updated_at
                  FROM message_templates
                 WHERE id = $1
        `
        if err := scanMessageTemplate(r.pool.QueryRow(ctx, q, id), &t); err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrTemplateNotFound
                }
                return nil, fmt.Errorf("template repo: get by id: %w", err)
        }
        return &t, nil
}

// List returns all templates, optionally filtered by status. Ordered by name.
func (r *MessageTemplateRepository) List(ctx context.Context, status string) ([]MessageTemplate, error) {
        var (
                q     string
                args  []any
                rows  pgx.Rows
                err   error
        )
        if status != "" {
                q = `SELECT id, name, category, language, status, body, variables, created_at, updated_at FROM message_templates WHERE status = $1 ORDER BY name ASC`
                rows, err = r.pool.Query(ctx, q, status)
                args = []any{status}
        } else {
                q = `SELECT id, name, category, language, status, body, variables, created_at, updated_at FROM message_templates ORDER BY name ASC`
                rows, err = r.pool.Query(ctx, q)
        }
        _ = args
        if err != nil {
                return nil, fmt.Errorf("template repo: list: %w", err)
        }
        defer rows.Close()
        var out []MessageTemplate
        for rows.Next() {
                var t MessageTemplate
                if err := scanMessageTemplate(rows, &t); err != nil {
                        return nil, err
                }
                out = append(out, t)
        }
        return out, rows.Err()
}

// Update mutates the mutable columns of a template (category, language,
// status, body, variables). Name is NOT mutable — it's the Meta identifier.
func (r *MessageTemplateRepository) Update(ctx context.Context, id uuid.UUID, t *MessageTemplate) (*MessageTemplate, error) {
        if t == nil {
                return nil, errors.New("template is nil")
        }
        if t.Variables == nil {
                t.Variables = []byte("[]")
        }
        const q = `
                UPDATE message_templates
                   SET category = $2, language = $3, status = $4, body = $5, variables = $6::jsonb, updated_at = now()
                 WHERE id = $1
                RETURNING id, name, category, language, status, body, variables, created_at, updated_at
        `
        if err := scanMessageTemplate(r.pool.QueryRow(ctx, q,
                id, t.Category, defaultStr(t.Language, "fr"), defaultStr(t.Status, "pending"),
                t.Body, []byte(t.Variables)), t); err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrTemplateNotFound
                }
                return nil, fmt.Errorf("template repo: update: %w", err)
        }
        return t, nil
}

// Delete removes a template by ID. Hard delete — Meta revocation should be
// handled separately by the SyncFromMeta flow.
func (r *MessageTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
        ct, err := r.pool.Exec(ctx, `DELETE FROM message_templates WHERE id = $1`, id)
        if err != nil {
                return fmt.Errorf("template repo: delete: %w", err)
        }
        if ct.RowsAffected() == 0 {
                return ErrTemplateNotFound
        }
        return nil
}

// Upsert syncs a template with Meta: if the name doesn't exist locally, the
// row is inserted; if it does, the category/language/status/body are updated
// to match Meta's values. Used by TemplateManager.SyncFromMeta and by the
// EnsureNovaTemplates idempotent seed.
//
// Returns the upserted row + a flag (true = created, false = updated).
func (r *MessageTemplateRepository) Upsert(ctx context.Context, t *MessageTemplate) (*MessageTemplate, bool, error) {
        if t == nil {
                return nil, false, errors.New("template is nil")
        }
        if t.Name == "" {
                return nil, false, errors.New("template name is empty")
        }
        if t.Variables == nil {
                t.Variables = []byte("[]")
        }
        if t.Language == "" {
                t.Language = "fr"
        }
        if t.Status == "" {
                t.Status = "pending"
        }
        // Try INSERT ... ON CONFLICT (name) DO UPDATE — atomic upsert.
        const q = `
                INSERT INTO message_templates (name, category, language, status, body, variables)
                VALUES ($1, $2, $3, $4, $5, $6::jsonb)
                ON CONFLICT (name) DO UPDATE
                   SET category = EXCLUDED.category,
                       language = EXCLUDED.language,
                       status   = EXCLUDED.status,
                       body     = EXCLUDED.body,
                       variables = EXCLUDED.variables,
                       updated_at = now()
                RETURNING id, name, category, language, status, body, variables, created_at, updated_at
        `
        var out MessageTemplate
        if err := scanMessageTemplate(r.pool.QueryRow(ctx, q,
                t.Name, t.Category, t.Language, t.Status, t.Body, []byte(t.Variables)), &out); err != nil {
                return nil, false, fmt.Errorf("template repo: upsert: %w", err)
        }
        // Determine if it was a create vs update by comparing created_at == updated_at.
        created := out.CreatedAt.Equal(out.UpdatedAt)
        return &out, created, nil
}

// UpdateStatus updates only the status column of a template (used by
// SyncFromMeta when Meta's approval status changes). Returns
// ErrTemplateNotFound when no template matches the name.
func (r *MessageTemplateRepository) UpdateStatus(ctx context.Context, name, status string) error {
        if name == "" || status == "" {
                return errors.New("template repo: update status: name and status are required")
        }
        ct, err := r.pool.Exec(ctx, `UPDATE message_templates SET status = $2, updated_at = now() WHERE name = $1`, name, status)
        if err != nil {
                return fmt.Errorf("template repo: update status: %w", err)
        }
        if ct.RowsAffected() == 0 {
                return ErrTemplateNotFound
        }
        return nil
}

// --- helpers ----------------------------------------------------------------

type tplScanner interface {
        Scan(dest ...any) error
}

func scanMessageTemplate(s tplScanner, t *MessageTemplate) error {
        var variables []byte
        if err := s.Scan(&t.ID, &t.Name, &t.Category, &t.Language, &t.Status, &t.Body, &variables, &t.CreatedAt, &t.UpdatedAt); err != nil {
                return err
        }
        if variables == nil {
                variables = []byte("[]")
        }
        t.Variables = variables
        return nil
}

// defaultStr returns v when non-empty, else def.
func defaultStr(v, def string) string {
        if v == "" {
                return def
        }
        return v
}
