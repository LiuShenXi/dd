package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
)

type CarpoolReleaseMember struct {
	UserID         int64           `json:"user_id"`
	Email          string          `json:"email"`
	PlanCode       string          `json:"plan_code"`
	WeeklyQuotaUSD decimal.Decimal `json:"weekly_quota_usd"`
	DurationDays   int             `json:"duration_days"`
}

type CarpoolReleaseManifest struct {
	BatchKey        string                 `json:"batch_key"`
	GroupID         int64                  `json:"group_id"`
	AdminUserID     int64                  `json:"admin_user_id"`
	AdminKeyIDs     []int64                `json:"admin_key_ids"`
	ExcludedUserIDs []int64                `json:"excluded_user_ids"`
	CycleAnchor     time.Time              `json:"cycle_anchor"`
	Members         []CarpoolReleaseMember `json:"members"`
}

type CarpoolReleaseMemberResult struct {
	UserID            int64                      `json:"user_id"`
	TermID            int64                      `json:"term_id,omitempty"`
	CycleID           int64                      `json:"cycle_id,omitempty"`
	StartsAt          time.Time                  `json:"starts_at"`
	ExpiresAt         time.Time                  `json:"expires_at"`
	CycleStartsAt     time.Time                  `json:"cycle_starts_at"`
	CycleEndsAt       time.Time                  `json:"cycle_ends_at"`
	OpeningBalanceUSD decimal.Decimal            `json:"opening_balance_usd"`
	Snapshot          domain.CarpoolPlanSnapshot `json:"plan_snapshot"`
}

type CarpoolReleaseResult struct {
	BatchKey        string                       `json:"batch_key"`
	GroupID         int64                        `json:"group_id"`
	Fingerprint     string                       `json:"fingerprint"`
	CapturedAt      time.Time                    `json:"captured_at"`
	Committed       bool                         `json:"committed"`
	Replayed        bool                         `json:"replayed"`
	Members         []CarpoolReleaseMemberResult `json:"members"`
	ProtectedDigest string                       `json:"protected_digest"`
	KeysDigest      string                       `json:"keys_digest"`
	GroupDigest     string                       `json:"group_digest"`
}

type releaseUser struct {
	id                  int64
	email, role, status string
	createdAt           time.Time
	balance, frozen     decimal.Decimal
}

func normalizeReleaseManifest(input CarpoolReleaseManifest) (CarpoolReleaseManifest, string, error) {
	m := input
	m.Members = append([]CarpoolReleaseMember(nil), input.Members...)
	m.AdminKeyIDs = append([]int64(nil), input.AdminKeyIDs...)
	m.ExcludedUserIDs = append([]int64(nil), input.ExcludedUserIDs...)
	m.CycleAnchor = m.CycleAnchor.UTC()
	if strings.TrimSpace(m.BatchKey) != m.BatchKey || m.BatchKey == "" || len(m.BatchKey) > 128 || m.GroupID <= 0 || m.AdminUserID <= 0 || m.CycleAnchor.IsZero() || len(m.Members) == 0 || len(m.Members) > 500 || len(m.AdminKeyIDs) == 0 {
		return m, "", fmt.Errorf("invalid release manifest")
	}
	seen := map[int64]bool{m.AdminUserID: true}
	emails := map[string]bool{}
	for i := range m.Members {
		v := &m.Members[i]
		v.Email = strings.ToLower(strings.TrimSpace(v.Email))
		if v.UserID <= 0 || seen[v.UserID] || v.Email == "" || emails[v.Email] || !v.WeeklyQuotaUSD.IsPositive() || !v.WeeklyQuotaUSD.Equal(v.WeeklyQuotaUSD.Round(8)) || (v.DurationDays != 7 && v.DurationDays != 28) {
			return m, "", fmt.Errorf("invalid or duplicate release member")
		}
		if v.PlanCode != "four_seat" && v.PlanCode != "three_seat" && v.PlanCode != "two_seat" {
			return m, "", fmt.Errorf("unsupported release tier")
		}
		seen[v.UserID], emails[v.Email] = true, true
	}
	for _, id := range m.ExcludedUserIDs {
		if id <= 0 || seen[id] {
			return m, "", fmt.Errorf("invalid excluded user")
		}
		seen[id] = true
	}
	keys := map[int64]bool{}
	for _, id := range m.AdminKeyIDs {
		if id <= 0 || keys[id] {
			return m, "", fmt.Errorf("invalid administrator key")
		}
		keys[id] = true
	}
	sort.Slice(m.Members, func(i, j int) bool { return m.Members[i].UserID < m.Members[j].UserID })
	sort.Slice(m.AdminKeyIDs, func(i, j int) bool { return m.AdminKeyIDs[i] < m.AdminKeyIDs[j] })
	sort.Slice(m.ExcludedUserIDs, func(i, j int) bool { return m.ExcludedUserIDs[i] < m.ExcludedUserIDs[j] })
	b, err := json.Marshal(m)
	if err != nil {
		return m, "", err
	}
	h := sha256.Sum256(b)
	return m, hex.EncodeToString(h[:]), nil
}

