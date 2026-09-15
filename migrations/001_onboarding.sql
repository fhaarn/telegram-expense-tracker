CREATE TABLE users (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 telegram_user_id BIGINT NOT NULL UNIQUE CHECK (telegram_user_id > 0),
 telegram_chat_id BIGINT NOT NULL,
 telegram_username TEXT,
 display_name TEXT,
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active')),
 timezone TEXT NOT NULL DEFAULT 'Asia/Jakarta',
 currency TEXT NOT NULL DEFAULT 'IDR' CHECK (currency = 'IDR'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK (status <> 'active' OR (display_name IS NOT NULL AND char_length(display_name) BETWEEN 1 AND 40))
);
CREATE TABLE categories (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 40),
 normalized_name TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(user_id, normalized_name),
 UNIQUE(user_id, id)
);
CREATE TABLE user_interactions (
 user_id BIGINT PRIMARY KEY REFERENCES users(id),
 kind TEXT NOT NULL CHECK (kind = 'onboarding'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE inbound_updates (
 update_id BIGINT PRIMARY KEY,
 user_id BIGINT REFERENCES users(id),
 processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE notification_outbox (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 update_id BIGINT NOT NULL UNIQUE REFERENCES inbound_updates(update_id),
 user_id BIGINT NOT NULL REFERENCES users(id),
 chat_id BIGINT NOT NULL,
 body TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbox_pending ON notification_outbox(available_at) WHERE status IN ('pending','sending');
