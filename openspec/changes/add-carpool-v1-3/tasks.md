# Acceptance Checklist

Only mark complete after the main agent inspects the actual diff and evidence.

## Official Global Reset Acceptance (2026-09-08)

- [x] O1: Document immediate confirmed scope-wide reset, request-only replay protection and independent card scheduling before code.
- [x] O2: Director inspected 7 official/shared PostgreSQL cases in run 20260908T034920Z-5983 and 12 card/carry regressions in 20260908T035010Z-6146. Confirmation unit/package compilation passed; independent review has no remaining blockers.
- [x] O3: Admin confirmation, cancel, submitting/retry states and source/effective-time labels pass 20 focused tests, full frontend suite, typecheck, lint and build. Director inspected retry-key retention and success rotation tests.
- [x] O4: Director reviewed source and verified actual local HTTP/browser confirmation: 5 effective members advance once, 2 future and 1 expired terms excluded, replay/cancel/authorization/ordinary balances/expiry/history correct. Desktop/mobile screenshots inspected. See [official reset acceptance](../../../validation/rolling-reset/evidence/official-reset-20260908/acceptance.md).

## Reset Successor Acceptance (2026-09-08)

- [x] S1: Architecture updated before implementation: successful natural/special resets each open a numbered successor; 28-day expiry remains fixed; user page removes filled-to annotations.
- [x] S2: Success, zero increment, replay, failure, coincident natural boundary and more than five periods pass real PostgreSQL checks. Director inspected all 7 passing cases in run 20260908T022827Z-90678.
- [x] S3: Pending receipts settle against original periods; carried boost/manual value is conserved across multiple special resets and cannot revive after natural or term expiry. Director inspected runs 20260908T023006Z-91064 and 20260908T023052Z-91186, 16 top-level cases total, and independent review.
- [x] S4: Percentage projection, current cycle, unavailable states and annotation removal pass 23 frontend tests, typecheck/lint/build and independent desktop/mobile real HTTP acceptance. See [successor acceptance](../../../validation/rolling-reset/evidence/successor-20260908/acceptance.md).

## Current Rolling Refill Acceptance (2026-09-07)

Direct Sol-worker coordination; no Trellis workflow. Authority:
[current architecture](../../../sub2api-carpool-architecture.md).

- [x] R1: Architecture and API contract updated before implementation; ownership separated and existing dirty work inspected.
- [x] R2: New 28-day terms use on-demand rolling periods, full final-short-period quota, consistent pending/active/expired next-refill projections. Director inspected source and real PG run 20260907T093726Z-63154.
- [x] R3: Special success moves the natural deadline once, including zero grants; old fixed boundaries, failure and replay cannot grant or move it again. Real PG run 20260907T093726Z-63154 passed consecutive, zero/replay, cap, race and rollback cases.
- [x] R4: Real PostgreSQL tests prove admission/settlement, natural/reset races, downtime, expiry and ledger/ordinary-balance conservation. Director inspected runs 20260907T093726Z-63154 (11 main cases plus 4 receipt subcases) and 20260907T094833Z-65125 (8 regression cases); relevant Go unit tests also pass.
- [x] R5: Continuous member timeline and admin opening/renewal/takeover workflows agree with server projections; frontend typecheck, lint, build and 77 tests across 9 focused files pass. Runtime browser acceptance remains R6.
- [x] R6: Current-source isolated runtime passes HTTP and desktop/mobile browser acceptance; director inspected user desktop/390px, admin overview, real synthetic UI opening and renewal preview. Source/image evidence and synthetic-reset limits are recorded in [local acceptance](../../../validation/rolling-reset/evidence/acceptance.md). No production changes.

## Historical v1.4 Acceptance (2026-09-06)

