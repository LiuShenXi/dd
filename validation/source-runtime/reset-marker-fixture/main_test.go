package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsolatedDatabaseDSNRejectsWrongScope(t *testing.T) {
	for _, testCase := range []struct {
		name, database, user, password string
	}{
		{name: "wrong database", database: "other", user: databaseUser, password: "test-only"},
		{name: "wrong user", database: databaseName, user: "other", password: "test-only"},
		{name: "missing password", database: databaseName, user: databaseUser},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("POSTGRES_DB", testCase.database)
			t.Setenv("POSTGRES_USER", testCase.user)
			t.Setenv("POSTGRES_PASSWORD", testCase.password)
			_, err := isolatedDatabaseDSN()
			require.Error(t, err)
			assertFailure(t, err, "scope", "environment")
		})
	}
}

func TestRuntimeBindingRequiresExactReviewedInputs(t *testing.T) {
	binding := testBinding()
	require.NoError(t, validateBinding(binding))
	for _, mutate := range []func(*runtimeBinding){
		func(value *runtimeBinding) { value.Task = "other" },
		func(value *runtimeBinding) { value.RunNonce = "short" },
		func(value *runtimeBinding) { value.RuntimeImageID = "not-an-image" },
		func(value *runtimeBinding) { value.CodeEvidenceSHA256 = "short" },
		func(value *runtimeBinding) { value.ImporterSHA256 = "short" },
	} {
		changed := binding
		mutate(&changed)
		require.Error(t, validateBinding(changed))
	}
}

