//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const groupModelCompatibilityMigration = "234z_group_model_allowlist_compatibility.sql"

type groupModelCompatibilityFixture struct {
	schema string
	table  string
}

func newGroupModelCompatibilityFixture(t *testing.T, columns string) groupModelCompatibilityFixture {
	t.Helper()
	schema := pq.QuoteIdentifier("group_model_compat_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	_, err := integrationDB.ExecContext(context.Background(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, err)
	})
	table := schema + ".groups"
	_, err = integrationDB.ExecContext(context.Background(), "CREATE TABLE "+table+" (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL DEFAULT ''"+columns+")")
	require.NoError(t, err)
	return groupModelCompatibilityFixture{schema: schema, table: table}
}

func applyGroupModelCompatibilityFixture(fixture groupModelCompatibilityFixture, withUpstream bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := integrationDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SET LOCAL search_path TO "+fixture.schema); err != nil {
		return err
	}
	names := []string{groupModelCompatibilityMigration}
	if withUpstream {
		names = append(names, "235_group_model_allowlist.sql", "236_group_model_allowlist_repair.sql")
	}
	for _, name := range names {
		body, readErr := migrations.FS.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return tx.Commit()
}

func assertGroupModelCompatibilityValue(t *testing.T, fixture groupModelCompatibilityFixture, id int64, expected string) {
	t.Helper()
	var legacy, current string
	err := integrationDB.QueryRowContext(context.Background(), "SELECT models_list_config::text, model_allowlist::text FROM "+fixture.table+" WHERE id=$1", id).Scan(&legacy, &current)
	require.NoError(t, err)
	require.JSONEq(t, expected, legacy, "old-image query")
	require.JSONEq(t, expected, current, "new-image query")
}

func TestGroupModelAllowlistCompatibility_LegacyUpgradeReplayAndBothWriters(t *testing.T) {
	ctx := context.Background()
	fixture := newGroupModelCompatibilityFixture(t, ", models_list_config JSONB DEFAULT '{}'::jsonb")
	const oldConfig = `{"enabled":true,"models":["gpt-old"]}`
	const newConfig = `{"enabled":true,"models":["gpt-new"]}`
	_, err := integrationDB.ExecContext(ctx, "INSERT INTO "+fixture.table+" (name,models_list_config) VALUES ('preserve',$1::jsonb),('null',NULL)", oldConfig)
	require.NoError(t, err)
	require.NoError(t, applyGroupModelCompatibilityFixture(fixture, true))
	assertGroupModelCompatibilityValue(t, fixture, 1, oldConfig)
	assertGroupModelCompatibilityValue(t, fixture, 2, `{}`)

	for _, column := range []string{"models_list_config", "model_allowlist"} {
		t.Run(column, func(t *testing.T) {
			var id int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, "INSERT INTO "+fixture.table+" ("+column+") VALUES ($1::jsonb) RETURNING id", oldConfig).Scan(&id))
			assertGroupModelCompatibilityValue(t, fixture, id, oldConfig)
			_, err := integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET "+column+"=$1::jsonb WHERE id=$2", newConfig, id)
			require.NoError(t, err)
			assertGroupModelCompatibilityValue(t, fixture, id, newConfig)
			_, err = integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET "+column+"='{}'::jsonb WHERE id=$1", id)
			require.NoError(t, err)
			assertGroupModelCompatibilityValue(t, fixture, id, `{}`)
			_, err = integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET "+column+"=$1::jsonb WHERE id=$2", newConfig, id)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET "+column+"=NULL WHERE id=$1", id)
			require.NoError(t, err)
			assertGroupModelCompatibilityValue(t, fixture, id, `{}`)
			var nullID int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, "INSERT INTO "+fixture.table+" ("+column+") VALUES (NULL) RETURNING id").Scan(&nullID))
			assertGroupModelCompatibilityValue(t, fixture, nullID, `{}`)
		})
	}
	var defaultID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "INSERT INTO "+fixture.table+" DEFAULT VALUES RETURNING id").Scan(&defaultID))
	assertGroupModelCompatibilityValue(t, fixture, defaultID, `{}`)
	_, err = integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET name='unrelated edit' WHERE id=1")
	require.NoError(t, err)
	assertGroupModelCompatibilityValue(t, fixture, 1, oldConfig)

	_, err = integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET models_list_config=$1::jsonb,model_allowlist=$1::jsonb WHERE id=1", newConfig)
	require.NoError(t, err)
	assertGroupModelCompatibilityValue(t, fixture, 1, newConfig)
	_, err = integrationDB.ExecContext(ctx, "UPDATE "+fixture.table+" SET models_list_config='{}'::jsonb,model_allowlist=$1::jsonb WHERE id=1", oldConfig)
	require.ErrorContains(t, err, "conflicting legacy and current group model configurations")
	assertGroupModelCompatibilityValue(t, fixture, 1, newConfig)
	_, err = integrationDB.ExecContext(ctx, "INSERT INTO "+fixture.table+" (models_list_config,model_allowlist) VALUES ($1::jsonb,$2::jsonb)", oldConfig, newConfig)
	require.ErrorContains(t, err, "conflicting legacy and current group model configurations")

	// Replaying exact SQL, including the upstream repair, neither erases current
	// configuration nor removes the column selected by the rollback binary.
	require.NoError(t, applyGroupModelCompatibilityFixture(fixture, true))
	assertGroupModelCompatibilityValue(t, fixture, 1, newConfig)
}

