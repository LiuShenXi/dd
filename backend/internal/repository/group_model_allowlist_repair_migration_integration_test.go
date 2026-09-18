//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

const groupModelAllowlistRepairMigration = "236_group_model_allowlist_repair.sql"

// 236 是可重放的修复迁移：235 的重命名一旦被记账就不会重跑，数据库若回到旧结构
// （手工改回列名、按旧结构部分恢复）应用仍能启动，但所有关联 groups 的查询都会
// 报 column groups.model_allowlist does not exist（issue #6780）。
func TestMigration236RenamesLegacyModelsListConfigColumn(t *testing.T) {
	tx := groupModelAllowlistRepairTx(t, ", models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb")
	ctx := context.Background()

	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, models_list_config)
VALUES ('migration-236-rename', '{"enabled":true,"models":["claude-sonnet-5"]}'::jsonb)
RETURNING id
`).Scan(&groupID))

	applyGroupModelAllowlistRepair(ctx, t, tx)

	// 重命名保留原数据，且新列恢复 NOT NULL DEFAULT '{}' 的形状。
	var allowlist string
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text FROM groups WHERE id = $1", groupID).Scan(&allowlist))
	require.JSONEq(t, `{"enabled":true,"models":["claude-sonnet-5"]}`, allowlist)
	requireModelAllowlistColumnShape(ctx, t, tx)

	// 可重放：重复执行不报错也不改变结果。
	applyGroupModelAllowlistRepair(ctx, t, tx)
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text FROM groups WHERE id = $1", groupID).Scan(&allowlist))
	require.JSONEq(t, `{"enabled":true,"models":["claude-sonnet-5"]}`, allowlist)
}

func TestMigration236BackfillsWhenBothColumnsExist(t *testing.T) {
	tx := groupModelAllowlistRepairTx(t, ", model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb, models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb")
	ctx := context.Background()

	// 新列仍是默认空值：旧列里的配置应该被补回来。
	var staleID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, model_allowlist, models_list_config)
VALUES ('migration-236-backfill', '{}'::jsonb, '{"enabled":true,"models":["legacy-model"]}'::jsonb)
RETURNING id
`).Scan(&staleID))

	// 新列已有配置：不能被旧列覆盖。
	var currentID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, model_allowlist, models_list_config)
VALUES ('migration-236-keep', '{"enabled":true,"models":["current-model"]}'::jsonb, '{"enabled":true,"models":["legacy-model"]}'::jsonb)
RETURNING id
`).Scan(&currentID))

	applyGroupModelAllowlistRepair(ctx, t, tx)

	var backfilled, kept string
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text FROM groups WHERE id = $1", staleID).Scan(&backfilled))
	require.JSONEq(t, `{"enabled":true,"models":["legacy-model"]}`, backfilled)
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text FROM groups WHERE id = $1", currentID).Scan(&kept))
	require.JSONEq(t, `{"enabled":true,"models":["current-model"]}`, kept)
	requireModelAllowlistColumnShape(ctx, t, tx)
}

func TestMigration236RecreatesMissingModelAllowlistColumn(t *testing.T) {
	tx := groupModelAllowlistRepairTx(t, "")
	ctx := context.Background()

	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name)
VALUES ('migration-236-recreate')
RETURNING id
`).Scan(&groupID))

	applyGroupModelAllowlistRepair(ctx, t, tx)

	var allowlist string
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text FROM groups WHERE id = $1", groupID).Scan(&allowlist))
	require.JSONEq(t, `{}`, allowlist)
	requireModelAllowlistColumnShape(ctx, t, tx)
}

func groupModelAllowlistRepairTx(t *testing.T, columns string) *sql.Tx {
	t.Helper()

	// Reconstruct the historical schema instead of altering the fully migrated
	// public table, whose compatibility bridge keeps both columns and a trigger.
	fixture := newGroupModelCompatibilityFixture(t, columns)
	tx := testTx(t)
	_, err := tx.ExecContext(context.Background(), "SET LOCAL search_path TO "+fixture.schema)
	require.NoError(t, err)
	return tx
}

func applyGroupModelAllowlistRepair(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	migrationSQL, err := dbmigrations.FS.ReadFile(groupModelAllowlistRepairMigration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
}

func requireModelAllowlistColumnShape(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	var isNullable, columnDefault string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'groups' AND column_name = 'model_allowlist'
`).Scan(&isNullable, &columnDefault))
	require.Equal(t, "NO", isNullable)
	require.Contains(t, columnDefault, "'{}'::jsonb")
}
