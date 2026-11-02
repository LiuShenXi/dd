package domain

import "time"

const SubscriptionQuotaMaintenanceStatus = "quota_maintenance"
const SubscriptionQuotaRolloutCapabilityVersion = 1
const SubscriptionQuotaLeaseProtocolVersion = 1

type SubscriptionQuotaAllocation struct {
	UserID   int64  `json:"user_id"`
	QuotaUSD string `json:"quota_usd"`
}
type SubscriptionQuotaReplica struct {
	InstanceToken            string    `json:"-"`
	ReplicaID                string    `json:"replica_id"`
	HeartbeatAt              time.Time `json:"heartbeat_at"`
	CapabilityVersion        int       `json:"capability_version"`
	ReceiptVersion           int       `json:"receipt_version"`
	LeaseProtocolVersion     int       `json:"lease_protocol_version"`
	ClaudeDefaultConcurrency int       `json:"claude_default_concurrency"`
	IssueEnabled             bool      `json:"issue_enabled"`
	AdmissionEnabled         bool      `json:"admission_enabled"`
	RecoveryEnabled          bool      `json:"recovery_enabled"`
	ProtectedGroupIDs        []int64   `json:"protected_group_ids"`
	EnabledGroupIDs          []int64   `json:"enabled_group_ids"`
	ExpectedReplicaIDs       []string  `json:"expected_replica_ids"`
}
type SubscriptionQuotaRolloutKeyUser struct {
	UserID   int64  `json:"user_id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	KeyCount int64  `json:"key_count"`
}
type SubscriptionQuotaRolloutState struct {
	GroupID              int64                             `json:"group_id"`
	QuotaMode            string                            `json:"quota_mode"`
	GroupStatus          string                            `json:"group_status"`
	Maintenance          bool                              `json:"maintenance"`
	Converted            bool                              `json:"converted"`
	Revision             int64                             `json:"revision"`
	ServerTime           time.Time                         `json:"server_time"`
	Allocations          []SubscriptionQuotaAllocation     `json:"allocations"`
	KeyUsers             []SubscriptionQuotaRolloutKeyUser `json:"key_users"`
	ProtectedPending     int64                             `json:"protected_pending"`
	PendingOrders        int64                             `json:"pending_orders"`
	LegacyTerminalOrders int64                             `json:"legacy_terminal_orders"`
	Replicas             []SubscriptionQuotaReplica        `json:"replicas"`
	Blockers             []string                          `json:"blockers"`
	PreviewToken         string                            `json:"preview_token"`
}
type SubscriptionQuotaRolloutPolicy struct{ ExpectedReplicaIDs []string }
type SubscriptionQuotaRolloutParams struct {
	GroupID          int64
	ActorID          int64
	ExpectedRevision int64
	IdempotencyKey   string
	PreviewToken     string
	Allocations      []SubscriptionQuotaAllocation
	Policy           SubscriptionQuotaRolloutPolicy
}
type SubscriptionQuotaMaintenanceParams struct {
	GroupID          int64
	ActorID          int64
	ExpectedRevision int64
	IdempotencyKey   string
	Enabled          bool
	Reason           string
	Policy           SubscriptionQuotaRolloutPolicy
}
type SubscriptionQuotaRolloutResult struct {
	State    SubscriptionQuotaRolloutState `json:"state"`
	Terms    []SubscriptionQuotaTerm       `json:"terms"`
	Replayed bool                          `json:"replayed"`
}
