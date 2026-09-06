# Bug Analysis: Missing-Usage Diagnostic Round Trip

## 1. Root Cause Category

Categories B (cross-layer contract) and D (test coverage gap). `RecordUsage` distinguished absent usage with `ErrCarpoolUsageUnknown`, but WS AfterTurn classified every error as known-usage persistence failure. After correcting this, the repository's API projection normalized the already-canonical stored category a second time; two canonical outputs were missing from the classifier's accepted inputs.

## 2. Why The First Fix Did Not Complete Acceptance

The first correction was necessary and reached the live path: rebuilt-image evidence shows `reconcile_required`, `usage_receipt_missing`, NULL actual cost/payload and zero debit. Its targeted regression observed the handler's marked reason and absence of billing commands, but not the full repository-to-HTTP projection.

The unchanged strict runtime driver first passed that SQL predicate, then failed `exception_not_sanitized` on the real administrator list. This discriminates a projection defect from an unreachable gateway fix or connection-cleanup overwrite. Gateway deletes the admission snapshot before marking, so deferred drain cannot overwrite it. Director, source and gateway independently verified this sequence.

## 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Regression | Table-test idempotence for every canonical classifier output and preserve unknown-input redaction | Passed; director seven-package selection x3 |
| P0 | Integration | Persist missing/persistence categories and assert actual ListBillingExceptions read-back | Passed; real PG run 20260905T194859Z-4843a365 |
| P0 | Runtime | Rerun unchanged strict WS SQL and actual admin API assertions on rebuilt corrected image | Pending |
| P1 | Code-spec | Document normalization idempotence and two-sided projection checks | Done |

## 4. Systematic Expansion

Both `usage_receipt_missing` and `usage_persistence_or_settlement_failed` need stable round trips, not only the currently failing missing-usage value. Existing timeout/recovery categories and generic unknown-input fallback must stay stable. No forwarding changes or relaxed safety predicates are needed for this second correction.

## 5. Knowledge Capture

Updated `.trellis/spec/backend/carpool-contracts.md` with the executable `category(category(raw)) == category(raw)` contract and required unit/real-list/runtime assertions. The Trellis break-loop skill shaped this review. Template synchronization is not applicable to this application repository, which is not the Trellis template source; no unrelated template tree is created. No commit is authorized without the task's required user confirmation.
