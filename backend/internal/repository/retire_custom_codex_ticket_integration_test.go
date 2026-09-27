//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration246RetiresOnlyLegacyTicketStorage(t *testing.T) {
	data, err := dbmigrations.FS.ReadFile("246_retire_custom_codex_ticket_storage.sql")
	require.NoError(t, err)
	tx := testTx(t)
	ctx := context.Background()
	_, err = tx.ExecContext(ctx, `
		CREATE TEMP TABLE accounts (id BIGINT PRIMARY KEY, extra JSONB) ON COMMIT DROP;
		CREATE TEMP TABLE settings (key TEXT PRIMARY KEY, value TEXT) ON COMMIT DROP;
		CREATE TEMP TABLE openai_codex_ticket_audits (id BIGINT) ON COMMIT DROP;
		CREATE TEMP TABLE codex_ticket_attempts (id BIGINT) ON COMMIT DROP;
		INSERT INTO codex_ticket_attempts VALUES (7);
		INSERT INTO accounts VALUES
			(1, '{"codex_turn_ticket:gpt-6-astra":{"state":"legacy"},"codex_harvest_proxy_url":"legacy","codex_ticket_ready_models":["gpt-6-astra"],"account_timezone_detected":{"timezone":"Asia/Shanghai"},"codex_allow_without_ticket":false,"unrelated":42}'),
			(2, '{"unrelated":43}'), (3, NULL), (4, '{}');
		INSERT INTO settings VALUES
			('openai_codex_ticket_policy', 'legacy'),
			('openai_codex_ticket_harvest_proxy_url', 'legacy'),
			('openai_astra_group_id', '10'),
			('openai_oauth_default_timezone', 'Asia/Shanghai'),
			('openai_codex_ticket_proxy_pool', '{"mode":"all","proxy_ids":[]}');
	`)
	require.NoError(t, err)
	// A repeated execution must not affect unrelated state.
	for range 2 {
		_, err = tx.ExecContext(ctx, string(data))
		require.NoError(t, err)
	}
	var extra string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT extra::text FROM accounts WHERE id=1").Scan(&extra))
	require.JSONEq(t, `{"account_timezone_detected":{"timezone":"Asia/Shanghai"},"codex_allow_without_ticket":false,"unrelated":42}`, extra)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT extra::text FROM accounts WHERE id=2").Scan(&extra))
	require.JSONEq(t, `{"unrelated":43}`, extra)
	var count int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM settings").Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM accounts").Scan(&count))
	require.Equal(t, 4, count)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM codex_ticket_attempts").Scan(&count))
	require.Equal(t, 1, count)
	var removed bool
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT to_regclass('pg_temp.openai_codex_ticket_audits') IS NULL").Scan(&removed))
	require.True(t, removed)
}
