package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

const (
	fixtureVersion       = 1
	fixtureSource        = "synthetic-local-only"
	fixtureBaseURL       = "http://carpool-app:8080"
	fixtureMockURL       = "http://carpool-mock:8090"
	databaseName         = "carpool_test"
	databaseUser         = "carpool_test"
	disabledPasswordHash = "!local-copy-login-disabled!"
	localUserPattern     = "local-user-%@example.invalid"
	fixturePasswordBytes = 24
	maxFixtureBytes      = 64 * 1024
	bootstrapLockID      = int64(0x434152504f4f4c13)
)

type fixtureUser struct {
	Name     string `json:"name"`
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type fixtureDocument struct {
	Version int           `json:"version"`
	Source  string        `json:"source"`
	BaseURL string        `json:"base_url"`
	MockURL string        `json:"mock_url"`
	Users   []fixtureUser `json:"users"`
}

type syntheticUserSpec struct {
	Name    string
	Role    string
	Balance string
}

var syntheticUserSpecs = []syntheticUserSpec{
	{Name: "admin", Role: "admin", Balance: "0"},
	{Name: "four", Role: "user", Balance: "0"},
	{Name: "three", Role: "user", Balance: "0"},
	{Name: "two", Role: "user", Balance: "0"},
	{Name: "fifth_four", Role: "user", Balance: "0"},
	{Name: "fifth_three", Role: "user", Balance: "0"},
	{Name: "fifth_two", Role: "user", Balance: "0"},
	{Name: "ordinary", Role: "user", Balance: "100"},
	{Name: "expired", Role: "user", Balance: "0"},
	{Name: "renewal", Role: "user", Balance: "0"},
	{Name: "termination", Role: "user", Balance: "0"},
	{Name: "takeover", Role: "user", Balance: "25"},
}

type storedSyntheticUser struct {
	ID           int64
	Email        string
	PasswordHash string
}

type bootstrapFailure struct {
	stage    string
	category string
	cause    error
}

func (e *bootstrapFailure) Error() string { return e.stage + ":" + e.category }
func (e *bootstrapFailure) Unwrap() error { return e.cause }

type bootstrapDependencies struct {
	generateCredential func() (string, string, error)
	link               func(string, string) error
	syncDirectory      func(string) error
}

func main() {
	output := flag.String("output", "/tmp/carpool-fixtures.json", "Private synthetic fixture output")
	flag.Parse()
	if err := bootstrap(*output); err != nil {
		stage, category := "internal", "unknown"
		var failure *bootstrapFailure
		if errors.As(err, &failure) {
			stage, category = failure.stage, failure.category
		}
		fmt.Fprintf(os.Stderr, "SYNTHETIC_BOOTSTRAP_FAILED stage=%s category=%s\n", stage, category)
		os.Exit(1)
	}
	fmt.Println("SYNTHETIC_BOOTSTRAP_READY: 12 synthetic identities verified; credentials remain in private file")
}

func bootstrap(output string) error {
	dsn, err := isolatedDatabaseDSN()
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return failure("database", "connect", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return bootstrapWithDatabase(ctx, db, output, productionDependencies())
}

func isolatedDatabaseDSN() (string, error) {
	if os.Getenv("POSTGRES_DB") != databaseName || os.Getenv("POSTGRES_USER") != databaseUser || os.Getenv("POSTGRES_PASSWORD") == "" {
		return "", failure("scope", "environment", errors.New("isolated PostgreSQL environment required"))
	}
	dsn := &url.URL{Scheme: "postgres", Host: "127.0.0.1:5432", Path: databaseName, User: url.UserPassword(databaseUser, os.Getenv("POSTGRES_PASSWORD"))}
	dsn.RawQuery = "sslmode=disable&connect_timeout=5"
	return dsn.String(), nil
}

func productionDependencies() bootstrapDependencies {
	return bootstrapDependencies{
		generateCredential: func() (string, string, error) {
			passwordBytes := make([]byte, fixturePasswordBytes)
			if _, err := rand.Read(passwordBytes); err != nil {
				return "", "", err
			}
			password := hex.EncodeToString(passwordBytes)
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return "", "", err
			}
			return password, string(hash), nil
		},
		link: os.Link,
		syncDirectory: func(path string) error {
			// The runtime binary is Linux-only; Windows uses this path only for focused tests.
			if runtime.GOOS == "windows" {
				return nil
			}
			directory, err := os.Open(path)
			if err != nil {
				return err
			}
			defer directory.Close()
			return directory.Sync()
		},
	}
}

func bootstrapWithDatabase(ctx context.Context, db *sql.DB, output string, dependencies bootstrapDependencies) (resultErr error) {
	if output == "" || filepath.Clean(output) == "." {
		return failure("scope", "output_path", errors.New("fixture output path required"))
	}
	pending := output + ".pending"
	connection, err := db.Conn(ctx)
	if err != nil {
		return failure("database", "connection", err)
	}
	if _, err = connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, bootstrapLockID); err != nil {
		_ = connection.Close()
		return failure("database", "lock", err)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, unlockErr := connection.ExecContext(unlockContext, `SELECT pg_advisory_unlock($1)`, bootstrapLockID)
		_ = connection.Close()
		if resultErr == nil && unlockErr != nil {
			resultErr = failure("database", "unlock", unlockErr)
		}
	}()

	tx, err := connection.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return failure("database", "transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err = validateDatabaseScope(ctx, tx); err != nil {
		return err
	}
	storedUsers, err := loadStoredSyntheticUsers(ctx, tx)
	if err != nil {
		return err
	}
	finalExists, err := regularPrivateFileExists(output)
	if err != nil {
		return failure("fixture", "final_metadata", err)
	}
	pendingExists, err := regularPrivateFileExists(pending)
	if err != nil {
		return failure("fixture", "pending_metadata", err)
	}

	if finalExists {
		document, readErr := readFixtureDocument(output)
		if readErr != nil {
			return failure("fixture", "final_invalid", readErr)
		}
		if !fixtureMatchesStoredUsers(document, storedUsers) {
			return failure("fixture", "final_database_mismatch", errors.New("final fixture does not match database"))
		}
		if pendingExists {
			pendingDocument, pendingErr := readFixtureDocument(pending)
			if pendingErr != nil || !reflect.DeepEqual(document, pendingDocument) {
				return failure("fixture", "pending_conflict", errors.New("pending fixture conflicts with final fixture"))
			}
		}
		if err = tx.Commit(); err != nil {
			return failure("commit", "uncertain", err)
		}
		committed = true
		if pendingExists {
			return removePending(pending, dependencies)
		}
		return nil
	}

	if pendingExists {
		switch len(storedUsers) {
		case 0:
			if err = removePending(pending, dependencies); err != nil {
				return err
			}
		case len(syntheticUserSpecs):
			document, readErr := readFixtureDocument(pending)
			if readErr != nil {
				return failure("fixture", "pending_invalid", readErr)
			}
			if !fixtureMatchesStoredUsers(document, storedUsers) {
				return failure("fixture", "pending_database_mismatch", errors.New("pending fixture does not match database"))
			}
			if err = tx.Commit(); err != nil {
				return failure("commit", "uncertain", err)
			}
			committed = true
			return publishPending(pending, output, document, dependencies)
		default:
			return failure("database", "partial_synthetic_state", errors.New("partial synthetic identity set"))
		}
	} else if len(storedUsers) != 0 {
		return failure("database", "credentials_missing", errors.New("synthetic identities exist without recoverable credentials"))
	}

	document, err := seedSyntheticUsers(ctx, tx, dependencies.generateCredential)
	if err != nil {
		return err
	}
	if err = writePendingDocument(pending, document, dependencies.syncDirectory); err != nil {
		return failure("pending", "write", err)
	}
	if err = tx.Commit(); err != nil {
		// Commit errors are outcome-ambiguous. Keep the synced pending file for a guarded retry.
		return failure("commit", "uncertain", err)
	}
	committed = true
	return publishPending(pending, output, document, dependencies)
}

