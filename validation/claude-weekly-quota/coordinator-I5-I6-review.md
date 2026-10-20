# I5/I6 coordinator review — 2026-10-20

**F1–F4 scoped functional/browser evidence and the complete PostgreSQL 18.6
migration/history rehearsal passed. Local implementation and validation are complete.**
The 74 functional cases consolidate actual I1–I6 receipts with scoped F1–F4
follow-up verification; they are not 74 tests rerun on F4. The current I5/I6 batch
awaits the required one-shot commit-plan confirmation. No production deployment,
real-user card issuance or paid upstream consumption is authorized by this
acceptance.

## Scope and resulting behavior

The user resumed the dedicated Claude carpool task after I4; the checkout was
clean and current at `203047eda` on `codex/claude-weekly-quota`. I5 now connects
plan defaults and manual overrides to immutable paid order rights: exact quota,
group and 604800-second duration are committed before provider creation, and
paid fulfillment uses its durable lease and snapshot rather than today's plan.
Card, operation, assignment audit and cache outbox commit together; replay or
disabled sales do not revoke an already-paid frozen entitlement. Legacy refunds,
day-redemption and native lifecycle writes cannot silently alter those rights.

I6 provides actual admin maintenance, stable explicit-allocation preview,
idempotent conversion and separate release. Pending queue/async markers and paid
orders block unsafe conversion; actor, preview facts and trusted replica
advertisements are rechecked before committing. Only explicitly approved
allocations create cards; an explicit empty list is valid. Converted groups stay
maintained until release. GPT retains its original accounting/concurrency path.

Historical terminal orders have a manual-handling boundary: a valid CANCELLED
callback may recover PAID but cannot infer missing term entitlement after
conversion. EXPIRED only recovers inside its existing five-minute grace; after
grace it records `PAYMENT_AFTER_EXPIRY` without automatic PAID/rights. The
terminal-order warning never supplies recipients or allowances.

## Frozen build and change attribution

| Artifact | F1 | F2 | F3 |
| --- | --- | --- | --- |
| Source SHA-256 | `2ceb119eb44d2d8d9ba68e37e0cfe4646b51cc067628cc34175734607daaa55d` | `56f189a7e2c4c32a96ea439d15687e809f9839670f63181e5312851642b757a4` | `d6ac92adeb97249c1a792e2586c358f63da20790283c5481fee6baafa8cae1b8` |
| R2 image | `sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483` | `sha256:3e18edda6b675cb73d108c92acf139dfe1f6e45b92e5abc8aef1594d2eec39f3` | `sha256:3c491e7a7033a72206fa30cf8d4895a7678908a6781912a66e16cedcbbe435bf` |
| Compatible R1 | `sha256:7d78d912b323875ca61a4c9c716b0018716f361a7146a1443bd94c6882b3523a` | `sha256:9940143ccd871a797893e37b685d211996ce4b75e930747f84d7a099be9e6dc8` | `sha256:677a6f41dc672b1658c30e6c048bddfceab3eb770a5ac120bd5aa79ef4c1e092` |

Sources: `office-I6-build-receipt.json`, `office-I6-build-F2.json`,
`office-I6-build-F3.json`. Each build retained stable before/after source hashes.
Build success itself is distinct from the runtime results below.

`office-I6-final-source-audit.json` now covers F1→F4 and confirms the working
build inputs match F4. In its F1→F2 delta, the only production changes are
`service/payment_config_plans.go` and `service/payment_order.go`: one shared
fixed-seven-day validator accepts UI `days` and historical `day`. Both CRUD and
checkout previously rejected the UI spelling. The frozen entitlement remains
604800s; other day counts and month/year units remain invalid.

F3 fixes the separate real browser failure on the signed `resume_token` result
path. `handler/payment_handler.go` now exposes only the typed, validated frozen
quota snapshot in `PublicOrderResult`; anonymous out-trade-number verification
retains its minimal fields. Malformed, future-version or unknown-field snapshots
are not reconstructed from current plans. Frontend payment-result labels and
plan-duration display were corrected. F3 frontend distribution SHA-256 is
`29d5416ccca084276f6706984a5f096b36949a56daaace416671c74303173c36`.
The handler correction has actual unit evidence and the coordinator has observed
F3 completed results in Chinese and English at 390px. See
`I6-payment-resume-snapshot-receipt.md` and `office-I6-browser-receipt.json`.

