-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 002 : Boutiques, comptes utilisateurs et rattachement (shop_members)
-- ============================================================================
-- Toutes les tables métier porteront shop_id (ch. 9.3 du cahier des charges).
-- Les users sont globaux (un compte = une personne, potentiellement multi-
-- boutiques). Le rattachement à une boutique se fait via shop_members.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- shops : Boutique (locataire du SaaS)
-- ---------------------------------------------------------------------------
CREATE TABLE shops (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    text        NOT NULL,
    slug                    text        NOT NULL UNIQUE,
    phone                   text,                          -- numéro de contact
    whatsapp_number         text,                          -- numéro WhatsApp (E.164)
    address                 text,
    commune                 text,                          -- commune d'Abidjan ou ville
    hours                   jsonb       NOT NULL DEFAULT '{}'::jsonb,  -- horaires d'ouverture
    description             text,
    categories              text[]      NOT NULL DEFAULT '{}',
    sale_conditions         text,                          -- conditions de vente
    accepted_payment_modes  payment_mode[] NOT NULL DEFAULT '{}',
    ai_settings             jsonb       NOT NULL DEFAULT '{}'::jsonb,
                                                            -- {tone, confirmation_mode, ...}
    status                  text        NOT NULL DEFAULT 'active'
                                CHECK (status IN ('active','suspended','terminated')),
    logo_url                text,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    deleted_at              timestamptz                       -- suppression logique
);

COMMENT ON TABLE  shops IS 'Boutique locataire du SaaS NOVA. Toute donnée métier se rattache à une boutique.';
COMMENT ON COLUMN shops.ai_settings IS 'Paramètres IA: ton (formel/detendu), confirmation_mode (auto/manual), etc.';
COMMENT ON COLUMN shops.accepted_payment_modes IS 'Modes de paiement acceptés par la boutique (mobile money, cash...).';
COMMENT ON COLUMN shops.hours IS 'Horaires d''ouverture en JSONB (ex: {"mon":["08:00","18:00"]}).';
COMMENT ON COLUMN shops.deleted_at IS 'Suppression logique: NULL = actif, sinon date de suppression.';

CREATE INDEX shops_status_idx        ON shops (status) WHERE deleted_at IS NULL;
CREATE INDEX shops_deleted_at_idx    ON shops (deleted_at) WHERE deleted_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- users : Compte utilisateur global (plateforme + boutiques)
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email                 text        NOT NULL UNIQUE,
    phone                 text,                          -- téléphone de l'utilisateur
    password_hash         text,                          -- Argon2id ou bcrypt
    full_name             text        NOT NULL,
    role                  user_role   NOT NULL DEFAULT 'owner',
    two_factor_secret     text,                          -- secret TOTP (chiffré applicativement)
    two_factor_enabled    boolean     NOT NULL DEFAULT false,
    failed_login_count    integer     NOT NULL DEFAULT 0,
    locked_until          timestamptz,                    -- verrouillage progressif
    last_login_at         timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    deleted_at            timestamptz
);

COMMENT ON TABLE  users IS 'Compte utilisateur global. Le rôle global user_role distingue les administrateurs NOVA (super_admin, admin) des rôles boutique (owner, employee). Le rôle exact dans une boutique est porté par shop_members.role.';
COMMENT ON COLUMN users.password_hash IS 'Empreinte du mot de passe (Argon2id recommandé).';
COMMENT ON COLUMN users.two_factor_enabled IS 'Optionnel pour les owners, obligatoire pour les admins NOVA.';
COMMENT ON COLUMN users.locked_until IS 'Verrouillage progressif après échecs de connexion (ch. 9.4).';
COMMENT ON COLUMN users.deleted_at IS 'Suppression logique: NULL = actif.';

CREATE INDEX users_role_idx        ON users (role) WHERE deleted_at IS NULL;
CREATE INDEX users_deleted_at_idx  ON users (deleted_at) WHERE deleted_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- shop_members : rattachement d'un utilisateur à une boutique (n-aire)
-- ---------------------------------------------------------------------------
CREATE TABLE shop_members (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         user_role   NOT NULL,
    permissions  jsonb       NOT NULL DEFAULT '{}'::jsonb,  -- permissions fines par feature
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, user_id)
);

COMMENT ON TABLE  shop_members IS 'Rattachement d''un utilisateur à une boutique avec un rôle (owner/employee). Un utilisateur peut appartenir à plusieurs boutiques.';
COMMENT ON COLUMN shop_members.role IS 'Rôle dans la boutique: owner ou employee. Les rôles super_admin/admin sont réservés à la plateforme (table users.role).';
COMMENT ON COLUMN shop_members.permissions IS 'Permissions fines au format JSONB (ex: {"orders":{"refund":true}}).';

CREATE INDEX shop_members_shop_id_idx   ON shop_members (shop_id);
CREATE INDEX shop_members_user_id_idx   ON shop_members (user_id);
-- +goose StatementEnd
