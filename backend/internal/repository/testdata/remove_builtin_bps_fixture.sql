-- Recreate the relevant legacy state using synthetic data only.
ALTER TABLE user_platform_quotas DROP CONSTRAINT user_platform_quotas_platform_check;
ALTER TABLE composite_model_routes DROP CONSTRAINT composite_model_routes_target_platform_check;
ALTER TABLE channel_monitors DROP CONSTRAINT channel_monitors_provider_check;
ALTER TABLE channel_monitor_request_templates DROP CONSTRAINT channel_monitor_request_templates_provider_check;
INSERT INTO users (id, email, password_hash) VALUES (-24801, 'remove-bps@test.invalid', 'fixture');
INSERT INTO groups (id, name, platform) VALUES
    (-24801, 'remove-bps-legacy', 'openai_bps'), (-24802, 'remove-bps-normal', 'openai');
INSERT INTO accounts (id, name, platform, type, credentials, extra) VALUES
    (-24801, 'remove-bps-legacy', 'openai_bps', 'oauth', '{"access_token":"fixture"}', '{}'),
    (-24802, 'remove-bps-normal', 'openai', 'oauth', '{"access_token":"normal-fixture"}',
     '{"openai_bps_enabled":true,"openai_bps_models":{"gpt-test":true},"openai_bps_credential_state":{},"codex_ticket_harvest_enabled":false,"keep":123}');
INSERT INTO accounts (id, name, platform, type, credentials, parent_account_id, quota_dimension)
VALUES (-24803, 'remove-bps-shadow', 'openai', 'oauth', '{}', -24801, 'spark');
INSERT INTO api_keys (id, user_id, key, name, group_id) VALUES
    (-24801, -24801, 'remove-bps-legacy-fixture', 'legacy', -24801),
    (-24802, -24801, 'remove-bps-normal-fixture', 'normal', -24802);
INSERT INTO account_groups (account_id, group_id) VALUES (-24801, -24801), (-24802, -24802);
INSERT INTO usage_logs (id, user_id, api_key_id, account_id, model) VALUES
    (-24801, -24801, -24801, -24801, 'gpt-test'), (-24802, -24801, -24802, -24802, 'gpt-test');
INSERT INTO subscription_plans (id, group_id, name, price) VALUES (-24801, -24801, 'legacy', 1);
INSERT INTO user_platform_quotas (user_id, platform) VALUES (-24801, 'openai_bps'), (-24801, 'openai');
INSERT INTO composite_model_routes (group_id, public_model, target_platform) VALUES
    (-24802, 'remove-bps-legacy', 'openai_bps'), (-24802, 'remove-bps-normal', 'openai');
INSERT INTO channel_monitor_request_templates (id, name, provider) VALUES (-24801, 'legacy', 'openai_bps');
INSERT INTO channel_monitors (id, name, provider, endpoint, api_key_encrypted, primary_model, interval_seconds, created_by, template_id) VALUES
    (-24801, 'legacy', 'openai_bps', 'https://example.invalid', 'fixture', 'gpt-test', 60, -24801, -24801),
    (-24802, 'normal', 'openai', 'https://example.invalid', 'fixture', 'gpt-test', 60, -24801, -24801);
INSERT INTO channel_monitor_v2_config (id, platforms, group_ids)
VALUES (1, '[{"platform":"openai_bps"},{"platform":"openai"}]', ARRAY[-24801,-24802])
ON CONFLICT (id) DO UPDATE SET platforms = EXCLUDED.platforms, group_ids = EXCLUDED.group_ids;

-- Old imports can contain non-object JSON; unrelated rows must not break startup.
INSERT INTO accounts (id, name, platform, type, credentials, extra) VALUES
    (-24804, 'remove-bps-array-extra', 'anthropic', 'oauth', '{}', '[]'),
    (-24805, 'remove-bps-null-extra', 'anthropic', 'oauth', '{}', 'null');