// ImportRelease must be called only while the serving application's release gate
// is locked and all other old-balance writers have been drained. The database
// role is deliberately unable to UPDATE users.balance, even indirectly by grants.
// Preview is a read-only transaction and never creates a partial import.
func (r *CarpoolRepository) ImportRelease(ctx context.Context, input CarpoolReleaseManifest, apply bool) (*CarpoolReleaseResult, error) {
	m, fp, err := normalizeReleaseManifest(input)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: !apply})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout='3s'`); err != nil {
		return nil, err
	}
	if apply {
		var writable bool
		if err = tx.QueryRowContext(ctx, `SELECT has_column_privilege(current_user,'users','balance','UPDATE')`).Scan(&writable); err != nil {
			return nil, err
		}
		if writable {
			return nil, fmt.Errorf("release role must not have UPDATE privilege on users.balance")
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "carpool-release:"+m.BatchKey); err != nil {
			return nil, err
		}
		if err = lockCarpoolScopeMembershipTx(ctx, tx, domain.CarpoolGlobalScopeID); err != nil {
			return nil, err
		}
	}
	var storedFP, stored string
	err = tx.QueryRowContext(ctx, `SELECT request_fingerprint,result::text FROM carpool_release_imports WHERE batch_key=$1`, m.BatchKey).Scan(&storedFP, &stored)
	if err == nil {
		if fp != storedFP {
			return nil, fmt.Errorf("release manifest conflicts with committed batch")
		}
		var result CarpoolReleaseResult
		if err = json.Unmarshal([]byte(stored), &result); err != nil {
			return nil, err
		}
		result.Replayed = true
		return &result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	if m.CycleAnchor.After(now) || !now.Before(m.CycleAnchor.Add(7*24*time.Hour)) {
		return nil, fmt.Errorf("release anchor does not cover current database time")
	}
	var groupBefore string
	groupQuery := `SELECT to_jsonb(g)::text FROM groups g WHERE id=$1 AND deleted_at IS NULL`
	if apply {
		groupQuery += ` FOR SHARE`
	}
	if err = tx.QueryRowContext(ctx, groupQuery, m.GroupID).Scan(&groupBefore); err != nil {
		return nil, err
	}
	var g struct {
		Status          string `json:"status"`
		Platform        string `json:"platform"`
		Type            string `json:"subscription_type"`
		Fallback        *int64 `json:"fallback_group_id"`
		InvalidFallback *int64 `json:"fallback_group_id_on_invalid_request"`
		Live            bool   `json:"allow_live"`
		Batch           bool   `json:"allow_batch_image_generation"`
	}
	if err = json.Unmarshal([]byte(groupBefore), &g); err != nil {
		return nil, err
	}
	if g.Status != "active" || g.Platform != "openai" || g.Type != "standard" || g.Fallback != nil || g.InvalidFallback != nil || g.Live || g.Batch {
		return nil, fmt.Errorf("source group is not supported for unchanged-routing takeover")
	}
	users, err := releaseUsersTx(ctx, tx, apply)
	if err != nil {
		return nil, err
	}
	if len(users) != 1+len(m.ExcludedUserIDs)+len(m.Members) {
		return nil, fmt.Errorf("live user count differs from approved manifest")
	}
	admin, ok := users[m.AdminUserID]
	if !ok || admin.role != "admin" || admin.status != "active" {
		return nil, fmt.Errorf("administrator identity mismatch")
	}
	var adminSubscriptions int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM carpool_terms WHERE user_id=$1)+(SELECT COUNT(*) FROM carpool_billing_bindings WHERE user_id=$1)`, m.AdminUserID).Scan(&adminSubscriptions); err != nil {
		return nil, err
	}
	if adminSubscriptions != 0 {
		return nil, fmt.Errorf("administrator already has subscription billing records")
	}
	for _, id := range m.ExcludedUserIDs {
		u, ok := users[id]
		if !ok || u.role != "user" || !u.balance.IsZero() || !u.frozen.IsZero() {
			return nil, fmt.Errorf("excluded user %d is not unactivated zero-balance user", id)
		}
	}
	keyBefore, err := releaseKeysTx(ctx, tx, apply)
	if err != nil {
		return nil, err
	}
	if err = validateReleaseKeys(ctx, tx, m); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	var jobs int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM batch_image_jobs WHERE status NOT IN ('completed','failed','cancelled','output_deleted') OR (status='completed' AND settled_at IS NULL)`).Scan(&jobs); err != nil {
		return nil, err
	}
	if jobs != 0 {
		return nil, fmt.Errorf("persistent batch image work remains")
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_orders WHERE status IN ('PENDING','PAID','RECHARGING','FAILED')`).Scan(&jobs); err != nil {
		return nil, err
	}
	if jobs != 0 {
		return nil, fmt.Errorf("unresolved payment work remains")
	}
	result := &CarpoolReleaseResult{BatchKey: m.BatchKey, GroupID: m.GroupID, Fingerprint: fp, CapturedAt: now, Committed: apply, Members: make([]CarpoolReleaseMemberResult, 0, len(m.Members))}
	result.ProtectedDigest = releaseUsersDigest(users)
	result.KeysDigest = keyBefore
	groupHash := sha256.Sum256([]byte(groupBefore))
	result.GroupDigest = hex.EncodeToString(groupHash[:])
	for _, member := range m.Members {
		u, ok := users[member.UserID]
		if !ok || u.role != "user" || u.status != "active" || strings.ToLower(strings.TrimSpace(u.email)) != member.Email || !u.frozen.IsZero() {
			return nil, fmt.Errorf("release member %d identity or frozen balance mismatch", member.UserID)
		}
		var conflicts int
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM carpool_terms WHERE user_id=$1)+(SELECT COUNT(*) FROM carpool_billing_bindings WHERE user_id=$1)`, u.id).Scan(&conflicts); err != nil {
			return nil, err
		}
		if conflicts != 0 {
			return nil, fmt.Errorf("release member %d already has carpool records", u.id)
		}
		plan, planErr := scanCarpoolPlan(tx.QueryRowContext(ctx, `SELECT id,code,name,list_price_cny::text,weekly_quota_usd::text,duration_days,cycle_days,boost_ratio::text,boost_count,enabled,version FROM carpool_plans WHERE code=$1 ORDER BY version DESC,id DESC LIMIT 1`, member.PlanCode))
		if planErr != nil {
			return nil, planErr
		}
		if !plan.Enabled {
			return nil, fmt.Errorf("release tier is disabled")
		}
		snapshot := plan.Snapshot()
		if !snapshot.WeeklyQuotaUSD.Equal(member.WeeklyQuotaUSD) {
			snapshot = snapshot.WithWeeklyQuotaOverride(member.WeeklyQuotaUSD)
		}
		if snapshot.DurationDays != member.DurationDays {
			snapshot = snapshot.WithDurationOverride(member.DurationDays)
		}
		takeover := domain.CarpoolTakeoverInput{CurrentCycleStartsAt: &m.CycleAnchor, CurrentBaseBalanceUSD: u.balance, HistoryComplete: false, StatisticsSince: &now}
		cycles, cycleErr := domain.BuildCarpoolTakeoverCycles(u.createdAt, now, snapshot, &takeover)
		if cycleErr != nil {
			return nil, fmt.Errorf("user %d cycle: %w", u.id, cycleErr)
		}
		if len(cycles) != 1 {
			return nil, fmt.Errorf("release requires one actual current cycle")
		}
		item := CarpoolReleaseMemberResult{UserID: u.id, StartsAt: u.createdAt, ExpiresAt: u.createdAt.Add(time.Duration(member.DurationDays) * 24 * time.Hour), CycleStartsAt: cycles[0].StartsAt, CycleEndsAt: cycles[0].EndsAt, OpeningBalanceUSD: u.balance, Snapshot: snapshot}
		if apply {
			if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_billing_bindings(user_id,group_id) VALUES($1,$2)`, u.id, m.GroupID); err != nil {
				return nil, err
			}
			body, marshalErr := json.Marshal(snapshot)
			if marshalErr != nil {
				return nil, marshalErr
			}
			if err = tx.QueryRowContext(ctx, `INSERT INTO carpool_terms(user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,status,boost_used,source_mode,history_complete,statistics_since,created_by,notes) VALUES($1,1,$2,$3,$4::jsonb,$5,$6,'active',0,'takeover',FALSE,$7,$8,$9) RETURNING id`, u.id, m.GroupID, plan.ID, string(body), item.StartsAt, item.ExpiresAt, now, m.AdminUserID, "balance-preserving release "+m.BatchKey).Scan(&item.TermID); err != nil {
				return nil, err
			}
			if err = tx.QueryRowContext(ctx, `INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at) VALUES($1,1,$2,$3,$4,$5,0,0,'active',$6) RETURNING id`, item.TermID, item.CycleStartsAt, item.CycleEndsAt, snapshot.WeeklyQuotaUSD.StringFixed(8), u.balance.StringFixed(8), now).Scan(&item.CycleID); err != nil {
				return nil, err
			}
			if err = insertLedgerTx(ctx, tx, u.id, item.TermID, item.CycleID, "takeover_opening", domain.CarpoolBucketBase, u.balance, fmt.Sprintf("release:%s:%d:opening", m.BatchKey, u.id), nil, nil, nil, nil, &m.AdminUserID, nil, "remaining quota imported without modifying ordinary balance", now); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE users SET restrict_public_groups=TRUE,updated_at=NOW() WHERE id=$1`, u.id); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM user_allowed_groups WHERE user_id=$1`, u.id); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)`, u.id, m.GroupID); err != nil {
				return nil, err
			}
		}
		result.Members = append(result.Members, item)
	}
	if !apply {
		return result, nil
	}
	var groupAfter string
	if err = tx.QueryRowContext(ctx, `SELECT to_jsonb(g)::text FROM groups g WHERE id=$1`, m.GroupID).Scan(&groupAfter); err != nil {
		return nil, err
	}
	if groupAfter != groupBefore {
		return nil, fmt.Errorf("release changed shared group")
	}
	keyAfter, err := releaseKeysTx(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	if keyBefore != keyAfter {
		return nil, fmt.Errorf("release changed API keys")
	}
	after, err := releaseUsersTx(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	for id, before := range users {
		u, ok := after[id]
		if !ok || !u.balance.Equal(before.balance) || !u.frozen.Equal(before.frozen) || !u.createdAt.Equal(before.createdAt) {
			return nil, fmt.Errorf("release changed protected user fields for %d", id)
		}
	}
	var commitTime time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&commitTime); err != nil {
		return nil, err
	}
	for _, item := range result.Members {
		if !commitTime.Before(item.CycleEndsAt) {
			return nil, fmt.Errorf("member %d current cycle expired during release", item.UserID)
		}
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_release_imports(batch_key,request_fingerprint,actor_id,group_id,cycle_anchor,result) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, m.BatchKey, fp, m.AdminUserID, m.GroupID, m.CycleAnchor, string(body)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("release commit outcome must be verified before resume: %w", err)
	}
	return result, nil
}

func releaseUsersDigest(users map[int64]releaseUser) string {
	ids := make([]int64, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	h := sha256.New()
	for _, id := range ids {
		u := users[id]
		fmt.Fprintf(h, "%d:%s:%s:%s\n", id, u.balance.StringFixed(8), u.frozen.StringFixed(8), u.createdAt.UTC().Format(time.RFC3339Nano))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyRelease verifies the committed opening state before any request is resumed.
// After live usage resumes, opening balances are intentionally no longer equal.
func (r *CarpoolRepository) VerifyRelease(ctx context.Context, input CarpoolReleaseManifest) (*CarpoolReleaseResult, error) {
	m, fp, err := normalizeReleaseManifest(input)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var storedFP, raw string
	if err = tx.QueryRowContext(ctx, `SELECT request_fingerprint,result::text FROM carpool_release_imports WHERE batch_key=$1`, m.BatchKey).Scan(&storedFP, &raw); err != nil {
		return nil, err
	}
	if fp != storedFP {
		return nil, fmt.Errorf("release manifest conflicts with committed batch")
	}
	var result CarpoolReleaseResult
	if err = json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	users, err := releaseUsersTx(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	keys, err := releaseKeysTx(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	var group string
	if err = tx.QueryRowContext(ctx, `SELECT to_jsonb(g)::text FROM groups g WHERE id=$1`, m.GroupID).Scan(&group); err != nil {
		return nil, err
	}
	groupHash := sha256.Sum256([]byte(group))
	if releaseUsersDigest(users) != result.ProtectedDigest || keys != result.KeysDigest || hex.EncodeToString(groupHash[:]) != result.GroupDigest {
		return nil, fmt.Errorf("protected balance, registration, key, or group state differs from cutover receipt")
	}
	if len(result.Members) != len(m.Members) {
		return nil, fmt.Errorf("receipt member count mismatch")
	}
	for _, item := range result.Members {
		var valid bool
		snapshot, marshalErr := json.Marshal(item.Snapshot)
		if marshalErr != nil {
			return nil, marshalErr
		}
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM carpool_terms t JOIN carpool_cycles c ON c.term_id=t.id JOIN carpool_billing_bindings b ON b.user_id=t.user_id JOIN users u ON u.id=t.user_id WHERE t.id=$1 AND c.id=$2 AND t.user_id=$3 AND t.group_id=$4 AND b.group_id=$4 AND t.starts_at=$5 AND t.expires_at=$6 AND c.starts_at=$7 AND c.ends_at=$8 AND c.base_balance_usd=$9 AND c.base_quota_usd=$10 AND t.plan_snapshot=$11::jsonb AND t.status='active' AND c.state='active' AND t.boost_used=0 AND c.boost_balance_usd=0 AND c.manual_balance_usd=0 AND u.restrict_public_groups AND (SELECT COUNT(*) FROM user_allowed_groups a WHERE a.user_id=u.id)=1 AND EXISTS(SELECT 1 FROM user_allowed_groups a WHERE a.user_id=u.id AND a.group_id=$4) AND (SELECT COUNT(*) FROM carpool_cycles x WHERE x.term_id=t.id)=1 AND (SELECT COALESCE(SUM(l.delta_usd),0) FROM carpool_ledger l WHERE l.term_id=t.id)=$9 AND NOT EXISTS(SELECT 1 FROM carpool_ledger l WHERE l.term_id=t.id AND l.event_type<>'takeover_opening'))`, item.TermID, item.CycleID, item.UserID, m.GroupID, item.StartsAt, item.ExpiresAt, item.CycleStartsAt, item.CycleEndsAt, item.OpeningBalanceUSD.StringFixed(8), item.Snapshot.WeeklyQuotaUSD.StringFixed(8), string(snapshot)).Scan(&valid)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, fmt.Errorf("imported member %d fails opening verification", item.UserID)
		}
	}
	return &result, nil
}

