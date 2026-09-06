package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

const (
	CarpoolResetStatusQualified   = "qualified"
	CarpoolResetStatusScheduled   = "scheduled"
	CarpoolResetStatusRunning     = "running"
	CarpoolResetStatusCompleted   = "completed"
	CarpoolResetStatusCancelled   = "cancelled"
	CarpoolResetStatusNeedsReview = "needs_review"

	CarpoolResetTargetSucceeded = "succeeded"
)

type CarpoolResetBatch struct {
	ID                  int64           `json:"id"`
	ScopeID             int64           `json:"scope_id"`
	Status              string          `json:"status"`
	DetectedAt          time.Time       `json:"detected_at"`
	QualifiedAt         *time.Time      `json:"qualified_at"`
	SlotAt              *time.Time      `json:"slot_at"`
	ScheduledAt         *time.Time      `json:"scheduled_at"`
	ScheduleRevision    int             `json:"schedule_revision"`
	EffectiveAt         *time.Time      `json:"effective_at"`
	CompletedAt         *time.Time      `json:"completed_at"`
	DelayReason         *string         `json:"delay_reason"`
	AnnouncementState   string          `json:"announcement_state"`
	TargetCount         int             `json:"target_count"`
	GrantedUSD          decimal.Decimal `json:"granted_usd"`
	QualificationSource string          `json:"-"`
	SourceEventKeyHash  string          `json:"-"`
}

type CarpoolResetBatchFilters struct {
	Page, PageSize int
	ScopeID        *int64
	Status         string
}

type CarpoolResetObservation struct {
	UpstreamIdentityHash  string     `json:"upstream_identity_hash"`
	BaselineComplete      bool       `json:"baseline_complete"`
	LastCompleteAt        *time.Time `json:"last_complete_at"`
	HealthStatus          string     `json:"health_status"`
	KnownCreditCount      int        `json:"known_credit_count"`
	UnassignedCreditCount int        `json:"unassigned_credit_count"`
}

type CarpoolResetObservationFilters struct {
	Page, PageSize int
}

type CarpoolResetScanResult struct {
	Scanned       int `json:"scanned"`
	Complete      int `json:"complete"`
	Incomplete    int `json:"incomplete"`
	NewCandidates int `json:"new_candidates"`
}

type CarpoolResetCreditEvidence struct {
	CreditHash string
	ExpiresAt  *time.Time
}

type CarpoolResetObservationInput struct {
	UpstreamIdentityHash    string
	RepresentativeAccountID int64
	ObservedAt              time.Time
	Complete                bool
	HealthStatus            string
	IncompleteReason        string
	Credits                 []CarpoolResetCreditEvidence
}

type CarpoolResetObservationResult struct {
	Complete      bool
	NewCandidates int
	Batch         *CarpoolResetBatch
}
