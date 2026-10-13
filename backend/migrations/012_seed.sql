-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 012 : Seed de développement (plan Essentiel + boutique démo + produits)
-- ============================================================================
-- ATTENTION: ce seed est pour l'environnement de DÉVELOPPEMENT uniquement.
-- En production, le plan Essentiel est créé séparément (outils d'admin).
--
-- On positionne les settings RLS pour pouvoir écrire (sinon RLS bloque):
--   app.user_role = 'super_admin'
-- Cela désactive l'isolation par boutique le temps du seed.
-- ============================================================================

SET LOCAL app.user_role = 'super_admin';

-- ---------------------------------------------------------------------------
-- 1) Plan Essentiel (cf. cahier des charges ch. 7)
-- ---------------------------------------------------------------------------
INSERT INTO plans (id, name, price, setup_fee, message_quota, product_limit, employee_limit, features, active)
VALUES (
    '11111111-0000-0000-0000-000000000001',
    'Essentiel',
    10000,                          -- 10 000 FCFA / mois
    20000,                          -- 20 000 FCFA frais d'installation
    1000,                           -- 1000 messages / mois
    100,                            -- 100 produits max
    2,                              -- 2 employés max
    '{"ai_confirmation_mode":"manual","ai_tone":"detendu"}'::jsonb,
    true
)
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2) Boutique démo "Boutique Démo CI"
-- ---------------------------------------------------------------------------
INSERT INTO shops (id, name, slug, phone, whatsapp_number, address, commune, hours, description, categories, accepted_payment_modes, ai_settings, status)
VALUES (
    '22222222-0000-0000-0000-000000000001',
    'Boutique Démo CI',
    'boutique-demo-ci',
    '+2250700000001',
    '+2250700000001',
    'Rue des Jardins, Cocody',
    'Cocody',
    '{"mon":["08:00","18:00"],"tue":["08:00","18:00"],"wed":["08:00","18:00"],"thu":["08:00","18:00"],"fri":["08:00","18:00"],"sat":["09:00","17:00"]}'::jsonb,
    'Boutique démo pour tests NOVA',
    ARRAY['Mode','Cosmétique','Accessoires'],
    ARRAY['cash','orange_money','mtn_momo','wave']::payment_mode[],
    '{"tone":"detendu","confirmation_mode":"manual","language":"fr"}'::jsonb,
    'active'
)
ON CONFLICT (slug) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3) Owner de la boutique démo
--    Mot de passe: "demo1234" — placeholder bcrypt ($2a$10$...).
--    À REMPLACER par un hash Argon2id généré côté application en prod.
-- ---------------------------------------------------------------------------
INSERT INTO users (id, email, phone, password_hash, full_name, role, two_factor_enabled)
VALUES (
    '33333333-0000-0000-0000-000000000001',
    'owner@boutique-demo.ci',
    '+2250700000001',
    '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',  -- placeholder bcrypt("demo1234")
    'Awa Owner',
    'owner',
    false
)
ON CONFLICT (email) DO NOTHING;

-- Rattachement owner -> boutique démo
INSERT INTO shop_members (id, shop_id, user_id, role, permissions)
VALUES (
    '44444444-0000-0000-0000-000000000001',
    '22222222-0000-0000-0000-000000000001',
    '33333333-0000-0000-0000-000000000001',
    'owner',
    '{"orders":{"refund":true,"cancel":true},"stock":{"adjust":true},"catalog":{"manage":true}}'::jsonb
)
ON CONFLICT (shop_id, user_id) DO NOTHING;

-- Abonnement de la boutique démo au plan Essentiel
INSERT INTO subscriptions (id, shop_id, plan_id, status, started_at, next_billing_at)
VALUES (
    '55555555-0000-0000-0000-000000000001',
    '22222222-0000-0000-0000-000000000001',
    '11111111-0000-0000-0000-000000000001',
    'trial',
    now(),
    now() + interval '14 days'
)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 4) Produits démo (catalogue de test)
-- ---------------------------------------------------------------------------
INSERT INTO products (id, shop_id, name, description, category, brand, status)
VALUES
    ('66666666-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
     'T-shirt NOVA', 'T-shirt en coton bio, plusieurs tailles et couleurs.',
     'Mode', 'NOVA Brand', 'published'),
    ('66666666-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001',
     'Sac à main cuir', 'Sac à main en cuir véritable, fait main.',
     'Accessoires', 'Atelier Cocody', 'published'),
    ('66666666-0000-0000-0000-000000000003', '22222222-0000-0000-0000-000000000001',
     'Crème hydratante karité', 'Crème hydratante au beurre de karité, 200ml.',
     'Cosmétique', 'Karité Bio', 'published')
