-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 009 : Consommation IA, modèles WhatsApp et consentements clients
-- ============================================================================

-- ---------------------------------------------------------------------------
-- ai_usage : Journal de consommation IA (coûts, latence, par conversation)
-- ---------------------------------------------------------------------------
CREATE TABLE ai_usage (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id         uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    conversation_id uuid        REFERENCES conversations(id) ON DELETE SET NULL,
    model           text        NOT NULL,                  -- nom du modèle LLM
    tokens_in       integer     NOT NULL DEFAULT 0,
    tokens_out      integer     NOT NULL DEFAULT 0,
    latency_ms      integer     NOT NULL DEFAULT 0,
    estimated_cost  numeric(12,6) NOT NULL DEFAULT 0,     -- coût USD estimé
    created_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  ai_usage IS 'Journal de consommation IA (tokens, latence, coût estimé) par boutique et par conversation. Sert au quota et à la facturation.';
COMMENT ON COLUMN ai_usage.model IS 'Nom du modèle LLM (ex: gpt-4o-mini, claude-3-haiku).';
COMMENT ON COLUMN ai_usage.estimated_cost IS 'Coût USD estimé (numeric pour précision). Conversion FCFA faite côté application.';

CREATE INDEX ai_usage_shop_id_created_at_idx        ON ai_usage (shop_id, created_at);
CREATE INDEX ai_usage_shop_id_conversation_id_idx   ON ai_usage (shop_id, conversation_id);
CREATE INDEX ai_usage_shop_id_model_idx             ON ai_usage (shop_id, model, created_at);

-- ---------------------------------------------------------------------------
-- message_templates : Modèles WhatsApp approuvés par Meta
-- ---------------------------------------------------------------------------
CREATE TABLE message_templates (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text        NOT NULL UNIQUE,             -- nom Meta (ex: order_confirmation_fr)
    category    text        NOT NULL
                    CHECK (category IN ('marketing','utility','authentication')),
    language    text        NOT NULL DEFAULT 'fr',
    status      text        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','approved','rejected')),
    body        text        NOT NULL,                    -- corps du modèle (avec variables {{1}}, {{2}})
    variables   jsonb       NOT NULL DEFAULT '[]'::jsonb, -- métadonnées des variables
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  message_templates IS 'Modèles WhatsApp approuvés par Meta (obligatoires hors fenêtre 24h). Globaux à la plateforme.';
COMMENT ON COLUMN message_templates.category IS 'Catégorie Meta: marketing, utility, authentication.';
COMMENT ON COLUMN message_templates.status IS 'Statut d''approbation Meta: pending, approved, rejected.';
COMMENT ON COLUMN message_templates.body IS 'Corps du modèle avec variables Meta (ex: Bonjour {{1}}, votre commande {{2}}...).';
COMMENT ON COLUMN message_templates.variables IS 'Métadonnées des variables (libellés, types) en JSONB.';

CREATE INDEX message_templates_status_idx    ON message_templates (status);
CREATE INDEX message_templates_category_idx  ON message_templates (category);

-- ---------------------------------------------------------------------------
-- consents : Consentements clients (RGPD-like, multi-types)
-- ---------------------------------------------------------------------------
CREATE TABLE consents (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  uuid        NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    type         text        NOT NULL,                    -- ex: 'marketing', 'analytics'
    source       text,                                    -- origine (chat, formulaire, ...)
    granted_at   timestamptz NOT NULL DEFAULT now(),
    withdrawn_at timestamptz                             -- retrait (NULL = actif)
);

COMMENT ON TABLE  consents IS 'Consentements clients par type (marketing, analytics, ...). Permet le suivi RGPD-like: opt-in et opt-out datés.';
COMMENT ON COLUMN consents.type IS 'Type de consentement (marketing, analytics, ...).';
COMMENT ON COLUMN consents.source IS 'Origine du consentement (chat WhatsApp, formulaire web, import).';
COMMENT ON COLUMN consents.granted_at IS 'Date d''octroi du consentement.';
COMMENT ON COLUMN consents.withdrawn_at IS 'Date de retrait du consentement (NULL = toujours actif).';

CREATE INDEX consents_customer_id_idx     ON consents (customer_id);
CREATE INDEX consents_shop_id_idx         ON consents (shop_id);
CREATE INDEX consents_shop_id_type_idx    ON consents (shop_id, type) WHERE withdrawn_at IS NULL;
-- +goose StatementEnd
