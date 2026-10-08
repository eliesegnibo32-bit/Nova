-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS audit_logs_append_only ON audit_logs;
DROP FUNCTION IF EXISTS nova_enforce_append_only_audit_logs();
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS notifications;
-- +goose StatementEnd
