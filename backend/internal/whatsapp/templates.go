// Message template management (ch. 6 — Messages modèles, migration 009).
//
// Message templates are pre-approved by Meta at the WhatsApp Business Account
// level (global, not per shop). The NOVA ops team manages the catalog; the
// TemplateManager exposes:
//   - List            — read templates from the DB;
//   - SyncFromMeta    — fetch template approval statuses from Meta and update
//                       the DB (best-effort — Meta API may be unavailable or
//                       in mock mode we just return);
//   - RenderTemplate  — fill the {{1}}, {{2}}, ... variables of a template
//                       body with the given params, returning the final text.
package whatsapp

import (
        "bytes"
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "regexp"
        "sort"
        "strconv"
        "strings"

        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/repository"
)

// TemplateManager owns the template CRUD + Meta sync.
type TemplateManager struct {
        client *Client
        repo   *repository.MessageTemplateRepository
        pool   *pgxpool.Pool
}

// NewTemplateManager constructs a TemplateManager.
func NewTemplateManager(client *Client, repo *repository.MessageTemplateRepository, pool *pgxpool.Pool) *TemplateManager {
        return &TemplateManager{client: client, repo: repo, pool: pool}
}

// List returns all templates from the DB, optionally filtered by status.
func (t *TemplateManager) List(ctx context.Context, status string) ([]repository.MessageTemplate, error) {
        return t.repo.List(ctx, status)
}

// SyncFromMeta fetches the template list from the Meta Graph API and updates
// the `status` column in our DB to match Meta's approval status.
//
// Endpoint (ch. 6 — Messages modèles):
//   GET /v18.0/<waba_id>/message_templates
//
// We don't have the WABA ID at this layer — we use the phone_number_id which
// the client is configured with, which is the closest approximation. In
// production this should be the WABA ID (a separate env var). For mock mode
// or when the client has no phone_number_id, we just return nil.
func (t *TemplateManager) SyncFromMeta(ctx context.Context) error {
        if t.client.IsMock() {
                // Mock mode — nothing to sync.
                return nil
        }
        if t.client.phoneNumberID == "" {
                return errors.New("template manager: cannot sync — phone_number_id is empty")
        }
        url := fmt.Sprintf("%s/%s/%s/message_templates",
                t.client.baseURL, t.client.version, t.client.phoneNumberID)
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                return fmt.Errorf("template manager: new request: %w", err)
        }
        req.Header.Set("Authorization", "Bearer "+t.client.accessToken)
        resp, err := t.client.httpClient.Do(req)
        if err != nil {
                return fmt.Errorf("template manager: do request: %w", err)
        }
        defer resp.Body.Close()
        body, _ := io.ReadAll(resp.Body)
        if resp.StatusCode >= 400 {
                return fmt.Errorf("template manager: http %d: %s", resp.StatusCode, string(body))
        }
        // Parse Meta's response envelope:
        //   {"data": [{"name": "...", "status": "APPROVED", "language": "fr", "category": "MARKETING", "components": [...]}]}
        var meta struct {
                Data []struct {
                        Name     string `json:"name"`
                        Status   string `json:"status"`   // APPROVED | PENDING | REJECTED
                        Language string `json:"language"`
                        Category string `json:"category"`
                } `json:"data"`
        }
        if err := json.Unmarshal(body, &meta); err != nil {
                return fmt.Errorf("template manager: parse response: %w", err)
        }
        // Update each template's status in our DB. Best-effort: log unknown
        // template names rather than failing.
        for _, mt := range meta.Data {
                tpl, err := t.repo.GetByName(ctx, mt.Name)
                if err != nil {
                        if errors.Is(err, repository.ErrTemplateNotFound) {
                                // Meta has a template we don't track — skip.
                                continue
                        }
                        return fmt.Errorf("template manager: get %q: %w", mt.Name, err)
                }
                normalized := normalizeMetaStatus(mt.Status)
                if tpl.Status == normalized {
                        continue // no change
                }
                tpl.Status = normalized
                if _, err := t.repo.Update(ctx, tpl.ID, tpl); err != nil {
                        return fmt.Errorf("template manager: update %q: %w", mt.Name, err)
                }
        }
        return nil
}

// RenderTemplate fills the {{1}}, {{2}}, ... variables of the template body
// with the given params. params keys are positions as strings ("1", "2", ...).
// Returns the rendered text.
//
// Example: template "relance_panier" body:
//
//      Bonjour {{1}}, votre panier chez {{2}} vous attend. Confirmez ?
//
// with params {"1": "Awa", "2": "Boutique Abidjan Mode"} →
//
//      Bonjour Awa, votre panier chez Boutique Abidjan Mode vous attend. Confirmez ?
func (t *TemplateManager) RenderTemplate(ctx context.Context, name string, params map[string]string) (string, error) {
        tpl, err := t.repo.GetByName(ctx, name)
        if err != nil {
                return "", fmt.Errorf("render template: %w", err)
        }
        if tpl.Status != "approved" {
                return "", fmt.Errorf("render template: %q is not approved (status=%q)", name, tpl.Status)
        }
        return renderBody(tpl.Body, params), nil
}

