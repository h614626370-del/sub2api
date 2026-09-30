-- Snapshot existing eligible sources once. New groups never join implicitly.
WITH eligible_sources AS (
    SELECT COALESCE(json_agg(id ORDER BY id), '[]'::json)::text AS ids
    FROM groups
    WHERE deleted_at IS NULL
      AND status = 'active'
      AND platform IN ('openai', 'composite')
      AND subscription_type <> 'special'
), routes(source_key, target_key) AS (
    VALUES ('openai_astra_source_group_ids', 'openai_astra_group_id'),
           ('openai_sol_source_group_ids', 'openai_sol_group_id')
)
INSERT INTO settings (key, value)
SELECT routes.source_key,
       CASE WHEN COALESCE(NULLIF(btrim(target.value), ''), '0')::bigint > 0
            THEN eligible_sources.ids ELSE '[]' END
FROM routes
CROSS JOIN eligible_sources
LEFT JOIN settings AS target ON target.key = routes.target_key
WHERE NOT EXISTS (SELECT 1 FROM settings AS existing WHERE existing.key = routes.source_key)
ON CONFLICT (key) DO NOTHING;
