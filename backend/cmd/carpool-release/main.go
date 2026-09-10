// carpool-release imports approved subscription openings at a locked release gate.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	_ "github.com/lib/pq"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "release failed:", err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "preview", "preview, apply, verify, or schema")
	manifestPath := flag.String("manifest", "", "private approved JSON manifest")
	resultPath := flag.String("result", "", "new private JSON result file")
	flag.Parse()
	if *mode == "schema" {
		return migrateSchema()
	}
	if *mode != "preview" && *mode != "apply" && *mode != "verify" {
		return errors.New("unsupported mode")
	}
	if *manifestPath == "" || *resultPath == "" {
		return errors.New("manifest and result paths are required")
	}
	info, err := os.Stat(*manifestPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("manifest must be a private regular file (0600)")
	}
	f, err := os.Open(*manifestPath)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	var manifest repository.CarpoolReleaseManifest
	if err = decoder.Decode(&manifest); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing manifest content")
	}
	// Reserve the receipt path before touching the database. Never overwrite evidence.
	out, err := os.OpenFile(*resultPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	dsn := os.Getenv("CARPOOL_RELEASE_DATABASE_URL")
	if dsn == "" {
		return errors.New("CARPOOL_RELEASE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if *mode != "preview" {
		if err = verifyGate(ctx); err != nil {
			return err
		}
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return errors.New("invalid release database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		return errors.New("release database connection failed")
	}
	repo := repository.NewCarpoolRepository(db)
	var result *repository.CarpoolReleaseResult
	if *mode == "verify" {
		result, err = repo.VerifyRelease(ctx, manifest)
	} else {
		result, err = repo.ImportRelease(ctx, manifest, *mode == "apply")
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err = enc.Encode(result); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	fmt.Printf("%s succeeded: members=%d committed=%t replayed=%t fingerprint=%s\n", *mode, len(result.Members), result.Committed, result.Replayed, result.Fingerprint)
	return nil
}

func migrateSchema() error {
	dsn := os.Getenv("CARPOOL_RELEASE_DATABASE_URL")
	if dsn == "" {
		return errors.New("CARPOOL_RELEASE_DATABASE_URL is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return errors.New("invalid release database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var baseline string
	if err = db.QueryRowContext(ctx, `SELECT MAX(LEFT(filename,3)) FROM schema_migrations`).Scan(&baseline); err != nil {
		return err
	}
	if baseline < "234" || baseline > "244" {
		return errors.New("schema differs from reviewed release baseline")
	}
	if _, err = db.ExecContext(ctx, `SET lock_timeout='2s'; SET statement_timeout='30s'`); err != nil {
		return err
	}
	if err = repository.ApplyMigrations(ctx, db); err != nil {
		return err
	}
	fmt.Println("reviewed release schema applied successfully")
	return nil
}

func verifyGate(ctx context.Context) error {
	base, err := url.Parse(os.Getenv("CARPOOL_RELEASE_APP_URL"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("valid CARPOOL_RELEASE_APP_URL required")
	}
	operationID := os.Getenv("CARPOOL_RELEASE_OPERATION_ID")
	token := strings.TrimSpace(os.Getenv("CARPOOL_RELEASE_ADMIN_TOKEN"))
	adminKey := strings.TrimSpace(os.Getenv("CARPOOL_RELEASE_ADMIN_API_KEY"))
	if operationID == "" || (token == "" && adminKey == "") || (token != "" && adminKey != "") {
		return errors.New("release operation ID and administrator token required")
	}
	base.Path = "/api/v1/admin/release/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return err
	}
	if adminKey != "" {
		req.Header.Set("x-api-key", adminKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("cannot verify serving release gate")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("release gate authentication or status failed")
	}
	var body struct {
		Code *int `json:"code"`
		Data *struct {
			OperationID  string `json:"operation_id"`
			State        string `json:"state"`
			ActiveHTTP   *int64 `json:"active_http"`
			PendingUsage *int64 `json:"pending_usage"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err = decoder.Decode(&body); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing release status content")
	}
	s := body.Data
	if body.Code == nil || *body.Code != 0 || s == nil || s.ActiveHTTP == nil || s.PendingUsage == nil || s.State != "migrating" || s.OperationID != operationID || *s.ActiveHTTP != 0 || *s.PendingUsage != 0 {
		return errors.New("serving release gate is not locked and idle for this operation")
	}
	return nil
}
