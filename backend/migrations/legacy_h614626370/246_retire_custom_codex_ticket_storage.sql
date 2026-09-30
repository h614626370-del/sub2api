-- Retire only the previous custom ticket implementation. Historical migration
-- files remain unchanged so existing installations retain valid checksums.
DROP TABLE IF EXISTS openai_codex_ticket_audits;

UPDATE accounts AS a
SET extra = a.extra - ARRAY(
    SELECT key
    FROM jsonb_object_keys(a.extra) AS key
    WHERE starts_with(key, 'codex_turn_ticket:')
       OR key IN ('codex_harvest_proxy_url', 'codex_ticket_ready_models')
)
WHERE jsonb_typeof(a.extra) = 'object'
  AND EXISTS (
    SELECT 1
    FROM jsonb_object_keys(a.extra) AS key
    WHERE starts_with(key, 'codex_turn_ticket:')
       OR key IN ('codex_harvest_proxy_url', 'codex_ticket_ready_models')
  );

DELETE FROM settings
WHERE key IN ('openai_codex_ticket_policy', 'openai_codex_ticket_harvest_proxy_url');
