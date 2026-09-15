ALTER TABLE users ADD COLUMN pending_comparison_hash TEXT;
CREATE TABLE comparison_invites (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 creator_id BIGINT NOT NULL REFERENCES users(id),
 token_hash TEXT NOT NULL UNIQUE,
 expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + interval '24 hours',
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','revoked','consumed')),
 consumed_by BIGINT REFERENCES users(id),
 consumed_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX comparison_one_pending ON comparison_invites(creator_id) WHERE state='pending';
CREATE TABLE comparison_pairs (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 connected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 ended_at TIMESTAMPTZ
);
CREATE TABLE active_pair_members (
 user_id BIGINT PRIMARY KEY REFERENCES users(id),
 pair_id BIGINT NOT NULL REFERENCES comparison_pairs(id),
 slot SMALLINT NOT NULL CHECK(slot IN (1,2)),
 UNIQUE(pair_id,slot)
);
