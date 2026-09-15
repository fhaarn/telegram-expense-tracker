CREATE TABLE category_mappings (
 user_id BIGINT NOT NULL REFERENCES users(id), description_key TEXT NOT NULL,
 category_id BIGINT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(user_id,description_key),
 FOREIGN KEY(user_id,category_id) REFERENCES categories(user_id,id)
);
CREATE TABLE expense_drafts (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id), description TEXT NOT NULL,
 description_key TEXT NOT NULL, amount_minor BIGINT NOT NULL CHECK(amount_minor > 0 AND amount_minor % 100 = 0),
 category_id BIGINT, expense_date DATE NOT NULL,
 version BIGINT NOT NULL DEFAULT 1, status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','confirmed','cancelled','expired')),
 learn BOOLEAN NOT NULL DEFAULT false, mapping_version BIGINT NOT NULL DEFAULT 0,
 target_id BIGINT, target_version BIGINT, result_id BIGINT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '24 hours',
 UNIQUE(user_id,id), FOREIGN KEY(user_id,category_id) REFERENCES categories(user_id,id)
);
CREATE TABLE expenses (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id),
 originating_draft_id BIGINT NOT NULL UNIQUE, description TEXT NOT NULL,
 amount_minor BIGINT NOT NULL CHECK(amount_minor > 0 AND amount_minor % 100 = 0),
 category_id BIGINT NOT NULL, expense_date DATE NOT NULL, version BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 UNIQUE(user_id,id), FOREIGN KEY(user_id,category_id) REFERENCES categories(user_id,id),
 FOREIGN KEY(user_id,originating_draft_id) REFERENCES expense_drafts(user_id,id)
);
ALTER TABLE expense_drafts ADD FOREIGN KEY(user_id,target_id) REFERENCES expenses(user_id,id);
ALTER TABLE expense_drafts ADD FOREIGN KEY(user_id,result_id) REFERENCES expenses(user_id,id);
ALTER TABLE user_interactions DROP CONSTRAINT user_interactions_kind_check;
ALTER TABLE user_interactions ADD CONSTRAINT user_interactions_kind_check CHECK(kind IN ('onboarding','expense'));
ALTER TABLE user_interactions ADD COLUMN draft_id BIGINT;
ALTER TABLE user_interactions ADD COLUMN field TEXT;
ALTER TABLE user_interactions ADD FOREIGN KEY(user_id,draft_id) REFERENCES expense_drafts(user_id,id);
CREATE INDEX expenses_report ON expenses(user_id,expense_date,id) WHERE deleted_at IS NULL;
