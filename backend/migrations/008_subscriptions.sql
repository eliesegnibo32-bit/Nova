-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 008 : Plans, abonnements et paiements d'abonnement (SaaS billing)
-- ============================================================================
-- Les plans sont globaux (plateforme). Les subscriptions sont par boutique.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- plans : Offres d'abonnement NOVA (gérés par la plateforme)
-- ---------------------------------------------------------------------------
CREATE TABLE plans (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text        NOT NULL UNIQUE,
    price           bigint      NOT NULL CHECK (price >= 0),   -- mensuel FCFA
    setup_fee       bigint      NOT NULL DEFAULT 0 CHECK (setup_fee >= 0),
    message_quota   integer     NOT NULL CHECK (message_quota >= 0),
    product_limit   integer     CHECK (product_limit IS NULL OR product_limit >= 0),
    employee_limit  integer     CHECK (employee_limit IS NULL OR employee_limit >= 0),
    features        jsonb       NOT NULL DEFAULT '{}'::jsonb,  -- feature flags / limites additionnelles
    active          boolean     NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  plans IS 'Offres d''abonnement NOVA (Essentiel, Pro, ...). Gérés par la plateforme, pas par boutique.';
COMMENT ON COLUMN plans.price IS 'Prix mensuel en FCFA.';
COMMENT ON COLUMN plans.setup_fee IS 'Frais d''installation uniques (FCFA).';
COMMENT ON COLUMN plans.message_quota IS 'Quota mensuel de messages WhatsApp.';
COMMENT ON COLUMN plans.product_limit IS 'Nombre maximum de produits (NULL = illimité).';
COMMENT ON COLUMN plans.employee_limit IS 'Nombre maximum d''employés (NULL = illimité).';
COMMENT ON COLUMN plans.features IS 'Feature flags et limites additionnelles (JSONB).';

CREATE INDEX plans_active_idx ON plans (active) WHERE active = true;

-- ---------------------------------------------------------------------------
-- subscriptions : Abonnement d'une boutique à un plan
-- ---------------------------------------------------------------------------
CREATE TABLE subscriptions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id         uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    plan_id         uuid        NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    status          subscription_status NOT NULL DEFAULT 'trial',
    started_at      timestamptz NOT NULL DEFAULT now(),
    next_billing_at timestamptz,                              -- prochaine échéance
    grace_until     timestamptz,                              -- période de grâce
    suspended_at    timestamptz,                              -- suspension
    terminated_at   timestamptz,                              -- résiliation
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE  subscriptions IS 'Abonnement d''une boutique à un plan. Cycle: trial -> active -> (late -> grace_period -> suspended | active) -> terminated.';
COMMENT ON COLUMN subscriptions.next_billing_at IS 'Prochaine échéance de paiement (mensuelle).';
COMMENT ON COLUMN subscriptions.grace_until IS 'Fin de période de grâce (paiement en retard toléré).';
COMMENT ON COLUMN subscriptions.suspended_at IS 'Date de suspension (service coupé, données conservées).';
COMMENT ON COLUMN subscriptions.terminated_at IS 'Date de résiliation (définitive).';

CREATE INDEX subscriptions_shop_id_idx                  ON subscriptions (shop_id);
CREATE INDEX subscriptions_status_next_billing_at_idx   ON subscriptions (status, next_billing_at);

-- ---------------------------------------------------------------------------
-- subscription_payments : Paiements d'abonnement reçus
-- ---------------------------------------------------------------------------
CREATE TABLE subscription_payments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid        NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    shop_id         uuid        NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    amount          bigint      NOT NULL CHECK (amount >= 0),  -- FCFA
    mode            payment_mode NOT NULL,
    reference       text,                                     -- référence paiement (mobile money)
    period_start    date        NOT NULL,                     -- début de période couverte
    period_end      date        NOT NULL,                     -- fin de période couverte
    recorded_by     uuid        REFERENCES users(id) ON DELETE SET NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (period_end >= period_start)
);

COMMENT ON TABLE  subscription_payments IS 'Paiements d''abonnement reçus (mensuels). Historique comptable pour la plateforme.';
COMMENT ON COLUMN subscription_payments.amount IS 'Montant payé en FCFA.';
COMMENT ON COLUMN subscription_payments.mode IS 'Mode de paiement (mobile_money, cash, ...).';
COMMENT ON COLUMN subscription_payments.reference IS 'Référence transaction (ID mobile money, numéro de reçu).';
COMMENT ON COLUMN subscription_payments.period_start IS 'Début de période couverte par le paiement.';
COMMENT ON COLUMN subscription_payments.period_end IS 'Fin de période couverte par le paiement.';

CREATE INDEX subscription_payments_subscription_id_idx   ON subscription_payments (subscription_id);
CREATE INDEX subscription_payments_shop_id_recorded_idx  ON subscription_payments (shop_id, recorded_at);
-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS subscription_payments;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS plans;
-- +goose StatementEnd
