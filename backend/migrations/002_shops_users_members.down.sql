-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS shop_members;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS shops;
-- +goose StatementEnd
