-- One-time bridge from h614626370-del to the unmodified ranxi2001 v2.9.4 schema.
-- BACK UP THE DATABASE BEFORE STARTUP. The three retired ticket/audit tables
-- below are intentionally deleted, including partitions and stored tickets.
-- Never rewrite historical checksums or use CASCADE to discard unknown data.
DO $$
BEGIN
    -- The normal runner records this migration after committing its effects.
    -- Also protect later administrator choices from manual re-execution.
    IF EXISTS (SELECT 1 FROM schema_migrations
               WHERE filename = '260z_h614626370_transition.sql') THEN
        RETURN;
    END IF;

    -- Fresh installations and existing ranxi2001 installations are untouched.
    IF NOT EXISTS (
        SELECT 1 FROM schema_migrations WHERE filename IN (
            '239_openai_codex_ticket_audits.sql',
            '239_codex_ticket_attempts.sql',
            '241_openai_astra_routing.sql',
            '242_openai_model_route_sources.sql',
            '246_retire_custom_codex_ticket_storage.sql',
            '248_remove_builtin_bps.sql'
        )
    ) THEN
        RETURN;
    END IF;

    -- Earlier experimental versions could own real subscription/usage rows.
    -- Do not replay the old BPS purge or delete accounts to make an upgrade pass.
    IF EXISTS (SELECT 1 FROM accounts WHERE platform = 'openai_bps')
       OR EXISTS (SELECT 1 FROM groups WHERE platform = 'openai_bps') THEN
        RAISE EXCEPTION 'Legacy openai_bps accounts/groups require a reviewed data conversion before this transition; no legacy business rows have been deleted';
    END IF;

    DROP TABLE IF EXISTS openai_codex_ticket_audits;
    DROP TABLE IF EXISTS codex_ticket_invalidations;
    DROP TABLE IF EXISTS codex_ticket_attempts;
    DROP SEQUENCE IF EXISTS codex_ticket_attempts_id_seq;

    -- Shared names must not reactivate harvesting with incompatible old tickets.
    INSERT INTO settings (key, value, updated_at)
    VALUES ('openai_codex_ticket_enabled', 'false', NOW()),
           ('openai_codex_ticket_fail_closed', 'false', NOW())
    ON CONFLICT (key) DO UPDATE
    SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at;

    DELETE FROM settings WHERE key IN (
        'openai_astra_group_id', 'openai_sol_group_id',
        'openai_astra_source_group_ids', 'openai_sol_source_group_ids',
        'openai_oauth_default_timezone', 'openai_codex_ticket_policy',
        'openai_codex_ticket_prompt_template',
        'openai_codex_ticket_allow_without_ticket',
        'openai_codex_ticket_harvest_proxy_url'
    );

    -- Keep credentials, model mappings, proxies, groups and unrelated extras.
    -- CASE also accepts historical NULL/scalar/array JSON without data loss.
    WITH changed AS (
        UPDATE accounts AS a
        SET extra = a.extra - ARRAY(
            SELECT key FROM jsonb_object_keys(
                CASE WHEN jsonb_typeof(a.extra) = 'object'
                     THEN a.extra ELSE '{}'::jsonb END
            ) AS key
            WHERE starts_with(key, 'codex_turn_ticket:')
               OR starts_with(key, 'openai_bps_')
               OR key IN ('codex_ticket_ready_models', 'codex_harvest_proxy_url',
                          'codex_ticket_harvest_enabled',
                          'account_timezone_detected', 'account_timezone_override')
        ), updated_at = NOW()
        WHERE EXISTS (
            SELECT 1 FROM jsonb_object_keys(
                CASE WHEN jsonb_typeof(a.extra) = 'object'
                     THEN a.extra ELSE '{}'::jsonb END
            ) AS key
            WHERE starts_with(key, 'codex_turn_ticket:')
               OR starts_with(key, 'openai_bps_')
               OR key IN ('codex_ticket_ready_models', 'codex_harvest_proxy_url',
                          'codex_ticket_harvest_enabled',
                          'account_timezone_detected', 'account_timezone_override')
        )
        RETURNING id
    )
    INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
    SELECT 'account_changed', id, NULL, NULL FROM changed;
END $$;