The F4 change is confined to the actual `AdminOrdersView`: its refund action
and details display had not received the existing quota-aware dialog behavior.
The 18 actual-view and four shared-dialog focused tests (22 total), targeted lint,
typecheck and frontend production build passed. `office-I6-build-F4.json` now
records a stable source build. Actual F4 CUA passed on `/admin/orders` for two
COMPLETED rows: no automatic refund action, manual guidance, and the connected
detail shows frozen 125.12345678/168h, sale price 12.34 USD and paid 86.38 CNY despite
the changed/unlisted plan. Chinese desktop and Chinese/English 390px, keyboard
Return/Escape/focus and no horizontal overflow were verified. Other refund
statuses and delayed-response guards are covered by the 22 focused tests, not
claimed as CUA. `office-I6-browser-receipt.json` records 15 screenshot hashes with
their actual F1–F4 candidates. Frozen F4 identities:

- Source: `8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`.
- Frontend: `27e20aee4b1a066adfc079d51df9654ca105f5a2e4ac386d14b612107624d5f6`.
- R2: `sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`.
- Compatible R1: `sha256:c8083c2153e7d43598fd36b5c50f54667de05e34d62e72f609ae4110e05f9cee`.

Unchanged F1 lease/conversion/billing/metadata/WS paths retain their original
evidence under these scoped changes; do not relabel those runs as F2/F3/F4.
Affected payment/UI and actual F2/F3 rollback were separately rerun. Source
aggregate hashes and separately hashed provider/build recipes are distinct
provenance scopes, as documented by `I6-root-change-review.md`. The coordinator
has bound F4's actual changes and final artifact in the final source audit: one
production view, two test files and 225 embedded frontend paths changed from F3;
all backend production files and migration SQL are identical to F3.

## Executed verification and limits

Counts below overlap and must not be summed into a unique-test total.

| Evidence | Observed result | Scope limit |
| --- | --- | --- |
| `I6-final-unit-receipt.json` | 895 top-level / 1891 including subtests PASS across related backend packages | Regex-selected suite; predates isolated metric-log and final duration fixes |
| `I6-final-pg-receipt.json` | 193 top-level / 402 including subtests PASS, 0 failures | PG 18.1/Redis 8.4; 2 top-level / 3 total preexisting skips: two GPT release-import suites need a separate runner, one batch-load case is TODO-skipped |
| `I6-final-metrics-unit-receipt.json` | 20 tests PASS | Includes blocked-log-sink and real keeper safety timing; not upstream runtime proof |
| `I6-controlled-rollout-receipt.md` | 11 top-level / 13 total PG PASS, 0 failures/skips | Real handlers/PG, strict inputs, locks/drift, atomic faults, queue/async drain and replicas; not a production release |
| `I6-plan-duration-and-promotion-receipt.md` | F2 correction service 32/32 PASS; I5 PG 16 top-level / 32 total PASS, 0 failures/skips | Actual admin JSON POST/PUT→checkout→signed loopback provider/callback for both `days`/`day`; synthetic auth and local provider |
| `I6-payment-resume-snapshot-receipt.md` | 8 top-level / 12 total handler PASS, 0 failures/skips | Real signed-token handler contract; valid/invalid/unknown fields and anonymous non-disclosure; browser separate |
| `I6-PG18.6-rehearsal-receipt.md` | Final 12 top-level PASS, 0 failures/skips, all four history scales through 300k | Exact production-version image/executable; synthetic amd64-on-arm64 environment, not production capacity; earlier disk-stopped run retained |
| `I5-integration-receipt.md` | Earlier I5 payment 15/29 PASS; shared manual-domain 26/84 PASS | Included/overlapping regression evidence, not additional unique cases |
| `I6-performance-receipt.md` | Indexed scaling and migration-retry selection: 4 top-level PASS, 0 failures/skips | Local capacity diagnostic with a remaining measured settlement/GPT impact |
| `I5-frontend-receipt.md`, `I6-frontend-receipt.md` | I5 full 325 files/2467 tests; I6 full 327 files/2485 tests, lint/typecheck/build PASS | I6 full run precedes later warning/duplicate-replica edits, which received focused 30 and 18-test checks plus build; browser is separate |

All selected new quota/payment/conversion/migration tests in the broad PG
selection executed. Existing skipped GPT suites are not passed tests and must
remain disclosed. Later focused runs validate their actual source deltas;
they do not turn the earlier broad run into a new final-source run.

F1 actual runtime receipts already read and confirmed:

