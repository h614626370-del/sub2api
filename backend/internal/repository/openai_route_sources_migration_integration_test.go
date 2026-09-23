//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration242OpenAIRouteSources(t *testing.T) {
	for _, tc := range []struct {
		name, target, existing string
	}{
		{"legacy enabled", "20", ""},
		{"disabled", "0", ""},
		{"new install", "", ""},
		{"existing empty", "20", "[]"},
		{"existing list", "20", "[999]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx(t)
			ctx := context.Background()
			migration, err := dbmigrations.FS.ReadFile("242_openai_model_route_sources.sql")
			require.NoError(t, err)
			// Temporary tables isolate the migration from all harness fixtures.
			_, err = tx.ExecContext(ctx, `
CREATE TEMP TABLE settings (key text PRIMARY KEY, value text NOT NULL) ON COMMIT DROP;
CREATE TEMP TABLE groups (id bigint PRIMARY KEY, platform text, status text, subscription_type text, deleted_at timestamptz) ON COMMIT DROP;
INSERT INTO groups VALUES
 (10, 'openai', 'active', 'standard', NULL),
 (11, 'composite', 'active', 'subscription', NULL),
 (12, 'openai', 'disabled', 'standard', NULL),
 (13, 'openai', 'active', 'special', NULL),
 (14, 'anthropic', 'active', 'standard', NULL),
 (15, 'openai', 'active', 'standard', now());`)
			require.NoError(t, err)
			if tc.target != "" {
				_, err = tx.ExecContext(ctx, "INSERT INTO settings VALUES ('openai_astra_group_id', $1)", tc.target)
				require.NoError(t, err)
			}
			if tc.existing != "" {
				_, err = tx.ExecContext(ctx, "INSERT INTO settings VALUES ('openai_astra_source_group_ids', $1)", tc.existing)
				require.NoError(t, err)
			}
			_, err = tx.ExecContext(ctx, string(migration))
			require.NoError(t, err)
			var raw string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='openai_astra_source_group_ids'").Scan(&raw))
			want := "[]"
			if tc.target == "20" {
				want = "[10,11]"
			}
			if tc.existing != "" {
				want = tc.existing
			}
			require.JSONEq(t, want, raw)
			_, err = tx.ExecContext(ctx, "INSERT INTO groups VALUES (99, 'openai', 'active', 'standard', NULL)")
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, string(migration))
			require.NoError(t, err)
			var repeated string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='openai_astra_source_group_ids'").Scan(&repeated))
			require.JSONEq(t, raw, repeated)
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='openai_sol_source_group_ids'").Scan(&raw))
			var ids []int64
			require.NoError(t, json.Unmarshal([]byte(raw), &ids))
			require.Empty(t, ids)
		})
	}
}
