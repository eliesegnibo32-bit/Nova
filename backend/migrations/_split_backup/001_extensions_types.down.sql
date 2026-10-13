-- +goose Down
-- +goose StatementBegin
DROP TYPE IF EXISTS message_status;
DROP TYPE IF EXISTS message_direction;
DROP TYPE IF EXISTS customer_status;
DROP TYPE IF EXISTS stock_movement_type;
DROP TYPE IF EXISTS conversation_state;
DROP TYPE IF EXISTS subscription_status;
DROP TYPE IF EXISTS payment_mode;
DROP TYPE IF EXISTS payment_status;
DROP TYPE IF EXISTS order_status;
DROP TYPE IF EXISTS product_status;
DROP TYPE IF EXISTS user_role;

-- On ne supprime pas pgcrypto/uuid-ossp : d'autres schémas peuvent en dépendre.
-- +goose StatementEnd
