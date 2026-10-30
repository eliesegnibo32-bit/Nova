-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 010 : Notifications internes et journaux d'audit
-- ============================================================================

-- ---------------------------------------------------------------------------
-- notifications : Alertes internes destinées aux utilisateurs
-- ---------------------------------------------------------------------------
CREATE TABLE notifications (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id       uuid        REFERENCES shops(id) ON DELETE CASCADE,  -- NULL = notif plateforme
    recipient_id  uuid        REFERENCES users(id) ON DELETE CASCADE,
    type          text        NOT NULL,                    -- ex: 'low_stock', 'new_order', 'payment_due'
    payload       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    read          boolean     NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    read_at       timestamptz
);

COMMENT ON TABLE  notifications IS 'Notifications internes affichées dans le dashboard (stock bas, nouvelle commande, paiement dû...).';
COMMENT ON COLUMN notifications.shop_id IS 'NULL pour les notifications plateforme (admin NOVA).';
COMMENT ON COLUMN notifications.recipient_id IS 'Utilisateur destinataire (NULL pour les notifications broadcast à toute la boutique).';
COMMENT ON COLUMN notifications.type IS 'Type de notification (low_stock, new_order, payment_due, ...).';
COMMENT ON COLUMN notifications.payload IS 'Données contextuelles (ex: {"variant_id":"...","stock":3}).';

CREATE INDEX notifications_recipient_id_read_idx   ON notifications (recipient_id, read) WHERE read = false;
CREATE INDEX notifications_shop_id_created_at_idx  ON notifications (shop_id, created_at DESC) WHERE shop_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- audit_logs : Journal d'audit append-only (conformité + traçabilité)
-- ---------------------------------------------------------------------------
CREATE TABLE audit_logs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id       uuid        REFERENCES shops(id) ON DELETE CASCADE,  -- NULL = action plateforme
    actor_id      uuid        REFERENCES users(id) ON DELETE SET NULL,
    actor_role    text,                                    -- rôle au moment de l'action
    action        text        NOT NULL,                    -- ex: 'order.refund', 'stock.adjust'
    object_type   text        NOT NULL,                    -- ex: 'order', 'product'
    object_id     uuid,                                    -- id de l'objet impacté
    before        jsonb,                                   -- état avant (NULL si création)
    after         jsonb,                                   -- état après (NULL si suppression)
    ip_address    inet,                                    -- IP de l'auteur
    user_agent    text,                                    -- user-agent HTTP
    created_at    timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  audit_logs IS 'Journal d''audit append-only. Traçabilité de toutes les actions sensibles (changement de stock, commande, abonnement, permissions, admin).';
COMMENT ON COLUMN audit_logs.action IS 'Action au format dot.notation (ex: order.refund, stock.adjust, user.role.change).';
COMMENT ON COLUMN audit_logs.before IS 'État avant l''action (JSONB, NULL pour une création).';
COMMENT ON COLUMN audit_logs.after IS 'État après l''action (JSONB, NULL pour une suppression).';
COMMENT ON COLUMN audit_logs.ip_address IS 'Adresse IP de l''auteur (inet).';

CREATE INDEX audit_logs_shop_id_created_at_idx  ON audit_logs (shop_id, created_at DESC) WHERE shop_id IS NOT NULL;
CREATE INDEX audit_logs_actor_id_created_at_idx ON audit_logs (actor_id, created_at);
CREATE INDEX audit_logs_action_created_at_idx   ON audit_logs (action, created_at DESC);
CREATE INDEX audit_logs_object_idx              ON audit_logs (object_type, object_id) WHERE object_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Trigger : empêcher UPDATE et DELETE sur le journal d'audit (append-only)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION nova_enforce_append_only_audit_logs()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF current_setting('app.allow_audit_mutation', true) IS DISTINCT FROM 'true' THEN
        RAISE EXCEPTION 'audit_logs est un journal append-only: UPDATE/DELETE interdits'
            USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER audit_logs_append_only
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW
    EXECUTE FUNCTION nova_enforce_append_only_audit_logs();
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS audit_logs_append_only ON audit_logs;
DROP FUNCTION IF EXISTS nova_enforce_append_only_audit_logs();
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS notifications;
-- +goose StatementEnd
