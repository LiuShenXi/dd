package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"regexp"
	"runtime"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

const (
	fixtureVersion             = 1
	fixtureSource              = "historical-synthetic-reset-marker"
	fixturePurpose             = "user-ui-reset-marker-projection-only"
	fixtureTask                = "09-05-sub2api-carpool-v1-3"
	fixtureScopeID       int64 = 2_147_480_914
	fixtureUserEmail           = "carpool-reset-marker-v14@example.invalid"
	fixtureUsername            = "Carpool Reset Marker V14"
	fixtureGroupName           = "carpool-reset-marker-v14-local-only"
	fixtureMarkerKind          = "historical_fixture_import"
	databaseName               = "carpool_test"
	databaseUser               = "carpool_test"
	fixturePasswordBytes       = 24
	maxManifestBytes           = 64 * 1024
	fixtureLockID        int64 = 0x434152504f4f4c28
	depletionUSD               = "163.00000000"
	fullQuotaUSD               = "700.00000000"
)

var (
	hex32Pattern   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex64Pattern   = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	imageIDPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type runtimeBinding struct {
	Task               string `json:"task"`
	RunNonce           string `json:"run_nonce"`
	RuntimeImageID     string `json:"runtime_image_id"`
	CodeEvidenceSHA256 string `json:"code_evidence_sha256"`
	ImporterSHA256     string `json:"importer_sha256"`
}

type fixtureUser struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type fixtureResetEvent struct {
	BatchID    int64     `json:"batch_id"`
	TargetID   int64     `json:"target_id"`
	GrantedUSD string    `json:"granted_usd"`
	ExecutedAt time.Time `json:"executed_at"`
}

type fixtureDocument struct {
	Version                  int                 `json:"version"`
	Source                   string              `json:"source"`
	Purpose                  string              `json:"purpose"`
	HistoricalFixture        bool                `json:"historical_fixture"`
	ActualSchedulerExecution bool                `json:"actual_scheduler_execution"`
	Binding                  runtimeBinding      `json:"binding"`
	ScopeID                  int64               `json:"scope_id"`
	User                     fixtureUser         `json:"user"`
	GroupID                  int64               `json:"group_id"`
	PlanID                   int64               `json:"plan_id"`
	TermID                   int64               `json:"term_id"`
	CycleIDs                 []int64             `json:"cycle_ids"`
	QualificationIDs         []int64             `json:"qualification_ids"`
	LedgerIDs                []int64             `json:"ledger_ids"`
	MarkerOperationID        int64               `json:"marker_operation_id"`
	TermStartsAt             time.Time           `json:"term_starts_at"`
	TermExpiresAt            time.Time           `json:"term_expires_at"`
	AvailableUSD             string              `json:"available_usd"`
	ResetEvents              []fixtureResetEvent `json:"reset_events"`
	CreatedAt                time.Time           `json:"created_at"`
}

type fixtureSeed struct {
	UserID            int64
	GroupID           int64
	PlanID            int64
	TermID            int64
	CycleIDs          [4]int64
	BatchIDs          [2]int64
	TargetIDs         [2]int64
	QualificationIDs  [2]int64
	LedgerIDs         [3]int64
	MarkerOperationID int64
	TermStartsAt      time.Time
	TermExpiresAt     time.Time
	ResetTimes        [2]time.Time
}

type importerFailure struct {
	stage    string
	category string
	cause    error
}

func (e *importerFailure) Error() string { return e.stage + ":" + e.category }
func (e *importerFailure) Unwrap() error { return e.cause }

type importerDependencies struct {
	generateCredential func() (string, string, error)
	link               func(string, string) error
	syncDirectory      func(string) error
}

func main() {
	output := flag.String("output", "", "Private create-only result manifest")
	task := flag.String("task", "", "Bound Trellis task")
	runNonce := flag.String("run-nonce", "", "Private import attempt nonce")
	runtimeImageID := flag.String("runtime-image-id", "", "Reviewed local runtime image")
	codeEvidenceSHA256 := flag.String("code-evidence-sha256", "", "Reviewed code evidence hash")
	importerSHA256 := flag.String("importer-sha256", "", "Installed importer binary hash")
	flag.Parse()
	binding := runtimeBinding{
		Task: *task, RunNonce: *runNonce, RuntimeImageID: *runtimeImageID,
		CodeEvidenceSHA256: *codeEvidenceSHA256, ImporterSHA256: *importerSHA256,
	}
	if err := importFixture(*output, binding); err != nil {
		stage, category := "internal", "unknown"
		var failure *importerFailure
		if errors.As(err, &failure) {
			stage, category = failure.stage, failure.category
		}
		fmt.Fprintf(os.Stderr, "RESET_MARKER_FIXTURE_FAILED stage=%s category=%s\n", stage, category)
		os.Exit(1)
	}
	fmt.Println("RESET_MARKER_FIXTURE_READY: historical synthetic UI fixture committed; private credentials remain in manifest")
}

func importFixture(output string, binding runtimeBinding) error {
	if err := validateBinding(binding); err != nil {
		return failure("scope", "binding", err)
	}
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
	return importFixtureWithDatabase(ctx, db, output, binding, productionDependencies())
}

func validateBinding(binding runtimeBinding) error {
	if binding.Task != fixtureTask || !hex32Pattern.MatchString(binding.RunNonce) ||
		!imageIDPattern.MatchString(binding.RuntimeImageID) ||
		!hex64Pattern.MatchString(binding.CodeEvidenceSHA256) ||
		!hex64Pattern.MatchString(binding.ImporterSHA256) {
		return errors.New("runtime binding is incomplete")
	}
	return nil
}

func isolatedDatabaseDSN() (string, error) {
	if os.Getenv("POSTGRES_DB") != databaseName || os.Getenv("POSTGRES_USER") != databaseUser || os.Getenv("POSTGRES_PASSWORD") == "" {
		return "", failure("scope", "environment", errors.New("isolated PostgreSQL environment required"))
	}
	dsn := &url.URL{Scheme: "postgres", Host: "127.0.0.1:5432", Path: databaseName, User: url.UserPassword(databaseUser, os.Getenv("POSTGRES_PASSWORD"))}
	dsn.RawQuery = "sslmode=disable&connect_timeout=5"
	return dsn.String(), nil
}

func productionDependencies() importerDependencies {
	return importerDependencies{
		generateCredential: func() (string, string, error) {
			passwordBytes := make([]byte, fixturePasswordBytes)
			if _, err := rand.Read(passwordBytes); err != nil {
				return "", "", err
			}
			password := hex.EncodeToString(passwordBytes)
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			return password, string(hash), err
		},
		link: os.Link,
		syncDirectory: func(path string) error {
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

func importFixtureWithDatabase(ctx context.Context, db *sql.DB, output string, binding runtimeBinding, dependencies importerDependencies) (resultErr error) {
	if err := validateBinding(binding); err != nil {
		return failure("scope", "binding", err)
	}
	if output == "" || !filepath.IsAbs(output) {
		return failure("scope", "output_path", errors.New("absolute result path required"))
	}
	pending := output + ".pending"
	for _, path := range []string{output, pending} {
		exists, err := regularPrivateFileExists(path)
		if err != nil {
			return failure("fixture", "manifest_metadata", err)
		}
		if exists {
			return failure("fixture", "replay_refused", errors.New("fixture manifest already exists"))
		}
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return failure("database", "transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, fixtureLockID); err != nil {
		return failure("database", "lock", err)
	}
	if err = validateDatabaseScope(ctx, tx); err != nil {
		return err
	}
	if err = validateCreateOnlyBoundary(ctx, tx); err != nil {
		return err
	}
	password, passwordHash, err := dependencies.generateCredential()
	if err != nil {
		return failure("fixture", "credential", err)
	}
	seed, err := seedFixture(ctx, tx, passwordHash)
	if err != nil {
		return err
	}
	if err = validateSeededFixture(ctx, tx, seed); err != nil {
		return err
	}
	document := newFixtureDocument(binding, password, seed)
	if err = writePendingDocument(pending, document, dependencies.syncDirectory); err != nil {
		return failure("manifest", "pending_write", err)
	}
	if err = tx.Commit(); err != nil {
		return failure("database", "commit_uncertain", err)
	}
	committed = true
	return publishPending(pending, output, document, dependencies)
}

func validateDatabaseScope(ctx context.Context, tx *sql.Tx) error {
	var safe bool
	err := tx.QueryRowContext(ctx, `SELECT current_database()=$1 AND current_user=$2
		AND inet_server_addr()=inet '127.0.0.1'
		AND to_regclass('public.users') IS NOT NULL
		AND to_regclass('public.groups') IS NOT NULL
		AND to_regclass('public.user_allowed_groups') IS NOT NULL
		AND to_regclass('public.carpool_plans') IS NOT NULL
		AND to_regclass('public.carpool_terms') IS NOT NULL
		AND to_regclass('public.carpool_cycles') IS NOT NULL
		AND to_regclass('public.carpool_ledger') IS NOT NULL
		AND to_regclass('public.carpool_operations') IS NOT NULL
		AND to_regclass('public.carpool_reset_scope_states') IS NOT NULL
		AND to_regclass('public.carpool_reset_batches') IS NOT NULL
		AND to_regclass('public.carpool_reset_targets') IS NOT NULL
		AND to_regclass('public.carpool_reset_qualifications') IS NOT NULL
		AND (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND (table_name,column_name,data_type) IN (
			('users','email','character varying'),('users','password_hash','character varying'),
			('user_allowed_groups','user_id','bigint'),('user_allowed_groups','group_id','bigint'),
			('groups','subscription_type','character varying'),('groups','duplicate_operation_id','character varying'),
			('carpool_terms','plan_snapshot','jsonb'),('carpool_terms','scope_id','bigint'),
			('carpool_cycles','base_quota_usd','numeric'),('carpool_cycles','base_balance_usd','numeric'),
			('carpool_ledger','reset_batch_id','bigint'),('carpool_ledger','event_key','character varying'),
			('carpool_reset_scope_states','last_successful_reset_at','timestamp with time zone'),
			('carpool_reset_batches','evidence','jsonb'),('carpool_reset_batches','completed_at','timestamp with time zone'),
			('carpool_reset_targets','executed_at','timestamp with time zone'),('carpool_reset_targets','granted_usd','numeric'),
			('carpool_reset_qualifications','source_event_key_hash','character varying'),
			('carpool_operations','response','jsonb')
		))=19
		AND EXISTS (
			SELECT 1 FROM carpool_plans p WHERE p.code='three_seat' AND p.duration_days=28
			AND p.cycle_days=7 AND p.boost_count=3 AND p.boost_ratio=0.10000000
			AND p.id=(SELECT p2.id FROM carpool_plans p2 WHERE p2.code='three_seat' ORDER BY p2.version DESC,p2.id DESC LIMIT 1)
		)`, databaseName, databaseUser).Scan(&safe)
	if err != nil || !safe {
		return failure("database", "scope_or_schema_guard", err)
	}
	return nil
}

func validateCreateOnlyBoundary(ctx context.Context, tx *sql.Tx) error {
	markerKeyHash := hashLabel("reset-marker-fixture:v1:operation")
	var empty bool
	err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT 1 FROM users WHERE email=$1)
		AND NOT EXISTS (SELECT 1 FROM groups WHERE name=$2 OR duplicate_operation_id=$3)
		AND NOT EXISTS (SELECT 1 FROM carpool_terms WHERE scope_id=$4)
		AND NOT EXISTS (SELECT 1 FROM carpool_reset_scope_states WHERE scope_id=$4)
		AND NOT EXISTS (SELECT 1 FROM carpool_reset_batches WHERE scope_id=$4)
		AND NOT EXISTS (SELECT 1 FROM carpool_reset_qualifications WHERE scope_id=$4)
		AND NOT EXISTS (SELECT 1 FROM carpool_operations WHERE kind=$5 OR key_hash=$6)
		AND NOT EXISTS (SELECT 1 FROM carpool_ledger WHERE event_key LIKE 'historical-fixture:v1:%')`,
		fixtureUserEmail, fixtureGroupName, fixtureGroupName, fixtureScopeID, fixtureMarkerKind, markerKeyHash).Scan(&empty)
	if err != nil || !empty {
		return failure("database", "create_only_boundary", err)
	}
	return nil
}

const seedFixtureSQL = `WITH fixture_now AS (
	SELECT clock_timestamp() AS created_at
), fixture_times AS (
	SELECT created_at,
		((date_trunc('day',created_at AT TIME ZONE 'Asia/Shanghai')-interval '5 days'+interval '12 hours') AT TIME ZONE 'Asia/Shanghai') AS starts_at,
		((date_trunc('day',created_at AT TIME ZONE 'Asia/Shanghai')-interval '4 days'+interval '22 hours 20 seconds') AT TIME ZONE 'Asia/Shanghai') AS reset_one_at,
		((date_trunc('day',created_at AT TIME ZONE 'Asia/Shanghai')-interval '2 days'+interval '22 hours 20 seconds') AT TIME ZONE 'Asia/Shanghai') AS reset_two_at
	FROM fixture_now
), fixture_plan AS (
	SELECT * FROM carpool_plans WHERE code='three_seat' AND duration_days=28 AND cycle_days=7 AND boost_count=3 AND boost_ratio=0.10000000
	ORDER BY version DESC,id DESC LIMIT 1
), fixture_group AS (
	INSERT INTO groups(name,description,rate_multiplier,is_exclusive,status,platform,subscription_type,default_validity_days,duplicate_operation_id)
	VALUES($4,'Historical synthetic reset-marker UI fixture; local-only and not scheduler evidence',1.0,true,'active','openai','carpool',28,$5)
	RETURNING id
), fixture_user AS (
	INSERT INTO users(email,password_hash,username,role,balance,concurrency,status,balance_notify_enabled,restrict_public_groups,notes)
	VALUES($1,$2,$3,'user',0,5,'active',false,true,'Historical synthetic reset-marker UI fixture; local-only')
	RETURNING id
), fixture_user_group AS (
	INSERT INTO user_allowed_groups(user_id,group_id)
	SELECT u.id,g.id FROM fixture_user u CROSS JOIN fixture_group g RETURNING user_id,group_id
), fixture_term AS (
	INSERT INTO carpool_terms(user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,status,boost_used,source_mode,history_complete,statistics_since,created_by,notes)
	SELECT u.id,$6,g.id,p.id,jsonb_build_object(
		'plan_id',p.id,'code',p.code,'name',p.name,'version',p.version,
		'list_price_cny',p.list_price_cny::text,'weekly_quota_usd',p.weekly_quota_usd::text,
		'cycle_5_quota_usd',round(p.weekly_quota_usd*2/7,0)::text,'duration_days',28,'cycle_days',7,
		'boost_ratio',p.boost_ratio::text,'boost_amount_usd',round(p.weekly_quota_usd*p.boost_ratio,8)::text,
		'boost_count',3,'rounding_mode','half_up','enabled',p.enabled
	),t.starts_at,t.starts_at+interval '28 days','active',0,'new',true,t.starts_at,u.id,
	'Historical synthetic reset-marker UI fixture; imported history, not scheduler execution'
	FROM fixture_user_group ug JOIN fixture_user u ON u.id=ug.user_id JOIN fixture_group g ON g.id=ug.group_id
	CROSS JOIN fixture_plan p CROSS JOIN fixture_times t RETURNING id,user_id,group_id,plan_id,starts_at,expires_at
), fixture_cycles AS (
	INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,revision,activated_at)
	SELECT term.id,n,term.starts_at+(n-1)*interval '7 days',term.starts_at+n*interval '7 days',p.weekly_quota_usd,
		CASE WHEN n=1 THEN p.weekly_quota_usd ELSE 0 END,0,0,CASE WHEN n=1 THEN 'active' ELSE 'scheduled' END,
		CASE WHEN n=1 THEN 3 ELSE 0 END,CASE WHEN n=1 THEN term.starts_at ELSE NULL END
	FROM fixture_term term CROSS JOIN fixture_plan p CROSS JOIN generate_series(1,4) n RETURNING id,term_id,cycle_no
), cycle_one AS (
	SELECT id,term_id FROM fixture_cycles WHERE cycle_no=1
), fixture_scope AS (
	INSERT INTO carpool_reset_scope_states(scope_id,timezone,last_successful_reset_at,pending_batch_id,revision)
	SELECT $6,'Asia/Shanghai',reset_two_at,NULL,2 FROM fixture_times RETURNING scope_id
), batch_one AS (
	INSERT INTO carpool_reset_batches(scope_id,status,detected_at,qualified_at,slot_at,scheduled_at,schedule_revision,effective_at,completed_at,evidence,announcement_state,qualification_source,source_event_key_hash)
	SELECT scope_id,'completed',t.reset_one_at-interval '30 minutes',t.reset_one_at-interval '20 minutes',date_trunc('minute',t.reset_one_at),t.reset_one_at,0,t.reset_one_at,t.reset_one_at,
		jsonb_build_object('fixture',$11::text,'historical',true,'actual_scheduler_execution',false,'sequence',1),'published','administrator',$7
	FROM fixture_scope CROSS JOIN fixture_times t RETURNING id
), batch_two AS (
	INSERT INTO carpool_reset_batches(scope_id,status,detected_at,qualified_at,slot_at,scheduled_at,schedule_revision,effective_at,completed_at,evidence,announcement_state,qualification_source,source_event_key_hash)
	SELECT scope_id,'completed',t.reset_two_at-interval '30 minutes',t.reset_two_at-interval '20 minutes',date_trunc('minute',t.reset_two_at),t.reset_two_at,0,t.reset_two_at,t.reset_two_at,
		jsonb_build_object('fixture',$11::text,'historical',true,'actual_scheduler_execution',false,'sequence',2),'published','administrator',$8
	FROM fixture_scope CROSS JOIN fixture_times t RETURNING id
), qualification_one AS (
	INSERT INTO carpool_reset_qualifications(scope_id,batch_id,source,source_event_key_hash,reason,confirmed_at)
	SELECT $6,b.id,'administrator',$7,'Historical synthetic fixture; not live qualification execution',t.reset_one_at-interval '20 minutes'
	FROM batch_one b CROSS JOIN fixture_times t RETURNING id
), qualification_two AS (
	INSERT INTO carpool_reset_qualifications(scope_id,batch_id,source,source_event_key_hash,reason,confirmed_at)
	SELECT $6,b.id,'administrator',$8,'Historical synthetic fixture; not live qualification execution',t.reset_two_at-interval '20 minutes'
	FROM batch_two b CROSS JOIN fixture_times t RETURNING id
), target_one AS (
	INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at)
	SELECT b.id,term.id,c.id,'succeeded',163.00000000,t.reset_one_at FROM batch_one b CROSS JOIN fixture_term term CROSS JOIN cycle_one c CROSS JOIN fixture_times t RETURNING id
), target_two AS (
	INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at)
	SELECT b.id,term.id,c.id,'succeeded',0.00000000,t.reset_two_at FROM batch_two b CROSS JOIN fixture_term term CROSS JOIN cycle_one c CROSS JOIN fixture_times t RETURNING id
), ledger_initial AS (
	INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,actor_id,reason,effective_at)
	SELECT term.user_id,term.id,c.id,'cycle_initial','base',700.00000000,'historical-fixture:v1:cycle-initial',term.user_id,'Historical synthetic fixture opening',term.starts_at
	FROM fixture_term term CROSS JOIN cycle_one c RETURNING id
), ledger_usage AS (
	INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,reason,effective_at)
	SELECT term.user_id,term.id,c.id,'usage','base',-163.00000000,'historical-fixture:v1:depletion','Historical synthetic fixture depletion',t.reset_one_at-interval '1 hour'
	FROM fixture_term term CROSS JOIN cycle_one c CROSS JOIN fixture_times t RETURNING id
), ledger_reset AS (
	INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,reset_batch_id,actor_id,reason,effective_at)
	SELECT term.user_id,term.id,c.id,'reset','base',163.00000000,'historical-fixture:v1:reset-positive',b.id,term.user_id,
	'Historical synthetic fixture positive reset; not scheduler execution',t.reset_one_at
	FROM fixture_term term CROSS JOIN cycle_one c CROSS JOIN batch_one b CROSS JOIN fixture_times t RETURNING id
), marker_operation AS (
	INSERT INTO carpool_operations(kind,actor_id,key_hash,request_fingerprint,resource_type,resource_id,response)
	SELECT $9,term.user_id,$10,$12,'term',term.id,jsonb_build_object('fixture',$11::text,'scope_id',$6,'historical',true,'actual_scheduler_execution',false)
	FROM fixture_term term RETURNING id
)
SELECT u.id,g.id,p.id,term.id,
	(SELECT id FROM fixture_cycles WHERE cycle_no=1),(SELECT id FROM fixture_cycles WHERE cycle_no=2),
	(SELECT id FROM fixture_cycles WHERE cycle_no=3),(SELECT id FROM fixture_cycles WHERE cycle_no=4),
	b1.id,b2.id,t1.id,t2.id,q1.id,q2.id,li.id,lu.id,lr.id,m.id,
	term.starts_at,term.expires_at,t.reset_one_at,t.reset_two_at