| Runtime proof | Result/source |
| --- | --- |
| Wallet matrix and metadata/unsupported routes | `office-I6-wallets-receipt.json`: 1 top-level/25 total; `office-I6-metadata-receipt.json`: 1/15; both PASS |
| Mixed actual replica and two-instance GPT5+Claude2 independent pools | `office-I6-mixed-receipt.json`, `office-I6-pools-receipt.json`: 1 top-level PASS each |
| GPT WebSocket bridge while Claude holds 2 slots | `office-I6-ws-proof.json`: two actual completed turns; Live 403/provider 0 is refusal only, not successful Live generation |
| Card boundary, Redis failure and ownership loss | `office-I6-boundary-receipt.json`, `office-I6-redis-fault-receipt.json`, `office-I6-lost-lease-receipt.json`: 1 PASS each with actual upstream behavior |
| Controlled conversion / T72 | `I6-conversion-runtime-receipt.md` and V4 machine receipts: 1 PASS, 77.92s; real pre-slot/async drain, stale windows cache refused before legacy Redis operations, preserved GPT HTTP/WS ownership and exact settlement |
| Long actual SSE / T68 | `office-I6-long-sse-receipt.json`: 1 PASS; 16m 14s, 63 observations, same original owners renewed, continuing client events, two settled/log-acknowledged receipts and exact 0.00120000 debit |
| Real queue promotion / T67 | `office-I6-promotion-receipt.json`: 1 PASS, corrected final3 driver `c228980169044eff0a381975577a2e717bd541cf197fb1baa84241e6cf3b8969`; default 3 held streams, fourth waits then promotes, four settled/logged receipts, exact 0.00240000 debit and owned-slot cleanup |
| Scope operation evidence | `office-I6-metrics-receipt.json`: 1 PASS; actual `promote.active.acquired=2`, renewal/lost-lease/cleanup/safety-deadline counters present |
| R0→R1→R2→compatibleR1, then R1 restart | `I6-image-proof-receipt.md` and corrected 02:08:51 receipt: 1 PASS on F1 with real startup migrations, signed paid callback and known Claude/GPT recovery |

Actual F2 and F3 image upgrade/rollback each passed independently: one top-level
PASS, zero failures/skips, test times 9.57s and 9.78s respectively. See
`I6-F2-image-proof-receipt.md`, `I6-F3-image-proof-receipt.md` and their machine
receipts. Both repeat signed paid callback/atomic-failure recovery, one immutable
125.12345678/604800 entitlement, known Claude 0.50000000 and original GPT 0.25000000
recovery, disabled admission and stable restart fingerprints on their named
application images. Their TestMain database is 18.1, separate from the 18.6 run.

Actual browser evidence includes manual 300 default then 75.12345678 override,
canonical `days` save, purchase 125.12345678 despite later plan 999/unlisting,
and controlled maintenance/explicit preview/stale 409/reopen/convert/release.
Zero-card conversion was previewed in browser; execution is PG evidence. F3's
signed completed result now shows frozen 125.12345678/168h and sale amount 86.38 CNY
in Chinese and English at 390px without horizontal overflow. The prior missing
snapshot remains a recorded failure, not a successful F2 result-page check.
The admin orders route's refund/details defect is separately fixed and verified
on F4; earlier failing screenshots remain preserved.

Earlier runtime failures remain named in their receipts. Conversion corrections
waited for the actual 30s delayed second invalidation and decoded structured JSON
errors; rollback corrections fixed synthetic recovery configuration and kept
the HTTP bridge input open. Promotion corrections preserve unrelated account
owners and exclude only an unrelated recovery retry's `updated_at` from the
foreign-card fingerprint. All financial/identity/interval/lifecycle/revision
fields remain checked. Product behavior was not bypassed. A changed-during-run
driver remains invalid; successful reruns supersede failures only for their
stated image and test scope.

## Local acceptance and commit boundary

The coordinator matrix now records 74 functional cases passed across the actual
I1–I6 evidence. This is a scoped evidence merge, not 74 tests rerun on F4. Runtime,
browser, source-delta and frontend checks retain their actual candidate and
coverage boundaries. The earlier `I6-acceptance-delta-draft.md` remains a
historical 02:08 snapshot.

The final PostgreSQL 18.6 run separately passed 12 top-level tests across
0/10k/100k/300k history. No local implementation or validation gate remains in
this review. Preserve the first disk-stopped attempt and separate 18.1 evidence.
The remaining workflow step is the concrete Phase 3.4 commit-plan confirmation;
no commit or push has been performed. Production inputs remain separate below.

