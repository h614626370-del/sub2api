-- Synthetic regression data only. No real credentials or external requests.
INSERT INTO users(email,password_hash,balance,frozen_balance)
VALUES ('bridge-fixture@example.invalid','fixture-password-hash',123.456789,12.50);
INSERT INTO groups(name,platform,subscription_type,rate_multiplier)
VALUES ('bridge_fixture_group','openai','subscription',0.75);
INSERT INTO proxies(name,protocol,host,port)
VALUES ('bridge_fixture_proxy','http','127.0.0.1',1);
INSERT INTO accounts(name,platform,type,credentials,extra,proxy_id)
VALUES ('bridge_fixture_account','openai','oauth',
        '{"access_token":"fixture-access","refresh_token":"fixture-refresh","base_url":"https://example.invalid"}',
        '{"keep_extra":{"nested":true},"openai_excel_bps":true,"openai_bps_enabled":true,"codex_turn_ticket:gpt-test":{"ticket":"fixture"},"codex_ticket_ready_models":["gpt-test"],"codex_ticket_harvest_enabled":true,"account_timezone_override":"Asia/Shanghai","account_timezone_detected":{"timezone":"Asia/Shanghai"}}',
        (SELECT id FROM proxies WHERE name='bridge_fixture_proxy'));
INSERT INTO account_groups(account_id,group_id)
SELECT a.id,g.id FROM accounts a,groups g
WHERE a.name='bridge_fixture_account' AND g.name='bridge_fixture_group';
INSERT INTO api_keys(user_id,key,name,group_id)
SELECT u.id,'sk-bridge-fixture-not-a-real-key','bridge_fixture_key',g.id
FROM users u,groups g WHERE u.email='bridge-fixture@example.invalid' AND g.name='bridge_fixture_group';
INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_usage_usd,monthly_usage_usd)
SELECT u.id,g.id,NOW(),NOW()+INTERVAL '45 days',1.23456789,9.87654321
FROM users u,groups g WHERE u.email='bridge-fixture@example.invalid' AND g.name='bridge_fixture_group';
INSERT INTO payment_orders(user_id,amount,pay_amount,status,expires_at)
SELECT id,100.00,103.00,'COMPLETED',NOW()+INTERVAL '1 day'
FROM users WHERE email='bridge-fixture@example.invalid';
INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,subscription_id,request_id,model,input_tokens,output_tokens,total_cost,actual_cost)
SELECT u.id,k.id,a.id,g.id,s.id,'bridge-fixture-request','gpt-test',100,50,0.123456789,0.09259259175
FROM users u,api_keys k,accounts a,groups g,user_subscriptions s
WHERE u.email='bridge-fixture@example.invalid' AND k.name='bridge_fixture_key'
AND a.name='bridge_fixture_account' AND g.name='bridge_fixture_group'
AND s.user_id=u.id AND s.group_id=g.id;
INSERT INTO settings(key,value) VALUES
('openai_codex_ticket_enabled','true'),
('openai_astra_group_id','123'),
('openai_sol_source_group_ids','[123]'),
('openai_oauth_default_timezone','America/Los_Angeles'),
('bridge_unrelated_setting','keep-me')
ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value;