- [x] A1: New three-tier previews/openings/renewals have four7-day cycles, exact28-day expiry and base totals2200/2800/4400; boundary tests pass.
- [x] A2: Three10% boosts per term, four-way concurrent exhaustion, exact replay and expiry/renewal regressions; ordinary balance unchanged.
- [x] A3: Append migration/replay preserves legacy30-day5-cycle2-boost snapshots, custom tier settings and disabled state; no old ledger changes.
- [x] A4: Live dashboard/header quota refresh after boost/reload, ordinary-user baseline and async/error/identity tests.
- [x] A5: Actual successful-reset projection includes zero grants and excludes pending/failed/foreign records; real desktop/mobile four-segment markers/copy/times below without overlap.
- [x] A6: Focused backend unit/realPG and frontend typecheck/lint/tests/build; independently verified source/image identities and explicit residual limits.
- [x] A7: Updated architecture/PRD/contracts, retained copied records/previous test evidence, usable local URL without commit/push/deploy.

Current acceptance: `.trellis/tasks/09-05-sub2api-carpool-v1-3/evidence/director-v14-final-acceptance.md`. The director verified the new image, HTTP71/71, WS11/11, frontend1930 tests, 11 selected PostgreSQL cases across recorded runs, actual boost/reload behavior, desktop/mobile reset markers and final conservation. Imported historical reset UI fixtures and true PostgreSQL execution evidence remain explicitly distinct; unrelated baseline failures and capture limits are retained.

The checked sections below are historical v1.3 evidence only. They remain intact for audit, including the balance-display gap discovered later, and do not accept v1.4.

## Historical v1.3 Acceptance

## Setup

- [x] Read the full architecture and updated precise section 8.5.
- [x] Verify clean newer base and create an independent codex worktree.
- [x] Persist PRD, ownership/design and acceptance checklist before implementation.
- [x] Establish isolated compatible Go/PostgreSQL/Redis/frontend runtime.
- [x] Copied-user local fixtures use registration time as term start, preserve expired/future states, and create terms only for confirmed tier mappings; synthetic fixtures independently cover all three plans and boundary states. No copied tier mapping was verifiable, so copied-user terms remain zero; no fabricated memberships.

## Core and Billing

- [x] Appended migrations and Ent generation agree; migration replay preserves data.
- [x] Opening null/future/past starts, overlapping concurrency, immutable snapshots, renewal and termination.
- [x] Exact day 7/14/21/28/30 boundaries; quotas 157/200/314 and totals 2357/3000/4714.
- [x] Restart skips missed cycles, activates only current, no duplicate grants, closing waits for unresolved receipts. Direct lifecycle PG run20260906T001828Z-46e6ee52 passed, including repeated maintenance and all four unresolved receipt states.
- [x] Concurrent/idempotent boosts: two slots per term, amounts 55/70/110 including cycle 5; retry after expiry/renewal returns original result. Direct final lifecycle PG run also proves original-response replay and expired new-claim refusal without extra slots/boost ledger rows.
- [x] Dedicated admin-authorized group/key; zero ordinary balance allowed, no ordinary fallback, native behavior preserved.
- [x] All relevant HTTP and WS turns freeze final-admission cycle after waiting; retry/failover retains it.
- [x] Durable intent/receipt and replay; actual-cost debit in same usage dedup SQL transaction, no dual charge.
- [x] Multi-bucket sum equals actual cost; negative overuse, grants offset debt, expiry preserves net ledger consistency.
- [x] Auditable CNY payments/refunds, manual adjustment/reversal, explicit takeover preview/transfer with no duplicate spend.

## Reset and Announcements

- [x] Complete baselines including zero; initial stock never auto grants; 1->1 replacement detected; incomplete query never establishes zero baseline.
- [x] Same upstream identity/shadow/card dedup and candidate merge; no card-count multiplication; no consume calls from observer.
- [x] Qualification persisted with one pending scope batch and announcement event; repeated publication does not duplicate.
- [x] 21:59 vs 22:00 qualification boundary; exactly >=48h; prior 22:00:20 permits second day 22:00:20.
- [x] Zero grant counts once; cycle 5 target matches rounded initial quota; no boost/cycle/term reset.
- [x] Transactional all-or-nothing targets/ledger/success timestamp; restart retries or reschedules missed minute.
- [x] Reschedule increments revision, corrects old promise and emits one correction; no false completion announcement.
- [x] Current/future-term audience checks apply equally to list/read/mark-read; absolute Shanghai date plus correct relative phrase.

## UI, Permission and Final Evidence

