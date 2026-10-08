-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 018 : Drop delivery tracking (NOVA v3 — spec section 13)
-- ============================================================================
-- BREAKING (intentionnel): on supprime la table `deliveries` qui servait au
-- suivi de livraison (status du livreur, driver, etc.). Le spec v3 dit:
--   "Pas de suivi de livraison — on garde delivery_zones pour le calcul des
--    frais, mais pas de feature 'suivi de livraison'."
--
-- On garde:
--   - delivery_zones (pour le calcul des frais)
--   - orders.delivery_zone_id + delivery_address (pour savoir où livrer)
--
-- On supprime:
--   - la table deliveries
--   - ses index
--   - sa politique RLS
-- ============================================================================

DROP POLICY IF EXISTS tenant_isolation_deliveries ON deliveries;
DROP TABLE IF EXISTS deliveries;

-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
-- ============================================================================
-- Rollback : recréer deliveries (au cas où on voudrait revenir en arrière)
-- ============================================================================
CREATE TABLE IF NOT EXISTS deliveries (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     uuid        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    zone_id      uuid        REFERENCES delivery_zones(id) ON DELETE SET NULL,
    address      text        NOT NULL,
    status       text        NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','preparing','out_for_delivery','delivered','failed')),
    driver       text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS deliveries_shop_id_status_idx ON deliveries (shop_id, status);
CREATE INDEX IF NOT EXISTS deliveries_order_id_idx       ON deliveries (order_id);

ALTER TABLE deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_deliveries ON deliveries
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());
-- +goose StatementEnd
