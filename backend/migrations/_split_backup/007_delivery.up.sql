-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 007 : Zones de livraison et livraisons
-- ============================================================================
-- On ajoute ici la FK manquante entre orders.delivery_zone_id et delivery_zones
-- (la colonne orders.delivery_zone_id a été créée en 006 sans FK pour respecter
--  l'ordre des dépendances).
-- ============================================================================

-- ---------------------------------------------------------------------------
-- delivery_zones : Zones de livraison d'une boutique (tarifs, délais, seuils)
-- ---------------------------------------------------------------------------
CREATE TABLE delivery_zones (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id           uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    name              text        NOT NULL,                -- ex: "Cocody", "Yopougon"
    aliases           text[]      NOT NULL DEFAULT '{}',   -- alias reconnus par l'IA
    fee               bigint      NOT NULL CHECK (fee >= 0), -- tarif FCFA
    estimated_delay   text,                                  -- ex: "2h", "Jour J+1"
    free_from         bigint      CHECK (free_from IS NULL OR free_from >= 0),
    min_order_amount  bigint      CHECK (min_order_amount IS NULL OR min_order_amount >= 0),
    active            boolean     NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  delivery_zones IS 'Zones de livraison d''une boutique. Chaque zone a un tarif, un délai estimé, et des seuils (gratuité, montant minimum).';
COMMENT ON COLUMN delivery_zones.aliases IS 'Alias reconnus par l''IA (ex: ["Cocody","Cocody Angré","Angré"] pour la zone Cocody).';
COMMENT ON COLUMN delivery_zones.fee IS 'Tarif de livraison en FCFA.';
COMMENT ON COLUMN delivery_zones.free_from IS 'Montant à partir duquel la livraison est gratuite (NULL = jamais gratuit).';
COMMENT ON COLUMN delivery_zones.min_order_amount IS 'Montant minimum de commande pour cette zone (NULL = pas de minimum).';

CREATE INDEX delivery_zones_shop_id_active_idx ON delivery_zones (shop_id, active);

-- Ajout rétroactif de la FK orders.delivery_zone_id -> delivery_zones.id
ALTER TABLE orders
    ADD CONSTRAINT orders_delivery_zone_id_fkey
    FOREIGN KEY (delivery_zone_id) REFERENCES delivery_zones(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- deliveries : Suivi de livraison (lie order -> zone -> statut -> livreur)
-- ---------------------------------------------------------------------------
CREATE TABLE deliveries (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     uuid        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    zone_id      uuid        REFERENCES delivery_zones(id) ON DELETE SET NULL,
    address      text        NOT NULL,
    status       text        NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','preparing','out_for_delivery','delivered','failed')),
    driver       text,                                  -- texte libre au MVP (nom du livreur)
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  deliveries IS 'Suivi de livraison d''une commande. Pilote: 1 commande = au plus 1 livraison active.';
COMMENT ON COLUMN deliveries.status IS 'pending, preparing, out_for_delivery, delivered, failed.';
COMMENT ON COLUMN deliveries.driver IS 'Livreur au MVP (texte libre). Migration future vers une table drivers.';

CREATE INDEX deliveries_shop_id_status_idx ON deliveries (shop_id, status);
CREATE INDEX deliveries_order_id_idx       ON deliveries (order_id);
-- +goose StatementEnd
