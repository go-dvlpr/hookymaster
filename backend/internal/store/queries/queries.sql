-- name: CreateProject :one
INSERT INTO projects (code, name, description, buyer)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProjectByCode :one
SELECT * FROM projects WHERE code = $1;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY id;

-- name: UpdateProjectSettings :one
UPDATE projects
SET name = $2, description = $3, min_impressions = $4, min_purchases = $5, max_cpa_cents = $6,
    ref_share_pct = $7, min_refs = $8, max_refs = $9, max_anti = $10, code = $11,
    weight_cpa = $12, weight_hook_rate = $13, weight_purchases = $14, weight_ctr = $15, buyer = $16
WHERE id = $1
RETURNING *;

-- name: CreateHook :one
INSERT INTO hooks (project_id, number, text, on_screen_text, source, status, hook_type, pain, emotion, mode, based_on, rationale, batch_id, exported_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetHook :one
SELECT * FROM hooks WHERE id = $1 AND deleted_at IS NULL;

-- name: GetHookByNumber :one
-- Deleted hooks too: their numbers stay taken. Callers check deleted_at.
SELECT * FROM hooks WHERE project_id = $1 AND number = $2;

-- name: ListProjectHooks :many
SELECT * FROM hooks WHERE project_id = $1 AND deleted_at IS NULL ORDER BY id;

-- name: ListDeletedHookTexts :many
-- Deleted hooks are hidden, but new generations must not repeat them.
SELECT text FROM hooks WHERE project_id = $1 AND deleted_at IS NOT NULL AND text <> '' ORDER BY id;

-- name: MaxHookNumber :one
-- Deleted numbers count too, so they are never given out again.
SELECT GREATEST(p.max_deleted_number, COALESCE(MAX(h.number), 0))::int AS max_number
FROM projects p LEFT JOIN hooks h ON h.project_id = p.id
WHERE p.id = $1
GROUP BY p.id;

-- name: UpdateHookText :one
UPDATE hooks
SET text = $2, on_screen_text = $3, ad_name = $4, hook_type = NULL, pain = NULL, emotion = NULL, embedding = NULL, updated_at = now()
WHERE id = $1 AND number IS NOT NULL AND deleted_at IS NULL
RETURNING *;

-- name: CountHooksWithoutText :many
SELECT project_id, COUNT(*)::int AS missing FROM hooks WHERE number IS NOT NULL AND text = '' AND deleted_at IS NULL GROUP BY project_id;

-- name: SetHookLabels :exec
UPDATE hooks SET hook_type = $2, pain = $3, emotion = $4, updated_at = now() WHERE id = $1;

-- name: SetHookEmbedding :exec
UPDATE hooks SET embedding = $2 WHERE id = $1;

-- name: SelectHook :one
UPDATE hooks
SET status = 'selected', number = $2, text = $3, on_screen_text = $4, updated_at = now()
WHERE id = $1 AND status = 'candidate' AND deleted_at IS NULL
RETURNING *;

-- name: RejectHook :one
UPDATE hooks SET status = 'rejected', updated_at = now()
WHERE id = $1 AND status = 'candidate' AND deleted_at IS NULL
RETURNING *;

-- name: UndoHook :one
UPDATE hooks SET status = 'candidate', number = NULL, updated_at = now()
WHERE id = $1 AND status IN ('selected', 'rejected') AND deleted_at IS NULL
RETURNING *;

-- name: SimilarHooks :many
-- Deleted hooks count: a deleted idea must not come back.
SELECT id, text, (1 - (embedding <=> $2))::float8 AS similarity
FROM hooks
WHERE project_id = $1 AND embedding IS NOT NULL
ORDER BY embedding <=> $2
LIMIT $3;

-- name: CreateBatch :one
INSERT INTO batches (project_id, requested, exploit_share, explore_types, dropped, model)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: LatestBatch :one
SELECT * FROM batches WHERE project_id = $1 ORDER BY id DESC LIMIT 1;

-- name: ListBatchHooks :many
SELECT * FROM hooks WHERE batch_id = $1 AND deleted_at IS NULL ORDER BY id;

-- name: CountOpenCandidates :many
-- Only the latest batch: the Candidates screen shows nothing else.
SELECT h.project_id, COUNT(*)::int AS open
FROM hooks h
WHERE h.status = 'candidate' AND h.deleted_at IS NULL
  AND h.batch_id = (SELECT MAX(b.id) FROM batches b WHERE b.project_id = h.project_id)
GROUP BY h.project_id;

-- name: CreateImport :one
INSERT INTO imports (project_id, file_name, period_start, period_end, rows_total, rows_matched, hooks_updated, hooks_created,
                     skipped_other_projects, skipped_other_markets, skipped_bad_names, unrecognized, report)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: LatestImport :one
SELECT * FROM imports WHERE project_id = $1 ORDER BY id DESC LIMIT 1;

-- name: ReplaceSnapshot :exec
INSERT INTO metric_snapshots (hook_id, market, period_start, period_end, impressions, spend_cents, plays_3s, link_clicks, purchases, import_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (hook_id, market, period_start, period_end) DO UPDATE
SET impressions = EXCLUDED.impressions,
    spend_cents = EXCLUDED.spend_cents,
    plays_3s    = EXCLUDED.plays_3s,
    link_clicks = EXCLUDED.link_clicks,
    purchases   = EXCLUDED.purchases,
    import_id   = EXCLUDED.import_id;

-- name: ProjectHookMetrics :many
SELECT h.id AS hook_id,
       COALESCE(SUM(m.impressions), 0)::bigint AS impressions,
       COALESCE(SUM(m.spend_cents), 0)::bigint AS spend_cents,
       COALESCE(SUM(m.plays_3s), 0)::bigint AS plays_3s,
       COALESCE(SUM(m.link_clicks), 0)::bigint AS link_clicks,
       COALESCE(SUM(m.purchases), 0)::bigint AS purchases
FROM hooks h
JOIN metric_snapshots m ON m.hook_id = h.id AND m.market = $2
WHERE h.project_id = $1 AND h.deleted_at IS NULL
GROUP BY h.id;

-- name: ListQueueHooks :many
SELECT h.*, p.code AS project_code, p.market AS project_market,
       EXISTS (SELECT 1 FROM metric_snapshots m WHERE m.hook_id = h.id) AS has_metrics
FROM hooks h
JOIN projects p ON p.id = h.project_id
WHERE h.status IN ('selected', 'exported') AND h.deleted_at IS NULL
ORDER BY p.code, h.number;

-- name: MarkExported :many
UPDATE hooks SET status = 'exported', exported_at = now(), updated_at = now()
WHERE id = ANY(@ids::bigint[]) AND status = 'selected' AND deleted_at IS NULL
RETURNING id;

-- name: CountSelected :one
-- Same rule as the Ready tab of the queue: a hook with results is live.
SELECT COUNT(*)::int FROM hooks h
WHERE h.status = 'selected' AND h.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM metric_snapshots m WHERE m.hook_id = h.id);

-- name: ListHooksByIDs :many
SELECT h.*, p.code AS project_code, p.market AS project_market
FROM hooks h
JOIN projects p ON p.id = h.project_id
WHERE h.id = ANY(@ids::bigint[]) AND h.deleted_at IS NULL
ORDER BY p.code, h.number;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: SetHookAdName :exec
UPDATE hooks SET ad_name = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: DeleteHook :one
-- Soft delete: the hook is hidden but still filters new generations.
UPDATE hooks SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING project_id, number;

-- name: RememberDeletedNumber :exec
UPDATE projects SET max_deleted_number = GREATEST(max_deleted_number, $2) WHERE id = $1;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1;
