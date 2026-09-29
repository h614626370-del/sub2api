DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=-24804 AND extra='[]'::jsonb)
       OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=-24805 AND extra='null'::jsonb) THEN
        RAISE EXCEPTION 'Unrelated non-object JSON was changed';
    END IF;
    IF EXISTS (SELECT 1 FROM accounts WHERE id IN (-24801,-24803))
       OR EXISTS (SELECT 1 FROM groups WHERE id=-24801)
       OR EXISTS (SELECT 1 FROM usage_logs WHERE id=-24801)
       OR EXISTS (SELECT 1 FROM subscription_plans WHERE id=-24801)
       OR EXISTS (SELECT 1 FROM account_groups WHERE account_id=-24801)
       OR EXISTS (SELECT 1 FROM user_platform_quotas WHERE platform='openai_bps')
       OR EXISTS (SELECT 1 FROM composite_model_routes WHERE target_platform='openai_bps')
       OR EXISTS (SELECT 1 FROM channel_monitors WHERE id=-24801)
       OR EXISTS (SELECT 1 FROM channel_monitor_request_templates WHERE id=-24801) THEN
        RAISE EXCEPTION 'Legacy BPS data remains';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE id=-24802
        AND credentials='{"access_token":"normal-fixture"}'::jsonb
        AND extra='{"codex_ticket_harvest_enabled":false,"keep":123,"openai_long_context_billing_enabled":false}'::jsonb)
       OR NOT EXISTS (SELECT 1 FROM usage_logs WHERE id=-24802)
       OR NOT EXISTS (SELECT 1 FROM account_groups WHERE account_id=-24802 AND group_id=-24802)
       OR NOT EXISTS (SELECT 1 FROM api_keys WHERE id=-24802 AND status='active' AND group_id=-24802)
       OR NOT EXISTS (SELECT 1 FROM user_platform_quotas WHERE user_id=-24801 AND platform='openai')
       OR NOT EXISTS (SELECT 1 FROM composite_model_routes WHERE public_model='remove-bps-normal' AND target_platform='openai') THEN
        RAISE EXCEPTION 'Normal OpenAI state changed';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM api_keys WHERE id=-24801 AND status='inactive' AND group_id IS NULL)
       OR NOT EXISTS (SELECT 1 FROM channel_monitors WHERE id=-24802 AND NOT enabled AND template_id IS NULL)
       OR NOT EXISTS (SELECT 1 FROM channel_monitor_v2_config WHERE id=1
           AND platforms='[{"platform":"openai"}]'::jsonb AND group_ids=ARRAY[-24802]::bigint[]) THEN
        RAISE EXCEPTION 'Legacy permissions or monitor configuration were not removed';
    END IF;
    IF (SELECT count(*) FROM pg_constraint WHERE conname IN
        ('user_platform_quotas_platform_check','composite_model_routes_target_platform_check',
         'channel_monitors_provider_check','channel_monitor_request_templates_provider_check')
        AND pg_get_constraintdef(oid) NOT LIKE '%openai_bps%') <> 4 THEN
        RAISE EXCEPTION 'Platform constraints were not restored';
    END IF;
END $$;