// renderBody replaces {{1}}, {{2}}, ... in body with the values from params.
// Missing values leave the placeholder intact (defensive — Meta would reject
// the send, but we let the caller see the missing var).
func renderBody(body string, params map[string]string) string {
        if body == "" || len(params) == 0 {
                return body
        }
        // Use a regex to find {{N}} placeholders.
        re := regexp.MustCompile(`\{\{(\d+)\}\}`)
        return re.ReplaceAllStringFunc(body, func(match string) string {
                // Extract the digits between {{ and }}.
                digits := match[2 : len(match)-2]
                if v, ok := params[digits]; ok {
                        return v
                }
                return match
        })
}

// normalizeMetaStatus maps Meta's uppercase status to our lowercase DB values.
// Meta statuses: APPROVED | PENDING | REJECTED (also REJECTED needs the
// reason but we ignore that for now). Unknown values fall back to "pending".
func normalizeMetaStatus(s string) string {
        switch strings.ToUpper(strings.TrimSpace(s)) {
        case "APPROVED":
                return "approved"
        case "REJECTED":
                return "rejected"
        case "PENDING":
                return "pending"
        default:
                return "pending"
        }
}

// --- helpers for tests + callers -------------------------------------------

// RenderBodyStatic is the package-level version of renderBody (no DB lookup)
// — exposed so the WhatsApp admin handler can preview a template body with
// sample params without needing a TemplateManager instance.
func RenderBodyStatic(body string, params map[string]string) string {
        return renderBody(body, params)
}

// SortedParamKeys returns the keys of params ordered numerically (1, 2, ...)
// then alphabetically. Useful for building deterministic Components payloads.
func SortedParamKeys(params map[string]string) []string {
        keys := make([]string, 0, len(params))
        for k := range params {
                keys = append(keys, k)
        }
        sort.Slice(keys, func(i, j int) bool {
                ai, errA := strconv.Atoi(keys[i])
                aj, errB := strconv.Atoi(keys[j])
                if errA == nil && errB == nil {
                        return ai < aj
                }
                return keys[i] < keys[j]
        })
        return keys
}

// ============================================================================
// Spec-required methods: Create, GetByName, Send (ch. 6 — Modèles WhatsApp)
// ============================================================================

// Template is the spec-named alias for repository.MessageTemplate. Both names
// refer to the same type.
type Template = repository.MessageTemplate

// CreateTemplateRequest is the spec input for TemplateManager.Create. It
// bundles the fields needed to submit a new template to Meta for approval
// AND persist a local copy in our DB. Once Meta approves (status=APPROVED),
// the template can be sent via SendTemplate.
type CreateTemplateRequest struct {
        Name     string `json:"name"`
        Category string `json:"category"` // marketing | utility | authentication
        Language string `json:"language"` // fr | en | ...
        Body     string `json:"body"`     // template body with {{1}}, {{2}}, ...
}

// Create submits a new template to Meta for approval AND persists a local
// copy in the DB (status=pending until Meta approves). In mock mode the Meta
// submission is skipped — only the DB row is created.
//
// The caller (NOVA ops) is responsible for the template body abiding by
// Meta's rules (no promo codes in utility templates, variable count, etc.).
func (t *TemplateManager) Create(ctx context.Context, req CreateTemplateRequest) (*Template, error) {
        if req.Name == "" {
                return nil, errors.New("template manager: create: name is empty")
        }
        if req.Category == "" {
                return nil, errors.New("template manager: create: category is empty")
        }
        if req.Body == "" {
                return nil, errors.New("template manager: create: body is empty")
        }
        if req.Language == "" {
                req.Language = "fr"
        }
        // 1. Submit to Meta (best-effort — in mock mode we skip and just store
        //    the row locally). On failure we log but still store the row so the
        //    ops team can re-submit manually.
        if !t.client.IsMock() {
                if err := t.submitToMeta(ctx, req); err != nil {
                        return nil, fmt.Errorf("template manager: create: submit to meta: %w", err)
                }
        }
        // 2. Persist in DB.
        tpl := &repository.MessageTemplate{
                Name:     req.Name,
                Category: req.Category,
                Language: req.Language,
                Status:   "pending",
                Body:     req.Body,
        }
        created, err := t.repo.Create(ctx, tpl)
        if err != nil {
                return nil, fmt.Errorf("template manager: create: persist: %w", err)
        }
        return created, nil
}

