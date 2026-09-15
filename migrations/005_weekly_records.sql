CREATE TABLE weekly_category_records (
 pair_id BIGINT NOT NULL REFERENCES comparison_pairs(id),
 week_start DATE NOT NULL CHECK (extract(isodow FROM week_start)=1),
 category_key TEXT NOT NULL,
 expense_id BIGINT REFERENCES expenses(id),
 amount_minor BIGINT CHECK (amount_minor > 0 AND amount_minor % 100=0),
 evaluated_through DATE NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(pair_id,week_start,category_key),
 CHECK ((expense_id IS NULL) = (amount_minor IS NULL))
);
