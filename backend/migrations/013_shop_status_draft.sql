-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 013 : Étend la contrainte de shops.status pour permettre 'draft'
-- ============================================================================
-- Le cahier des charges (ch. 4.1) impose qu'une boutique nouvellement créée ne
-- puisse pas être activée tant que: au moins un produit publié, au moins une
-- zone de livraison active, et les horaires sont renseignés. Pour distinguer
-- cet état "en onboarding" de l'état "active" réel, on introduit le statut
-- 'draft'. La boutique est créée en 'draft', puis passe à 'active' après
-- validation des critères d'activation par le endpoint POST /api/shops/{id}/activate.
-- ============================================================================

-- Supprimer l'ancienne contrainte et en créer une nouvelle avec 'draft'.
-- Le nom de la contrainte est auto-généré par PostgreSQL (shops_status_check)
-- d'après la convention `<table>_<column>_check`.
ALTER TABLE shops DROP CONSTRAINT IF EXISTS shops_status_check;

ALTER TABLE shops
    ADD CONSTRAINT shops_status_check
    CHECK (status IN ('draft','active','suspended','terminated'));

-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
-- ============================================================================
-- Rollback: on revient à la contrainte d'origine (sans 'draft').
-- ============================================================================
-- ATTENTION: si des boutiques sont en statut 'draft', cette contrainte échouera.
-- On force d'abord les boutiques 'draft' en 'active' pour ne pas casser l'état.

UPDATE shops SET status = 'active' WHERE status = 'draft';

ALTER TABLE shops DROP CONSTRAINT IF EXISTS shops_status_check;

ALTER TABLE shops
    ADD CONSTRAINT shops_status_check
    CHECK (status IN ('active','suspended','terminated'));
-- +goose StatementEnd
