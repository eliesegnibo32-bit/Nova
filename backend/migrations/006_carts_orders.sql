-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 006 : Paniers, commandes, lignes et historique des statuts
-- ============================================================================
-- Les montants (subtotal, delivery_fee, total, unit_price, line_total) sont
-- en FCFA entiers (bigint par sécurité, même si int suffirait).
-- Les snapshots (product_name, variant_info, unit_price) sont figés au moment
-- de la commande pour préserver la comptabilité si le catalogue change.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- carts : Panier en cours (conversation -> commande)
-- ---------------------------------------------------------------------------
CREATE TABLE carts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id         uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    conversation_id uuid        REFERENCES conversations(id) ON DELETE SET NULL,
    customer_id     uuid        REFERENCES customers(id) ON DELETE SET NULL,
    status          text        NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active','abandoned','converted')),
    expires_at      timestamptz,                          -- expiration du panier (anti-ghost)
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  carts IS 'Panier d''achat lié à une conversation. Peut expirer (abandoned) ou se convertir en commande (converted).';
COMMENT ON COLUMN carts.status IS 'active: en cours. abandoned: expiré sans commande. converted: transformé en commande.';
COMMENT ON COLUMN carts.expires_at IS 'Expiration: le panier est abandonné après cette date sans activité.';

