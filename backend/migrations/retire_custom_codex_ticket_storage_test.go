package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetireCustomCodexTicketStorageScope(t *testing.T) {
	data, err := FS.ReadFile("246_retire_custom_codex_ticket_storage.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "DROP TABLE IF EXISTS openai_codex_ticket_audits")
	require.NotContains(t, strings.ToUpper(sql), "CASCADE")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM ACCOUNTS")
	require.NotContains(t, sql, "account_timezone_detected")
	require.NotContains(t, sql, "openai_astra")
	require.NotContains(t, sql, "openai_sol")
	require.NotContains(t, sql, "DROP TABLE IF EXISTS codex_ticket_attempts")
	require.Contains(t, sql, "starts_with(key, 'codex_turn_ticket:')")
	require.Contains(t, sql, "'openai_codex_ticket_policy'")
}
