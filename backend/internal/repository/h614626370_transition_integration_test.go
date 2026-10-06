//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const transitionMigration = "260z_h614626370_transition.sql"

func transitionBaseline(t *testing.T, version string) fs.FS {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations", "legacy_h614626370")
	data, err := os.ReadFile(filepath.Join(dir, version+".json"))
	require.NoError(t, err)
	var manifest struct {
		Migrations map[string]string `json:"migrations"`
	}
	require.NoError(t, json.Unmarshal(data, &manifest))
	require.NotEmpty(t, manifest.Migrations)
	baseline := fstest.MapFS{}
	for name, checksum := range manifest.Migrations {
		content, err := migrations.FS.ReadFile(name)
		if os.IsNotExist(err) {
			content, err = os.ReadFile(filepath.Join(dir, name))
		}
		require.NoError(t, err, name)
		sum := sha256.Sum256([]byte(strings.TrimSpace(string(content))))
		require.Equal(t, checksum, hex.EncodeToString(sum[:]), name)
		baseline[name] = &fstest.MapFile{Data: content}
	}
	return baseline
}

func transitionUpstream(t *testing.T) fs.FS {
	t.Helper()
	upstream := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	for _, name := range names {
		if name == transitionMigration {
			continue
		}
		data, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		upstream[name] = &fstest.MapFile{Data: data}
	}
	return upstream
}

// Use actual historical migration files, the real runner, and independent test
// databases. No production credentials, accounts, or network model calls.
func TestH614626370TransitionUpgrade(t *testing.T) {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, selectDockerImage(ctx, postgresImageTag),
		tcpostgres.WithDatabase("bridge_admin"), tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(ctx)) })
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })

	for i, version := range []string{"v0.2.7.8", "v0.2.10.1", "upstream"} {
		t.Run(version, func(t *testing.T) {
			name := fmt.Sprintf("bridge_case_%d", i)
			_, err := admin.ExecContext(ctx, "CREATE DATABASE "+name)
			require.NoError(t, err)
			u, err := url.Parse(dsn)
			require.NoError(t, err)
			u.Path = "/" + name
			db, err := sql.Open("postgres", u.String())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			var baseline fs.FS = transitionUpstream(t)
			if version != "upstream" {
				baseline = transitionBaseline(t, version)
			}
			require.NoError(t, applyMigrationsFS(ctx, db, baseline))
			fixture, err := os.ReadFile("testdata/h614626370_transition_fixture.sql")
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, string(fixture))
			require.NoError(t, err)
			before := transitionBusinessSnapshot(t, db)
			var oldLedger string
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT jsonb_object_agg(filename, checksum)::text FROM schema_migrations`).Scan(&oldLedger))

			require.NoError(t, ApplyMigrations(ctx, db))
			require.Equal(t, before, transitionBusinessSnapshot(t, db), "business data changed")
			var nonDefaultKeys, nonDefaultOrders int
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT count(*) FROM api_keys WHERE concurrency_limit <> 0`).Scan(&nonDefaultKeys))
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT count(*) FROM payment_orders WHERE bonus_amount <> 0`).Scan(&nonDefaultOrders))
			require.Zero(t, nonDefaultKeys, "new concurrency limits must preserve unrestricted keys")
			require.Zero(t, nonDefaultOrders, "historical orders must not acquire a bonus")
			var preservesLedger bool
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT jsonb_object_agg(filename, checksum) @> $1::jsonb FROM schema_migrations`, oldLedger).Scan(&preservesLedger))
			require.True(t, preservesLedger)
			var enabled, extras string
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT value FROM settings WHERE key='openai_codex_ticket_enabled'`).Scan(&enabled))
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT extra::text FROM accounts WHERE name='bridge_fixture_account'`).Scan(&extras))
			if version == "upstream" {
				require.Equal(t, "true", enabled, "upstream settings must not be reset")
				require.Contains(t, extras, "codex_turn_ticket:gpt-test")
			} else {
				require.Equal(t, "false", enabled)
				require.NotContains(t, extras, "codex_turn_ticket:")
				require.NotContains(t, extras, "account_timezone_")
				require.NotContains(t, extras, "openai_bps_enabled")
				for _, table := range []string{"openai_codex_ticket_audits", "codex_ticket_attempts", "codex_ticket_invalidations", "codex_ticket_attempts_id_seq"} {
					var absent bool
					require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NULL`, table).Scan(&absent))
					require.True(t, absent, table)
				}
			}
			require.Contains(t, extras, "openai_excel_bps")
			require.Contains(t, extras, "keep_extra")
			// Restart must not override administrator choices or regenerate tickets.
			_, err = db.ExecContext(ctx, `UPDATE settings SET value='true' WHERE key='openai_codex_ticket_enabled'`)
			require.NoError(t, err)
			require.NoError(t, ApplyMigrations(ctx, db))
			bridge, err := migrations.FS.ReadFile(transitionMigration)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, string(bridge))
			require.NoError(t, err)
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT value FROM settings WHERE key='openai_codex_ticket_enabled'`).Scan(&enabled))
			require.Equal(t, "true", enabled)

			// Hand off to the exact upstream migration set, without the bridge.
			require.NoError(t, applyMigrationsFS(ctx, db, transitionUpstream(t)))
			require.Equal(t, before, transitionBusinessSnapshot(t, db))
		})
	}
}

func transitionBusinessSnapshot(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	queries := map[string]string{
		"users":         `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'observer_group_ids' ORDER BY id), '[]')::text FROM users t`,
		"groups":        `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'stream_only' ORDER BY id), '[]')::text FROM groups t`,
		"api_keys":      `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'concurrency_limit' ORDER BY id), '[]')::text FROM api_keys t`,
		"subscriptions": `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id), '[]')::text FROM user_subscriptions t`,
		"orders":        `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'bonus_amount' ORDER BY id), '[]')::text FROM payment_orders t`,
		"usage":         `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id), '[]')::text FROM usage_logs t`,
		"credentials":   `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'extra'-'updated_at'-'group_rate_multiplier' ORDER BY id), '[]')::text FROM accounts t`,
		"memberships":   `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'allowed_models' ORDER BY account_id,group_id), '[]')::text FROM account_groups t`,
		"proxies":       `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id), '[]')::text FROM proxies t`,
	}
	result := make(map[string]string, len(queries))
	for key, query := range queries {
		var value string
		require.NoError(t, db.QueryRow(query).Scan(&value), key)
		result[key] = value
	}
	return result
}

func TestH614626370TransitionRefusesUnknownDependency(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE filename=$1`, transitionMigration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(filename,checksum)
		VALUES ('239_openai_codex_ticket_audits.sql','fixture') ON CONFLICT DO NOTHING;
		CREATE TABLE openai_codex_ticket_audits(id bigint);
		CREATE VIEW bridge_unknown_dependency AS SELECT id FROM openai_codex_ticket_audits;`)
	require.NoError(t, err)
	data, err := migrations.FS.ReadFile(transitionMigration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(data))
	require.ErrorContains(t, err, "depend")
}

func TestH614626370TransitionRefusesLegacyBPSData(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE filename=$1`, transitionMigration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(filename,checksum)
		VALUES ('239_openai_codex_ticket_audits.sql','fixture') ON CONFLICT DO NOTHING;
		INSERT INTO accounts(name,platform,type) VALUES ('legacy_bps_fixture','openai_bps','oauth');`)
	require.NoError(t, err)
	data, err := migrations.FS.ReadFile(transitionMigration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(data))
	require.ErrorContains(t, err, "reviewed data conversion")
}
