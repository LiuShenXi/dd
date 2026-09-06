package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func validFixtureForTest() fixtureFile {
	fixture := fixtureFile{Version: 1, Source: fixtureSource, BaseURL: "http://carpool-app:8080", MockURL: "http://carpool-mock:8090"}
	for index, name := range requiredFixtureNames {
		fixture.Users = append(fixture.Users, fixtureUser{Name: name, ID: int64(index + 1), Email: bootstrapFixtureEmail(name), Password: strings.Repeat("a", fixturePasswordBytes*2)})
	}
	return fixture
}

func TestValidateFixtureAcceptsOnlySyntheticContract(t *testing.T) {
	fixture := validFixtureForTest()
	if err := validateFixture(fixture); err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
	fixture.Source = "production"
	if err := validateFixture(fixture); err == nil {
		t.Fatal("non-synthetic source accepted")
	}
}

func TestValidateFixtureRejectsDuplicateIDs(t *testing.T) {
	fixture := validFixtureForTest()
	fixture.Users[1].ID = fixture.Users[0].ID
	if err := validateFixture(fixture); err == nil {
		t.Fatal("duplicate fixture IDs accepted")
	}
}

func TestValidateFixtureRejectsUntrustedBootstrapProvenance(t *testing.T) {
	fixture := validFixtureForTest()
	fixture.Users[1].Email = "copied-user@example.invalid"
	if err := validateFixture(fixture); err == nil {
		t.Fatal("unexpected bootstrap email accepted")
	}
	fixture = validFixtureForTest()
	fixture.Users[1].Password = "not-bootstrap-generated"
	if err := validateFixture(fixture); err == nil {
		t.Fatal("unexpected bootstrap password accepted")
	}
	fixture = validFixtureForTest()
	fixture.Users = append(fixture.Users, fixtureUser{Name: "extra", ID: 999, Email: bootstrapFixtureEmail("extra"), Password: strings.Repeat("b", fixturePasswordBytes*2)})
	if err := validateFixture(fixture); err == nil {
		t.Fatal("extra fixture user accepted")
	}
}

func TestValidateLoginIdentityRequiresExpectedAdmin(t *testing.T) {
	expected := validFixtureForTest().Users[0]
	data := map[string]any{"user": map[string]any{"id": json.Number("1"), "email": expected.Email, "role": "admin"}}
	if err := validateLoginIdentity(data, expected, true); err != nil {
		t.Fatalf("valid admin identity rejected: %v", err)
	}
	data["user"].(map[string]any)["id"] = json.Number("2")
	if err := validateLoginIdentity(data, expected, true); err == nil {
		t.Fatal("wrong authenticated admin id accepted")
	}
}

func TestWriteBatchFixtureIsPrivateAndRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch.json")
	fixture := validFixtureForTest()
	if err := writeBatchFixture(path, fixture); err != nil {
		t.Fatalf("write batch fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat batch fixture: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("batch fixture mode = %o, want 600", info.Mode().Perm())
	}
	if err := writeBatchFixture(path, fixture); err == nil {
		t.Fatal("existing batch fixture was overwritten")
	}
}

func TestCreatedFixtureUserValidatesReturnedIdentityAndBalance(t *testing.T) {
	spec := syntheticUserSpec{Name: "ordinary", Balance: 100, Concurrency: 20}
	data := map[string]any{
		"id": json.Number("101"), "email": "carpool-accept-run-ordinary@example.invalid",
		"role": "user", "status": "active", "concurrency": json.Number("20"), "balance": json.Number("100"),
	}
	user, err := createdFixtureUser(spec, data["email"].(string), strings.Repeat("c", 48), data, map[int64]struct{}{1: {}})
	if err != nil {
		t.Fatalf("valid created user rejected: %v", err)
	}
	if user.ID != 101 || user.Name != "ordinary" {
		t.Fatalf("unexpected created fixture: %#v", user)
	}
	data["balance"] = json.Number("99")
	if _, err := createdFixtureUser(spec, data["email"].(string), strings.Repeat("c", 48), data, map[int64]struct{}{}); err == nil {
		t.Fatal("wrong created balance accepted")
	}
}

func TestAPIClientsRejectURLVariants(t *testing.T) {
	for _, candidate := range []string{
		"http://carpool-app:8080/",
		"http://carpool-app:8080?target=other",
		"http://user@carpool-app:8080",
		"https://carpool-app:8080",
		"http://127.0.0.1:8080",
	} {
		if _, err := newAPIClient(candidate, applicationBaseURL); err == nil {
			t.Fatalf("application URL variant accepted: %q", candidate)
		}
	}
	if _, err := newAPIClient(mockBaseURL, mockBaseURL); err != nil {
		t.Fatalf("exact mock URL rejected: %v", err)
	}
}

