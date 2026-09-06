package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestIsolatedDatabaseDSNRejectsWrongScope(t *testing.T) {
	testCases := []struct {
		name     string
		database string
		user     string
		password string
	}{
		{name: "wrong database", database: "not_carpool_test", user: databaseUser, password: "test-only-password"},
		{name: "wrong user", database: databaseName, user: "not_carpool_test", password: "test-only-password"},
		{name: "missing password", database: databaseName, user: databaseUser},
	}
	for _, testCase := range testCases {
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

func TestDatabaseScopeGuardFailureStopsBeforeFixtureAccess(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	db, mock := newBootstrapMock(t)
	mock.ExpectQuery(`SELECT current_database\(\)=\$1 AND current_user=\$2`).
		WithArgs(databaseName, databaseUser, disabledPasswordHash, localUserPattern, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"safe"}).AddRow(false))
	mock.ExpectRollback()
	expectUnlock(mock)

	err := bootstrapWithDatabase(context.Background(), db, output, testDependencies())
	require.Error(t, err)
	assertFailure(t, err, "database", "scope_guard")
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	_, pendingErr := os.Stat(output + ".pending")
	assert.ErrorIs(t, pendingErr, os.ErrNotExist)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExistingFinalIsVerifiedWithoutOverwrite(t *testing.T) {
	document, storedUsers := testFixtureDocument(t, 100)
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	require.NoError(t, writePendingDocument(output, document, func(string) error { return nil }))
	original, err := os.ReadFile(output)
	require.NoError(t, err)

	db, mock := newBootstrapMock(t)
	expectExistingState(mock, storedUsers)
	mock.ExpectCommit()
	expectUnlock(mock)

	require.NoError(t, bootstrapWithDatabase(context.Background(), db, output, testDependencies()))
	after, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, original, after)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExistingFinalMismatchFailsClosedWithoutOverwrite(t *testing.T) {
	document, storedUsers := testFixtureDocument(t, 150)
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	require.NoError(t, writePendingDocument(output, document, func(string) error { return nil }))
	original, err := os.ReadFile(output)
	require.NoError(t, err)
	firstEmail := document.Users[0].Email
	storedUsers[firstEmail] = storedSyntheticUser{ID: document.Users[0].ID, Email: firstEmail, PasswordHash: storedUsers[document.Users[1].Email].PasswordHash}

	db, mock := newBootstrapMock(t)
	expectExistingState(mock, storedUsers)
	mock.ExpectRollback()
	expectUnlock(mock)
	err = bootstrapWithDatabase(context.Background(), db, output, testDependencies())
	require.Error(t, err)
	assertFailure(t, err, "fixture", "final_database_mismatch")
	after, readErr := os.ReadFile(output)
	require.NoError(t, readErr)
	assert.Equal(t, original, after)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCommitFailureDoesNotPublishFinal(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	db, mock := newBootstrapMock(t)
	expectExistingState(mock, nil)
	expectSyntheticInserts(mock, 200)
	mock.ExpectCommit().WillReturnError(errors.New("commit outcome unavailable"))
	expectUnlock(mock)

	err := bootstrapWithDatabase(context.Background(), db, output, deterministicDependencies(t))
	require.Error(t, err)
	assertFailure(t, err, "commit", "uncertain")
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	_, pendingErr := os.Stat(output + ".pending")
	assert.NoError(t, pendingErr, "synced pending must remain for ambiguous commit recovery")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPendingDurabilityFailureRollsBackAndRemovesPending(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	db, mock := newBootstrapMock(t)
	expectExistingState(mock, nil)
	expectSyntheticInserts(mock, 250)
	mock.ExpectRollback()
	expectUnlock(mock)
	dependencies := deterministicDependencies(t)
	dependencies.syncDirectory = func(string) error { return errors.New("injected directory sync failure") }

	err := bootstrapWithDatabase(context.Background(), db, output, dependencies)
	require.Error(t, err)
	assertFailure(t, err, "pending", "write")
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	_, pendingErr := os.Stat(output + ".pending")
	assert.ErrorIs(t, pendingErr, os.ErrNotExist)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCommittedPendingRecoversAfterPublishFailure(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	firstDB, firstMock := newBootstrapMock(t)
	expectExistingState(firstMock, nil)
	expectSyntheticInserts(firstMock, 300)
	firstMock.ExpectCommit()
	expectUnlock(firstMock)
	dependencies := deterministicDependencies(t)
	dependencies.link = func(string, string) error { return errors.New("injected publish failure") }

	err := bootstrapWithDatabase(context.Background(), firstDB, output, dependencies)
	require.Error(t, err)
	assertFailure(t, err, "publish", "link")
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	pendingDocument, err := readFixtureDocument(output + ".pending")
	require.NoError(t, err)

	storedUsers := storedUsersForDocument(t, pendingDocument)
	secondDB, secondMock := newBootstrapMock(t)
	expectExistingState(secondMock, storedUsers)
	secondMock.ExpectCommit()
	expectUnlock(secondMock)
	require.NoError(t, bootstrapWithDatabase(context.Background(), secondDB, output, testDependencies()))

	finalDocument, err := readFixtureDocument(output)
	require.NoError(t, err)
	assert.Equal(t, pendingDocument, finalDocument)
	_, pendingErr := os.Stat(output + ".pending")
	assert.ErrorIs(t, pendingErr, os.ErrNotExist)
	require.NoError(t, firstMock.ExpectationsWereMet())
	require.NoError(t, secondMock.ExpectationsWereMet())
}

func TestUncommittedPendingIsRemovedBeforeSafeReseed(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	require.NoError(t, os.WriteFile(output+".pending", []byte("{incomplete"), 0600))

	db, mock := newBootstrapMock(t)
	expectExistingState(mock, nil)
	expectSyntheticInserts(mock, 500)
	mock.ExpectCommit()
	expectUnlock(mock)
	require.NoError(t, bootstrapWithDatabase(context.Background(), db, output, deterministicDependencies(t)))

	finalDocument, err := readFixtureDocument(output)
	require.NoError(t, err)
	assert.Len(t, finalDocument.Users, len(syntheticUserSpecs))
	_, pendingErr := os.Stat(output + ".pending")
	assert.ErrorIs(t, pendingErr, os.ErrNotExist)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPartialSyntheticStateFailsClosedAndKeepsPending(t *testing.T) {
	document, storedUsers := testFixtureDocument(t, 600)
	directory := t.TempDir()
	output := filepath.Join(directory, "fixtures.json")
	require.NoError(t, writePendingDocument(output+".pending", document, func(string) error { return nil }))
	delete(storedUsers, document.Users[0].Email)

	db, mock := newBootstrapMock(t)
	expectExistingState(mock, storedUsers)
	mock.ExpectRollback()
	expectUnlock(mock)
	err := bootstrapWithDatabase(context.Background(), db, output, testDependencies())
	require.Error(t, err)
	assertFailure(t, err, "database", "partial_synthetic_state")
	_, pendingErr := os.Stat(output + ".pending")
	assert.NoError(t, pendingErr)
	_, finalErr := os.Stat(output)
	assert.ErrorIs(t, finalErr, os.ErrNotExist)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newBootstrapMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_lock($1)`)).WithArgs(bootstrapLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectBegin()
	return db, mock
}

func expectUnlock(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_unlock($1)`)).WithArgs(bootstrapLockID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectExistingState(mock sqlmock.Sqlmock, storedUsers map[string]storedSyntheticUser) {
	mock.ExpectQuery(`SELECT current_database\(\)=\$1 AND current_user=\$2`).
		WithArgs(databaseName, databaseUser, disabledPasswordHash, localUserPattern, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"safe"}).AddRow(true))
	rows := sqlmock.NewRows([]string{"id", "email", "password_hash"})
	for _, spec := range syntheticUserSpecs {
		if user, ok := storedUsers[syntheticEmail(spec.Name)]; ok {
			rows.AddRow(user.ID, user.Email, user.PasswordHash)
		}
	}
	mock.ExpectQuery(`SELECT id,email,password_hash FROM users`).WithArgs(sqlmock.AnyArg()).WillReturnRows(rows)
}

func expectSyntheticInserts(mock sqlmock.Sqlmock, firstID int64) {
	for index, spec := range syntheticUserSpecs {
		mock.ExpectQuery(`INSERT INTO users`).
			WithArgs(syntheticEmail(spec.Name), sqlmock.AnyArg(), "Carpool Test "+spec.Name, spec.Role, spec.Balance).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(firstID + int64(index)))
	}
}

func testFixtureDocument(t *testing.T, firstID int64) (fixtureDocument, map[string]storedSyntheticUser) {
	t.Helper()
	document := fixtureDocument{Version: fixtureVersion, Source: fixtureSource, BaseURL: fixtureBaseURL, MockURL: fixtureMockURL}
	for index, spec := range syntheticUserSpecs {
		password := fmt.Sprintf("%048x", firstID+int64(index))
		document.Users = append(document.Users, fixtureUser{Name: spec.Name, ID: firstID + int64(index), Email: syntheticEmail(spec.Name), Password: password})
	}
	return document, storedUsersForDocument(t, document)
}

func storedUsersForDocument(t *testing.T, document fixtureDocument) map[string]storedSyntheticUser {
	t.Helper()
	users := make(map[string]storedSyntheticUser, len(document.Users))
	for _, fixture := range document.Users {
		hash, err := bcrypt.GenerateFromPassword([]byte(fixture.Password), bcrypt.MinCost)
		require.NoError(t, err)
		users[fixture.Email] = storedSyntheticUser{ID: fixture.ID, Email: fixture.Email, PasswordHash: string(hash)}
	}
	return users
}

func deterministicDependencies(t *testing.T) bootstrapDependencies {
	t.Helper()
	counter := int64(1)
	dependencies := testDependencies()
	dependencies.generateCredential = func() (string, string, error) {
		password := fmt.Sprintf("%048x", counter+9000)
		counter++
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		return password, string(hash), err
	}
	return dependencies
}

func testDependencies() bootstrapDependencies {
	dependencies := productionDependencies()
	dependencies.syncDirectory = func(string) error { return nil }
	return dependencies
}

func assertFailure(t *testing.T, err error, stage, category string) {
	t.Helper()
	var failure *bootstrapFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, stage, failure.stage)
	assert.Equal(t, category, failure.category)
}