ON CONFLICT DO NOTHING;

-- Variantes (avec SKU unique par boutique)
INSERT INTO product_variants (id, product_id, shop_id, sku, size, color, price, active)
VALUES
    -- T-shirt NOVA: 3 tailles x 2 couleurs
    ('77777777-0000-0000-0000-000000000001', '66666666-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
     'TSHIRT-NOVA-S-BLC', 'S', 'Blanc', 5000, true),
    ('77777777-0000-0000-0000-000000000002', '66666666-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
     'TSHIRT-NOVA-M-BLC', 'M', 'Blanc', 5000, true),
    ('77777777-0000-0000-0000-000000000003', '66666666-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
     'TSHIRT-NOVA-L-BLC', 'L', 'Blanc', 5000, true),
    ('77777777-0000-0000-0000-000000000004', '66666666-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
     'TSHIRT-NOVA-M-BLE', 'M', 'Bleu', 5000, true),
    -- Sac à main: une seule variante
    ('77777777-0000-0000-0000-000000000005', '66666666-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001',
     'SAC-CUIR-UNI', NULL, 'Marron', 25000, true),
    -- Crème karité: une seule variante
    ('77777777-0000-0000-0000-000000000006', '66666666-0000-0000-0000-000000000003', '22222222-0000-0000-0000-000000000001',
     'CREME-KARITE-200', '200ml', NULL, 3500, true)
ON CONFLICT DO NOTHING;

-- Stock initial pour chaque variante (mouvements de type 'receipt')
INSERT INTO inventory (variant_id, shop_id, on_hand, reserved, alert_threshold)
VALUES
    ('77777777-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001', 20, 0, 5),
    ('77777777-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001', 15, 0, 5),
    ('77777777-0000-0000-0000-000000000003', '22222222-0000-0000-0000-000000000001', 3,  0, 5),  -- stock bas démo
    ('77777777-0000-0000-0000-000000000004', '22222222-0000-0000-0000-000000000001', 10, 0, 5),
    ('77777777-0000-0000-0000-000000000005', '22222222-0000-0000-0000-000000000001', 8,  0, 3),
    ('77777777-0000-0000-0000-000000000006', '22222222-0000-0000-0000-000000000001', 30, 0, 5)
ON CONFLICT (variant_id) DO NOTHING;

-- Journal de stock initial (mouvements 'receipt')
INSERT INTO stock_movements (variant_id, shop_id, type, quantity, reason, author_id)
VALUES
    ('77777777-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001', 'receipt', 20, 'Stock initial (seed)', NULL),
    ('77777777-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001', 'receipt', 15, 'Stock initial (seed)', NULL),
    ('77777777-0000-0000-0000-000000000003', '22222222-0000-0000-0000-000000000001', 'receipt',  3, 'Stock initial (seed)', NULL),
    ('77777777-0000-0000-0000-000000000004', '22222222-0000-0000-0000-000000000001', 'receipt', 10, 'Stock initial (seed)', NULL),
    ('77777777-0000-0000-0000-000000000005', '22222222-0000-0000-0000-000000000001', 'receipt',  8, 'Stock initial (seed)', NULL),
    ('77777777-0000-0000-0000-000000000006', '22222222-0000-0000-0000-000000000001', 'receipt', 30, 'Stock initial (seed)', NULL)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- 5) Zones de livraison démo (communes d'Abidjan)
-- ---------------------------------------------------------------------------
INSERT INTO delivery_zones (id, shop_id, name, aliases, fee, estimated_delay, free_from, min_order_amount, active)
VALUES
    ('88888888-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
     'Cocody', ARRAY['Cocody','Angré','Riviera','II Plateaux'], 1000, '2h', 50000, NULL, true),
    ('88888888-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001',
     'Plateau', ARRAY['Plateau','Centre-ville'], 1500, '3h', 50000, 5000, true),
    ('88888888-0000-0000-0000-000000000003', '22222222-0000-0000-0000-000000000001',
     'Yopougon', ARRAY['Yopougon','Sicogi'], 2000, 'Jour J+1', 80000, 10000, true)
ON CONFLICT DO NOTHING;
-- +goose StatementEnd


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