- [x] Admin workflow pages and Users entry work with live isolated backend; user route below subscriptions and dashboard boosts.
- [x] User details/boost DTOs contain permitted fields only; manipulated target IDs do not access another user; admin 401/403.
- [x] Cumulative net actual usage crosses renewals; unknown imported history marked incomplete; reset count is per term. Actual PG joined same-user old/new-term usage with linked reversal and selected-term reset count passed in final run20260906T001828Z-46e6ee52. Earlier overall failed fixture run is retained as failed.
- [x] Time bar 7:7:7:7:2 with no quota numbers in segments/hover; calibrated server clock and expiry refresh.
- [x] No qualification means no countdown; reaching zero refetches, never assumes success; eligibility reflects term coverage. Four direct fake-clock/API-store regressions passed, including unchanged accounting while boundary response is pending.
- [x] Wire, routes, worker startup/shutdown and version polling integrated; fixed UI strings in Chinese and English, unknown protocol values and free-form audit text intentionally preserved.
- [x] Backend targeted unit/transaction/concurrency/regression tests and build captured.
- [x] Frontend typecheck/build/full lint and full tests captured for immutable V5 (265 files/1907 tests passed); additive test-only full suite265files/1911tests passed and V5 rebuilt-image UI completed.
- [x] Independent cross-owner review resolved and main-agent diff/evidence review recorded; V5 fixed-enum correction verified in actual localized ledger/preview screenshots.
- [x] Desktop and narrow-screen real-page screenshots plus core interactions verified; no overlap or broken navigation in accepted captures. One V4 post-open refresh timeout recovered by read-only Retry; cause remains unknown and is documented.
- [x] Deliver worktree, baseline, summary, evidence, remaining risks and usable local URL; no push/deploy. See `evidence/director-final-acceptance.md`; commit/archive remain user-controlled.

## Evidence Boundaries

All paths below are relative to `.trellis/tasks/09-05-sub2api-carpool-v1-3/`.

- Core/accounting: `evidence/director-pg18-intermediate.md` records independently inspected real PG runs (17-test core/reconciliation run, actual lock-boundary cases,20-test reset/recovery run,2-test canonical projection run). `evidence/director-final-image-v3-backend-focused-three.log` covers the unchanged seven-package focused unit selection repeated three times. `research/director-integration.md` records source/diff review, resolved findings and exact build identities.
- Runtime HTTP/WS: V5 `evidence/source-runtime-http-20260905T235449813Z.json`67/67 and `evidence/source-runtime-websocket-20260905T235608621Z.json`11/11 bind the immutable reviewed image. All provider interactions use the isolated mock, not real cards or upstream billing.
- Reset/audience: V5 `evidence/source-runtime-reset-20260905T235712249Z.json`22/22 is pending_merge. Actual due execution was NOT performed in this HTTP run; precise execution is covered by real PG tests. First-publication evidence is explicitly composed from the original V3 passing assertions, stable read-only audience checks, deterministic driver-race regression, coherent pending-merge checks and PG publication cases. The original20-pass/1-fail report remains failed; its unretained baseline values remain unknown.
- UI: `evidence/director-ui-acceptance.md` records actual desktop/390px screenshots, one synthetic boost and exactly one UI opening for127/group17/plan8, unchanged ordinary100USD/key64 and no payment. V5 localized ledger/preview and user dashboard/details screenshots were inspected; V5 UI was read-only and viewport reset. Final conservation/local delivery remain separate gates.
- Full backend service suite is NOT green: exact-base Windows plugin rename failures and intermittent moderation timeout are reproduced in `evidence/director-baseline-service-review.md`. Compile-only results do not count as executed business assertions.
- Copied-user tier mapping was unavailable, so copied-user terms remain zero. Migration/legacy conservation uses the authorized neutralized copy; all new carpool behavior and UI evidence uses synthetic identities. No push, deployment, commit or archive is included in this acceptance.
- V5 final checklist audit reopened four adjacent-test coverage gaps. They are now closed by director-inspected exact lifecycle PG5top-level/4nested passes and frontend full1911-test pass, with unchanged product bytes proven separately. `evidence/director-final-acceptance.md` records hashes, final conservation, historical failed runs and all acceptance limits. No shared copied-runtime clock/SQL manipulation occurred.
