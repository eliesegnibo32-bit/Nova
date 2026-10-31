-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 005 : Clients, conversations et messages WhatsApp
-- ============================================================================

-- ---------------------------------------------------------------------------
-- customers : Clients et prospects d'une boutique
-- ---------------------------------------------------------------------------
CREATE TABLE customers (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id             uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    phone               text        NOT NULL,                  -- E.164
    name                text,                                  -- nom (NULL si inconnu)
    status              customer_status NOT NULL DEFAULT 'prospect',
    consent_marketing   boolean     NOT NULL DEFAULT false,    -- opt-in marketing
    consent_source      text,                                  -- origine du consentement
    consent_at          timestamptz,                           -- date du consentement
    last_interaction_at timestamptz,                           -- dernière activité
    total_orders        integer     NOT NULL DEFAULT 0,
    total_spent         bigint      NOT NULL DEFAULT 0,        -- FCFA
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    deleted_at          timestamptz,
    UNIQUE (shop_id, phone)
);

COMMENT ON TABLE  customers IS 'Clients et prospects d''une boutique. Identifiés par téléphone (un seul par boutique).';
COMMENT ON COLUMN customers.phone IS 'Numéro de téléphone au format E.164 (ex: +2250700000000).';
COMMENT ON COLUMN customers.status IS 'Statut commercial: prospect (jamais commandé), client (1+ commande), recurring (récurrent).';
COMMENT ON COLUMN customers.consent_marketing IS 'Consentement marketing (opt-in RGPD-like, requis pour campagnes WhatsApp).';
COMMENT ON COLUMN customers.total_orders IS 'Compteur dénormalisé: nombre total de commandes livrées.';
COMMENT ON COLUMN customers.total_spent IS 'Cumul dénormalisé: montant total dépensé en FCFA.';
COMMENT ON COLUMN customers.deleted_at IS 'Suppression logique pour préserver l''historique conversationnel et comptable.';

CREATE INDEX customers_shop_id_status_idx       ON customers (shop_id, status) WHERE deleted_at IS NULL;
CREATE INDEX customers_shop_id_phone_idx        ON customers (shop_id, phone) WHERE deleted_at IS NULL;
CREATE INDEX customers_shop_id_last_inter_idx   ON customers (shop_id, last_interaction_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX customers_deleted_at_idx           ON customers (deleted_at) WHERE deleted_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- conversations : Fil de discussion avec un client
-- ---------------------------------------------------------------------------
CREATE TABLE conversations (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id               uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    customer_id           uuid        NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    channel               text        NOT NULL DEFAULT 'whatsapp',  -- canal (extensible)
    state                 conversation_state NOT NULL DEFAULT 'ai',
    window_24h_expires_at timestamptz,                              -- fenêtre de réponse gratuite Meta 24h
    taken_over_by         uuid        REFERENCES users(id) ON DELETE SET NULL,
    summary               text,                                     -- résumé IA du fil
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  conversations IS 'Fil de discussion avec un client. Peut être géré par l''IA (state=ai), repris par un humain (state=human) ou clôturé (state=closed).';
COMMENT ON COLUMN conversations.window_24h_expires_at IS 'Expiration de la fenêtre de réponse gratuite WhatsApp (24h après dernier message client).';
COMMENT ON COLUMN conversations.taken_over_by IS 'Employé ayant repris la conversation (reprise humaine).';
COMMENT ON COLUMN conversations.summary IS 'Résumé automatique du fil pour le handover humain.';

CREATE INDEX conversations_shop_id_state_idx     ON conversations (shop_id, state);
CREATE INDEX conversations_shop_id_customer_idx  ON conversations (shop_id, customer_id);
CREATE INDEX conversations_shop_id_updated_idx   ON conversations (shop_id, updated_at DESC);

-- ---------------------------------------------------------------------------
-- messages : Messages individuels (entrants et sortants)
-- ---------------------------------------------------------------------------
CREATE TABLE messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    shop_id         uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    direction       message_direction NOT NULL,
    type            text        NOT NULL DEFAULT 'text'
                        CHECK (type IN ('text','image','audio','video','document','button','interactive','template')),
    content         text,                                  -- texte ou payload JSON pour non-text
    wamid           text        UNIQUE,                    -- WhatsApp Message ID (API Cloud Meta)
    status          message_status NOT NULL DEFAULT 'queued',
    created_at      timestamptz NOT NULL DEFAULT now(),
    delivered_at    timestamptz,
    read_at         timestamptz
);

COMMENT ON TABLE  messages IS 'Messages individuels d''une conversation WhatsApp. wamid est unique (clé de déduplication des webhooks Meta).';
COMMENT ON COLUMN messages.direction IS 'inbound: reçu du client. outbound: envoyé par NOVA ou un employé.';
COMMENT ON COLUMN messages.type IS 'Type de message WhatsApp (text, image, audio, button, interactive...).';
COMMENT ON COLUMN messages.wamid IS 'Identifiant Meta (whatsapp_message_id). Unique. Sert de clé d''idempotence pour les webhooks.';
COMMENT ON COLUMN messages.status IS 'Statut d''envoi: queued, sent, delivered, read, failed.';

CREATE INDEX messages_conversation_id_created_idx ON messages (conversation_id, created_at);
CREATE INDEX messages_shop_id_created_idx         ON messages (shop_id, created_at);
CREATE INDEX messages_status_idx                  ON messages (status) WHERE status IN ('queued','failed');
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS customers;
-- +goose StatementEnd
