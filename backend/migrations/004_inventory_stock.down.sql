-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS stock_movements_append_only ON stock_movements;
DROP FUNCTION IF EXISTS nova_enforce_append_only_stock_movements();
DROP TABLE IF EXISTS stock_movements;
DROP TABLE IF EXISTS inventory;
-- +goose StatementEnd