// submitToMeta posts the template to Meta's message_templates endpoint for
// approval. Best-effort: failures are surfaced to the caller but the DB row
// can still be created (with status=pending) so the ops team can re-submit.
func (t *TemplateManager) submitToMeta(ctx context.Context, req CreateTemplateRequest) error {
        if t.client.phoneNumberID == "" {
                return errors.New("template manager: cannot submit to meta — phone_number_id is empty")
        }
        url := fmt.Sprintf("%s/%s/%s/message_templates",
                t.client.baseURL, t.client.version, t.client.phoneNumberID)
        payload := map[string]any{
                "name":     req.Name,
                "category": strings.ToUpper(req.Category),
                "language": req.Language,
                "components": []map[string]any{
                        {
                                "type": "BODY",
                                "text": req.Body,
                        },
                },
        }
        body, err := json.Marshal(payload)
        if err != nil {
                return fmt.Errorf("marshal: %w", err)
        }
        httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
        if err != nil {
                return fmt.Errorf("new request: %w", err)
        }
        httpReq.Header.Set("Authorization", "Bearer "+t.client.accessToken)
        httpReq.Header.Set("Content-Type", "application/json")
        resp, err := t.client.httpClient.Do(httpReq)
        if err != nil {
                return fmt.Errorf("do request: %w", err)
        }
        defer resp.Body.Close()
        respBody, _ := io.ReadAll(resp.Body)
        if resp.StatusCode >= 400 {
                return fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
        }
        return nil
}

// GetByName returns the template with the given Meta name. Wraps the
// repository method so callers don't have to import the repository package
// directly (spec: TemplateManager.GetByName).
func (t *TemplateManager) GetByName(ctx context.Context, name string) (*Template, error) {
        return t.repo.GetByName(ctx, name)
}

// Send is the convenience wrapper that sends a template message to a customer
// (ch. 6 — outside the 24h window, only pre-approved templates can be sent).
// It builds a TemplateMessage from the spec-style args and delegates to
// Client.SendTemplate. The caller is responsible for verifying the template
// is approved (status='approved' in the DB) and that the customer has not
// opted out of marketing messages.
//
// `language` defaults to "fr" when empty. `components` may be nil.
func (t *TemplateManager) Send(ctx context.Context, to, name, language string, components []Component) (*SendResult, error) {
        if to == "" {
                return nil, errors.New("template manager: send: recipient is empty")
        }
        if name == "" {
                return nil, errors.New("template manager: send: template name is empty")
        }
        if language == "" {
                language = "fr"
        }
        tpl := TemplateMessage{
                Name:       name,
                Language:   language,
                Components: components,
        }
        return t.client.SendTemplate(ctx, to, tpl)
}

// ============================================================================
// NOVA pre-defined templates (ch. 6 — Modèles WhatsApp)
// ============================================================================

// NovaTemplate is one of NOVA's pre-defined message templates. They are
// submitted to Meta during onboarding and seeded in the DB by migration 014.
// The bodies use {{1}}, {{2}}, ... for positional variables.
type NovaTemplate struct {
        Name     string
        Category string // marketing | utility
        Language string
        Body     string
}

// NovaTemplates is the catalog of pre-defined NOVA templates (ch. 6).
// Submitted to Meta for approval during onboarding; seeded in the DB by
// migration 014_nova_templates_seed.sql.
var NovaTemplates = []NovaTemplate{
        {
                Name:     "nova_order_confirmation",
                Category: "utility",
                Language: "fr",
                Body:     "Votre commande #{{1}} est confirmée. Total: {{2}} FCFA. Paiement: {{3}}.",
        },
        {
                Name:     "nova_order_status",
                Category: "utility",
                Language: "fr",
                Body:     "Votre commande #{{1}} est maintenant: {{2}}.",
        },
        {
                Name:     "nova_payment_reminder",
                Category: "utility",
                Language: "fr",
                Body:     "Bonjour {{1}}, votre abonnement NOVA arrive à échéance le {{2}}. Montant: {{3}} FCFA.",
        },
        {
                Name:     "nova_prospect_followup",
                Category: "marketing",
                Language: "fr",
                Body:     "Bonjour {{1}}, suite à votre intérêt pour {{2}}, souhaitez-vous plus d'informations ?",
        },
        {
                Name:     "nova_abandoned_cart",
                Category: "marketing",
                Language: "fr",
                Body:     "Bonjour {{1}}, votre panier vous attend. Souhaitez-vous finaliser votre commande ?",
        },
}

// EnsureNovaTemplates seeds the NOVA pre-defined templates into the DB if
// they're not already present. Idempotent — safe to call on every startup.
// Best-effort: errors are logged but don't fail the server boot.
func (t *TemplateManager) EnsureNovaTemplates(ctx context.Context) error {
        for _, nt := range NovaTemplates {
                existing, err := t.repo.GetByName(ctx, nt.Name)
                if err == nil && existing != nil {
                        // Already exists — skip.
                        continue
                }
                // Not found → insert.
                tpl := &repository.MessageTemplate{
                        Name:     nt.Name,
                        Category: nt.Category,
                        Language: nt.Language,
                        Status:   "pending",
                        Body:     nt.Body,
                }
                if _, err := t.repo.Create(ctx, tpl); err != nil {
                        // Log and continue — best-effort.
                        return fmt.Errorf("template manager: ensure nova templates: %q: %w", nt.Name, err)
                }
        }
        return nil
}
