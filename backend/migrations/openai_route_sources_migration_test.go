package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRouteSourcesMigrationSnapshotAndPreservation(t *testing.T) {
	sql, err := FS.ReadFile("242_openai_model_route_sources.sql")
	require.NoError(t, err)
	for _, fragment := range []string{
		"openai_astra_source_group_ids", "openai_sol_source_group_ids",
		"json_agg(id ORDER BY id)", "deleted_at IS NULL", "status = 'active'",
		"platform IN ('openai', 'composite')", "subscription_type <> 'special'",
		"WHERE NOT EXISTS", "ON CONFLICT (key) DO NOTHING", "ELSE '[]'",
	} {
		require.Contains(t, string(sql), fragment)
	}
	require.NotContains(t, strings.ToUpper(string(sql)), "CREATE TABLE")
}