## Production boundary and performance result

No production state changed. The recorded main-server deployment is one Compose
project `sub2api2`, service `sub2api`, container `sub2api2`, config
`/opt/sub2api2/docker-compose.migrated.yml`, behind 127.0.0.1:29080. Verified R0
`.13` provenance is retained. Operating controls, exact env names, complete
drain and compatible rollback are in `I6-production-handoff.md`; historical
BWH blue/green commands do not apply.

Earlier broad correctness and application-image runtime fixtures use 18.1.
The isolated 18.6 rehearsal verified the exact recorded production image's nine
RootFS layers and executable hash, plus running `server_version_num=180006`.
The first attempt completed 11 migration/history-index tests and 0/10k/100k,
then halted during 300k expansion at the 1.2GiB disk guard (exit -15, overall
`pass=false`). That receipt and its partial measurements remain preserved.

The separate final run in
`office-PG18.6-rehearsal-20261020T025846Z.json` passed 12 top-level tests,
zero failures/skips, in 77.26s; the scaling test itself took 47.57s. It exercised
all four scales 0/10k/100k/300k and reached 300289 request/ledger rows in a
910603967-byte database. At 300k, admission/backlog p95 were 4.602458/0.694292ms;
Claude Apply p95 was 116.195375ms and same-scope wait 111.828917ms. The full ledger
SUM's EXPLAIN `Execution Time` was 43.578ms. GPT control/mixed throughput was
342.705832/276.590470 requests/s (**19.29% lower**), with p95 latency
6.932375/11.425292ms. All commands and exact financial checks passed without
deadlocks. Free disk never fell below 4343877632 bytes (4.34GB), and the run's
owned fixtures were removed. The raw plans are in
`office-PG18.6-performance-20261020T025846Z.json`.

This completes the production-version local correctness/scaling rehearsal.
Its amd64-on-arm64 synthetic environment does not establish a production SLO,
a safe arrival rate or a maintenance-window budget. Do not combine its samples
with 18.1, interpret the different GPT percentages as a version improvement, or
use small correctness-test durations as representative production migration
timing. Production-shaped data/hardware and an agreed latency/lock/backlog
budget remain deployment prerequisites.

At 300k history in the earlier **PostgreSQL 18.1** run, optimized admission p95 was 4.23ms and backlog 0.64ms, but the full
ledger SUM remained 16.421ms, Apply p95 50.45ms and same-scope wait 47.15ms. GPT
throughput under the local mixed burst was 319.5/s versus 456.3/s alone (**30.0%
lower**), with p95 9.66ms versus 5.91ms. No deadlocks, failed commands or monetary
disagreement occurred. These 96-per-lane/two-worker measurements are not an R0
benchmark, confidence interval, production SLO or safe arrival-rate commitment.
Retaining exact ledger consistency is deliberate; the settlement bottleneck
and original GPT impact remain disclosed, with an agreed deployment budget
and limited-cohort observation needed before expansion.

Business/operator inputs still needed: exact target groups; complete explicit
user/quota allocations or expressly approved empty set; handling owner for
pending/late-paid orders; maintenance window and operational latency/backlog
budget; approved plan default quota, price/currency/listing if selling; and an
authorized test identity/consumption allowance for any paid upstream smoke test.
Recheck live replica inventory, admin access, readable backups and frozen R1.
Development completion does not supply those decisions or authorize deployment.

## Workflow Phase 3.4 conclusion

The local workflow still requires one concrete batched commit confirmation.
`.trellis/workflow.md:600` marks 3.4 `[required · once]`; step5 says
**“Present the plan once, ask for one-shot confirmation”** and step6 says
**“On confirmation”** to run explicit `git add`/`git commit`, **“Do not amend.
Do not push.”** The `trellis-finish-work/SKILL.md` Step2 routes uncommitted
current-task work back to 3.4; it does not grant permission to auto-commit it.

The current “拉取最新的代码，完成剩下的” authorizes the remaining work, not a
reviewed I5/I6 commit plan. Earlier I4-only commit/push approval does not cover
this new batch. After all authorized implementation/validation/doc work is
concrete, the coordinator should inspect/classify every dirty file, exclude or
identify unfamiliar work, learn the existing message style and present a single
logical file/message plan. Ask once and explain that the requirement comes from
this exact workflow; do not ask separately per commit or re-request existing
scope approvals. Do not push. Archive/journal auto-commits belong after approved
work commits, never before them.

