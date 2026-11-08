-- +goose Up
-- Who really buys: often not the person in the video. The generator writes
-- hooks for this person.
ALTER TABLE projects ADD COLUMN buyer TEXT NOT NULL DEFAULT '';

-- Hook rate tracks attention more than sales, so it weighs less for new
-- projects. Existing projects keep their weights.
ALTER TABLE projects
    ALTER COLUMN weight_cpa SET DEFAULT 70,
    ALTER COLUMN weight_hook_rate SET DEFAULT 15;

-- +goose Down
ALTER TABLE projects
    ALTER COLUMN weight_cpa SET DEFAULT 60,
    ALTER COLUMN weight_hook_rate SET DEFAULT 25;
ALTER TABLE projects DROP COLUMN buyer;
