package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetireStandaloneBPSMigration(t *testing.T) {
	data, err := FS.ReadFile("247_retire_standalone_bps.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "status = 'inactive', group_id = NULL")
	require.Contains(t, sql, "credentials = '{}'::jsonb")
	require.Contains(t, sql, "deleted_at = COALESCE(deleted_at, NOW())")
	require.Contains(t, sql, "WHERE platform = 'openai_bps'")
	for _, table := range []string{"usage_logs", "audit_logs", "users"} {
		require.NotContains(t, strings.ToLower(sql), "delete from "+table)
		require.NotContains(t, strings.ToLower(sql), "update "+table+" ")
	}
	require.NotContains(t, sql, "platform = 'openai'")
}
