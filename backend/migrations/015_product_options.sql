-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 015 : Product options — plats / accompagnements / boissons (NOVA v3)
-- ============================================================================
-- Pour les boutiques alimentaires, un produit (plat principal) peut avoir
-- des options associées:
--   - accompagnement (ketchup, mayonnaise, piment, frites)
--   - boisson       (Coca, eau, jus, Fanta)
-- Les options peuvent aussi être autonomes (product_id NULL) — par exemple
-- un plat ou une boisson vendu seul.
--
-- Chaque option a:
--   - type        : plat | accompagnement | boisson
--   - price       : bigint (0 = "Offert", jamais "0 FCFA")
--   - stock_mode  : quantite | epuise | illimite
--   - stock_qty   : int, NULL si illimite
--
-- Modes de stock:
--   quantite — stock géré (décrémenté comme inventory)
--   epuise   — explicitement en rupture (NOVA ne propose pas)
--   illimite — jamais décrémenté (plats faits à la commande)
--
-- RLS: shop_id = current_shop_id() OR is_platform_admin()
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Enum: option_type
-- ---------------------------------------------------------------------------
DO $$ BEGIN
    CREATE TYPE product_option_type AS ENUM (
        'plat',
        'accompagnement',
        'boisson'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- ---------------------------------------------------------------------------
-- Enum: stock_mode (réutilisé pour inventory + product_options)
-- ---------------------------------------------------------------------------
DO $$ BEGIN
    CREATE TYPE stock_mode AS ENUM (
        'quantite',
        'epuise',
        'illimite'
    );
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- ---------------------------------------------------------------------------
-- product_options : options de produits alimentaires
-- ---------------------------------------------------------------------------
CREATE TABLE product_options (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    product_id   uuid        REFERENCES products(id) ON DELETE CASCADE,
    type         product_option_type NOT NULL,
    name         text        NOT NULL,
    price        bigint      NOT NULL DEFAULT 0 CHECK (price >= 0),
    stock_mode   stock_mode  NOT NULL DEFAULT 'quantite',
    stock_qty    integer     CHECK (stock_qty IS NULL OR stock_qty >= 0),
    active       boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  product_options IS 'Options de produits alimentaires (plats, accompagnements, boissons). Peut être autonome (product_id NULL) ou rattaché à un produit.';
COMMENT ON COLUMN product_options.type IS 'plat (plat principal) | accompagnement (accompagnement) | boisson (boisson).';
COMMENT ON COLUMN product_options.price IS 'Prix FCFA. 0 = "Offert" (jamais affiché "0 FCFA").';
COMMENT ON COLUMN product_options.stock_mode IS 'quantite (stock décrémenté) | epuise (rupture explicite) | illimite (jamais décrémenté).';
COMMENT ON COLUMN product_options.stock_qty IS 'Stock courant. NULL si illimite. Décrémenté à chaque vente (mode quantite).';
COMMENT ON COLUMN product_options.product_id IS 'Produit parent (NULL si option autonome — par exemple une boisson vendue seule).';

CREATE INDEX product_options_shop_id_type_idx   ON product_options (shop_id, type);
CREATE INDEX product_options_product_id_idx     ON product_options (product_id) WHERE product_id IS NOT NULL;
CREATE INDEX product_options_shop_id_active_idx ON product_options (shop_id, active);

-- Trigger: empêcher la modification de stock_qty à la baisse sans mouvement
-- (best-effort — l'application doit gérer les mouvements). On garde simple:
-- pas de trigger, l'application gère.

-- ---------------------------------------------------------------------------
-- RLS: shop_id isolation
-- ---------------------------------------------------------------------------
ALTER TABLE product_options ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_product_options ON product_options
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation_product_options ON product_options;
ALTER TABLE product_options DISABLE ROW LEVEL SECURITY;
DROP TABLE IF EXISTS product_options;
DROP TYPE IF EXISTS product_option_type;
-- stock_mode is shared with inventory (migration 017); keep it if it's still referenced.
-- +goose StatementEnd
