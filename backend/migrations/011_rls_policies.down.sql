-- +goose Down
-- +goose StatementBegin
-- ============================================================================
-- Rollback des politiques RLS et des fonctions utilitaires
-- ============================================================================

DROP POLICY IF EXISTS tenant_isolation_audit_logs_insert         ON audit_logs;
DROP POLICY IF EXISTS tenant_isolation_audit_logs_select         ON audit_logs;
ALTER TABLE audit_logs DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_notifications_delete      ON notifications;
DROP POLICY IF EXISTS tenant_isolation_notifications_update      ON notifications;
DROP POLICY IF EXISTS tenant_isolation_notifications_insert      ON notifications;
DROP POLICY IF EXISTS tenant_isolation_notifications_select      ON notifications;
ALTER TABLE notifications DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_consents                  ON consents;
ALTER TABLE consents DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_ai_usage_insert           ON ai_usage;
DROP POLICY IF EXISTS tenant_isolation_ai_usage_select           ON ai_usage;
ALTER TABLE ai_usage DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_subscription_payments     ON subscription_payments;
ALTER TABLE subscription_payments DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_subscriptions             ON subscriptions;
ALTER TABLE subscriptions DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_deliveries                ON deliveries;
ALTER TABLE deliveries DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_delivery_zones            ON delivery_zones;
ALTER TABLE delivery_zones DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_order_events_insert       ON order_events;
DROP POLICY IF EXISTS tenant_isolation_order_events_select       ON order_events;
ALTER TABLE order_events DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_order_items               ON order_items;
ALTER TABLE order_items DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_orders                    ON orders;
ALTER TABLE orders DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_cart_items                ON cart_items;
ALTER TABLE cart_items DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_carts                     ON carts;
ALTER TABLE carts DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_messages                  ON messages;
ALTER TABLE messages DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_conversations             ON conversations;
ALTER TABLE conversations DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_customers                 ON customers;
ALTER TABLE customers DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_stock_movements_insert    ON stock_movements;
DROP POLICY IF EXISTS tenant_isolation_stock_movements_select    ON stock_movements;
ALTER TABLE stock_movements DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_inventory                 ON inventory;
ALTER TABLE inventory DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_product_images            ON product_images;
ALTER TABLE product_images DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_product_variants          ON product_variants;
ALTER TABLE product_variants DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_products                  ON products;
ALTER TABLE products DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_shop_members_delete       ON shop_members;
DROP POLICY IF EXISTS tenant_isolation_shop_members_update       ON shop_members;
DROP POLICY IF EXISTS tenant_isolation_shop_members_insert       ON shop_members;
DROP POLICY IF EXISTS tenant_isolation_shop_members_select       ON shop_members;
ALTER TABLE shop_members DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_users_delete              ON users;
DROP POLICY IF EXISTS tenant_isolation_users_update              ON users;
DROP POLICY IF EXISTS tenant_isolation_users_insert              ON users;
DROP POLICY IF EXISTS tenant_isolation_users_select              ON users;
ALTER TABLE users DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_shops_delete              ON shops;
DROP POLICY IF EXISTS tenant_isolation_shops_update              ON shops;
DROP POLICY IF EXISTS tenant_isolation_shops_insert              ON shops;
DROP POLICY IF EXISTS tenant_isolation_shops_select              ON shops;
ALTER TABLE shops DISABLE ROW LEVEL SECURITY;

DROP FUNCTION IF EXISTS is_shop_member_of(uuid, uuid);
DROP FUNCTION IF EXISTS is_platform_admin();
DROP FUNCTION IF EXISTS current_user_id();
DROP FUNCTION IF EXISTS current_shop_id();
-- +goose StatementEnd
