-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS consents;
DROP TABLE IF EXISTS message_templates;
DROP TABLE IF EXISTS ai_usage;
-- +goose StatementEnd