func releaseUsersTx(ctx context.Context, tx *sql.Tx, lock bool) (map[int64]releaseUser, error) {
	query := `SELECT id,email,role,status,created_at,balance::text,COALESCE(frozen_balance,0)::text FROM users WHERE deleted_at IS NULL ORDER BY id`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]releaseUser{}
	for rows.Next() {
		var u releaseUser
		var b, f string
		if err = rows.Scan(&u.id, &u.email, &u.role, &u.status, &u.createdAt, &b, &f); err != nil {
			return nil, err
		}
		u.balance, err = decimal.NewFromString(b)
		if err != nil {
			return nil, err
		}
		u.frozen, err = decimal.NewFromString(f)
		if err != nil {
			return nil, err
		}
		out[u.id] = u
	}
	return out, rows.Err()
}

func releaseKeysTx(ctx context.Context, tx *sql.Tx, lock bool) (string, error) {
	query := `SELECT id,to_jsonb(k)::text FROM api_keys k WHERE deleted_at IS NULL ORDER BY id`
	if lock {
		query += ` FOR SHARE`
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		var id int64
		var body string
		if err = rows.Scan(&id, &body); err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%d:%d:%s\n", id, len(body), body)
	}
	return hex.EncodeToString(h.Sum(nil)), rows.Err()
}

