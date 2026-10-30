-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 016 : Payment config — configuration du paiement par boutique (NOVA v3)
-- ============================================================================
-- Chaque boutique configure son mode de paiement préféré:
--   paiement_livraison  — paiement à la livraison (pas de validation)
--   paiement_avance     — acompte (advance_amount obligatoire)
--   paiement_integral   — paiement intégral avant livraison
--
-- Pour paiement_avance / paiement_integral, le client doit payer avant que
-- la commande passe à en_cours. NOVA ne valide JAMAIS un paiement: seul le
-- commerçant peut confirmer (CONFIRMER / REFUSER).
--
-- delay_minutes: délai de paiement (défaut 120 min = 2h). Si dépassé, la
-- commande est annulée et le stock libéré (cron job).
--
-- active_methods: méthodes acceptées (Wave, Moov, Orange, MTN). Chaque
-- méthode a son lien / numéro (wave_link, wave_number, etc.) pour que NOVA
-- puisse les présenter au client sans jamais les inventer.
--
-- RLS: shop_id = current_shop_id() OR is_platform_admin()
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Enum: payment_config_mode
-- ---------------------------------------------------------------------------
DO $$ BEGIN
    CREATE TYPE payment_config_mode AS ENUM (
        'paiement_livraison',
        'paiement_avance',
        'paiement_integral'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- ---------------------------------------------------------------------------
-- payment_configs : config de paiement par boutique (1 row par shop)
-- ---------------------------------------------------------------------------
CREATE TABLE payment_configs (
    shop_id          uuid PRIMARY KEY REFERENCES shops(id) ON DELETE CASCADE,
    mode             payment_config_mode NOT NULL DEFAULT 'paiement_livraison',
    advance_amount   bigint      CHECK (advance_amount IS NULL OR advance_amount >= 0),
    delay_minutes    integer     NOT NULL DEFAULT 120 CHECK (delay_minutes > 0),
    wave_link        text,
    wave_number      text,
    moov_number      text,
    orange_number    text,
    mtn_number       text,
    active_methods   text[]      NOT NULL DEFAULT '{}',
    active           boolean     NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  payment_configs IS 'Configuration du paiement par boutique (1 row par shop).';
COMMENT ON COLUMN payment_configs.mode IS 'paiement_livraison (à la livraison) | paiement_avance (acompte) | paiement_integral (intégral avant livraison).';
COMMENT ON COLUMN payment_configs.advance_amount IS 'Montant de l''acompte FCFA (utilisé seulement si mode = paiement_avance).';
COMMENT ON COLUMN payment_configs.delay_minutes IS 'Délai de paiement en minutes (défaut 120 = 2h). Si dépassé, la commande est annulée et le stock libéré.';
COMMENT ON COLUMN payment_configs.active_methods IS 'Méthodes de paiement activées: ["wave","moov","orange","mtn"].';
COMMENT ON COLUMN payment_configs.wave_link IS 'Lien de paiement Wave (URL). NULL si non configuré.';
COMMENT ON COLUMN payment_configs.wave_number IS 'Numéro Wave (texte libre).';
COMMENT ON COLUMN payment_configs.moov_number IS 'Numéro Moov Money.';
COMMENT ON COLUMN payment_configs.orange_number IS 'Numéro Orange Money.';
COMMENT ON COLUMN payment_configs.mtn_number IS 'Numéro MTN MoMo.';

-- ---------------------------------------------------------------------------
-- Seed : default config pour les boutiques existantes (paiement_livraison, 120min)
-- ---------------------------------------------------------------------------
SET LOCAL app.user_role = 'super_admin';
INSERT INTO payment_configs (shop_id, mode, delay_minutes, active_methods)
SELECT s.id, 'paiement_livraison', 120, ARRAY['wave','moov','orange','mtn']::text[]
  FROM shops s
 WHERE s.deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM payment_configs pc WHERE pc.shop_id = s.id);

-- ---------------------------------------------------------------------------
-- RLS
-- ---------------------------------------------------------------------------
ALTER TABLE payment_configs ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_payment_configs ON payment_configs
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation_payment_configs ON payment_configs;
ALTER TABLE payment_configs DISABLE ROW LEVEL SECURITY;
DROP TABLE IF EXISTS payment_configs;
DROP TYPE IF EXISTS payment_config_mode;
-- +goose StatementEnd
