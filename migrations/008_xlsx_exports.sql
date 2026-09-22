ALTER TABLE notification_outbox ADD COLUMN export_payload JSONB;
ALTER TABLE users ADD COLUMN last_export_at TIMESTAMPTZ;
CREATE INDEX outbox_exports ON notification_outbox(user_id) WHERE export_payload IS NOT NULL AND status IN ('pending','sending');
