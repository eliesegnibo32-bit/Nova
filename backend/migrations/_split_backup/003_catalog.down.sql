-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS product_images;
DROP TABLE IF EXISTS product_variants;
DROP TABLE IF EXISTS products;
-- +goose StatementEnd
