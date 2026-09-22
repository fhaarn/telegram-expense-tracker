ALTER TABLE users ADD COLUMN access_status TEXT NOT NULL DEFAULT 'allowed' CHECK(access_status IN ('allowed','blocked'));
ALTER TABLE users ADD COLUMN access_updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX users_access_list ON users(access_status,id);
CREATE TABLE admin_access_events (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 previous_status TEXT NOT NULL CHECK(previous_status IN ('allowed','blocked')),
 new_status TEXT NOT NULL CHECK(new_status IN ('allowed','blocked')),
 reason TEXT CHECK(char_length(reason)<=500),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(previous_status<>new_status)
);
