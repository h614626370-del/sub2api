-- Remove the unpublished built-in BPS integration. It did not own any tables:
-- its state lived in shared account/group rows, account.extra and CHECK constraints.
-- Do not transfer BPS permissions to another platform or erase normal OAuth credentials.

UPDATE api_keys SET status = 'inactive', group_id = NULL, updated_at = NOW()
WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');

DELETE FROM subscription_plans
WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
DELETE FROM redeem_codes
WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
DELETE FROM composite_model_routes WHERE target_platform = 'openai_bps';
DELETE FROM user_platform_quotas WHERE platform = 'openai_bps';
DELETE FROM ops_error_logs WHERE platform = 'openai_bps';
DELETE FROM ops_metrics_hourly WHERE platform = 'openai_bps';
DELETE FROM ops_metrics_daily WHERE platform = 'openai_bps';
DELETE FROM ops_system_metrics WHERE platform = 'openai_bps';
DELETE FROM ops_system_logs WHERE platform = 'openai_bps';
DELETE FROM channel_account_stats_model_pricing WHERE platform = 'openai_bps';
DELETE FROM channel_model_pricing WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_metrics_1m WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_user_metrics_1m WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_error_metrics_1m WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_latency_histograms_1m WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_metrics_rollup WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_user_metrics_rollup WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_error_metrics_rollup WHERE platform = 'openai_bps';
DELETE FROM channel_monitor_v2_latency_histograms_rollup WHERE platform = 'openai_bps';

DELETE FROM channel_monitors
WHERE provider = 'openai_bps'
   OR account_id IN (SELECT id FROM accounts WHERE platform = 'openai_bps');
UPDATE channel_monitors SET enabled = FALSE, template_id = NULL, updated_at = NOW()
WHERE template_id IN (SELECT id FROM channel_monitor_request_templates WHERE provider = 'openai_bps');
DELETE FROM channel_monitor_request_templates WHERE provider = 'openai_bps';

UPDATE channel_monitor_v2_config SET
    platforms = COALESCE((SELECT jsonb_agg(p) FROM jsonb_array_elements(platforms) AS p
                          WHERE p->>'platform' <> 'openai_bps'), '[]'::jsonb),
    group_ids = ARRAY(SELECT id FROM unnest(group_ids) AS id
                      WHERE id NOT IN (SELECT id FROM groups WHERE platform = 'openai_bps')),
    version = version + 1, updated_at = NOW()
WHERE platforms @> '[{"platform":"openai_bps"}]'::jsonb
   OR group_ids && ARRAY(SELECT id FROM groups WHERE platform = 'openai_bps');

-- Publish invalidations before deleting rows, including soft-deleted legacy rows.
INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
SELECT 'account_changed', id, NULL, NULL FROM accounts
WHERE platform = 'openai_bps' OR EXISTS (
    SELECT 1 FROM jsonb_object_keys(CASE WHEN jsonb_typeof(extra) = 'object' THEN extra ELSE '{}'::jsonb END) AS key WHERE starts_with(key, 'openai_bps_')
);
INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
SELECT 'group_changed', NULL, id, NULL FROM groups WHERE platform = 'openai_bps';

UPDATE accounts AS a SET extra = a.extra - ARRAY(
    SELECT key FROM jsonb_object_keys(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END) AS key WHERE starts_with(key, 'openai_bps_')
), updated_at = NOW()
WHERE jsonb_typeof(a.extra) = 'object' AND EXISTS (
    SELECT 1 FROM jsonb_object_keys(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END) AS key WHERE starts_with(key, 'openai_bps_')
);

-- Shadow accounts cannot survive without their credential parent. All dependent
-- usage and scheduler bindings are removed through the existing foreign keys.
WITH RECURSIVE retired_accounts AS (
    SELECT id FROM accounts WHERE platform = 'openai_bps'
    UNION
    SELECT a.id FROM accounts a JOIN retired_accounts r ON a.parent_account_id = r.id
)
DELETE FROM accounts WHERE id IN (SELECT id FROM retired_accounts);
DELETE FROM groups WHERE platform = 'openai_bps';

ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE user_platform_quotas ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go'));
ALTER TABLE composite_model_routes DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
ALTER TABLE composite_model_routes ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go'));
ALTER TABLE channel_monitors DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go'));
ALTER TABLE channel_monitor_request_templates DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go'));
