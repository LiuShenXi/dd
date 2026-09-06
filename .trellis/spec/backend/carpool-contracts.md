# Carpool Transaction And Recovery Contracts

## 1. Scope / Trigger

Apply when changing carpool admission, billing, reset execution, migrations or API projections. Business/API authority remains `openspec/changes/add-carpool-v1-3/`; this document records implementation constraints established by source review and regressions, not another product specification.

## 2. Signatures

`CarpoolRepositoryAPI` in `backend/internal/service/carpool_contract.go` exposes `Admit`, `PersistKnownUsage`, `RecoverPendingReceipts`, `MarkKnownUsageReconcileRequired`, `ApplyUsageDebitTx` and `MarkBillingSettledTx`. The latter two receive the existing `*sql.Tx`; they must not independently commit billing.

`OpenAIGatewayService.RecordUsage` returns `ErrCarpoolUsageUnknown` when actual usage cannot be established. `carpoolWebSocketUsageReconcileReason(error)` distinguishes this sentinel with `errors.Is`.

Reset ledger rows use `reset_batch_id`. Scope membership advisory locks use an int32-compatible scope key. Integration fixtures must fit that contract.

## 3. Contracts

- Admission freezes the database-authoritative microsecond timestamp and user/key/group/term/cycle identity after relevant lock waits. Each ingress request/WS turn gets a fresh identity; same-turn failover retains it.
- The known receipt is durable before debit. Usage dedup, original-cycle debit, ledger and settled state share one transaction. Unknown usage never becomes a fabricated zero-cost receipt.
- Recovery scans and each receipt Apply/promotion have bounded contexts independent of maintenance and of previous receipts. Parent cancellation stops work. Stale receipt selection skips locked rows; permanent/retry-exhausted promotion preserves known cost and payload.
- Reset execution refreshes database time after locks and verifies the actual execution minute, term/cycle coverage and exact cooldown before commit. Qualification, completion and announcement publication are distinct states.
- Successful terms/cycles/ledger/payments list responses encode `items: []`, never `null`.
- Diagnostic normalization must be idempotent: `category(category(raw)) == category(raw)`. Repository writes and administrator projections both call the classifier; each canonical output must therefore be an accepted input. Unknown raw inputs still map to the generic category, never pass through.
- Runtime acceptance binds both uncommitted-source and embedded-frontend SHA-256, not HEAD alone. Only the source coordinator owns the isolated copied runtime and shared integration runner.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Absent or ambiguous usage | No debit; manual reconciliation; `usage_receipt_missing` |
| Genuine known-usage persistence/settlement failure | Preserve durable facts; `usage_persistence_or_settlement_failed` |
| Replayed settled receipt | Original dedup result; no second debit or backwards state transition |
| Post-lock admission boundary crossing | Retry entire transaction; do not acquire a new cycle while retaining the key lock |
| Final reset term/cycle/minute boundary crossing | Roll back all targets/ledger/success time and reschedule |
| Same idempotency key, changed normalized payload | Conflict; no additional business write |

## 5. Good/Base/Bad Cases

- Good: a WS completion omits usage; response is forwarded, one admission is marked for reconciliation and no billing command is emitted.
- Base: a known receipt is applied once, then a retry converges on existing dedup without another charge.
- Bad: receipt one blocks until its timeout and receipt two inherits that cancelled context, preventing progress indefinitely.

## 6. Tests Required

Use `openai_ws_carpool_test.go` for real two-turn/passthrough omitted-usage paths and both diagnostic branches. Assert zero persisted known usage and zero billing commands for missing usage. Use `carpool_recovery_test.go` for blocked-first-receipt progress, parent cancellation and Stop.

Run real PG regressions for key/cycle boundary locks, adjustment-closure net balance/ledger equality, SKIP LOCKED recovery, final reset rollback, zero grants, SQL announcement failure/retry and audience checks. Compilation alone is not transaction evidence. Keep unit/build, real PG, final-image HTTP/WS and browser results separate; never classify unrelated baseline failures as a green full suite.

For sanitized diagnostics, table-test every canonical output and raw alias through two normalization passes, then persist/read actual `ListBillingExceptions` projections. A gateway test observing the raw marker cannot prove the user-visible API category. Strict WS acceptance checks both the stored row and actual administrator endpoint.

For API additions, test the actual `RegisterUserRoutes`/`RegisterAdminRoutes` router, not only a handler method. `GET /api/v1/announcements/version` must be registered inside the authenticated announcement group and return the version JSON projection; an implemented handler with no route still produces a live 404. Signature changes in lifecycle providers must also update `cmd/server` cleanup tests. Include `go test -tags=unit -run '^$' ./...`, full router/server tests and whole `go vet` in integration checks; the compile-only command does not execute business assertions.

`carpool_lifecycle_integration_test.go` joins maintenance after two-week downtime, expired/renewed boost replay, same-user usage/reversal across terms with current-term reset counts, and all four unresolved receipt statuses holding closing until original-cycle settlement. Assert repeated maintenance adds no ledger rows and closed-cycle balances equal ledger net. Fixture time changes belong only in a fresh synthetic PG harness, never the copied application runtime. Use one SQL command per parameterized ExecContext; semicolon-separated UPDATE commands are incompatible with the extended prepared protocol.

