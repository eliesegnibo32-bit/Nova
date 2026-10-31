-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 014 : Seed des modèles WhatsApp NOVA pré-définis (ch. 6 — Modèles WhatsApp)
-- ============================================================================
-- Ces 5 modèles sont soumis à Meta pour approbation pendant l'onboarding de
-- la plateforme NOVA. Ils couvrent:
--   - confirmation de commande (utility)
--   - suivi de statut de commande (utility)
--   - rappel d'échéance d'abonnement (utility)
--   - relance de prospect (marketing)
--   - relance de panier abandonné (marketing)
--
-- Une fois approuvés par Meta (statut=APPROVED), ils peuvent être envoyés
-- hors fenêtre 24h à un client qui a consenti aux messages marketing (pour
-- les modèles marketing) ou à tout client (pour les utility).
-- ============================================================================

-- Pas de RLS sur message_templates (table globale). On insère en idempotent
-- via ON CONFLICT (name) DO NOTHING — la mise à jour du statut se fait via
-- la route POST /api/shops/{shopId}/whatsapp/templates/sync qui appelle Meta.

INSERT INTO message_templates (name, category, language, status, body, variables)
VALUES
    ('nova_order_confirmation', 'utility', 'fr', 'pending',
     'Votre commande #{{1}} est confirmée. Total: {{2}} FCFA. Paiement: {{3}}.',
     '[{"key":"1","label":"Numéro de commande","type":"text"},{"key":"2","label":"Total FCFA","type":"text"},{"key":"3","label":"Mode de paiement","type":"text"}]'::jsonb),
    ('nova_order_status', 'utility', 'fr', 'pending',
     'Votre commande #{{1}} est maintenant: {{2}}.',
     '[{"key":"1","label":"Numéro de commande","type":"text"},{"key":"2","label":"Nouveau statut","type":"text"}]'::jsonb),
    ('nova_payment_reminder', 'utility', 'fr', 'pending',
     'Bonjour {{1}}, votre abonnement NOVA arrive à échéance le {{2}}. Montant: {{3}} FCFA.',
     '[{"key":"1","label":"Nom du client","type":"text"},{"key":"2","label":"Date d''échéance","type":"text"},{"key":"3","label":"Montant FCFA","type":"text"}]'::jsonb),
    ('nova_prospect_followup', 'marketing', 'fr', 'pending',
     'Bonjour {{1}}, suite à votre intérêt pour {{2}}, souhaitez-vous plus d''informations ?',
     '[{"key":"1","label":"Nom du client","type":"text"},{"key":"2","label":"Produit d''intérêt","type":"text"}]'::jsonb),
    ('nova_abandoned_cart', 'marketing', 'fr', 'pending',
     'Bonjour {{1}}, votre panier vous attend. Souhaitez-vous finaliser votre commande ?',
     '[{"key":"1","label":"Nom du client","type":"text"}]'::jsonb)
ON CONFLICT (name) DO NOTHING;
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
-- ============================================================================
-- Rollback: supprime les modèles NOVA pré-définis.
-- ============================================================================
DELETE FROM message_templates
 WHERE name IN (
    'nova_order_confirmation',
    'nova_order_status',
    'nova_payment_reminder',
    'nova_prospect_followup',
    'nova_abandoned_cart'
 );
-- +goose StatementEnd
