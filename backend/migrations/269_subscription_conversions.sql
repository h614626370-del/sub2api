-- Preserve lifetime subscription consumption across quota resets and usage-log cleanup.
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS conversion_used_usd NUMERIC(20,10) NOT NULL DEFAULT 0;
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS conversion_usage_complete BOOLEAN;
UPDATE user_subscriptions s SET
    conversion_used_usd = GREATEST(COALESCE((SELECT SUM(u.actual_cost) FROM usage_logs u WHERE u.subscription_id=s.id AND u.billing_type=1 AND u.created_at>=s.starts_at),0),s.daily_usage_usd,s.weekly_usage_usd,s.monthly_usage_usd),
    conversion_usage_complete = NOT EXISTS (SELECT 1 FROM usage_cleanup_tasks c WHERE c.deleted_rows>0 AND c.created_at>=s.starts_at)
        AND COALESCE((SELECT SUM(u.actual_cost) FROM usage_logs u WHERE u.subscription_id=s.id AND u.billing_type=1 AND u.created_at>=s.starts_at),0)+0.000001 >= GREATEST(s.daily_usage_usd,s.weekly_usage_usd,s.monthly_usage_usd)
WHERE s.conversion_usage_complete IS NULL;
ALTER TABLE user_subscriptions ALTER COLUMN conversion_usage_complete SET DEFAULT TRUE;
ALTER TABLE user_subscriptions ALTER COLUMN conversion_usage_complete SET NOT NULL;

-- Both the atomic billing path and legacy billing path increment daily usage.
-- Quota resets never refund lifetime consumption. An expired term renewal starts a new ledger.
CREATE OR REPLACE FUNCTION track_subscription_conversion_usage() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.starts_at IS DISTINCT FROM OLD.starts_at THEN
        NEW.conversion_used_usd := 0;
        NEW.conversion_usage_complete := TRUE;
    ELSE
        NEW.conversion_used_usd := OLD.conversion_used_usd + GREATEST(NEW.daily_usage_usd-OLD.daily_usage_usd,0);
        NEW.conversion_usage_complete := OLD.conversion_usage_complete;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS subscription_conversion_usage ON user_subscriptions;
CREATE TRIGGER subscription_conversion_usage BEFORE UPDATE ON user_subscriptions
    FOR EACH ROW EXECUTE FUNCTION track_subscription_conversion_usage();

CREATE TABLE IF NOT EXISTS subscription_conversions (
    subscription_id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL,
    group_name TEXT NOT NULL,
    amount_usd NUMERIC(20,6) NOT NULL CHECK (amount_usd > 0),
    calculation JSONB NOT NULL,
    converted_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS subscription_conversions_user_time ON subscription_conversions(user_id, converted_at DESC);

-- Every admitted subscription request owns a row until its response and all
-- background billing tasks finish. Rows are never expired automatically: an
-- uncertain settlement must not silently become convertible after a timeout.
CREATE TABLE IF NOT EXISTS subscription_conversion_leases (
    id TEXT PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id),
    user_id BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'unsettled')),
    -- A separate row survives a video-create HTTP response until its later
    -- status/content request has committed the charge.
    deferred_task_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS subscription_conversion_leases_subscription
    ON subscription_conversion_leases(subscription_id, state);
CREATE UNIQUE INDEX IF NOT EXISTS subscription_conversion_leases_deferred_task
    ON subscription_conversion_leases(subscription_id, deferred_task_key)
    WHERE deferred_task_key IS NOT NULL;