CREATE INDEX carts_shop_id_status_idx        ON carts (shop_id, status);
CREATE INDEX carts_conversation_id_idx       ON carts (conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX carts_customer_id_idx           ON carts (shop_id, customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX carts_expires_at_idx            ON carts (expires_at) WHERE status = 'active';

-- ---------------------------------------------------------------------------
-- cart_items : Lignes d'un panier (snapshot léger)
-- ---------------------------------------------------------------------------
CREATE TABLE cart_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_id      uuid        NOT NULL REFERENCES carts(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    variant_id   uuid        REFERENCES product_variants(id) ON DELETE SET NULL,
    product_name text        NOT NULL,                  -- snapshot du nom produit
    unit_price   bigint      NOT NULL CHECK (unit_price >= 0),  -- snapshot prix FCFA
    quantity     integer     NOT NULL CHECK (quantity > 0)
);

COMMENT ON TABLE  cart_items IS 'Lignes d''un panier. Le prix et le nom sont des snapshots au cas où le catalogue changerait avant validation.';
COMMENT ON COLUMN cart_items.unit_price IS 'Prix unitaire FCFA figé au moment de l''ajout au panier.';

CREATE INDEX cart_items_cart_id_idx     ON cart_items (cart_id);
CREATE INDEX cart_items_shop_id_idx     ON cart_items (shop_id);

-- ---------------------------------------------------------------------------
-- orders : Commande (issue d'un panier ou saisie directe)
-- ---------------------------------------------------------------------------
CREATE TABLE orders (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id           uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    number            text        NOT NULL,             -- numéro lisible (#1048)
    customer_id       uuid        NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    cart_id           uuid        REFERENCES carts(id) ON DELETE SET NULL,
    status            order_status NOT NULL DEFAULT 'pending',
    payment_status    payment_status NOT NULL DEFAULT 'on_delivery',
    payment_mode      payment_mode NOT NULL DEFAULT 'cash',
    payment_reference text,                              -- référence paiement (mobile money)
    subtotal          bigint      NOT NULL CHECK (subtotal >= 0),
    delivery_fee      bigint      NOT NULL DEFAULT 0 CHECK (delivery_fee >= 0),
    total             bigint      NOT NULL CHECK (total >= 0),
    idempotency_key   text,                              -- clé d'idempotence (client-side)
    delivery_zone_id  uuid,                              -- FK ajoutée en 007
    delivery_address  text,
    note              text,                              -- note interne ou du client
    created_at        timestamptz NOT NULL DEFAULT now(),
    confirmed_at      timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, number),
    UNIQUE (shop_id, idempotency_key),
    -- Cohérence: total = subtotal + delivery_fee (tolérance 0)
    CHECK (total = subtotal + delivery_fee)
);

COMMENT ON TABLE  orders IS 'Commande d''une boutique. Numéro lisible unique par boutique (ex: #1048). Clé d''idempotence pour éviter les doubles validations.';
COMMENT ON COLUMN orders.number IS 'Numéro lisible, unique par boutique. Format suggéré: #NNNN incrémental.';
COMMENT ON COLUMN orders.idempotency_key IS 'Clé d''idempotence client. Évite la double création d''une même commande.';
COMMENT ON COLUMN orders.subtotal IS 'Sous-total (somme des lignes) en FCFA.';
COMMENT ON COLUMN orders.delivery_fee IS 'Frais de livraison en FCFA (dépend de la zone).';
COMMENT ON COLUMN orders.total IS 'Total = subtotal + delivery_fee (vérifié par CHECK).';
COMMENT ON COLUMN orders.payment_status IS 'Statut du paiement (on_delivery, declared, paid, ...).';
COMMENT ON COLUMN orders.delivery_zone_id IS 'Zone de livraison (FK ajoutée en migration 007).';

CREATE INDEX orders_shop_id_status_idx           ON orders (shop_id, status);
CREATE INDEX orders_shop_id_created_at_idx       ON orders (shop_id, created_at DESC);
CREATE INDEX orders_shop_id_payment_status_idx   ON orders (shop_id, payment_status);
CREATE INDEX orders_shop_id_customer_id_idx      ON orders (shop_id, customer_id);
CREATE INDEX orders_customer_id_idx              ON orders (customer_id);

-- ---------------------------------------------------------------------------
-- order_items : Lignes de commande (prix figés)
-- ---------------------------------------------------------------------------
CREATE TABLE order_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id      uuid        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    shop_id       uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    variant_id    uuid        REFERENCES product_variants(id) ON DELETE SET NULL,
    product_name  text        NOT NULL,                -- snapshot nom produit
    variant_info  text,                                -- snapshot "Taille M, Bleu"
    unit_price    bigint      NOT NULL CHECK (unit_price >= 0),  -- prix figé FCFA
    quantity      integer     NOT NULL CHECK (quantity > 0),
    line_total    bigint      NOT NULL CHECK (line_total >= 0),
    CHECK (line_total = unit_price * quantity)
);

COMMENT ON TABLE  order_items IS 'Lignes figées d''une commande. Le prix unitaire et les libellés sont des snapshots pour préserver la comptabilité.';
COMMENT ON COLUMN order_items.variant_info IS 'Snapshot lisible de la variante (ex: "Taille M, Bleu").';
COMMENT ON COLUMN order_items.unit_price IS 'Prix unitaire FCFA figé au moment de la commande.';
COMMENT ON COLUMN order_items.line_total IS 'Total de ligne = unit_price * quantity (vérifié par CHECK).';

CREATE INDEX order_items_order_id_idx  ON order_items (order_id);
CREATE INDEX order_items_shop_id_idx   ON order_items (shop_id);

-- ---------------------------------------------------------------------------
-- order_events : Historique immuable des changements de statut
-- ---------------------------------------------------------------------------
CREATE TABLE order_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     uuid        NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    shop_id      uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    from_status  order_status,
    to_status    order_status NOT NULL,
    author_id    uuid        REFERENCES users(id) ON DELETE SET NULL,
    reason       text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  order_events IS 'Historique immuable des changements de statut d''une commande (machine à états du cahier des charges).';
COMMENT ON COLUMN order_events.from_status IS 'Statut précédent (NULL pour la création).';
COMMENT ON COLUMN order_events.to_status IS 'Nouveau statut.';
COMMENT ON COLUMN order_events.author_id IS 'Auteur du changement (employé ou NULL pour automation IA).';
COMMENT ON COLUMN order_events.reason IS 'Motif du changement (ex: "client absent", "paiement reçu").';

CREATE INDEX order_events_order_id_created_idx ON order_events (order_id, created_at);
CREATE INDEX order_events_shop_id_created_idx  ON order_events (shop_id, created_at);
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS order_events;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS cart_items;
DROP TABLE IF EXISTS carts;
-- +goose StatementEnd
