-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 003 : Catalogue — produits, variantes, photos
-- ============================================================================
-- La dénormalisation de shop_id sur product_variants permet:
--   - l'unicité du SKU par boutique UNIQUE (shop_id, sku)
--   - une vérification RLS efficace sans jointure vers products
-- ============================================================================

-- ---------------------------------------------------------------------------
-- products : Produit du catalogue boutique
-- ---------------------------------------------------------------------------
CREATE TABLE products (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    description  text,
    category     text,                          -- catégorie libre (texte)
    brand        text,                          -- marque / fournisseur
    status       product_status NOT NULL DEFAULT 'draft',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz                       -- suppression logique
);

COMMENT ON TABLE  products IS 'Produit du catalogue d''une boutique. Un produit peut avoir plusieurs variantes (taille/couleur/prix).';
COMMENT ON COLUMN products.category IS 'Catégorie libre (texte) - sert au filtrage catalogue.';
COMMENT ON COLUMN products.status IS 'Statut de publication: draft (brouillon), published (visible), archived (archivé).';
COMMENT ON COLUMN products.deleted_at IS 'Suppression logique pour préserver l''historique des commandes.';

CREATE INDEX products_shop_id_status_idx    ON products (shop_id, status) WHERE deleted_at IS NULL;
CREATE INDEX products_shop_id_category_idx  ON products (shop_id, category) WHERE deleted_at IS NULL;
CREATE INDEX products_shop_id_name_idx      ON products (shop_id, name) WHERE deleted_at IS NULL;
CREATE INDEX products_deleted_at_idx        ON products (deleted_at) WHERE deleted_at IS NOT NULL;

-- ---------------------------------------------------------------------------
-- product_variants : Variante vendable (SKU, taille, couleur, prix)
-- shop_id est dénormalisé pour RLS et unicité (shop_id, sku)
-- ---------------------------------------------------------------------------
CREATE TABLE product_variants (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id   uuid        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    sku          text        NOT NULL,
    size         text,                          -- taille (S/M/L/XL...)
    color        text,                          -- couleur
    price        bigint      NOT NULL CHECK (price >= 0),       -- prix en FCFA (entiers)
    promo_price  bigint      CHECK (promo_price IS NULL OR promo_price >= 0),
    promo_start  timestamptz,
    promo_end    timestamptz,
    active       boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, sku),
    -- Une promo ne peut pas être plus chère que le prix normal
    CHECK (promo_price IS NULL OR promo_price < price),
    -- Cohérence des dates de promo
    CHECK (
        (promo_start IS NULL AND promo_end IS NULL AND promo_price IS NULL)
        OR (promo_start IS NOT NULL AND promo_end IS NOT NULL AND promo_price IS NOT NULL)
    ),
    CHECK (promo_start IS NULL OR promo_end IS NULL OR promo_start < promo_end)
);

COMMENT ON TABLE  product_variants IS 'Variante vendable d''un produit (combinaison taille/couleur/prix). shop_id est dénormalisé pour RLS.';
COMMENT ON COLUMN product_variants.sku IS 'SKU unique par boutique. Sert de référence métier (stock, commandes).';
COMMENT ON COLUMN product_variants.price IS 'Prix en FCFA (entiers - pas de décimales en FCFA).';
COMMENT ON COLUMN product_variants.promo_price IS 'Prix promo en FCFA, optionnel. Doit être strictement inférieur à price.';
COMMENT ON COLUMN product_variants.active IS 'Variante active = proposée par l''IA dans le panier.';

CREATE INDEX product_variants_product_id_idx     ON product_variants (product_id);
CREATE INDEX product_variants_shop_id_active_idx ON product_variants (shop_id, active);
CREATE INDEX product_variants_shop_id_sku_idx    ON product_variants (shop_id, sku);

-- ---------------------------------------------------------------------------
-- product_images : Photos d'un produit (ordonnées)
-- ---------------------------------------------------------------------------
CREATE TABLE product_images (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id   uuid        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    url          text        NOT NULL,                       -- URL R2 / CDN
    ord          integer     NOT NULL DEFAULT 0,             -- ordre d'affichage
    created_at   timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  product_images IS 'Photos d''un produit, ordonnées pour l''affichage (catalogue + WhatsApp).';
COMMENT ON COLUMN product_images.ord IS 'Ordre d''affichage (0 = principale).';
COMMENT ON COLUMN product_images.shop_id IS 'Dénormalisé pour RLS efficace.';

CREATE INDEX product_images_product_id_ord_idx ON product_images (product_id, ord);
CREATE INDEX product_images_shop_id_idx        ON product_images (shop_id);
-- +goose StatementEnd
