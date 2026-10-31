-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS deliveries;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_delivery_zone_id_fkey;
DROP TABLE IF EXISTS delivery_zones;
-- +goose StatementEnd
