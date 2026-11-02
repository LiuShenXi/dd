# I6 retained obligations — updated 2026-10-20

> Latest update: the user explicitly authorized commits and deployment after
> this review. Those actions and uninterrupted blue/green setup are complete;
> see `production-deployment-20261020.md`. Historical approval blockers below
> no longer apply. Real group conversion/issuance still needs explicit business inputs.

This review incorporates completed F1 runtime and F2/F3 rollback/browser
results, F4 actual admin-orders CUA PASS and the complete production-version
PostgreSQL 18.6 rehearsal through 300k history. Local implementation and validation
are complete. The continuation authorizes I5/I6 development and local verification; it does not inherit I4 commit/push
approval or authorize production conversion, real-user issuance or paid upstream
use. The coordinator owns the final matrix and completion decision.

## Implemented and evidenced

- I5 exact-decimal frozen entitlements, durable leased fulfillment, recovery
  with new issuance disabled, legacy/refund guards and manual defaults with
  explicit overrides are implemented. F2 fixes the UI `days`/historical `day`
  mismatch via one fixed-seven-day validator. Service 32 and I5 PG 16 top-level/32
  total passed with no failure/skip; see the duration/promotion receipt.
- F3 fixes signed `resume_token` results to expose the typed, validated frozen
  snapshot. Invalid/future/unknown-field snapshots are stripped and anonymous
  out-trade-number verification remains minimal. Handler 8 top-level/12 total
  passed. The coordinator's actual F3 Chinese/English 390px result shows
  125.12345678/168h separately from 86.38 CNY. The F2 missing-snapshot failure is
  retained; it was not a successful completed-result check.
- Maintenance/preview/confirmed conversion and explicit release use explicit
  allocations, ordered locks, final actor/group checks and atomic
  mode/card/operation/outbox. PG 11 top-level/13 total passed. Queue/async markers
  block conversion; unknown usage remains held. Actual F1 V4 conversion passed
  with real drain, stale-cache refusal before legacy Redis and preserved GPT
  HTTP/WS ownership/settlement. Browser stale 409 and close/reopen during actual
  requests passed; zero-card conversion execution is PG evidence, while browser
  evidence covers its explicit empty preview/confirmation requirement.
- Actual F1 mixed-replica, independent 5+2 pools, wallet/metadata, GPT WS, card
  boundary, Redis outage and lost-lease cancellation passed. Fixture 5/2 are not
  the production default 3. Live 403/provider 0 proves refusal only.
- F1 long SSE passed after 16m 14s with 63 observations, continuing client events,
  renewed original owners and two exact settled/log-acknowledged receipts
  totaling 0.00120000. Actual default 3 promotion passed: three held streams,
  fourth queued without upstream call, one release promotes it, four settled/
  logged streams totaling 0.00240000, other financial state unchanged and all
  newly owned slots/tickets cleared. Existing unrelated account leases were
  preserved. See the long-SSE and promotion machine receipts.
- Actual final scope counters passed, including `promote.active.acquired=2`,
  active renewal/error/missing, lost lease, cleanup and safety deadline. These
  supplement the 20 metric/lease unit passes, including a blocked logger.
- History indexes/backlog filters retain the full ledger SUM. The 18.1 300k
  experiment passed correctness checks but measured substantial settlement/GPT
  cost. The exact 18.6 final migration/history/scaling run passed 12 top-level
  tests, zero failures/skips, at 0/10k/100k/300k. Its initial disk-stopped attempt
  remains retained; the final result is not substituted by 18.1 evidence.
- Actual R0→R1→R2→compatibleR1 recovery/restart passed independently on F1, F2
  and F3, each bound to its image/source/driver. F2/F3 each have one top-level
  PASS and zero failures/skips. They prove paid recovery and exact known
  Claude/GPT recovery with new issue/admission disabled. Earlier failed attempts
  remain preserved. Unchanged F1 paths retain F1 attribution.

## Functional acceptance and local closeout

