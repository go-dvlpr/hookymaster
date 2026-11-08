-- +goose Up
-- A deleted hook stays in the table: it is hidden everywhere, but its text
-- and embedding still keep new generations from repeating it. It keeps its
-- number, so the number is never given out again.
ALTER TABLE hooks ADD COLUMN deleted_at TIMESTAMPTZ;

-- The ad name of a deleted hook can be used by another hook.
DROP INDEX hooks_project_ad_name;
CREATE UNIQUE INDEX hooks_project_ad_name ON hooks (project_id, upper(ad_name)) WHERE ad_name IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DELETE FROM hooks WHERE deleted_at IS NOT NULL;
DROP INDEX hooks_project_ad_name;
CREATE UNIQUE INDEX hooks_project_ad_name ON hooks (project_id, upper(ad_name)) WHERE ad_name IS NOT NULL;
ALTER TABLE hooks DROP COLUMN deleted_at;
