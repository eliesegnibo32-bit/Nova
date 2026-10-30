-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 011 : Row Level Security — isolation multi-boutiques
-- ============================================================================
-- Le RLS est la SECONDE ligne de défense (ch. 9.3 du cahier des charges).
-- La première ligne est le contrôle applicatif côté Go, qui positionne la
-- boutique courante via:
--     SET LOCAL app.current_shop_id = '<uuid>';
--     SET LOCAL app.user_id        = '<uuid>';
--     SET LOCAL app.user_role      = 'owner';  -- ou 'super_admin' / 'admin'
--
-- Ces paramètres sont LOCAUX À LA TRANSACTION: un pool en mode transaction
-- (pgx + BeginTx) garantit qu'ils ne fuient pas entre requêtes.
--
-- Convention de nommage des politiques:
--   {action}_{table}             ex: select_products, insert_products
--   Les politiques combinées utilisent tenant_isolation_{table}.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Fonctions utilitaires STABLE (pas SECURITY DEFINER pour celles-ci)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION current_shop_id()
RETURNS uuid
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.current_shop_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION current_user_id()
RETURNS uuid
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION is_platform_admin()
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT COALESCE(current_setting('app.user_role', true), '') IN ('super_admin', 'admin')
$$;

-- Vérifie si un utilisateur est membre d'une boutique donnée.
-- SECURITY DEFINER pour contourner le RLS de shop_members (sinon récursion).
-- Le propriétaire de la fonction doit être un rôle qui bypass RLS (superuser
-- ou propriétaire des tables lors de la migration initiale).
CREATE OR REPLACE FUNCTION is_shop_member_of(p_shop_id uuid, p_user_id uuid)
RETURNS boolean
LANGUAGE sql
SECURITY DEFINER
STABLE
SET search_path = pg_catalog, public
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.shop_members sm
        WHERE sm.shop_id = p_shop_id
          AND sm.user_id = p_user_id
    )
$$;

COMMENT ON FUNCTION current_shop_id()      IS 'UUID de la boutique courante (positionnée par SET LOCAL app.current_shop_id). NULL si non défini.';
COMMENT ON FUNCTION current_user_id()      IS 'UUID de l''utilisateur courant (positionné par SET LOCAL app.user_id). NULL si non authentifié.';
COMMENT ON FUNCTION is_platform_admin()    IS 'Vrai si l''utilisateur courant est super_admin ou admin NOVA. Contourne l''isolation par boutique.';
COMMENT ON FUNCTION is_shop_member_of(uuid, uuid) IS 'Vrai si l''utilisateur appartient à la boutique. SECURITY DEFINER pour éviter la récursion RLS.';

-- ============================================================================
-- shops : isolation spéciale — la boutique EST le locataire
-- ============================================================================
ALTER TABLE shops ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_shops_select ON shops
    FOR SELECT USING (
        id = current_shop_id()
        OR is_platform_admin()
    );

CREATE POLICY tenant_isolation_shops_insert ON shops
    FOR INSERT WITH CHECK (is_platform_admin());

CREATE POLICY tenant_isolation_shops_update ON shops
    FOR UPDATE USING (is_platform_admin())
    WITH CHECK (is_platform_admin());

CREATE POLICY tenant_isolation_shops_delete ON shops
    FOR DELETE USING (is_platform_admin());

-- ============================================================================
-- users : isolation spéciale (les users sont globaux, multi-boutiques)
-- Un utilisateur peut SE LIRE lui-même, être lu par un admin plateforme,
-- ou être lu par un membre de la boutique courante (pour la liste du staff).
-- ============================================================================
ALTER TABLE users ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_users_select ON users
    FOR SELECT USING (
        is_platform_admin()
        OR id = current_user_id()
        OR is_shop_member_of(current_shop_id(), id)
    );

CREATE POLICY tenant_isolation_users_insert ON users
    FOR INSERT WITH CHECK (is_platform_admin());

CREATE POLICY tenant_isolation_users_update ON users
    FOR UPDATE USING (
        is_platform_admin()
        OR id = current_user_id()
    )
    WITH CHECK (
        is_platform_admin()
        OR id = current_user_id()
    );

CREATE POLICY tenant_isolation_users_delete ON users
    FOR DELETE USING (is_platform_admin());

-- ============================================================================
-- shop_members : visible par les membres de la boutique et les admins plateforme
-- ============================================================================
ALTER TABLE shop_members ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_shop_members_select ON shop_members
    FOR SELECT USING (
        is_platform_admin()
        OR shop_id = current_shop_id()
    );