func validateDatabaseScope(ctx context.Context, tx *sql.Tx) error {
	expectedEmails := make([]string, 0, len(syntheticUserSpecs))
	for _, spec := range syntheticUserSpecs {
		expectedEmails = append(expectedEmails, syntheticEmail(spec.Name))
	}
	var safe bool
	err := tx.QueryRowContext(ctx, `SELECT current_database()=$1 AND current_user=$2
		AND NOT EXISTS (SELECT 1 FROM users WHERE email LIKE $4 AND password_hash IS DISTINCT FROM $3)
		AND NOT EXISTS (SELECT 1 FROM users WHERE email IS NULL OR (email NOT LIKE $4 AND NOT (email = ANY($5::text[]))))
		AND NOT EXISTS (SELECT 1 FROM accounts WHERE status IS DISTINCT FROM 'disabled' OR schedulable IS DISTINCT FROM false)
		AND NOT EXISTS (SELECT 1 FROM api_keys WHERE status IS DISTINCT FROM 'disabled')`,
		databaseName, databaseUser, disabledPasswordHash, localUserPattern, pq.Array(expectedEmails)).Scan(&safe)
	if err != nil || !safe {
		return failure("database", "scope_guard", err)
	}
	return nil
}

func loadStoredSyntheticUsers(ctx context.Context, tx *sql.Tx) (map[string]storedSyntheticUser, error) {
	expectedEmails := make([]string, 0, len(syntheticUserSpecs))
	for _, spec := range syntheticUserSpecs {
		expectedEmails = append(expectedEmails, syntheticEmail(spec.Name))
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,email,password_hash FROM users WHERE email = ANY($1::text[]) ORDER BY email FOR UPDATE`, pq.Array(expectedEmails))
	if err != nil {
		return nil, failure("database", "synthetic_query", err)
	}
	defer rows.Close()
	users := make(map[string]storedSyntheticUser, len(syntheticUserSpecs))
	for rows.Next() {
		var user storedSyntheticUser
		if err = rows.Scan(&user.ID, &user.Email, &user.PasswordHash); err != nil {
			return nil, failure("database", "synthetic_scan", err)
		}
		if _, duplicate := users[user.Email]; duplicate {
			return nil, failure("database", "synthetic_duplicate", errors.New("duplicate synthetic email"))
		}
		users[user.Email] = user
	}
	if err = rows.Err(); err != nil {
		return nil, failure("database", "synthetic_rows", err)
	}
	return users, nil
}

func seedSyntheticUsers(ctx context.Context, tx *sql.Tx, generateCredential func() (string, string, error)) (fixtureDocument, error) {
	document := fixtureDocument{Version: fixtureVersion, Source: fixtureSource, BaseURL: fixtureBaseURL, MockURL: fixtureMockURL}
	for _, spec := range syntheticUserSpecs {
		password, passwordHash, err := generateCredential()
		if err != nil {
			return fixtureDocument{}, failure("seed", "credential", err)
		}
		user := fixtureUser{Name: spec.Name, Email: syntheticEmail(spec.Name), Password: password}
		err = tx.QueryRowContext(ctx, `INSERT INTO users
			(email,password_hash,username,role,balance,concurrency,status,balance_notify_enabled,restrict_public_groups,notes)
			VALUES ($1,$2,$3,$4,$5,20,'active',false,true,'Synthetic local carpool acceptance fixture') RETURNING id`,
			user.Email, passwordHash, "Carpool Test "+spec.Name, spec.Role, spec.Balance).Scan(&user.ID)
		if err != nil {
			return fixtureDocument{}, failure("seed", "insert", err)
		}
		document.Users = append(document.Users, user)
	}
	return document, nil
}

func syntheticEmail(name string) string { return "carpool-test-" + name + "@example.invalid" }

func fixtureMatchesStoredUsers(document fixtureDocument, storedUsers map[string]storedSyntheticUser) bool {
	if validateFixtureDocument(document) != nil || len(storedUsers) != len(document.Users) {
		return false
	}
	for _, fixture := range document.Users {
		stored, ok := storedUsers[fixture.Email]
		if !ok || stored.ID != fixture.ID || bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(fixture.Password)) != nil {
			return false
		}
	}
	return true
}

func validateFixtureDocument(document fixtureDocument) error {
	if document.Version != fixtureVersion || document.Source != fixtureSource || document.BaseURL != fixtureBaseURL || document.MockURL != fixtureMockURL {
		return errors.New("unexpected fixture envelope")
	}
	if len(document.Users) != len(syntheticUserSpecs) {
		return errors.New("unexpected fixture user count")
	}
	ids := make(map[int64]struct{}, len(document.Users))
	for index, user := range document.Users {
		spec := syntheticUserSpecs[index]
		decodedPassword, err := hex.DecodeString(user.Password)
		if user.Name != spec.Name || user.Email != syntheticEmail(spec.Name) || user.ID <= 0 || err != nil || len(decodedPassword) != fixturePasswordBytes {
			return errors.New("invalid fixture user")
		}
		if _, duplicate := ids[user.ID]; duplicate {
			return errors.New("duplicate fixture user id")
		}
		ids[user.ID] = struct{}{}
	}
	return nil
}

func regularPrivateFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		return false, errors.New("fixture path must be a private regular file")
	}
	return true, nil
}

func readFixtureDocument(path string) (fixtureDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return fixtureDocument{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxFixtureBytes+1))
	decoder.DisallowUnknownFields()
	var document fixtureDocument
	if err = decoder.Decode(&document); err != nil {
		return fixtureDocument{}, err
	}
	if err = validateFixtureDocument(document); err != nil {
		return fixtureDocument{}, err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fixtureDocument{}, errors.New("fixture has trailing data")
	}
	return document, nil
}

func writePendingDocument(path string, document fixtureDocument, syncDirectory func(string) error) (err error) {
	if err = validateFixtureDocument(document); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err = json.NewEncoder(file).Encode(document); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	complete = true
	return nil
}

func publishPending(pending, output string, document fixtureDocument, dependencies bootstrapDependencies) error {
	err := dependencies.link(pending, output)
	if err != nil {
		existing, readErr := readFixtureDocument(output)
		if readErr != nil || !reflect.DeepEqual(existing, document) {
			if errors.Is(err, os.ErrExist) {
				return failure("publish", "final_conflict", errors.New("final fixture already exists with different content"))
			}
			return failure("publish", "link", err)
		}
	}
	if err = dependencies.syncDirectory(filepath.Dir(output)); err != nil {
		return failure("publish", "directory_sync", err)
	}
	return removePending(pending, dependencies)
}

func removePending(path string, dependencies bootstrapDependencies) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failure("pending", "remove", err)
	}
	if err := dependencies.syncDirectory(filepath.Dir(path)); err != nil {
		return failure("pending", "directory_sync", err)
	}
	return nil
}

func failure(stage, category string, cause error) error {
	if cause == nil {
		cause = errors.New(category)
	}
	return &bootstrapFailure{stage: stage, category: category, cause: cause}
}