func TestLoadFixtureRejectsTrailingJSON(t *testing.T) {
	fixture := validFixtureForTest()
	encoded, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, append(encoded, []byte("\n{}")...), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := loadFixture(path); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestValidateEmptyPaginatedResultAcceptsOnlyZeroResultRepresentations(t *testing.T) {
	for _, items := range []any{nil, []any{}} {
		page := map[string]any{"items": items, "total": json.Number("0")}
		if err := validateEmptyPaginatedResult(page); err != nil {
			t.Fatalf("valid zero-result page rejected for items %#v: %v", items, err)
		}
	}

	tests := []struct {
		name string
		page map[string]any
	}{
		{name: "items absent", page: map[string]any{"total": json.Number("0")}},
		{name: "items non-array", page: map[string]any{"items": "", "total": json.Number("0")}},
		{name: "items object", page: map[string]any{"items": map[string]any{}, "total": json.Number("0")}},
		{name: "nonempty items", page: map[string]any{"items": []any{map[string]any{"id": json.Number("1")}}, "total": json.Number("0")}},
		{name: "positive total with null items", page: map[string]any{"items": nil, "total": json.Number("1")}},
		{name: "string total", page: map[string]any{"items": nil, "total": "0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateEmptyPaginatedResult(test.page); err == nil {
				t.Fatal("invalid zero-result page accepted")
			}
		})
	}
}

func TestDetailsWhitelistRejectsAdditionalFields(t *testing.T) {
	valid := map[string]any{
		"server_now":   "2026-09-06T12:00:00+08:00",
		"timezone":     "Asia/Shanghai",
		"usage":        map[string]any{"total_used_usd": "0.00000000", "history_complete": true, "statistics_since": nil},
		"quota":        nil,
		"term":         nil,
		"reset_window": map[string]any{"status": "none", "scheduled_at": nil, "schedule_revision": json.Number("0"), "eligible_for_me": false, "ineligible_reason": nil},
	}
	if err := validateDetailsDTO(valid); err != nil {
		t.Fatalf("valid details rejected: %v", err)
	}
	valid["ledger"] = []any{}
	if err := validateDetailsDTO(valid); err == nil {
		t.Fatal("operational field accepted")
	}
}

func TestBoostWhitelistRejectsAdditionalFields(t *testing.T) {
	valid := map[string]any{
		"eligible": true, "remaining": json.Number("2"), "total": json.Number("2"),
		"amount_usd": "55.00000000", "help_text": "Synthetic help", "unavailable_reason": nil,
	}
	if err := validateBoostDTO(valid); err != nil {
		t.Fatalf("valid boost details rejected: %v", err)
	}
	valid["term_id"] = json.Number("1")
	if err := validateBoostDTO(valid); err == nil {
		t.Fatal("operational boost field accepted")
	}
}

func TestValidateBoostStateRequiresExactTransition(t *testing.T) {
	cases := []struct {
		name       string
		eligible   bool
		remaining  string
		reason     any
		wantReason string
	}{
		{name: "available", eligible: true, remaining: "2", reason: nil},
		{name: "last claim", eligible: false, remaining: "0", reason: "boosts_exhausted", wantReason: "boosts_exhausted"},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			data := map[string]any{
				"eligible": current.eligible, "remaining": json.Number(current.remaining), "total": json.Number("2"),
				"amount_usd": "55.00000000", "help_text": "Synthetic help", "unavailable_reason": current.reason,
			}
			if err := validateBoostState(data, current.eligible, mustRat(current.remaining).Num().Int64(), "55", current.wantReason); err != nil {
				t.Fatalf("valid state rejected: %v", err)
			}
			data["amount_usd"] = "54.99999999"
			if err := validateBoostState(data, current.eligible, mustRat(current.remaining).Num().Int64(), "55", current.wantReason); err == nil {
				t.Fatal("wrong amount accepted")
			}
		})
	}
}