CREATE POLICY tenant_isolation_shop_members_insert ON shop_members
    FOR INSERT WITH CHECK (
        is_platform_admin()
        OR (shop_id = current_shop_id()
            AND is_shop_member_of(current_shop_id(), current_user_id()))
    );

CREATE POLICY tenant_isolation_shop_members_update ON shop_members
    FOR UPDATE USING (
        is_platform_admin()
        OR shop_id = current_shop_id()
    )
    WITH CHECK (
        is_platform_admin()
        OR shop_id = current_shop_id()
    );

CREATE POLICY tenant_isolation_shop_members_delete ON shop_members
    FOR DELETE USING (
        is_platform_admin()
        OR shop_id = current_shop_id()
    );

-- ============================================================================
-- Tables métier "standard" : isolation par shop_id = current_shop_id()
-- L'admin plateforme contourne l'isolation (utile pour le support NOVA).
-- ============================================================================
-- Pour la lisibilité, on crée une politique combinée FOR ALL par table.

-- products
ALTER TABLE products ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_products ON products
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- product_variants
ALTER TABLE product_variants ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_product_variants ON product_variants
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- product_images
ALTER TABLE product_images ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_product_images ON product_images
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- inventory
ALTER TABLE inventory ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_inventory ON inventory
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- stock_movements : INSERT + SELECT uniquement (journal append-only).
-- Pas de politique UPDATE/DELETE => RLS bloque par défaut.
ALTER TABLE stock_movements ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_stock_movements_select ON stock_movements
    FOR SELECT USING (shop_id = current_shop_id() OR is_platform_admin());
CREATE POLICY tenant_isolation_stock_movements_insert ON stock_movements
    FOR INSERT WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- customers
ALTER TABLE customers ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_customers ON customers
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- conversations
ALTER TABLE conversations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_conversations ON conversations
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- messages
ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_messages ON messages
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- carts
ALTER TABLE carts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_carts ON carts
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- cart_items
ALTER TABLE cart_items ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_cart_items ON cart_items
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- orders
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_orders ON orders
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- order_items
ALTER TABLE order_items ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_order_items ON order_items
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- order_events : INSERT + SELECT uniquement (journal append-only).
ALTER TABLE order_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_order_events_select ON order_events
    FOR SELECT USING (shop_id = current_shop_id() OR is_platform_admin());
CREATE POLICY tenant_isolation_order_events_insert ON order_events
    FOR INSERT WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- delivery_zones
ALTER TABLE delivery_zones ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_delivery_zones ON delivery_zones
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- deliveries
ALTER TABLE deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_deliveries ON deliveries
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- subscriptions
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_subscriptions ON subscriptions
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- subscription_payments
ALTER TABLE subscription_payments ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_subscription_payments ON subscription_payments
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- ai_usage : INSERT + SELECT (uniquement généré par l'API, jamais modifié)
ALTER TABLE ai_usage ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_ai_usage_select ON ai_usage
    FOR SELECT USING (shop_id = current_shop_id() OR is_platform_admin());
CREATE POLICY tenant_isolation_ai_usage_insert ON ai_usage
    FOR INSERT WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- consents
ALTER TABLE consents ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_consents ON consents
    FOR ALL USING (shop_id = current_shop_id() OR is_platform_admin())
    WITH CHECK (shop_id = current_shop_id() OR is_platform_admin());

-- notifications : le destinataire ne voit que ses propres notifs.
-- L'admin plateforme voit tout (shop_id NULL = plateforme).
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_notifications_select ON notifications
    FOR SELECT USING (
        is_platform_admin()
        OR recipient_id = current_user_id()
        OR (shop_id = current_shop_id() AND recipient_id IS NULL)
    );
CREATE POLICY tenant_isolation_notifications_insert ON notifications
    FOR INSERT WITH CHECK (
        is_platform_admin()
        OR shop_id = current_shop_id()
    );
CREATE POLICY tenant_isolation_notifications_update ON notifications
    FOR UPDATE USING (
        is_platform_admin()
        OR recipient_id = current_user_id()
    )
    WITH CHECK (
        is_platform_admin()
        OR recipient_id = current_user_id()
    );
CREATE POLICY tenant_isolation_notifications_delete ON notifications
    FOR DELETE USING (is_platform_admin());

-- audit_logs : INSERT + SELECT uniquement (append-only, vérifié par trigger).
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_audit_logs_select ON audit_logs
    FOR SELECT USING (
        is_platform_admin()
        OR shop_id = current_shop_id()
    );
CREATE POLICY tenant_isolation_audit_logs_insert ON audit_logs
    FOR INSERT WITH CHECK (
        is_platform_admin()
        OR shop_id = current_shop_id()
    );
-- +goose StatementEnd
