-- +goose Down
-- +goose StatementBegin
-- ============================================================================
-- Rollback du seed de développement
-- ============================================================================
SET LOCAL app.user_role = 'super_admin';

DELETE FROM delivery_zones WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM stock_movements WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM inventory        WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM product_variants WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM products         WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM subscriptions    WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM shop_members     WHERE shop_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM users            WHERE id      = '33333333-0000-0000-0000-000000000001';
DELETE FROM shops            WHERE id      = '22222222-0000-0000-0000-000000000001';
DELETE FROM plans            WHERE id      = '11111111-0000-0000-0000-000000000001';
-- +goose StatementEnd
