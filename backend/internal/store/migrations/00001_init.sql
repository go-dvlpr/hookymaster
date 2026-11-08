-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

-- A project is one ad concept. Its code is the original part of ad names,
-- e.g. O12 in US_O12_H02.
CREATE TABLE projects (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    market      TEXT NOT NULL DEFAULT 'US',
    -- Ranking settings.
    min_impressions BIGINT NOT NULL DEFAULT 3000,
    min_purchases   BIGINT NOT NULL DEFAULT 20,
    max_cpa_cents   BIGINT NOT NULL DEFAULT 5400,
    ref_share_pct   INT NOT NULL DEFAULT 15 CHECK (ref_share_pct BETWEEN 1 AND 100),
    min_refs        INT NOT NULL DEFAULT 3 CHECK (min_refs >= 0),
    max_refs        INT NOT NULL DEFAULT 10 CHECK (max_refs >= min_refs),
    max_anti        INT NOT NULL DEFAULT 5 CHECK (max_anti >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE batches (
    id            BIGSERIAL PRIMARY KEY,
    project_id    BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    requested     INT NOT NULL,
    exploit_share INT NOT NULL,
    explore_types TEXT[] NOT NULL DEFAULT '{}',
    dropped       JSONB NOT NULL DEFAULT '[]',
    model         TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A hook is the opening of an ad. number is the H-number from the ad name;
-- generated candidates get one when selected. A hook created from a Meta
-- export has no text until someone adds it.
CREATE TABLE hooks (
    id             BIGSERIAL PRIMARY KEY,
    project_id     BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    number         INT,
    ad_name        TEXT, -- set when the ad in Meta is named differently
    text           TEXT NOT NULL DEFAULT '',
    on_screen_text TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL CHECK (source IN ('manual', 'file', 'imported', 'generated')),
    status         TEXT NOT NULL CHECK (status IN ('candidate', 'rejected', 'selected', 'exported')),
    hook_type      TEXT,
    pain           TEXT,
    emotion        TEXT,
    mode           TEXT,
    based_on       TEXT,
    rationale      TEXT NOT NULL DEFAULT '',
    batch_id       BIGINT REFERENCES batches (id) ON DELETE SET NULL,
    embedding      vector(1536),
    exported_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX hooks_project_number ON hooks (project_id, number) WHERE number IS NOT NULL;
CREATE UNIQUE INDEX hooks_project_ad_name ON hooks (project_id, upper(ad_name)) WHERE ad_name IS NOT NULL;
CREATE INDEX hooks_project_status ON hooks (project_id, status);

-- An import is one Ads Manager file loaded into one project.
CREATE TABLE imports (
    id                     BIGSERIAL PRIMARY KEY,
    project_id             BIGINT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    file_name              TEXT NOT NULL,
    period_start           DATE NOT NULL,
    period_end             DATE NOT NULL,
    rows_total             INT NOT NULL,
    rows_matched           INT NOT NULL,
    hooks_updated          INT NOT NULL,
    hooks_created          INT NOT NULL,
    skipped_other_projects INT NOT NULL,
    skipped_other_markets  INT NOT NULL,
    skipped_bad_names      INT NOT NULL,
    unrecognized           JSONB NOT NULL DEFAULT '[]', -- ad names no hook matched
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Raw counts only: rates are always derived after aggregation.
CREATE TABLE metric_snapshots (
    id           BIGSERIAL PRIMARY KEY,
    hook_id      BIGINT NOT NULL REFERENCES hooks (id) ON DELETE CASCADE,
    market       TEXT NOT NULL,
    period_start DATE NOT NULL,
    period_end   DATE NOT NULL,
    impressions  BIGINT NOT NULL,
    spend_cents  BIGINT NOT NULL,
    plays_3s     BIGINT NOT NULL,
    link_clicks  BIGINT NOT NULL,
    purchases    BIGINT NOT NULL,
    import_id    BIGINT REFERENCES imports (id) ON DELETE SET NULL,
    UNIQUE (hook_id, market, period_start, period_end)
);

-- +goose Down
DROP TABLE metric_snapshots;
DROP TABLE imports;
DROP TABLE hooks;
DROP TABLE batches;
DROP TABLE projects;
