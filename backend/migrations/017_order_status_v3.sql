-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 017 : Order status v3 — nouvelle machine à états (NOVA v3)
-- ============================================================================
-- NOUVEAUX STATUTS (additifs — on garde les anciens pour la rétrocompatibilité):
--   en_attente_confirmation  — client a confirmé + infos collectées, stock RÉSERVÉ
--   en_attente_paiement      — commerçant a confirmé, en attente de paiement
--   paiement_signalé         — client dit "j'ai payé", commerçant doit vérifier
--   en_cours                 — stock DÉFINITIVEMENT déduit + CA compté
--   prete                    — prête pour récupération/livraison
--   terminee                 — terminée
--   refusee                  — commerçant a refusé — stock libéré
--   annulee                  — annulée (même après en_cours — stock réinstauré, CA retiré)
--
-- CRITICAL: le stock est définitivement déduit UNIQUEMENT quand status → en_cours.
-- Le CA est compté UNIQUEMENT quand status = en_cours ou plus tard.
-- Si une commande en_cours est annulée (→ annulee), le stock est réinstauré et
-- le CA retiré des stats.
--
-- On ne supprime PAS les anciennes valeurs de l'enum (draft, pending, etc.) —
-- les anciennes commandes continuent de fonctionner. Les nouvelles commandes
-- utilisent les nouveaux statuts.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- Ajouter les nouveaux statuts à l'enum order_status
-- ---------------------------------------------------------------------------
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'en_attente_confirmation';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'en_attente_paiement';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'paiement_signale';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'en_cours';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'prete';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'terminee';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'refusee';
ALTER TYPE order_status ADD VALUE IF NOT EXISTS 'annulee';

-- ---------------------------------------------------------------------------
-- Ajouter stock_mode à inventory (quantite | epuise | illimite, défaut quantite)
-- ---------------------------------------------------------------------------
ALTER TABLE inventory
    ADD COLUMN IF NOT EXISTS stock_mode stock_mode NOT NULL DEFAULT 'quantite';

COMMENT ON COLUMN inventory.stock_mode IS 'quantite (stock géré) | epuise (rupture explicite) | illimite (jamais décrémenté). Défaut: quantite.';

-- Quand stock_mode = illimite, on_hand reste libre (mais on l'affiche comme "∞").
-- Quand stock_mode = epuise, l'IA ne propose pas la variante.

-- ---------------------------------------------------------------------------
-- Ajouter payment_deadline à orders (pour suivre le délai de paiement)
-- ---------------------------------------------------------------------------
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS payment_deadline timestamptz;

COMMENT ON COLUMN orders.payment_deadline IS 'Date limite de paiement (now + delay_minutes). Si dépassée, la commande est annulée et le stock libéré.';

-- ---------------------------------------------------------------------------
-- Ajouter revenue_counted à orders (false par défaut, true quand en_cours)
-- ---------------------------------------------------------------------------
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS revenue_counted boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN orders.revenue_counted IS 'true si le CA de cette commande est compté (status >= en_cours). false si annulée/refusée ou avant en_cours.';

-- ---------------------------------------------------------------------------
-- Index pour le cron job (commandes dont le deadline de paiement est passé)
-- Note: on ne peut pas utiliser WHERE status = 'en_attente_paiement' ici car
-- la nouvelle valeur d'enum n'est pas visible dans la même transaction.
-- L'index est créé sans filtre — le cron filtrera en Go.
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS orders_payment_deadline_idx
    ON orders (payment_deadline)
    WHERE payment_deadline IS NOT NULL;

-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
-- ============================================================================
-- Rollback : retirer les colonnes ajoutées (les nouveaux statuts restent dans
-- l'enum — on ne peut pas retirer une valeur d'un enum PostgreSQL sans recréer
-- le type, ce qui casserait les commandes existantes).
-- ============================================================================

DROP INDEX IF EXISTS orders_payment_deadline_idx;

ALTER TABLE orders DROP COLUMN IF EXISTS revenue_counted;
ALTER TABLE orders DROP COLUMN IF EXISTS payment_deadline;

ALTER TABLE inventory DROP COLUMN IF EXISTS stock_mode;

-- Note: les valeurs d'enum ajoutées ne peuvent PAS être retirées sans DROP+RECREATE
-- du type. On les laisse pour ne pas casser les commandes existantes.
-- +goose StatementEnd