func TestGroupModelAllowlistCompatibility_AlreadyUpgradedAndPartialSchemas(t *testing.T) {
	const config = `{"enabled":true,"models":["gpt-preserved"]}`
	for _, test := range []struct {
		name, columns, insert, expected string
	}{
		{"upstream renamed", ", model_allowlist JSONB DEFAULT '{}'::jsonb", "(model_allowlist) VALUES ('" + config + "'::jsonb)", config},
		{"both current populated", ", models_list_config JSONB DEFAULT '{}'::jsonb, model_allowlist JSONB DEFAULT '{}'::jsonb", "(model_allowlist) VALUES ('" + config + "'::jsonb)", config},
		{"both legacy populated", ", models_list_config JSONB DEFAULT '{}'::jsonb, model_allowlist JSONB DEFAULT '{}'::jsonb", "(models_list_config) VALUES ('" + config + "'::jsonb)", config},
		{"both null", ", models_list_config JSONB, model_allowlist JSONB", "DEFAULT VALUES", `{}`},
		{"neither", "", "DEFAULT VALUES", `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newGroupModelCompatibilityFixture(t, test.columns)
			_, err := integrationDB.ExecContext(context.Background(), "INSERT INTO "+fixture.table+" "+test.insert)
			require.NoError(t, err)
			require.NoError(t, applyGroupModelCompatibilityFixture(fixture, true))
			assertGroupModelCompatibilityValue(t, fixture, 1, test.expected)
			require.NoError(t, applyGroupModelCompatibilityFixture(fixture, true))
			assertGroupModelCompatibilityValue(t, fixture, 1, test.expected)
		})
	}
}

func TestGroupModelAllowlistCompatibility_ConflictingExistingValuesRollBack(t *testing.T) {
	fixture := newGroupModelCompatibilityFixture(t, ", models_list_config JSONB, model_allowlist JSONB")
	const oldConfig = `{"models":["old"]}`
	const newConfig = `{"models":["new"]}`
	_, err := integrationDB.ExecContext(context.Background(), "INSERT INTO "+fixture.table+" (models_list_config,model_allowlist) VALUES ($1::jsonb,$2::jsonb)", oldConfig, newConfig)
	require.NoError(t, err)
	require.ErrorContains(t, applyGroupModelCompatibilityFixture(fixture, true), "conflicting legacy and current group model configurations")
	var legacy, current string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT models_list_config::text,model_allowlist::text FROM "+fixture.table+" WHERE id=1").Scan(&legacy, &current))
	require.JSONEq(t, oldConfig, legacy)
	require.JSONEq(t, newConfig, current)
}

func TestGroupModelAllowlistCompatibility_ConcurrentOldAndNewWritersSerialize(t *testing.T) {
	fixture := newGroupModelCompatibilityFixture(t, ", models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.NoError(t, applyGroupModelCompatibilityFixture(fixture, true))
	_, err := integrationDB.ExecContext(context.Background(), "INSERT INTO "+fixture.table+" DEFAULT VALUES")
	require.NoError(t, err)
	for _, firstColumn := range []string{"models_list_config", "model_allowlist"} {
		t.Run(firstColumn, func(t *testing.T) {
			secondColumn := "model_allowlist"
			if firstColumn == secondColumn {
				secondColumn = "models_list_config"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			first, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			require.NoError(t, err)
			defer func() { _ = first.Rollback() }()
			_, err = first.ExecContext(ctx, "UPDATE "+fixture.table+" SET "+firstColumn+"=$1::jsonb WHERE id=1", `{"models":["first"]}`)
			require.NoError(t, err)
			second, err := integrationDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			require.NoError(t, err)
			defer func() { _ = second.Rollback() }()
			var secondPID int
			require.NoError(t, second.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&secondPID))
			completed := make(chan error, 1)
			go func() {
				_, updateErr := second.ExecContext(ctx, "UPDATE "+fixture.table+" SET "+secondColumn+"=$1::jsonb WHERE id=1", `{"models":["second"]}`)
				if updateErr == nil {
					updateErr = second.Commit()
				}
				completed <- updateErr
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				queryErr := integrationDB.QueryRowContext(ctx, "SELECT cardinality(pg_blocking_pids($1)) > 0", secondPID).Scan(&blocked)
				return queryErr == nil && blocked
			}, 3*time.Second, 10*time.Millisecond, "second writer must actually wait on the first row lock")
			require.NoError(t, first.Commit())
			require.NoError(t, <-completed)
			assertGroupModelCompatibilityValue(t, fixture, 1, `{"models":["second"]}`)
		})
	}
}
