-- +goose Up
-- How much each metric counts in the score that picks references, in
-- percent. The four add up to 100.
ALTER TABLE projects
    ADD COLUMN weight_cpa       INT NOT NULL DEFAULT 60 CHECK (weight_cpa BETWEEN 0 AND 100),
    ADD COLUMN weight_hook_rate INT NOT NULL DEFAULT 25 CHECK (weight_hook_rate BETWEEN 0 AND 100),
    ADD COLUMN weight_purchases INT NOT NULL DEFAULT 10 CHECK (weight_purchases BETWEEN 0 AND 100),
    ADD COLUMN weight_ctr       INT NOT NULL DEFAULT 5 CHECK (weight_ctr BETWEEN 0 AND 100);

-- +goose Down
ALTER TABLE projects
    DROP COLUMN weight_ctr,
    DROP COLUMN weight_purchases,
    DROP COLUMN weight_hook_rate,
    DROP COLUMN weight_cpa;
