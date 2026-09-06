# Reset Package D Handoff

This is a routing/review aid, not a replacement for the complete architecture, task artifacts or API contract. The next explicitly assigned existing worker implements the full reset/observation/announcement slice. Do not start until the director grants this ownership.

## Ownership

- New domain/service/repository/admin-handler reset files, migration 237 onward, reset Ent schema sources.
- Existing announcement domain/service/repository/handler/schema files and focused tests, plus OpenAI quota query/parser observation hook. Never call provider consumption from the observer.
- Core exclusively owns public routes, handler/provider aggregates, Wire/lifecycle and generated Ent. Send constructor/method wiring requirements to core. Do not concurrently generate Ent.
- Core owns `carpool_repository.go`, `carpool_operations.go`, `carpool_queries.go`, migration235 and core service/DTOs. Gateway owns usage migration236 and forwarding/billing files. Source owns `validation/source-runtime/`, copied-data and startup evidence. The director owns task/API-contract docs.

## Existing Extension Points

- `service.CarpoolService.SetResetWindowReader` accepts `CarpoolResetWindowReader.UserResetWindow(ctx,userID,now)`. No concrete implementation exists at handoff creation.
- Core repository helpers are package-local and available from new reset repository files: `ensureCurrentCycleTx`, `insertLedgerTx`, operation identity/replay helpers. Check latest corrected code; director requested transaction-key serialization, canonical DB time and term-before-cycle order.
- `insertLedgerTx` skips zero deltas and the ledger rejects delta zero. Count successful reset targets, never infer participation from ledger row existence. Agree target columns (`batch_id`, `term_id`, `cycle_id`, result/status) with core for current-term reset counts.
- `OpenAIQuotaService.QueryUsage` calls usage then details, but currently returns success when the detail query is nil/incomplete. The internal parser has `CreditListPresent`, `AvailableCreditCount`, `AvailableCount` and internal `autoResetCandidates`; public credit DTOs deliberately omit raw IDs.
- Existing auto-reset flow calls QueryUsage before selecting/consuming a candidate. A query observation hook with internal hashed output can cover both scheduled observation and pre-consumption observation without adding consumption behavior.
- `AnnouncementTargeting.Matches` currently handles subscription/balance only. All `ListForUser`, `MarkRead`, `ListUserReadStatus` calls need the same current/future carpool membership predicate. Add user `/announcements/version` per the updated API contract, preserving old audience behavior.

## Required Correctness

Use the exact architecture 8.5 algorithm with Shanghai `slot_at`, `scheduled_at=max(slot,last_success+48h)` and strict `<slot+1 minute`. New qualification at/after 22:00 advances to the next date; due execution does not apply that new-qualification condition. Preserve seconds and canonical database timestamps. A missed execution minute reschedules with revision and correction, never grants immediately.

Transactions serialize scope, batch, ascending term IDs, then cycles. Confirm qualification, select execution-time active terms, activate each current cycle, grant `max(0,base_quota-base_balance)`, write even zero-grant targets, then complete the batch and update actual-success cooldown atomically. Do not change terms, cycle boundaries or boost counts. Failure rolls back every target/grant/cooldown change. New evidence joins an existing promise without delaying it. Same event/card never creates multiple qualifications.

Baselines include a complete empty list, initial stock does not auto-grant, replacements `1->1` are detected by unseen stable identity/card hashes, incomplete snapshots never imply zero, and duplicates/shadows normalize to one upstream identity. Poll all managed OAuth parents, including rate-limited ones; bound timeout/concurrency/backoff. Persist health and unassigned evidence without raw credentials/card IDs. Distinguish local observation from official campaign identity.

Persist announcement outbox events atomically with qualification/schedule/success. Unique source/batch/event/revision, stale publication suppression, exact original-announcement correction plus one correction notice, no false completion. User-relative text also includes absolute Shanghai date. Version/list/read/mark-read audience checks agree and never expose reset evidence.

## Acceptance and Environment

Read `local-data-copy.md` and `local-test-isolation.md` before using the cleared sandbox. Only task internal network and synthetic identities; source startup waits director code-ready. Web38088 only; do not touch18080, production, `my-sub2api`, copied-user dates/balances or credential files. No tier mapping means no copied-user terms.

Unit and PG18 synthetic tests must cover exact48h,22:00:20 seconds,21:59/22:00 qualification,missed minute,retry/rollback/zero grant,cycle5 targets,scope merging,duplicate cards,zero/incomplete baselines,shadow identity,announcement dedup/correction/audience and restart. Save raw commands/output/exit codes under `evidence/reset-*`. A compiler pass or mock acceptance alone is not full verification.
