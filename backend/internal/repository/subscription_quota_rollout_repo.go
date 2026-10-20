package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func NewSubscriptionQuotaRolloutRepository(db *sql.DB) service.SubscriptionQuotaRolloutRepository {
	return &subscriptionQuotaRepository{db: db}
}
func (r *subscriptionQuotaRepository) RegisterReplica(ctx context.Context, in domain.SubscriptionQuotaReplica) error {
	if r == nil || r.db == nil || strings.TrimSpace(in.ReplicaID) == "" || len(in.ReplicaID) > 128 || in.InstanceToken == "" || len(in.InstanceToken) > 128 {
		return service.ErrSubscriptionQuotaInput
	}
	// A running process may only advertise the capability after validating the
	// actual migrated database. Test/admin HTTP cannot submit this registration.
	if err := r.CheckBillingReadiness(ctx); err != nil {
		return err
	}
	sort.Strings(in.ExpectedReplicaIDs)
	sort.Slice(in.ProtectedGroupIDs, func(i, j int) bool { return in.ProtectedGroupIDs[i] < in.ProtectedGroupIDs[j] })
	sort.Slice(in.EnabledGroupIDs, func(i, j int) bool { return in.EnabledGroupIDs[i] < in.EnabledGroupIDs[j] })
	in.HeartbeatAt = time.Time{}
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO subscription_quota_runtime_replicas(replica_id,instance_token,capabilities) VALUES($1,$2,$3) ON CONFLICT(replica_id,instance_token) DO UPDATE SET capabilities=EXCLUDED.capabilities,heartbeat_at=clock_timestamp()`, in.ReplicaID, in.InstanceToken, data)
	return err
}
func rolloutLock(ctx context.Context, tx *sql.Tx, groupID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,1397839188))`, fmt.Sprintf("subscription_quota_rollout:%d", groupID))
	return err
}
func rolloutAdmin(ctx context.Context, q subscriptionQuotaQuerier, actor int64) error {
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, actor).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return service.ErrSubscriptionQuotaNotFound
	}
	return nil
}
func rolloutHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func rolloutSetWriter(ctx context.Context, tx *sql.Tx, groupID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('sub2api.quota_rollout_group',$1,true)`, fmt.Sprint(groupID))
	return err
}
func rolloutInvalidateGroup(ctx context.Context, tx *sql.Tx, groupID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO auth_cache_invalidation_outbox(cache_key) SELECT encode(sha256(convert_to(key,'UTF8')),'hex') FROM api_keys WHERE group_id=$1 AND deleted_at IS NULL`, groupID)
	return err
}
func rolloutReplay(ctx context.Context, tx *sql.Tx, groupID int64, key, kind, fingerprint string, out any) (bool, error) {
	var storedKind, storedFingerprint string
	var data []byte
	err := tx.QueryRowContext(ctx, `SELECT operation_kind,request_fingerprint,result FROM subscription_quota_rollout_operations WHERE group_id=$1 AND idempotency_key=$2`, groupID, key).Scan(&storedKind, &storedFingerprint, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if storedKind != kind || storedFingerprint != fingerprint {
		return false, service.ErrSubscriptionQuotaIdempotency
	}
	return true, json.Unmarshal(data, out)
}
func rolloutSave(ctx context.Context, tx *sql.Tx, groupID, actorID int64, key, kind, fingerprint string, out any) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_quota_rollout_operations(group_id,actor_id,idempotency_key,operation_kind,request_fingerprint,result) VALUES($1,$2,$3,$4,$5,$6)`, groupID, actorID, key, kind, fingerprint, data)
	return err
}

type rolloutView struct {
	state                          domain.SubscriptionQuotaRolloutState
	fingerprint                    string
	platform, kind, originalStatus string
	fallback                       bool
}

func rolloutContains(ids []int64, id int64) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
func rolloutReplicaBlockers(replicas []domain.SubscriptionQuotaReplica, groupID int64, policy domain.SubscriptionQuotaRolloutPolicy) []string {
	blockers := []string{}
	want := append([]string{}, policy.ExpectedReplicaIDs...)
	sort.Strings(want)
	if len(want) == 0 {
		return []string{"REPLICA_ROSTER_UNCONFIGURED"}
	}
	expected := map[string]bool{}
	for _, id := range want {
		if id == "" || expected[id] {
			return []string{"REPLICA_ROSTER_UNCONFIGURED"}
		}
		expected[id] = true
	}
	seen := map[string]int{}
	for _, replica := range replicas {
		seen[replica.ReplicaID]++
		if len(replicas) > 0 && (!reflect.DeepEqual(replica.ProtectedGroupIDs, replicas[0].ProtectedGroupIDs) || !reflect.DeepEqual(replica.EnabledGroupIDs, replicas[0].EnabledGroupIDs)) {
			blockers = append(blockers, "REPLICA_CONFIG_MISMATCH")
		}
		if !expected[replica.ReplicaID] {
			blockers = append(blockers, "REPLICA_UNEXPECTED")
		}
		if replica.CapabilityVersion != domain.SubscriptionQuotaRolloutCapabilityVersion || replica.ReceiptVersion != 1 || replica.LeaseProtocolVersion != domain.SubscriptionQuotaLeaseProtocolVersion {
			blockers = append(blockers, "REPLICA_CAPABILITY_MISMATCH")
		}
		roster := append([]string{}, replica.ExpectedReplicaIDs...)
		sort.Strings(roster)
		if replica.ClaudeDefaultConcurrency != 3 || !replica.IssueEnabled || !replica.AdmissionEnabled || !replica.RecoveryEnabled || !rolloutContains(replica.ProtectedGroupIDs, groupID) || !rolloutContains(replica.EnabledGroupIDs, groupID) || !reflect.DeepEqual(roster, want) {
			blockers = append(blockers, "REPLICA_CONFIG_MISMATCH")
		}
	}
	for _, id := range want {
		if seen[id] == 0 {
			blockers = append(blockers, "REPLICA_MISSING_OR_STALE")
		}
		if seen[id] > 1 {
			blockers = append(blockers, "REPLICA_DUPLICATE")
		}
	}
	sort.Strings(blockers)
	unique := blockers[:0]
	for _, code := range blockers {
		if len(unique) == 0 || unique[len(unique)-1] != code {
			unique = append(unique, code)
		}
	}
	return unique
}
func rolloutRead(ctx context.Context, q subscriptionQuotaQuerier, groupID int64, allocations []domain.SubscriptionQuotaAllocation, policy domain.SubscriptionQuotaRolloutPolicy) (rolloutView, error) {
	v := rolloutView{state: domain.SubscriptionQuotaRolloutState{GroupID: groupID, Allocations: append([]domain.SubscriptionQuotaAllocation{}, allocations...), KeyUsers: []domain.SubscriptionQuotaRolloutKeyUser{}, Replicas: []domain.SubscriptionQuotaReplica{}, Blockers: []string{}}}
	var groupJSON string
	var fallback, invalid sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT to_jsonb(g)::text,platform,subscription_type,subscription_quota_mode,status,fallback_group_id,fallback_group_id_on_invalid_request FROM groups g WHERE id=$1 AND deleted_at IS NULL`, groupID).Scan(&groupJSON, &v.platform, &v.kind, &v.state.QuotaMode, &v.state.GroupStatus, &fallback, &invalid)
	if errors.Is(err, sql.ErrNoRows) {
		return v, service.ErrSubscriptionQuotaNotFound
	}
	if err != nil {
		return v, err
	}
	v.fallback = fallback.Valid || invalid.Valid
	var token sql.NullString
	var savedAllocations []byte
	err = q.QueryRowContext(ctx, `SELECT maintenance,converted,revision,original_status,preview_token,preview_allocations FROM subscription_quota_rollouts WHERE group_id=$1`, groupID).Scan(&v.state.Maintenance, &v.state.Converted, &v.state.Revision, &v.originalStatus, &token, &savedAllocations)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if token.Valid {
		v.state.PreviewToken = token.String
	}
	if allocations == nil && len(savedAllocations) > 0 {
		if err = json.Unmarshal(savedAllocations, &v.state.Allocations); err != nil {
			return v, err
		}
	}
	now, err := subscriptionQuotaDBNow(ctx, q)
	if err != nil {
		return v, err
	}
	v.state.ServerTime = now
	rows, err := q.QueryContext(ctx, `SELECT u.id,u.email,u.username,count(k.id) FROM api_keys k JOIN users u ON u.id=k.user_id WHERE k.group_id=$1 AND k.deleted_at IS NULL GROUP BY u.id,u.email,u.username ORDER BY u.id`, groupID)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var user domain.SubscriptionQuotaRolloutKeyUser
		if err = rows.Scan(&user.UserID, &user.Email, &user.Username, &user.KeyCount); err != nil {
			rows.Close()
			return v, err
		}
		v.state.KeyUsers = append(v.state.KeyUsers, user)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM subscription_quota_protected_requests WHERE group_id=$1 AND state NOT IN ('completed','no_charge')`, groupID).Scan(&v.state.ProtectedPending); err != nil {
		return v, err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM payment_orders WHERE subscription_group_id=$1 AND (status IN ('PENDING','PAID','RECHARGING','REFUND_REQUESTED','REFUNDING','REFUND_PENDING','PARTIALLY_REFUNDED') OR (status='FAILED' AND paid_at IS NOT NULL) OR (status='EXPIRED' AND updated_at>=clock_timestamp()-interval '5 minutes'))`, groupID).Scan(&v.state.PendingOrders); err != nil {
		return v, err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM payment_orders WHERE subscription_group_id=$1 AND subscription_quota_snapshot IS NULL AND status IN ('CANCELLED','EXPIRED')`, groupID).Scan(&v.state.LegacyTerminalOrders); err != nil {
		return v, err
	}
	rows, err = q.QueryContext(ctx, `SELECT capabilities,heartbeat_at FROM subscription_quota_runtime_replicas WHERE heartbeat_at>=clock_timestamp()-interval '45 seconds' ORDER BY replica_id,instance_token`)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var data []byte
		var at time.Time
		var replica domain.SubscriptionQuotaReplica
		if err = rows.Scan(&data, &at); err != nil {
			rows.Close()
			return v, err
		}
		if err = json.Unmarshal(data, &replica); err != nil {
			rows.Close()
			return v, err
		}
		replica.HeartbeatAt = at
		v.state.Replicas = append(v.state.Replicas, replica)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	v.state.Blockers = append(v.state.Blockers, rolloutReplicaBlockers(v.state.Replicas, groupID, policy)...)
	if v.platform != "anthropic" {
		v.state.Blockers = append(v.state.Blockers, "GROUP_NOT_ANTHROPIC")
	}
	if !v.state.Converted && (v.state.QuotaMode != "windows" || (v.kind != "standard" && v.kind != "subscription")) {
		v.state.Blockers = append(v.state.Blockers, "GROUP_NOT_WINDOWS")
	}

	if v.fallback {
		v.state.Blockers = append(v.state.Blockers, "GROUP_FALLBACK_CONFIGURED")
	}
	if !v.state.Maintenance || v.state.GroupStatus != domain.SubscriptionQuotaMaintenanceStatus {
		v.state.Blockers = append(v.state.Blockers, "MAINTENANCE_REQUIRED")
	}
	if v.state.ProtectedPending > 0 {
		v.state.Blockers = append(v.state.Blockers, "PROTECTED_REQUESTS_PENDING")
	}
	if v.state.PendingOrders > 0 {
		v.state.Blockers = append(v.state.Blockers, "ORDERS_PENDING")
	}
	var bindings int64
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM carpool_billing_bindings WHERE group_id=$1`, groupID).Scan(&bindings); err != nil {
		return v, err
	}
	if bindings > 0 {
		v.state.Blockers = append(v.state.Blockers, "CARPOOL_BINDING_PRESENT")
	}
	ids := make([]int64, 0, len(v.state.Allocations))
	for _, allocation := range v.state.Allocations {
		ids = append(ids, allocation.UserID)
	}
	var validUsers int64
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id=ANY($1) AND deleted_at IS NULL AND status='active' AND (claude_subscription_concurrency IS NULL OR claude_subscription_concurrency>0)`, pq.Array(ids)).Scan(&validUsers); err != nil {
		return v, err
	}
	if validUsers != int64(len(ids)) {
		v.state.Blockers = append(v.state.Blockers, "ALLOCATION_USER_INVALID")
	}
	// Fingerprint only identities/policies/entitlements. GPT monetary changes do
	// not invalidate a Claude preview, nor do routine replica heartbeat timestamps.
	var keysJSON, usersJSON, parentsJSON, ordersJSON string
	if err = q.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(id,user_id,group_id,status,expires_at,deleted_at) ORDER BY id),'[]'::jsonb)::text FROM api_keys WHERE group_id=$1 AND deleted_at IS NULL`, groupID).Scan(&keysJSON); err != nil {
		return v, err
	}
	if err = q.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(id,status,deleted_at,claude_subscription_concurrency) ORDER BY id),'[]'::jsonb)::text FROM users WHERE id=ANY($1) OR id IN(SELECT user_id FROM api_keys WHERE group_id=$2 AND deleted_at IS NULL)`, pq.Array(ids), groupID).Scan(&usersJSON); err != nil {
		return v, err
	}
	if err = q.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(id,user_id,starts_at,expires_at,status,quota_revision,deleted_at) ORDER BY id),'[]'::jsonb)::text FROM user_subscriptions WHERE group_id=$1`, groupID).Scan(&parentsJSON); err != nil {
		return v, err
	}
	if err = q.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(id,user_id,status,paid_at,refund_amount,subscription_quota_snapshot) ORDER BY id),'[]'::jsonb)::text FROM payment_orders WHERE subscription_group_id=$1`, groupID).Scan(&ordersJSON); err != nil {
		return v, err
	}
	replicas := append([]domain.SubscriptionQuotaReplica{}, v.state.Replicas...)
	for i := range replicas {
		replicas[i].HeartbeatAt = time.Time{}
	}
	v.fingerprint = rolloutHash([]any{groupJSON, keysJSON, usersJSON, parentsJSON, ordersJSON, v.state.Revision, v.state.Allocations, replicas, v.state.ProtectedPending, v.state.PendingOrders})
	return v, nil
}
func (r *subscriptionQuotaRepository) GetRollout(ctx context.Context, groupID int64, policy domain.SubscriptionQuotaRolloutPolicy) (*domain.SubscriptionQuotaRolloutState, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	view, err := rolloutRead(ctx, tx, groupID, nil, policy)
	if err != nil {
		return nil, err
	}
	return &view.state, nil
}

func (r *subscriptionQuotaRepository) MaintainRollout(ctx context.Context, p domain.SubscriptionQuotaMaintenanceParams) (*domain.SubscriptionQuotaRolloutState, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if p.GroupID <= 0 || p.ActorID <= 0 || p.ExpectedRevision < 0 || !subscriptionQuotaValidKey(p.IdempotencyKey) || strings.TrimSpace(p.Reason) == "" {
		return nil, service.ErrSubscriptionQuotaInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = rolloutLock(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	fingerprint := rolloutHash([]any{p.GroupID, p.ActorID, p.Enabled, p.ExpectedRevision, p.Reason})
	var replay domain.SubscriptionQuotaRolloutState
	found, err := rolloutReplay(ctx, tx, p.GroupID, p.IdempotencyKey, "maintenance", fingerprint, &replay)
	if err != nil {
		return nil, err
	}
	if found {
		return &replay, nil
	}
	if err = rolloutAdmin(ctx, tx, p.ActorID); err != nil {
		return nil, err
	}
	var lockedID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, p.GroupID).Scan(&lockedID); err != nil {
		return nil, err
	}
	// Freeze capability advertisements during release. This also serializes new
	// process registrations, rather than trusting only the rows already present.
	if !p.Enabled {
		if _, err = tx.ExecContext(ctx, `LOCK TABLE subscription_quota_runtime_replicas IN SHARE MODE`); err != nil {
			return nil, err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL FOR SHARE`, p.ActorID).Scan(&lockedID); err != nil {
		return nil, service.ErrSubscriptionQuotaNotFound
	}
	view, err := rolloutRead(ctx, tx, p.GroupID, nil, p.Policy)
	if err != nil {
		return nil, err
	}
	if view.platform != "anthropic" || (view.kind != "standard" && view.kind != "subscription") || (view.state.QuotaMode != "windows" && view.state.QuotaMode != "term") {
		return nil, service.ErrSubscriptionQuotaGroupInvalid
	}
	if view.state.Revision != p.ExpectedRevision {
		return nil, service.ErrSubscriptionQuotaRolloutStale
	}
	if !p.Enabled {
		if !view.state.Maintenance {
			return nil, service.ErrSubscriptionQuotaRolloutStale
		}
		if len(rolloutReplicaBlockers(view.state.Replicas, p.GroupID, p.Policy)) > 0 {
			return nil, service.ErrSubscriptionQuotaRolloutBlocked
		}
	}
	if err = rolloutSetWriter(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	if p.Enabled {
		original := view.originalStatus
		if original == "" {
			original = view.state.GroupStatus
		}
		if original != service.StatusActive && original != "inactive" {
			return nil, service.ErrSubscriptionQuotaRolloutStale
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO subscription_quota_rollouts(group_id,maintenance,converted,original_status,revision) VALUES($1,true,$2,$3,1) ON CONFLICT(group_id) DO UPDATE SET maintenance=true,revision=subscription_quota_rollouts.revision+1,preview_token=NULL,preview_actor_id=NULL,preview_allocations='[]'::jsonb,updated_at=clock_timestamp()`, p.GroupID, view.state.QuotaMode == "term", original)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE groups SET status=$2,updated_at=clock_timestamp() WHERE id=$1`, p.GroupID, domain.SubscriptionQuotaMaintenanceStatus)
		}
	} else {
		status := view.originalStatus
		_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_rollouts SET maintenance=false,revision=revision+1,preview_token=NULL,preview_actor_id=NULL,updated_at=clock_timestamp() WHERE group_id=$1`, p.GroupID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE groups SET status=$2,updated_at=clock_timestamp() WHERE id=$1`, p.GroupID, status)
		}
	}
	if err != nil {
		return nil, err
	}
	if err = rolloutInvalidateGroup(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	view, err = rolloutRead(ctx, tx, p.GroupID, nil, p.Policy)
	if err != nil {
		return nil, err
	}
	if err = rolloutSave(ctx, tx, p.GroupID, p.ActorID, p.IdempotencyKey, "maintenance", fingerprint, view.state); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &view.state, nil
}
func (r *subscriptionQuotaRepository) PreviewRollout(ctx context.Context, p domain.SubscriptionQuotaRolloutParams) (*domain.SubscriptionQuotaRolloutState, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	allocations, err := service.NormalizeSubscriptionQuotaAllocations(p.Allocations)
	if err != nil {
		return nil, err
	}
	p.Allocations = allocations
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = rolloutLock(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	if err = rolloutAdmin(ctx, tx, p.ActorID); err != nil {
		return nil, err
	}
	view, err := rolloutRead(ctx, tx, p.GroupID, p.Allocations, p.Policy)
	if err != nil {
		return nil, err
	}
	if !view.state.Maintenance || view.state.Converted {
		return nil, service.ErrSubscriptionQuotaRolloutStale
	}
	// Blocked previews are reviewable but deliberately have no confirmable token.
	token := ""
	if len(view.state.Blockers) == 0 {
		token = rolloutHash([]any{p.ActorID, view.fingerprint})
	}
	data, err := json.Marshal(p.Allocations)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_rollouts SET preview_token=NULLIF($2,''),preview_actor_id=$3,preview_allocations=$4,updated_at=clock_timestamp() WHERE group_id=$1`, p.GroupID, token, p.ActorID, data)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	view.state.PreviewToken = token
	return &view.state, nil
}
func (r *subscriptionQuotaRepository) ConvertRollout(ctx context.Context, p domain.SubscriptionQuotaRolloutParams) (*domain.SubscriptionQuotaRolloutResult, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	if p.GroupID <= 0 || p.ActorID <= 0 || p.ExpectedRevision < 0 || len(p.PreviewToken) != 64 || !subscriptionQuotaValidKey(p.IdempotencyKey) {
		return nil, service.ErrSubscriptionQuotaInput
	}
	allocations, err := service.NormalizeSubscriptionQuotaAllocations(p.Allocations)
	if err != nil {
		return nil, err
	}
	p.Allocations = allocations
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = rolloutLock(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	fingerprint := rolloutHash([]any{p.GroupID, p.ActorID, p.ExpectedRevision, p.PreviewToken, p.Allocations})
	var replay domain.SubscriptionQuotaRolloutResult
	found, err := rolloutReplay(ctx, tx, p.GroupID, p.IdempotencyKey, "conversion", fingerprint, &replay)
	if err != nil {
		return nil, err
	}
	if found {
		replay.Replayed = true
		return &replay, nil
	}
	if err = rolloutAdmin(ctx, tx, p.ActorID); err != nil {
		return nil, err
	}
	var maintaining, converted bool
	var revision, previewActor int64
	var previewToken sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT maintenance,converted,revision,coalesce(preview_actor_id,0),preview_token FROM subscription_quota_rollouts WHERE group_id=$1`, p.GroupID).Scan(&maintaining, &converted, &revision, &previewActor, &previewToken)
	if err != nil {
		return nil, service.ErrSubscriptionQuotaRolloutStale
	}
	if !maintaining || converted || revision != p.ExpectedRevision || previewActor != p.ActorID || !previewToken.Valid || previewToken.String != p.PreviewToken {
		return nil, service.ErrSubscriptionQuotaRolloutStale
	}
	// Freeze every affected scope (including unallocated Key/native-subscription
	// users) in numeric order. Only the explicitly approved set receives cards.
	ids := make([]int64, 0, len(p.Allocations))
	for _, allocation := range p.Allocations {
		ids = append(ids, allocation.UserID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM api_keys WHERE group_id=$1 AND deleted_at IS NULL UNION SELECT user_id FROM user_subscriptions WHERE group_id=$1 AND deleted_at IS NULL`, p.GroupID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	unique := ids[:0]
	for _, id := range ids {
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	ids = unique
	for _, id := range ids {
		if err = subscriptionQuotaScopeLock(ctx, tx, id, p.GroupID); err != nil {
			return nil, err
		}
	}
	parents := map[int64]subscriptionQuotaParent{}
	terms := map[int64][]domain.SubscriptionQuotaTerm{}
	// Parent locks precede all term locks. Different scopes never reverse order.
	for _, id := range ids {
		parent, err := subscriptionQuotaReadParent(ctx, tx, id, p.GroupID, true)
		if err != nil {
			return nil, err
		}
		parents[id] = parent
	}
	for _, id := range ids {
		cards, err := subscriptionQuotaReadTerms(ctx, tx, parents[id].id, true)
		if err != nil {
			return nil, err
		}
		if len(cards) > 0 {
			return nil, service.ErrSubscriptionQuotaRolloutBlocked
		}
		terms[id] = cards
	}
	// The final group lock serializes mode change with pre-slot protected admits
	// and new orders. Key row locks prevent concurrent soft-delete/move drift.
	userIDs := append(append([]int64{}, ids...), p.ActorID)
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	rows, err = tx.QueryContext(ctx, `SELECT id FROM users WHERE id=ANY($1) ORDER BY id FOR SHARE`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var lockedID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM groups WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, p.GroupID).Scan(&lockedID); err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id FROM api_keys WHERE group_id=$1 AND deleted_at IS NULL ORDER BY id FOR SHARE`, p.GroupID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = rolloutAdmin(ctx, tx, p.ActorID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `LOCK TABLE subscription_quota_runtime_replicas IN SHARE MODE`); err != nil {
		return nil, err
	}
	view, err := rolloutRead(ctx, tx, p.GroupID, p.Allocations, p.Policy)
	if err != nil {
		return nil, err
	}
	if len(view.state.Blockers) > 0 {
		return nil, service.ErrSubscriptionQuotaRolloutBlocked
	}
	if rolloutHash([]any{p.ActorID, view.fingerprint}) != p.PreviewToken {
		return nil, service.ErrSubscriptionQuotaRolloutStale
	}
	if err = rolloutSetWriter(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE groups SET subscription_type='subscription',subscription_quota_mode='term',updated_at=clock_timestamp() WHERE id=$1 AND status=$2 AND platform='anthropic' AND subscription_quota_mode='windows'`, p.GroupID, domain.SubscriptionQuotaMaintenanceStatus)
	if err != nil {
		return nil, err
	}
	now, err := subscriptionQuotaDBNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	result := domain.SubscriptionQuotaRolloutResult{Terms: []domain.SubscriptionQuotaTerm{}}
	for _, allocation := range p.Allocations {
		source := domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, QuotaUSD: allocation.QuotaUSD, DurationHours: 168, QuotaSource: "admin"}
		data, err := json.Marshal(source)
		if err != nil {
			return nil, err
		}
		open := domain.SubscriptionQuotaOpenParams{UserID: allocation.UserID, ActorID: p.ActorID, GroupID: p.GroupID, Operation: "open", Notes: "approved controlled quota conversion"}
		issued, err := subscriptionQuotaCreateTermTx(ctx, tx, open, parents[allocation.UserID], terms[allocation.UserID], now, allocation.QuotaUSD, data, "admin", nil)
		if err != nil {
			return nil, err
		}
		result.Terms = append(result.Terms, issued.Term)
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_quota_rollouts SET converted=true,revision=revision+1,preview_token=NULL,preview_actor_id=NULL,updated_at=clock_timestamp() WHERE group_id=$1`, p.GroupID)
	if err != nil {
		return nil, err
	}
	if err = rolloutInvalidateGroup(ctx, tx, p.GroupID); err != nil {
		return nil, err
	}
	view, err = rolloutRead(ctx, tx, p.GroupID, p.Allocations, p.Policy)
	if err != nil {
		return nil, err
	}
	result.State = view.state
	if err = rolloutSave(ctx, tx, p.GroupID, p.ActorID, p.IdempotencyKey, "conversion", fingerprint, result); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}
