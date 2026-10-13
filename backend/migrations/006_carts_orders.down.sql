-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS order_events;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS cart_items;
DROP TABLE IF EXISTS carts;
-- +goose StatementEnd