func validateReleaseKeys(ctx context.Context, tx *sql.Tx, m CarpoolReleaseManifest) error {
	memberIDs := map[int64]bool{}
	excluded := map[int64]bool{}
	adminKeys := map[int64]bool{}
	for _, v := range m.Members {
		memberIDs[v.UserID] = true
	}
	for _, id := range m.ExcludedUserIDs {
		excluded[id] = true
	}
	for _, id := range m.AdminKeyIDs {
		adminKeys[id] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,user_id,group_id,status FROM api_keys WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var id, user int64
		var group sql.NullInt64
		var status string
		if err = rows.Scan(&id, &user, &group, &status); err != nil {
			return err
		}
		if user == m.AdminUserID {
			if !adminKeys[id] || !group.Valid || group.Int64 != m.GroupID || status != "active" {
				return fmt.Errorf("administrator key scope changed")
			}
			found[id] = true
		} else if memberIDs[user] {
			if !group.Valid || group.Int64 != m.GroupID {
				return fmt.Errorf("member %d key route differs from approved group", user)
			}
		} else if excluded[user] {
			return fmt.Errorf("excluded user %d has a key", user)
		} else {
			return fmt.Errorf("key belongs to an unapproved user")
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(found) != len(adminKeys) {
		return fmt.Errorf("administrator keys missing")
	}
	return nil
}
