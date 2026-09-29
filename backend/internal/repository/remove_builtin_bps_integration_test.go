//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestRemoveBuiltinBPSMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	fixture, err := os.ReadFile("testdata/remove_builtin_bps_fixture.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(fixture))
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("248_remove_builtin_bps.sql")
	require.NoError(t, err)
	checks, err := os.ReadFile("testdata/remove_builtin_bps_checks.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(checks))
		require.NoError(t, err)
	}
}
