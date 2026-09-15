ALTER TABLE notification_outbox DROP CONSTRAINT notification_outbox_update_id_key;
ALTER TABLE notification_outbox ADD CONSTRAINT outbox_update_recipient UNIQUE(update_id,user_id);
ALTER TABLE notification_outbox ADD COLUMN reply_markup JSONB NOT NULL DEFAULT '[]';
ALTER TABLE notification_outbox ADD COLUMN pair_id BIGINT REFERENCES comparison_pairs(id);

ALTER TABLE users ADD COLUMN request_window TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE users ADD COLUMN request_count INTEGER NOT NULL DEFAULT 0;