FROM fixture_user u CROSS JOIN fixture_group g CROSS JOIN fixture_plan p CROSS JOIN fixture_term term
CROSS JOIN batch_one b1 CROSS JOIN batch_two b2 CROSS JOIN target_one t1 CROSS JOIN target_two t2
CROSS JOIN qualification_one q1 CROSS JOIN qualification_two q2 CROSS JOIN ledger_initial li CROSS JOIN ledger_usage lu CROSS JOIN ledger_reset lr
CROSS JOIN marker_operation m CROSS JOIN fixture_times t`

func seedFixture(ctx context.Context, tx *sql.Tx, passwordHash string) (fixtureSeed, error) {
	resetHashOne := hashLabel("reset-marker-fixture:v1:reset:one")
	resetHashTwo := hashLabel("reset-marker-fixture:v1:reset:two")
	markerKeyHash := hashLabel("reset-marker-fixture:v1:operation")
	markerFingerprint := hashLabel("reset-marker-fixture:v1:payload")
	var seed fixtureSeed
	err := tx.QueryRowContext(ctx, seedFixtureSQL,
		fixtureUserEmail, passwordHash, fixtureUsername, fixtureGroupName, fixtureGroupName, fixtureScopeID,
		resetHashOne, resetHashTwo, fixtureMarkerKind, markerKeyHash, fixtureSource, markerFingerprint,
	).Scan(
		&seed.UserID, &seed.GroupID, &seed.PlanID, &seed.TermID,
		&seed.CycleIDs[0], &seed.CycleIDs[1], &seed.CycleIDs[2], &seed.CycleIDs[3],
		&seed.BatchIDs[0], &seed.BatchIDs[1], &seed.TargetIDs[0], &seed.TargetIDs[1],
		&seed.QualificationIDs[0], &seed.QualificationIDs[1],
		&seed.LedgerIDs[0], &seed.LedgerIDs[1], &seed.LedgerIDs[2], &seed.MarkerOperationID,
		&seed.TermStartsAt, &seed.TermExpiresAt, &seed.ResetTimes[0], &seed.ResetTimes[1],
	)
	if err != nil {
		return fixtureSeed{}, failure("database", "insert", err)
	}
	return seed, nil
}

func validateSeededFixture(ctx context.Context, tx *sql.Tx, seed fixtureSeed) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `/* reset-marker-fixture invariant validation */
		SELECT EXISTS (
			SELECT 1 FROM users u JOIN groups g ON g.id=$2 JOIN carpool_terms term ON term.id=$4
			WHERE u.id=$1 AND u.email=$14 AND u.balance=0 AND u.status='active'
			AND g.name=$15 AND g.subscription_type='carpool' AND g.status='active'
			AND EXISTS (SELECT 1 FROM user_allowed_groups ug WHERE ug.user_id=u.id AND ug.group_id=g.id)
			AND term.user_id=u.id AND term.group_id=g.id AND term.scope_id=$13 AND term.plan_id=$3
			AND term.status='active' AND term.expires_at=term.starts_at+interval '28 days'
			AND term.plan_snapshot->>'duration_days'='28' AND term.plan_snapshot->>'cycle_days'='7'
			AND term.plan_snapshot->>'boost_count'='3' AND term.plan_snapshot->>'weekly_quota_usd'='700.00000000'
		)
		AND (SELECT COUNT(*)=4 AND MIN(cycle_no)=1 AND MAX(cycle_no)=4
			AND bool_and(ends_at=starts_at+interval '7 days') AND bool_and(base_quota_usd=700.00000000)
			FROM carpool_cycles WHERE term_id=$4)
		AND EXISTS (SELECT 1 FROM carpool_cycles WHERE id=$5 AND term_id=$4 AND cycle_no=1 AND state='active'
			AND base_balance_usd=700.00000000 AND boost_balance_usd=0 AND manual_balance_usd=0)
		AND (SELECT COALESCE(SUM(delta_usd),0)=700.00000000 AND COUNT(*)=3 FROM carpool_ledger WHERE term_id=$4 AND cycle_id=$5)
		AND (SELECT COUNT(*)=2 AND COUNT(*) FILTER (WHERE status='succeeded')=2
			AND SUM(granted_usd)=163.00000000 AND COUNT(*) FILTER (WHERE granted_usd=0)=1
			AND bool_and(executed_at>=$16 AND executed_at<$17)
			FROM carpool_reset_targets WHERE term_id=$4 AND cycle_id=$5 AND id IN ($6,$7))
		AND (SELECT COUNT(*)=2 AND bool_and(status='completed') AND bool_and(effective_at=completed_at)
			FROM carpool_reset_batches WHERE id IN ($8,$9) AND scope_id=$13)
		AND (SELECT COUNT(*)=2 FROM carpool_reset_qualifications WHERE id IN ($10,$11) AND scope_id=$13)
		AND EXISTS (SELECT 1 FROM carpool_reset_scope_states WHERE scope_id=$13 AND pending_batch_id IS NULL
			AND revision=2 AND last_successful_reset_at=$18)
		AND EXISTS (SELECT 1 FROM carpool_operations WHERE id=$12 AND kind=$19 AND resource_type='term' AND resource_id=$4
			AND response->>'fixture'=$20 AND response->>'actual_scheduler_execution'='false')`,
		seed.UserID, seed.GroupID, seed.PlanID, seed.TermID, seed.CycleIDs[0], seed.TargetIDs[0], seed.TargetIDs[1],
		seed.BatchIDs[0], seed.BatchIDs[1], seed.QualificationIDs[0], seed.QualificationIDs[1], seed.MarkerOperationID,
		fixtureScopeID, fixtureUserEmail, fixtureGroupName, seed.TermStartsAt, seed.TermExpiresAt, seed.ResetTimes[1], fixtureMarkerKind, fixtureSource,
	).Scan(&valid)
	if err != nil || !valid {
		return failure("database", "fixture_invariants", err)
	}
	return nil
}

func newFixtureDocument(binding runtimeBinding, password string, seed fixtureSeed) fixtureDocument {
	return fixtureDocument{
		Version: fixtureVersion, Source: fixtureSource, Purpose: fixturePurpose,
		HistoricalFixture: true, ActualSchedulerExecution: false, Binding: binding, ScopeID: fixtureScopeID,
		User:    fixtureUser{ID: seed.UserID, Email: fixtureUserEmail, Username: fixtureUsername, Password: password},
		GroupID: seed.GroupID, PlanID: seed.PlanID, TermID: seed.TermID,
		CycleIDs:         []int64{seed.CycleIDs[0], seed.CycleIDs[1], seed.CycleIDs[2], seed.CycleIDs[3]},
		QualificationIDs: []int64{seed.QualificationIDs[0], seed.QualificationIDs[1]},
		LedgerIDs:        []int64{seed.LedgerIDs[0], seed.LedgerIDs[1], seed.LedgerIDs[2]}, MarkerOperationID: seed.MarkerOperationID,
		TermStartsAt: seed.TermStartsAt, TermExpiresAt: seed.TermExpiresAt, AvailableUSD: fullQuotaUSD,
		ResetEvents: []fixtureResetEvent{
			{BatchID: seed.BatchIDs[0], TargetID: seed.TargetIDs[0], GrantedUSD: depletionUSD, ExecutedAt: seed.ResetTimes[0]},
			{BatchID: seed.BatchIDs[1], TargetID: seed.TargetIDs[1], GrantedUSD: "0.00000000", ExecutedAt: seed.ResetTimes[1]},
		},
		CreatedAt: time.Now().UTC(),
	}
}

func validateFixtureDocument(document fixtureDocument) error {
	if document.Version != fixtureVersion || document.Source != fixtureSource || document.Purpose != fixturePurpose ||
		!document.HistoricalFixture || document.ActualSchedulerExecution || document.ScopeID != fixtureScopeID ||
		validateBinding(document.Binding) != nil || document.User.ID <= 0 || document.User.Email != fixtureUserEmail ||
		document.User.Username != fixtureUsername || document.User.Password == "" || document.GroupID <= 0 || document.PlanID <= 0 ||
		document.TermID <= 0 || document.MarkerOperationID <= 0 || document.AvailableUSD != fullQuotaUSD || document.CreatedAt.IsZero() {
		return errors.New("unexpected fixture envelope")
	}
	decodedPassword, err := hex.DecodeString(document.User.Password)
	if err != nil || len(decodedPassword) != fixturePasswordBytes || len(document.CycleIDs) != 4 ||
		len(document.QualificationIDs) != 2 || len(document.LedgerIDs) != 3 || len(document.ResetEvents) != 2 {
		return errors.New("unexpected fixture collection")
	}
	if !document.TermExpiresAt.Equal(document.TermStartsAt.Add(28*24*time.Hour)) ||
		document.ResetEvents[0].GrantedUSD != depletionUSD || document.ResetEvents[1].GrantedUSD != "0.00000000" ||
		!document.ResetEvents[0].ExecutedAt.Before(document.ResetEvents[1].ExecutedAt) {
		return errors.New("unexpected fixture accounting or timeline")
	}
	for _, id := range []int64{document.User.ID, document.GroupID, document.PlanID, document.TermID, document.MarkerOperationID} {
		if id <= 0 {
			return errors.New("fixture id is invalid")
		}
	}
	for _, event := range document.ResetEvents {
		if event.BatchID <= 0 || event.TargetID <= 0 {
			return errors.New("reset event id is invalid")
		}
		if event.ExecutedAt.Before(document.TermStartsAt) || !event.ExecutedAt.Before(document.TermExpiresAt) {
			return errors.New("reset event is outside term")
		}
	}
	for _, collection := range [][]int64{
		document.CycleIDs,
		document.QualificationIDs,
		document.LedgerIDs,
		{document.ResetEvents[0].BatchID, document.ResetEvents[1].BatchID},
		{document.ResetEvents[0].TargetID, document.ResetEvents[1].TargetID},
	} {
		ids := make(map[int64]struct{}, len(collection))
		for _, id := range collection {
			if id <= 0 {
				return errors.New("fixture id is invalid")
			}
			if _, duplicate := ids[id]; duplicate {
				return errors.New("fixture id is duplicated")
			}
			ids[id] = struct{}{}
		}
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
		return false, errors.New("manifest path must be a private regular file")
	}
	return true, nil
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

func publishPending(pending, output string, document fixtureDocument, dependencies importerDependencies) error {
	if err := dependencies.link(pending, output); err != nil {
		return failure("manifest", "publish", err)
	}
	if err := dependencies.syncDirectory(filepath.Dir(output)); err != nil {
		return failure("manifest", "directory_sync", err)
	}
	if err := os.Remove(pending); err != nil {
		return failure("manifest", "pending_remove", err)
	}
	if err := dependencies.syncDirectory(filepath.Dir(output)); err != nil {
		return failure("manifest", "directory_sync", err)
	}
	verified, err := readFixtureDocument(output)
	if err != nil || verified.User.ID != document.User.ID {
		return failure("manifest", "verification", err)
	}
	return nil
}

func readFixtureDocument(path string) (fixtureDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return fixtureDocument{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxManifestBytes+1))
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
		return fixtureDocument{}, errors.New("manifest has trailing data")
	}
	return document, nil
}

func hashLabel(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func failure(stage, category string, cause error) error {
	if cause == nil {
		cause = errors.New(category)
	}
	return &importerFailure{stage: stage, category: category, cause: cause}
}
