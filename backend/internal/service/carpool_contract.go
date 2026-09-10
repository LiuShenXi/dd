package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

var (
	ErrCarpoolNotFound            = infraerrors.NotFound("CARPOOL_NOT_FOUND", "carpool record not found")
	ErrCarpoolOverlap             = infraerrors.Conflict("CARPOOL_TERM_OVERLAP", "carpool term overlaps an existing term")
	ErrCarpoolUnavailable         = infraerrors.Forbidden("CARPOOL_UNAVAILABLE", "carpool membership is unavailable")
	ErrCarpoolQuotaExhausted      = infraerrors.Forbidden("CARPOOL_QUOTA_EXHAUSTED", "carpool quota is exhausted")
	ErrCarpoolBoostExhausted      = infraerrors.Conflict("CARPOOL_BOOST_EXHAUSTED", "carpool boosts are exhausted")
	ErrCarpoolIdempotencyConflict = infraerrors.Conflict("CARPOOL_IDEMPOTENCY_CONFLICT", "idempotency key was used with a different payload")
	ErrCarpoolAdmissionReplay     = infraerrors.Conflict("CARPOOL_ADMISSION_REPLAY", "billing admission identity has already been used")
	ErrCarpoolInvalidRelationship = infraerrors.Forbidden("CARPOOL_RELATIONSHIP_INVALID", "carpool user, key, group, term, and cycle relationship is invalid")
	ErrCarpoolOpeningBalance      = infraerrors.Conflict("CARPOOL_OPENING_BALANCE_REQUIRES_MIGRATION", "Existing balance requires the reviewed migration process before opening this subscription")
	ErrCarpoolOpeningKeyGroup     = infraerrors.Conflict("CARPOOL_OPENING_KEY_GROUP_CONFLICT", "All existing user API keys must belong to the selected group before opening this subscription")
	ErrCarpoolInvalidNaturalReset = infraerrors.BadRequest("CARPOOL_NATURAL_RESET_INVALID", "takeover next natural reset deadline is invalid")
)

type CarpoolRepositoryAPI interface {
	DatabaseNow(context.Context) (time.Time, error)
	GetPlan(context.Context, int64) (*domain.CarpoolPlan, error)
	GetPreviewPlan(context.Context, int64, int64) (*domain.CarpoolPlan, error)
	ListPlans(context.Context, bool) ([]domain.CarpoolPlan, error)
	CreatePlanVersion(context.Context, int64, domain.CarpoolPlan, domain.CarpoolOperation) (*domain.CarpoolPlan, error)
	CreateTerm(context.Context, domain.CreateCarpoolTermParams) (*domain.CarpoolTerm, []domain.CarpoolCycle, error)
	GetTerm(context.Context, int64) (*domain.CarpoolTerm, error)
	ListTermCycles(context.Context, int64) ([]domain.CarpoolCycle, error)
	TerminateTerm(context.Context, int64, int64, string, *time.Time, domain.CarpoolOperation) (*domain.CarpoolTerm, error)
	ClaimBoost(context.Context, int64, string, string) (*domain.CarpoolBoostResult, error)
	GetBoostStatus(context.Context, int64) (*domain.CarpoolBoostStatus, error)
	RecordPayment(context.Context, int64, int64, domain.CarpoolPaymentInput, domain.CarpoolOperation) (*domain.CarpoolPayment, error)
	ListPayments(context.Context, int64, int, int) ([]domain.CarpoolPayment, int64, error)
	AdjustCycle(context.Context, int64, int64, string, decimal.Decimal, string, *int64, domain.CarpoolOperation) ([]domain.CarpoolLedgerEntry, error)
	GetUserDetails(context.Context, int64) (*domain.CarpoolUserDetails, error)
	ListAdminTerms(context.Context, domain.CarpoolTermFilters) ([]domain.CarpoolAdminTerm, int64, error)
	ListAdminCycles(context.Context, domain.CarpoolCycleFilters) ([]domain.CarpoolAdminCycle, int64, error)
	ListAdminLedger(context.Context, domain.CarpoolLedgerFilters) ([]domain.CarpoolLedgerEntry, int64, error)
	EnsureCurrentCycle(context.Context, int64, time.Time) (*domain.CarpoolCycle, error)
	Admit(context.Context, int64, int64, int64, string, time.Time) (*domain.CarpoolBillingSnapshot, error)
	PersistKnownUsage(context.Context, domain.CarpoolBillingSnapshot, decimal.Decimal, json.RawMessage) error
	RecoverPendingReceipts(context.Context, int) ([]domain.CarpoolKnownUsage, error)
	MarkReconcileRequired(context.Context, domain.CarpoolBillingSnapshot, string) error
	MarkKnownUsageReconcileRequired(context.Context, domain.CarpoolBillingSnapshot, string) error
	MaintainCycles(context.Context, time.Time, int) error
	ListBillingExceptions(context.Context, domain.CarpoolBillingExceptionFilters) ([]domain.CarpoolBillingException, int64, error)
	ReconcileBillingException(context.Context, int64, int64, string, *decimal.Decimal, string, domain.CarpoolOperation) (*domain.CarpoolBillingException, error)
	ApplyUsageDebitTx(context.Context, *sql.Tx, *domain.CarpoolUsageDebit) ([]domain.CarpoolLedgerEntry, error)
	MarkBillingSettledTx(context.Context, *sql.Tx, int64, decimal.Decimal) error
}

type CarpoolResetWindowReader interface {
	UserResetWindow(context.Context, int64, time.Time) (domain.CarpoolUserResetWindow, error)
}
