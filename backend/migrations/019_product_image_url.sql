-- +goose Up
-- +goose StatementBegin
-- 019: Add image_url column to products table
-- Allows storing a product photo (R2 URL) directly on the product.
-- ============================================================================
ALTER TABLE products ADD COLUMN IF NOT EXISTS image_url text;
COMMENT ON COLUMN products.image_url IS 'URL de la photo principale du produit (stockée sur Cloudflare R2).';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE products DROP COLUMN IF EXISTS image_url;
-- +goose StatementEnd