func validActiveDetailsForTest() map[string]any {
	start := time.Date(2026, 9, 6, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	days := []int{7, 7, 7, 7}
	cycles := make([]any, 0, len(days))
	current := start
	for index, duration := range days {
		end := current.Add(time.Duration(duration) * 24 * time.Hour)
		status := "scheduled"
		if index == 0 {
			status = "active"
		}
		cycles = append(cycles, map[string]any{
			"cycle_no": json.Number(strconv.Itoa(index + 1)), "starts_at": current.Format(time.RFC3339),
			"ends_at": end.Format(time.RFC3339), "status": status,
		})
		current = end
	}
	return map[string]any{
		"server_now": "2026-09-06T12:00:00+08:00", "timezone": "Asia/Shanghai",
		"usage": map[string]any{"total_used_usd": "1.00000000", "history_complete": true, "statistics_since": nil},
		"quota": map[string]any{"available_usd": "549.00000000"},
		"term": map[string]any{
			"status": "active", "starts_at": start.Format(time.RFC3339), "expires_at": current.Format(time.RFC3339),
			"reset_count": json.Number("0"), "reset_count_basis": "current_term", "reset_events": []any{}, "current_cycle_no": json.Number("1"), "cycles": cycles,
		},
		"reset_window": map[string]any{"status": "none", "scheduled_at": nil, "schedule_revision": json.Number("0"), "eligible_for_me": false, "ineligible_reason": nil},
	}
}

func TestValidateDetailsDTORejectsMalformedSemanticFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "server time", mutate: func(data map[string]any) { data["server_now"] = "not-a-time" }},
		{name: "timezone", mutate: func(data map[string]any) { data["timezone"] = "UTC" }},
		{name: "reset status", mutate: func(data map[string]any) { data["reset_window"].(map[string]any)["status"] = "unknown" }},
		{name: "scheduled time", mutate: func(data map[string]any) { data["reset_window"].(map[string]any)["scheduled_at"] = "tomorrow" }},
		{name: "none with revision", mutate: func(data map[string]any) {
			data["reset_window"].(map[string]any)["schedule_revision"] = json.Number("1")
		}},
		{name: "eligible with reason", mutate: func(data map[string]any) {
			reset := data["reset_window"].(map[string]any)
			reset["eligible_for_me"] = true
			reset["ineligible_reason"] = "term_not_covered"
		}},
		{name: "scheduled ineligible without reason", mutate: func(data map[string]any) {
			reset := data["reset_window"].(map[string]any)
			reset["status"] = "scheduled"
			reset["scheduled_at"] = "2026-09-07T02:00:00+08:00"
		}},
		{name: "term status", mutate: func(data map[string]any) { data["term"].(map[string]any)["status"] = "unknown" }},
		{name: "term duration", mutate: func(data map[string]any) { data["term"].(map[string]any)["expires_at"] = "2026-10-07T12:00:00+08:00" }},
		{name: "current cycle", mutate: func(data map[string]any) { data["term"].(map[string]any)["current_cycle_no"] = json.Number("2") }},
		{name: "cycle status", mutate: func(data map[string]any) {
			data["term"].(map[string]any)["cycles"].([]any)[0].(map[string]any)["status"] = "unknown"
		}},
	}
	if err := validateDetailsDTO(validActiveDetailsForTest()); err != nil {
		t.Fatalf("valid active details rejected: %v", err)
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			data := validActiveDetailsForTest()
			current.mutate(data)
			if err := validateDetailsDTO(data); err == nil {
				t.Fatal("malformed details accepted")
			}
		})
	}
}

func TestValidateDetailsQuotaRequiresNonNegativeDecimalString(t *testing.T) {
	for _, current := range []struct {
		name  string
		value any
		valid bool
	}{
		{name: "zero", value: "0.00000000", valid: true},
		{name: "positive", value: "715.00000000", valid: true},
		{name: "number is not decimal string", value: json.Number("715"), valid: false},
		{name: "negative", value: "-0.00000001", valid: false},
	} {
		t.Run(current.name, func(t *testing.T) {
			data := validActiveDetailsForTest()
			data["quota"].(map[string]any)["available_usd"] = current.value
			err := validateDetailsDTO(data)
			if current.valid && err != nil {
				t.Fatalf("valid quota rejected: %v", err)
			}
			if !current.valid && err == nil {
				t.Fatal("invalid quota accepted")
			}
		})
	}

	data := validActiveDetailsForTest()
	data["quota"] = nil
	if err := validateDetailsDTO(data); err == nil {
		t.Fatal("active term without quota accepted")
	}
}

