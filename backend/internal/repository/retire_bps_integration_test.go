//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestRetireStandaloneBPSMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	id := func(query string, args ...any) int64 {
		t.Helper()
		var result int64
		require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&result))
		return result
	}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.ExecContext(ctx, query, args...)
		require.NoError(t, err)
	}
	group := id(`INSERT INTO groups(name, platform) VALUES ('retire-bps-test', 'openai_bps') RETURNING id`)
	normalGroup := id(`INSERT INTO groups(name, platform) VALUES ('retire-bps-normal', 'openai') RETURNING id`)
	user := id(`INSERT INTO users(email,password_hash) VALUES ('retire-bps@test.invalid','fixture') RETURNING id`)
	account := id(`INSERT INTO accounts(name,platform,type,credentials) VALUES ('old-bps','openai_bps','oauth','{"access_token":"fixture"}') RETURNING id`)
	normal := id(`INSERT INTO accounts(name,platform,type,credentials) VALUES ('normal','openai','oauth','{"access_token":"normal-fixture"}') RETURNING id`)
	key := id(`INSERT INTO api_keys(user_id,key,name,group_id) VALUES ($1,'retire-bps-fixture','old',$2) RETURNING id`, user, group)
	normalKey := id(`INSERT INTO api_keys(user_id,key,name,group_id) VALUES ($1,'retire-bps-normal-fixture','normal',$2) RETURNING id`, user, normalGroup)
	exec(`INSERT INTO account_groups(account_id,group_id) VALUES ($1,$2),($3,$4)`, account, group, normal, normalGroup)
	usage := id(`INSERT INTO usage_logs(user_id,api_key_id,account_id,model) VALUES ($1,$2,$3,'gpt-6-astra') RETURNING id`, user, key, account)
	script, err := migrations.FS.ReadFile("247_retire_standalone_bps.sql")
	require.NoError(t, err)
	for range 2 {
		exec(string(script))
	}
	var ok bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT status='inactive' AND group_id IS NULL FROM api_keys WHERE id=$1`, key).Scan(&ok))
	require.True(t, ok)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL AND NOT schedulable AND credentials='{}'::jsonb FROM accounts WHERE id=$1`, account).Scan(&ok))
	require.True(t, ok)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT deleted_at IS NULL AND credentials->>'access_token'='normal-fixture' FROM accounts WHERE id=$1`, normal).Scan(&ok))
	require.True(t, ok)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT status='active' AND group_id=$2 FROM api_keys WHERE id=$1`, normalKey, normalGroup).Scan(&ok))
	require.True(t, ok)
	var count int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM usage_logs WHERE id=$1 AND account_id=$2`, usage, account).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM account_groups WHERE account_id=$1`, account).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM account_groups WHERE account_id=$1`, normal).Scan(&count))
	require.Equal(t, 1, count)
}