For nullable fixture timestamps, bind a separate time.Time-or-nil parameter to activated_at instead of reusing a starts_at placeholder inside an untyped CASE expression. The shared-parameter variant passed Go compilation but failed actual PostgreSQL parameter inference before either lifecycle assertion ran; compilation cannot verify SQL parameter types.

## 7. Wrong vs Correct

Wrong: classify every `RecordUsage` error as persistence failure, or treat missing usage as known zero.

Correct, from `carpool_gateway.go`:

```go
if errors.Is(err, service.ErrCarpoolUsageUnknown) {
    return "websocket turn ended without durable known usage"
}
return "websocket known usage persistence or settlement failed"
```

The repository maps these static reasons to sanitized diagnostics. Raw upstream errors, payloads, credentials and copied-user records do not belong in public evidence.

## v1.4 Rule and Read-Projection Upgrade

### 1. Scope / Trigger

Applies to plan migrations/versioning, cycle construction, renewal, boost limits and own-user details.

### 2. Signatures

Current forward rules are duration28days, cycle7days, boost_count default2 and configurable2-or3, boost_ratio0.10. Migration240 changes defaults by appending versions, never rewriting migration239 or existing three-boost snapshots. `BuildCarpoolCycles` consumes the frozen snapshot, not current global defaults. Details adds `quota` and `term.reset_events` per OpenSpec API contract; reset count basis is `current_term`.

### 3. Contracts

Append migrations and latest plan versions; preserve custom price/weekly/name/enabled. Never rewrite existing terms/cycles/snapshots/ledger. Forward constraints admit new rules while legacy30-day/five-cycle/two-boost data remains valid. Preview expiry and renewal follow latest same-code snapshot; disabled/superseded rules cannot silently reopen.

Same-code defaulting applies only when renewal omits `plan_id`. An explicit administrator choice may upgrade/downgrade the next term to another latest enabled tier. Do not impose a new same-tier restriction on explicit requests: the existing HTTP renewal acceptance intentionally changes four-seat to three-seat. Successful operation replay must resolve before looking up today's latest plan so later version changes cannot invalidate an earlier response.

Preview and opening must agree on mode/takeover shape, nonnegative historical usage and `0 <= takeover.boost_used <= selectedPlan.BoostCount`. Do not return a successful preview for a request the matching opening rejects. Details-only lazy cycle activation rechecks database time after term/cycle lock waits and rolls back stale boundary activation before bounded reselection; do not change shared gateway admission or reset lock contracts for a read-projection fix.

For an active covered current cycle, quota is max(0,base+boost+manual), including zero; otherwise null. Reset events join succeeded targets, completed batches and selected own term/cycle. Use target.executed_at and immutable cycle.base_quota_usd, include zero grants, order deterministically, and expose only cycle number/time/target. No internal IDs, buckets, notes or raw ledgers. Reset scheduling/accounting transactions are unchanged.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| New28-day term | Four full7-day cycles, no empty fifth |
| Legacy30-day snapshot | Original five cycles and rounded partial quota |
| Four distinct concurrent boost claims | Two credits maximum for default new term; existing three-boost snapshots retain three |
| Replayed successful request after expiry | Original response, no extra credit |
| Disabled latest plan | No automatic enabling/opening |
| Invalid takeover shape, negative usage or boost-count overflow | Preview and opening both reject |
| Details waits through a cycle boundary | Reselect current cycle without committing a historical grant |
| Zero-grant succeeded target in completed batch | Visible successful event, reset_count includes it |
| Failed/pending or other-user target | Excluded from user projection |

### 5. Good/Base/Bad Cases

Good: append current-rule version and open a28-day term without changing its user's older30-day term. Base: return838 net carpool quota while ordinary balance remains0. Bad: update old expires_at, drop its fifth cycle, or report reset.granted_usd as the refill target.

### 6. Tests Required

Exact day7/14/21/28 boundaries, all three totals, legacy shape, omitted-plan renewal, custom/disabled migration replay, concurrent slots/idempotency, own-user quota clamping and zero/pending/failed/foreign reset projections. Execute the real appended migration and repository tests in fresh PG, not just Go compilation. Preserve previous copied-runtime records and clock.

Migration fixtures must obey the pre-upgrade schema: do not insert28-day/three-boost rows under old30-day/two-boost CHECK constraints before running the upgrade. Test already-current state and replay only after constraints permit those rows. Split parameterized SQL statements as required by PostgreSQL's extended protocol. Keep actual migration execution/fingerprint conservation and lock-wait boundary regressions; text-fragment assertions are not migration evidence.

### 7. Wrong vs Correct

Wrong: loop five times regardless of duration, hardcode preview+30days, or source success time from scheduled_at.

Correct: derive cycles/expiry from immutable snapshot; use latest same-tier forward rules; read committed successful target execution time and immutable cycle target.
