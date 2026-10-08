-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 004 : Stock — inventaire courant et journal immuable des mouvements
-- ============================================================================
-- L'inventaire est matérialisé par variante (snapshot rapide pour l'IA et le
-- panier). Le journal stock_movements est append-only et constitue la source
-- de vérité; l'inventaire est recalculable à partir du journal.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- inventory : Stock courant par variante (cache calculé)
--   available = on_hand - reserved  (colonne GENERATED STORED)
-- ---------------------------------------------------------------------------
CREATE TABLE inventory (
    variant_id       uuid PRIMARY KEY REFERENCES product_variants(id) ON DELETE CASCADE,
    shop_id          uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    on_hand          integer     NOT NULL DEFAULT 0,   -- stock physique
    reserved         integer     NOT NULL DEFAULT 0,   -- réservé par paniers/commandes en cours
    alert_threshold  integer     NOT NULL DEFAULT 5,   -- seuil d'alerte stock bas
    available        integer     GENERATED ALWAYS AS (on_hand - reserved) STORED,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (on_hand >= 0),
    CHECK (reserved >= 0),
    CHECK (reserved <= on_hand),
    CHECK (alert_threshold >= 0)
);

COMMENT ON TABLE  inventory IS 'Stock courant par variante. Le stock disponible (available) est calculé: on_hand - reserved. Table dérivée du journal stock_movements.';
COMMENT ON COLUMN inventory.on_hand IS 'Quantité physique en stock.';
COMMENT ON COLUMN inventory.reserved IS 'Quantité réservée par des paniers/commandes en cours (pas encore sortis).';
COMMENT ON COLUMN inventory.available IS 'Stock disponible à la vente = on_hand - reserved. Colonne calculée (GENERATED).';
COMMENT ON COLUMN inventory.alert_threshold IS 'Seuil d''alerte en dessous duquel un rappel de réassort est émis.';
COMMENT ON COLUMN inventory.shop_id IS 'Dénormalisé pour RLS. Toujours égal à product_variants.shop_id.';

CREATE INDEX inventory_shop_id_idx            ON inventory (shop_id);
CREATE INDEX inventory_shop_id_low_stock_idx  ON inventory (shop_id, available)
    WHERE available <= alert_threshold;

-- ---------------------------------------------------------------------------
-- stock_movements : Journal immuable des mouvements de stock
--   Append-only : UPDATE et DELETE doivent être bloqués par trigger (voire RLS)
-- ---------------------------------------------------------------------------
CREATE TABLE stock_movements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    variant_id   uuid        NOT NULL REFERENCES product_variants(id) ON DELETE RESTRICT,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    type         stock_movement_type NOT NULL,
    quantity     integer     NOT NULL,            -- positif: entrée; négatif: sortie
    reason       text,                              -- motif libre
    order_id     uuid,                               -- si lié à une commande (FK ajoutée plus tard)
    author_id    uuid        REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (quantity <> 0)
);

COMMENT ON TABLE  stock_movements IS 'Journal immuable des mouvements de stock. Append-only: toute modification est interdite (audit). Les triggers/RLS bloquent UPDATE/DELETE.';
COMMENT ON COLUMN stock_movements.type IS 'Type: receipt, reservation, release, exit, return, adjustment.';
COMMENT ON COLUMN stock_movements.quantity IS 'Quantité signée: >0 entrée, <0 sortie.';
COMMENT ON COLUMN stock_movements.order_id IS 'Commande liée (réservation/sortie/retour). Pas de FK dur: la commande peut être supprimée logiquement.';
COMMENT ON COLUMN stock_movements.author_id IS 'Utilisateur à l''origine du mouvement (NULL si automation IA).';

CREATE INDEX stock_movements_variant_id_created_at_idx ON stock_movements (variant_id, created_at);
CREATE INDEX stock_movements_shop_id_created_at_idx    ON stock_movements (shop_id, created_at);
CREATE INDEX stock_movements_type_idx                  ON stock_movements (type);

-- ---------------------------------------------------------------------------
-- Trigger : empêcher UPDATE et DELETE sur le journal (append-only strict)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION nova_enforce_append_only_stock_movements()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    -- Seul un super_admin NOVA (via setting session) peut altérer le journal
    IF current_setting('app.allow_stock_journal_mutation', true) IS DISTINCT FROM 'true' THEN
        RAISE EXCEPTION 'stock_movements est un journal append-only: UPDATE/DELETE interdits'
            USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER stock_movements_append_only
    BEFORE UPDATE OR DELETE ON stock_movements
    FOR EACH ROW
    EXECUTE FUNCTION nova_enforce_append_only_stock_movements();
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS stock_movements_append_only ON stock_movements;
DROP FUNCTION IF EXISTS nova_enforce_append_only_stock_movements();
DROP TABLE IF EXISTS stock_movements;
DROP TABLE IF EXISTS inventory;
-- +goose StatementEnd
