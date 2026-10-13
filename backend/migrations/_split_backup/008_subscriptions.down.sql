-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS subscription_payments;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS plans;
-- +goose StatementEnd
