-- +goose Up
-- The parsed file, so ad names can be assigned to hooks after the upload
-- without uploading the file again.
ALTER TABLE imports ADD COLUMN report JSONB NOT NULL DEFAULT '{}';

-- The highest H-number of a deleted hook. New hooks never reuse it, so old
-- ads in Meta never send their results to a new hook.
ALTER TABLE projects ADD COLUMN max_deleted_number INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE projects DROP COLUMN max_deleted_number;
ALTER TABLE imports DROP COLUMN report;