F4's actual `AdminOrdersView` correction passed 22 focused tests, lint, typecheck,
build and actual CUA. Two COMPLETED rows expose View/manual guidance and no
legacy refund action; detail preserves 125.12345678/168h separately from 12.34 USD
sale price and 86.38 CNY paid amount despite the edited/unlisted plan. Chinese
desktop and Chinese/English 390px, keyboard Return/Escape/focus and no horizontal
overflow passed. Other refund statuses and delayed-response guards are covered
by the 18 actual-view/four dialog tests, not claimed as CUA. The final browser
receipt contains 15 screenshot hashes with their actual candidates.

The final source audit binds F1→F4: F4 changes only one product view from F3,
plus two test files and 225 embedded frontend paths. Backend product source and
migrations remain identical. The coordinator matrix records 74 functional cases
passed across I1–I6; no claim that all 74 reran on F4.

No local implementation or validation task remains. The final PostgreSQL 18.6
receipt `office-PG18.6-rehearsal-20261020T025846Z.json` passed 12 top-level tests
with zero failures/skips; it is separate from the 74 functional cases. The first
run's 11 migration/history passes, completed 0/10k/100k measurements and overall
disk-stop/incomplete outcome remain preserved.

The remaining workflow requirement is one reviewable batched commit plan and
Phase 3.4 one-shot confirmation. Earlier I4-only authorization does not approve
these changes. No automatic push or pre-approval archive is authorized. The
coordinator owns final status recording; production obligations follow below.

## Production obligations remain separate

- Recheck actual main-server topology and immutable artifacts. Recorded topology
  is one Compose project `sub2api2`, service `sub2api`, container `sub2api2`, with
  `/opt/sub2api2/docker-compose.migrated.yml`; use full drain before replacing
  the sole app. Preserve existing startup cleanup. Two-instance tests start both
  apps before GPT traffic; starting a new old-cleanup process during live GPT
  is not evidence of preserved slots.
- The exact recorded PostgreSQL 18.6 image matches nine RootFS layers,
  executable hash and running version 180006. The final 12-test run completed
  all four scales through 300k (300289 request/ledger rows, 910603967 database
  bytes), with no command error/deadlock and exact financial invariants.
  Local runtime/version validation is complete. A synthetic amd64-on-arm64 run
  still does not supply representative production data/hardware, migration
  duration, lock budget or an authorized maintenance window. No down-migration,
  historical-ledger deletion, database restore over new money or term→wallet
  fallback is permitted.
- In the final 18.6 300k measurement, GPT throughput was 342.705832/s control and
  276.590470/s mixed (**19.29% lower**); GPT p95 was 6.932375/11.425292ms.
  Claude Apply p95 was 116.195375ms, same-scope wait 111.828917ms and full ledger
  SUM EXPLAIN `Execution Time` 43.578ms. Admission/backlog p95 were
  4.602458/0.694292ms. Preserve the earlier disk-interrupted attempt; do not pool
  its partial samples or the 18.1 values below into a version-improvement claim.
- At 300k history in the earlier **PostgreSQL 18.1** experiment, mixed GPT throughput was 319.5/s versus 456.3/s alone,
  about 30% lower; GPT p95 was 9.66ms versus 5.91ms. Claude Apply p95 was 50.45ms,
  same-scope wait 47.15ms and full ledger SUM 16.421ms. No numerical production SLO
  was provided. Do not claim negligible overhead, certify unlimited traffic or
  increase workers to bypass a scope-held SUM. Establish the deployment's own
  GPT/backlog/latency budget, monitor a limited approved cohort and stop expansion
  on unacceptable regression while retaining recovery. Higher-scale summary
  designs require independent corruption/replay/fault proof.
- Obtain exact Claude group IDs, explicit recipient user IDs and strict quota
  strings (or an explicitly approved empty set), pending/late-payment handling
  owner, maintenance window and operational budget. If selling, obtain separate
  plan default quota, price/currency and listing choice. Any paid smoke test
  needs a designated test identity and consumption allowance. Existing customer
  Keys, balances and plan values are not allocation or consumption approval.
- Enumerate every actual serving replica, establish authenticated operator
  access, verify readable backups and frozen compatible R1. The heartbeat table
  cannot discover an unlisted legacy process. Final local passes do not supply
  these operational inputs or authorize a production change.

See `coordinator-I5-I6-review.md` and `I6-production-handoff.md` for the current
review evidence and concrete control paths. The earlier I4 acceptance remains a
historical receipt; its stopped scope and push authorization are not current.