func TestValidateDetailsResetEventsRequireSuccessfulProjectionScalars(t *testing.T) {
	data := validActiveDetailsForTest()
	term := data["term"].(map[string]any)
	term["reset_count"] = json.Number("2")
	term["reset_events"] = []any{
		map[string]any{"cycle_no": json.Number("1"), "occurred_at": "2026-09-07T22:00:00+08:00", "target_quota_usd": "550.00000000"},
		map[string]any{"cycle_no": json.Number("1"), "occurred_at": "2026-09-09T22:00:00+08:00", "target_quota_usd": "550.00000000"},
	}
	if err := validateDetailsDTO(data); err != nil {
		t.Fatalf("valid reset event projection rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "numeric target", mutate: func(event map[string]any) { event["target_quota_usd"] = json.Number("550") }},
		{name: "zero target", mutate: func(event map[string]any) { event["target_quota_usd"] = "0.00000000" }},
		{name: "wrong cycle", mutate: func(event map[string]any) { event["cycle_no"] = json.Number("2") }},
		{name: "unknown field", mutate: func(event map[string]any) { event["batch_id"] = json.Number("1") }},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			candidate := validActiveDetailsForTest()
			candidateTerm := candidate["term"].(map[string]any)
			candidateTerm["reset_count"] = json.Number("1")
			event := map[string]any{"cycle_no": json.Number("1"), "occurred_at": "2026-09-07T22:00:00+08:00", "target_quota_usd": "550.00000000"}
			current.mutate(event)
			candidateTerm["reset_events"] = []any{event}
			if err := validateDetailsDTO(candidate); err == nil {
				t.Fatal("invalid reset event accepted")
			}
		})
	}
}

func TestValidateAdminPlanRequiresLatestProjection(t *testing.T) {
	expected := planExpectations[0]
	plan := map[string]any{
		"plan_id": json.Number("7"), "code": expected.Code, "name": expected.Name, "version": json.Number("3"),
		"list_price_cny": expected.ListPrice, "weekly_quota_usd": expected.Weekly, "cycle_5_quota_usd": "157",
		"duration_days": json.Number("28"), "cycle_days": json.Number("7"), "boost_ratio": "0.1", "boost_amount_usd": expected.Boost,
		"boost_count": json.Number("2"), "rounding_mode": "half_up", "enabled": true, "is_latest": true,
	}
	if err := validateAdminPlan(plan, expected, 7, 3); err != nil {
		t.Fatalf("valid admin plan rejected: %v", err)
	}
	plan["is_latest"] = false
	if err := validateAdminPlan(plan, expected, 7, 3); err == nil {
		t.Fatal("nonlatest plan accepted")
	}
}

func TestValidateBillingDeltaRequiresEachRequestExactlyOnce(t *testing.T) {
	before := billingState{used: mustRat("4"), available: mustRat("696"), ordinaryBalance: mustRat("0")}
	after := billingState{used: mustRat("5"), available: mustRat("695"), ordinaryBalance: mustRat("0")}
	if err := validateBillingDelta(before, after, "1"); err != nil {
		t.Fatalf("exact debit rejected: %v", err)
	}
	after.used = mustRat("6")
	if err := validateBillingDelta(before, after, "1"); err == nil {
		t.Fatal("double usage debit accepted")
	}
	after = billingState{used: mustRat("5"), available: mustRat("695"), ordinaryBalance: mustRat("1")}
	if err := validateBillingDelta(before, after, "1"); err == nil {
		t.Fatal("ordinary balance mutation accepted")
	}
}

func TestValidateCycleWindowsRequiresFourWeeks(t *testing.T) {
	start := time.Date(2026, 9, 6, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	days := []int{7, 7, 7, 7}
	cycles := make([]any, 0, len(days))
	current := start
	for index, duration := range days {
		end := current.Add(time.Duration(duration) * 24 * time.Hour)
		cycles = append(cycles, map[string]any{
			"cycle_no":  json.Number(strconv.Itoa(index + 1)),
			"starts_at": current.Format(time.RFC3339),
			"ends_at":   end.Format(time.RFC3339),
		})
		current = end
	}
	if err := validateCycleWindows(cycles); err != nil {
		t.Fatalf("valid cycle windows rejected: %v", err)
	}
	cycles = append(cycles, map[string]any{
		"cycle_no": json.Number("5"), "starts_at": current.Format(time.RFC3339), "ends_at": current.Add(2 * 24 * time.Hour).Format(time.RFC3339),
	})
	if err := validateCycleWindows(cycles); err == nil {
		t.Fatal("legacy fifth cycle accepted for a new plan")
	}
}

func TestDecimalComparisonIsExact(t *testing.T) {
	if !decimalEquals("157.00000000", "157") {
		t.Fatal("equivalent decimal strings did not compare equal")
	}
	if decimalEquals("156.99999999", "157") {
		t.Fatal("different decimal strings compared equal")
	}
}
