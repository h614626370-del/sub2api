-- Retire standalone BPS identities, never move their permissions to OpenAI.
-- Soft-deleted rows preserve historical usage foreign keys; credentials are erased.
UPDATE api_keys SET status = 'inactive', group_id = NULL, updated_at = NOW()
WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');

UPDATE user_subscriptions SET status = 'expired', deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps')
  AND deleted_at IS NULL;

UPDATE subscription_plans SET for_sale = FALSE, updated_at = NOW()
WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
UPDATE redeem_codes SET status = 'disabled'
WHERE status = 'unused' AND group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');

UPDATE composite_model_routes SET enabled = FALSE, deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
WHERE (target_platform = 'openai_bps' OR group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps'))
  AND deleted_at IS NULL;

UPDATE channel_monitors SET enabled = FALSE, api_key_encrypted = '', updated_at = NOW()
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

DELETE FROM account_groups
WHERE account_id IN (SELECT id FROM accounts WHERE platform = 'openai_bps')
   OR group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
DELETE FROM user_allowed_groups WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
DELETE FROM user_group_rate_multipliers WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
DELETE FROM user_platform_quotas WHERE platform = 'openai_bps';
DELETE FROM channel_groups WHERE group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');

UPDATE groups SET fallback_group_id = NULL, updated_at = NOW()
WHERE fallback_group_id IN (SELECT id FROM groups WHERE platform = 'openai_bps');
UPDATE groups SET fallback_group_id_on_invalid_request = NULL, updated_at = NOW()
WHERE fallback_group_id_on_invalid_request IN (SELECT id FROM groups WHERE platform = 'openai_bps');

UPDATE accounts SET credentials = '{}'::jsonb, extra = '{}'::jsonb,
    status = 'inactive', schedulable = FALSE, deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
WHERE platform = 'openai_bps'
  AND (deleted_at IS NULL OR credentials <> '{}'::jsonb OR extra <> '{}'::jsonb);
UPDATE groups SET status = 'inactive', deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
WHERE platform = 'openai_bps' AND deleted_at IS NULL;

INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
SELECT 'account_changed', id, NULL, NULL FROM accounts WHERE platform = 'openai_bps';

INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
SELECT 'group_changed', NULL, id, NULL FROM groups WHERE platform = 'openai_bps';
