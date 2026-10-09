CREATE TABLE expense_batches (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','confirmed','cancelled','expired')),
 stage TEXT NOT NULL DEFAULT 'categories' CHECK(stage IN ('categories','date','review','edit','description','amount','category')),
 item_index INTEGER NOT NULL DEFAULT 0 CHECK(item_index BETWEEN 0 AND 19),
 version BIGINT NOT NULL DEFAULT 1,
 expense_date DATE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '24 hours',
 UNIQUE(user_id,id)
);
CREATE UNIQUE INDEX one_pending_batch ON expense_batches(user_id) WHERE status='pending';
-- Reuse draft validation, mapping metadata and the existing expense provenance FK.
CREATE TABLE expense_batch_items (
 batch_id BIGINT NOT NULL,
 user_id BIGINT NOT NULL,
 position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 19),
 draft_id BIGINT NOT NULL UNIQUE,
 PRIMARY KEY(batch_id,position),
 FOREIGN KEY(user_id,batch_id) REFERENCES expense_batches(user_id,id),
 FOREIGN KEY(user_id,draft_id) REFERENCES expense_drafts(user_id,id)
);
CREATE TABLE admin_broadcasts (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 idempotency_key TEXT NOT NULL UNIQUE,
 message TEXT NOT NULL CHECK(char_length(message) BETWEEN 1 AND 4000),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE admin_broadcast_recipients (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 broadcast_id BIGINT NOT NULL REFERENCES admin_broadcasts(id),
 user_id BIGINT NOT NULL REFERENCES users(id),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','failed','skipped')),
 attempts INTEGER NOT NULL DEFAULT 0,
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_until TIMESTAMPTZ,
 failure_code TEXT,
 UNIQUE(broadcast_id,user_id)
);
CREATE INDEX broadcast_pending ON admin_broadcast_recipients(available_at,id) WHERE status IN ('pending','sending');
-- Durable pacing and Telegram rate-limit cooldown, shared by worker instances.
CREATE TABLE broadcast_delivery_state (
 id INTEGER PRIMARY KEY CHECK(id=1),
 available_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO broadcast_delivery_state(id) VALUES(1);