func TestSuccessfulImportPublishesPrivateManifest(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "result.json")
	db, mock := newImporterMock(t)
	seed := testSeed()
	expectPreflight(mock, true, true)
	expectSeed(mock, seed)
	expectInvariantValidation(mock, seed, true)
	mock.ExpectCommit()

	require.NoError(t, importFixtureWithDatabase(context.Background(), db, output, testBinding(), deterministicDependencies()))
	document, err := readFixtureDocument(output)
	require.NoError(t, err)
	assert.Equal(t, fixtureScopeID, document.ScopeID)
	assert.True(t, document.HistoricalFixture)
	assert.False(t, document.ActualSchedulerExecution)
	assert.Equal(t, depletionUSD, document.ResetEvents[0].GrantedUSD)
	assert.Equal(t, "0.00000000", document.ResetEvents[1].GrantedUSD)
	_, pendingErr := os.Stat(output + ".pending")
	assert.ErrorIs(t, pendingErr, os.ErrNotExist)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScopeGuardFailureRollsBackBeforeInsertOrManifest(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result.json")
	db, mock := newImporterMock(t)
	expectScopeGuard(mock, false)
	mock.ExpectRollback()

	err := importFixtureWithDatabase(context.Background(), db, output, testBinding(), deterministicDependencies())
	require.Error(t, err)
	assertFailure(t, err, "database", "scope_or_schema_guard")
	assertNoManifest(t, output)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateOnlyCollisionRollsBackBeforeInsertOrManifest(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result.json")
	db, mock := newImporterMock(t)
	expectPreflight(mock, true, false)
	mock.ExpectRollback()

	err := importFixtureWithDatabase(context.Background(), db, output, testBinding(), deterministicDependencies())
	require.Error(t, err)
	assertFailure(t, err, "database", "create_only_boundary")
	assertNoManifest(t, output)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInvariantFailureRollsBackAllInsertedRowsAndWritesNoManifest(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result.json")
	db, mock := newImporterMock(t)
	seed := testSeed()
	expectPreflight(mock, true, true)
	expectSeed(mock, seed)
	expectInvariantValidation(mock, seed, false)
	mock.ExpectRollback()

	err := importFixtureWithDatabase(context.Background(), db, output, testBinding(), deterministicDependencies())
	require.Error(t, err)
	assertFailure(t, err, "database", "fixture_invariants")
	assertNoManifest(t, output)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCommitFailureRetainsOnlyPendingManifestForManualReview(t *testing.T) {
	output := filepath.Join(t.TempDir(), "result.json")
	db, mock := newImporterMock(t)
	seed := testSeed()
	expectPreflight(mock, true, true)
	expectSeed(mock, seed)
	expectInvariantValidation(mock, seed, true)
	mock.ExpectCommit().WillReturnError(errors.New("commit result unknown"))

	err := importFixtureWithDatabase(context.Background(), db, output, testBinding(), deterministicDependencies())
	require.Error(t, err)
	assertFailure(t, err, "database", "commit_uncertain")
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	_, pendingErr := os.Stat(output + ".pending")
	assert.NoError(t, pendingErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExistingManifestRefusesReplayBeforeDatabaseAccess(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "result.json")
	require.NoError(t, os.WriteFile(output, []byte("create-only"), 0600))
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	err = importFixtureWithDatabase(context.Background(), db, output, testBinding(), deterministicDependencies())
	require.Error(t, err)
	assertFailure(t, err, "fixture", "replay_refused")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFixtureDocumentRejectsZeroGrantRelabeledAsSchedulerExecution(t *testing.T) {
	seed := testSeed()
	document := newFixtureDocument(testBinding(), fmt.Sprintf("%048x", 1), seed)
	require.NoError(t, validateFixtureDocument(document))
	document.ActualSchedulerExecution = true
	require.Error(t, validateFixtureDocument(document))
}

func newImporterMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock($1)`)).WithArgs(fixtureLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	return db, mock
}

func expectScopeGuard(mock sqlmock.Sqlmock, safe bool) {
	mock.ExpectQuery(`SELECT current_database\(\)=\$1 AND current_user=\$2`).
		WithArgs(databaseName, databaseUser).
		WillReturnRows(sqlmock.NewRows([]string{"safe"}).AddRow(safe))
}

func expectPreflight(mock sqlmock.Sqlmock, safe, empty bool) {
	expectScopeGuard(mock, safe)
	if !safe {
		return
	}
	mock.ExpectQuery(`SELECT NOT EXISTS \(SELECT 1 FROM users WHERE email=\$1\)`).
		WithArgs(fixtureUserEmail, fixtureGroupName, fixtureGroupName, fixtureScopeID, fixtureMarkerKind, hashLabel("reset-marker-fixture:v1:operation")).
		WillReturnRows(sqlmock.NewRows([]string{"empty"}).AddRow(empty))
}

func expectSeed(mock sqlmock.Sqlmock, seed fixtureSeed) {
	arguments := make([]driver.Value, 12)
	for index := range arguments {
		arguments[index] = sqlmock.AnyArg()
	}
	mock.ExpectQuery(`WITH fixture_now AS`).WithArgs(arguments...).WillReturnRows(sqlmock.NewRows([]string{
		"user_id", "group_id", "plan_id", "term_id", "cycle_1", "cycle_2", "cycle_3", "cycle_4",
		"batch_1", "batch_2", "target_1", "target_2", "qualification_1", "qualification_2",
		"ledger_initial", "ledger_usage", "ledger_reset", "marker_operation", "starts_at", "expires_at", "reset_one", "reset_two",
	}).AddRow(
		seed.UserID, seed.GroupID, seed.PlanID, seed.TermID,
		seed.CycleIDs[0], seed.CycleIDs[1], seed.CycleIDs[2], seed.CycleIDs[3],
		seed.BatchIDs[0], seed.BatchIDs[1], seed.TargetIDs[0], seed.TargetIDs[1],
		seed.QualificationIDs[0], seed.QualificationIDs[1], seed.LedgerIDs[0], seed.LedgerIDs[1], seed.LedgerIDs[2], seed.MarkerOperationID,
		seed.TermStartsAt, seed.TermExpiresAt, seed.ResetTimes[0], seed.ResetTimes[1],
	))
}

func expectInvariantValidation(mock sqlmock.Sqlmock, seed fixtureSeed, valid bool) {
	arguments := []driver.Value{
		seed.UserID, seed.GroupID, seed.PlanID, seed.TermID, seed.CycleIDs[0], seed.TargetIDs[0], seed.TargetIDs[1],
		seed.BatchIDs[0], seed.BatchIDs[1], seed.QualificationIDs[0], seed.QualificationIDs[1], seed.MarkerOperationID,
		fixtureScopeID, fixtureUserEmail, fixtureGroupName, seed.TermStartsAt, seed.TermExpiresAt, seed.ResetTimes[1], fixtureMarkerKind, fixtureSource,
	}
	mock.ExpectQuery(`/\* reset-marker-fixture invariant validation \*/`).WithArgs(arguments...).
		WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(valid))
}

func testBinding() runtimeBinding {
	return runtimeBinding{
		Task: fixtureTask, RunNonce: "0123456789abcdef0123456789abcdef",
		RuntimeImageID:     "sha256:" + stringOf('a', 64),
		CodeEvidenceSHA256: stringOf('b', 64), ImporterSHA256: stringOf('c', 64),
	}
}

func testSeed() fixtureSeed {
	start := time.Date(2026, 9, 1, 4, 0, 0, 0, time.UTC)
	return fixtureSeed{
		UserID: 201, GroupID: 202, PlanID: 8, TermID: 203,
		CycleIDs: [4]int64{301, 302, 303, 304}, BatchIDs: [2]int64{401, 402}, TargetIDs: [2]int64{501, 502},
		QualificationIDs: [2]int64{601, 602}, LedgerIDs: [3]int64{701, 702, 703}, MarkerOperationID: 801,
		TermStartsAt: start, TermExpiresAt: start.Add(28 * 24 * time.Hour),
		ResetTimes: [2]time.Time{start.Add(34 * time.Hour), start.Add(82 * time.Hour)},
	}
}

func deterministicDependencies() importerDependencies {
	dependencies := productionDependencies()
	password := fmt.Sprintf("%048x", 42)
	dependencies.generateCredential = func() (string, string, error) { return password, "synthetic-bcrypt-hash", nil }
	dependencies.syncDirectory = func(string) error { return nil }
	return dependencies
}

func assertNoManifest(t *testing.T, output string) {
	t.Helper()
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	_, pendingErr := os.Stat(output + ".pending")
	assert.ErrorIs(t, pendingErr, os.ErrNotExist)
}

func assertFailure(t *testing.T, err error, stage, category string) {
	t.Helper()
	var failure *importerFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, stage, failure.stage)
	assert.Equal(t, category, failure.category)
}

func stringOf(character byte, count int) string {
	value := make([]byte, count)
	for index := range value {
		value[index] = character
	}
	return string(value)
}

var _ = sql.LevelSerializable
