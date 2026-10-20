# I6 production handoff — prepared 2026-10-20

> Superseded topology: the user subsequently authorized production release and
> blue/green setup, completed 2026-10-20 03:58:50 UTC. Read
> `production-deployment-20261020.md` for live operation. The single-instance
> sequence below is historical. Business allocations are still required before
> converting real groups; code deployment is complete.

This is the reviewable operating sequence for the current single-instance
`main-server` deployment. It is not a record of a production release. Final image
identities and local acceptance are recorded separately in the office receipts.
Do not use historical BWH blue/green commands for this host.

**Local implementation, F4 scoped browser acceptance and the complete
PostgreSQL 18.6 rehearsal passed; no production deployment approval is implied.**
F1 long-stream/promotion/operation metrics and actual F2/F3 image rollback passed. F3's signed payment-result snapshot and Chinese/English
390px display are verified. F4's actual admin-orders refund/details CUA passed;
the PostgreSQL 18.6 migration/history/scaling run also passed through 300k.
The current review is `coordinator-I5-I6-review.md`; the coordinator owns final
status recording and the reviewed commit plan.

## Verified deployment and release inputs

The recorded read-only inventory (2026-10-19 17:20 UTC, not a fresh live
inspection by this document reviewer) found Compose project `sub2api2`, service
**`sub2api`**, application container **`sub2api2`**, and configuration
`/opt/sub2api2/docker-compose.migrated.yml`. Its other services are `postgres`
and `redis`, with containers `sub2api2-postgres` / `sub2api2-redis`. Nginx routes
to the sole app on `127.0.0.1:29080`. See
`coordinator-release-inventory-readonly.json` and
`coordinator-release-edge-readonly.json`; do not substitute `sub2api2` for the
Compose service name when preparing an app-only replacement.

The observed R0 tag is `sub2api:2.8.13-monas-bps.13`, revision
`8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`, linux/amd64. The 2026-10-20 office
verification in `office-r0-provenance.json` matches the production OCI graph,
all 20 RootFS layers and executable SHA-256. Containerd's displayed OCI index
`sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677`
is distinct from its image configuration digest
`sha256:7e6d174ece5f28faf73680802bd52ff0b34a30bd59600125e9aaf53e843aa7a6`;
compare the verified graph and binary, not just tag or displayed ID. Recheck
these facts, configuration digest and remote revision immediately before an
approved release. This document has not contacted production.

The remaining business inputs are the target Claude group, the complete explicit
user/quota allocation list (an explicitly approved empty list is also valid),
an owner and handling decision for pending/late-paid legacy orders, the
maintenance window and operational latency/backlog budget, and a designated
synthetic/test account with consumption
authorization for any paid upstream smoke test. If new plans will be sold, obtain
the approved plan default quota, sale price/currency and listing choice separately;
sale price is not an allowance. Existing balances, historical
keys and current plan values are not allocation instructions. Preserve account
bindings, models, rate multipliers and proxy routing unless separately requested.

## Exact configuration and control boundary

These are the actual Viper environment names. Values below are deployment
inputs, not permission to populate real recipients or activate a group.

| Setting | Compatible R1 / rollback | Reviewed R2 conversion |
| --- | --- | --- |
| `RUN_MODE` | `standard` | `standard` |
| `DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY` | `3` | `3`; positive per-user overrides remain independent of GPT |
| `SUBSCRIPTION_QUOTA_ISSUE_ENABLED` | `false` | `true` |
| `SUBSCRIPTION_QUOTA_ADMISSION_ENABLED` | `false` | `true` |
| `SUBSCRIPTION_QUOTA_RECOVERY_ENABLED` | `true` | `true` |
| `SUBSCRIPTION_QUOTA_ENABLED_GROUP_IDS` | Retain reviewed group list | Complete comma-separated positive IDs including the target |
| `SUBSCRIPTION_QUOTA_PROTECTED_GROUP_IDS` | Retain reviewed protected windows list | Complete comma-separated positive IDs including the target |
| `SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID` | Unique deployment identity | `app-1` for the verified single replica |
| `SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS` | Complete actual roster | `app-1` for the verified single replica; comma list if topology changes |

The R1 and R2 image recipes both default to issue/admission off and recovery on;
choosing the R2 tag alone does not enable it. These switches concern Claude term
issuance/admission, not the existing global payment or GPT switches. Paid frozen
orders and known usage continue recovery independently of new-sale policy.
Conversion and maintenance exit require all expected runtime advertisements to
agree on versions1/default 3, all three enabled flags, roster and enabled/protected
lists. Registration comes from the running config after DB readiness, never
from an admin request body. Heartbeats are every 10s, fresh for 45s. Entering
maintenance may stop traffic despite unhealthy replicas. A correct configured
roster must first be reconciled with the actual deployment inventory.

The process-wide replacement gate is separate from per-group quota maintenance:
GET `/api/v1/admin/release/status`, POST `/release/drain`, then POST
`/release/lock` with that process's `operation_id` after `active_http` and
`pending_usage` reach zero. These paths are under `/api/v1/admin` and require
admin authentication. On an aborted pre-mutation drain use `/release/cancel`
with the same operation ID and valid current state. `/release/resume` is for
that same process and requires the operation ID plus positive distinct
`user_ids` and `group_ids` for cache invalidation. The old process's ID cannot
resume a newly recreated process. Normal replacement keeps
`RELEASE_DRAIN_START_HELD=false`; verify new-process readiness and open state.
Do not invent a cross-process resume token or use the original GPT import CLI
to mint Claude cards. The inventory had no optional global admin API key;
authenticated operator access must be established without logging credentials.

## Single-instance upgrade

1. Verify the immutable R1/R2 image IDs, source/frontend/migration hashes and
   final local acceptance. Prepare a readable current database backup and retain
   the current Compose configuration privately, without emitting credentials.
2. Apply the existing release gate's full drain before replacing this sole app.
   Observe protected Claude requests, ordinary/GPT HTTP/SSE/WS traffic and known
   asynchronous billing until they have finished. Unknown usage remains an
   explicit blocker for investigation. Do not start a second instance while
   original GPT slots are populated: original startup cleanup is unchanged.
3. Start the compatible R1 image with new sales/admission disabled and recovery
   enabled. Normal startup applies additive migrations through 254. No old
   balance, ledger or native entitlement is rewritten. Confirm the selected
   image, readiness and exact unauthenticated business-route rejection, as well
   as original GPT behavior. A lock timeout leaves rollout stopped; investigate
   the blocker, do not extend timeouts blindly or mark a migration applied.
4. Configure the R2 instance with independent Claude default `3`, complete
   `SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS=app-1`, unique
   `SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID=app-1`, target group in both enabled and
   protected lists, and issue/admission/recovery flags enabled. Repeat complete
   drain for replacement. The target remains in its original windows mode until
   the controlled conversion API commits. Confirm the actual runtime heartbeat,
   versions and group lists; old duplicate identities age out after 45 seconds.

## Controlled group conversion

Use the actual admin group interface (or the equivalent authenticated API).
Every POST suffix below appends to the same full `/rollout` base shown by GET:

- GET `/api/v1/admin/groups/:id/subscription-quota/rollout` returns current state,
  affected key users, unfinished protected requests/orders, historical terminal
  order warning count and replica blockers.
- POST `/maintenance` with explicit `enabled:true`, `expected_revision`, reason
  and `Idempotency-Key`. The maintenance operation is available even when the
  replica roster is unhealthy so traffic can be stopped safely.
- Wait for every durable protected request to finish, including requests queued
  before a user slot and trusted usage awaiting accounting. Resolve unfinished
  orders without guessing entitlements. A valid CANCELLED callback may recover
  PAID; a now-term group with no frozen snapshot then fails fulfillment for
  manual review and issues no card. EXPIRED recovers only within its existing
  five-minute `updated_at` grace. Beyond grace the current code records
  `PAYMENT_AFTER_EXPIRY` audit evidence without automatically restoring PAID or
  issuing rights. Preserve and investigate this evidence.
- POST `/preview` with the complete explicit `allocations` array. Review each
  user and positive USD quota string with at most eight fractional digits
  (canonical storage/display has eight). The server binds the actor, revision,
  configuration and relevant database facts. No recipients are selected merely
  because they have a key.
- POST `/convert` with that exact allocation set, `expected_revision`,
  `preview_token` and a stable `Idempotency-Key`. A conflict requires refresh and
  review. On transport loss, replay the same body/key to recover its result.
  Mode, cards, the durable conversion-operation receipt and cache outbox
  commit together; the group stays maintained.
- Verify the committed cards, cache outbox and actual replica roster; review
  any remaining blocker or late-order manual case. Conversion requires all
  conversion blockers cleared; the separate maintenance-exit API specifically
  rechecks the replica safety gate. POST `/maintenance` with `enabled:false`,
  current revision,
  reason and a new stable operation key. This restores the prior group status.
- Use only the explicitly authorized test identity for paid upstream verification.
  Check exact per-request receipt/ledger/usage logs, original GPT wallet and
  concurrency isolation. No production customer key is a smoke-test credential.

## Rollback and observation

The compatible R1 is the same final executable/filesystem with new issuance and
admission disabled, while recovery stays enabled. After full drain, replace the
app with the frozen R1 configuration and retain the newest database and Redis
state. Known Claude usage and already-paid frozen orders must complete once;
new term generation refuses explicitly. Original GPT remains on its own path.
Do not switch a converted group back to windows, restore an old database over
new billing, delete quota tables, clear shared concurrency keys or roll back to
unpatched R0 `.13`.

Observe pending/known/reconcile counts, oldest age, ledger mismatches, usage-log
acknowledgements, outbox failures, and bounded `claude concurrency metrics`
operation counters. Stop expansion on any cross-wallet write, double debit,
unknown-to-zero conversion, replica inconsistency or new GPT regression.

The local high-history performance experiment is diagnostic, not a production
capacity promise. Partial indexes remove avoidable history scans from admission
and backlog; exact ledger SUM verification remains linear in a card's history.
At 300k settled history in **PostgreSQL 18.1**, the indexed run measured admission p95 **4.23ms**, backlog
p95 **0.64ms**, Apply p95 **50.45ms** and same-scope wait p95 **47.15ms**; the full
ledger SUM still took **16.421ms**. In that local mixed burst, original GPT
throughput was **319.5 requests/s versus 456.3/s** alone, approximately **30.0%
lower**; GPT p95 rose from 5.91ms to 9.66ms. This is a 96-request-per-lane local
experiment with two workers per lane, not an R0 hardware capacity comparison or
a production SLO. It observed no deadlock, failed command or ledger disagreement.
Do not describe it as negligible impact or use its roughly 59 Claude completions/s
as a configured safe arrival rate. See `I6-performance-receipt.md` for workload
and measurement limits. Before expansion, establish the deployment's actual GPT
baseline and explicit acceptable latency/backlog budget; stop expansion on rising
known-usage backlog or unacceptable GPT regression while retaining recovery.

The earlier broad correctness and application-image runtime harnesses use
**PostgreSQL 18.1**. A separate isolated rehearsal uses the exact recorded
production **18.6** image: all nine RootFS layers and the executable match, and
the live test server returned `server_version_num=180006`. The final run passed
12 top-level tests with zero failures/skips in 77.26s, including 11 migration/
history-index checks and the 47.57s scaling test. It covers the 297-migration
upgrade, replay, genuine invalid-index recovery, bounded timeout restoration,
and 0/10k/100k/300k history. See `office-PG18.6-runtime-provenance.json`,
`office-PG18.6-rehearsal-20261020T025846Z.json` and
`I6-PG18.6-rehearsal-receipt.md`.

The final 18.6 run reached 300289 request/ledger rows and 910603967 database bytes.
At 300k, admission/backlog p95 were 4.602458/0.694292ms, Claude Apply p95
116.195375ms, same-scope wait 111.828917ms and full ledger SUM EXPLAIN
`Execution Time` 43.578ms. GPT control/mixed throughput was 342.705832/276.590470
requests/s, **19.29% lower**, with p95 latency 6.932375/11.425292ms. There were no
command errors or deadlocks, and all financial invariants passed. Minimum free
disk was 4343877632 bytes; owned fixtures were cleaned. Raw measurements/plans:
`office-PG18.6-performance-20261020T025846Z.json`.

The first 18.6 attempt remains a separate incomplete result: 0/10k/100k finished,
then 300k expansion crossed the 1.2GiB disk floor (exit -15). Its receipt and
measurements were not overwritten as passes. Neither that sample nor the final
18.6 sample is combined with the historical 18.1 experiment above. These runs
use different timings/shared load, with 18.6 under amd64 emulation on arm64; their
percentages are not a measured PostgreSQL-version improvement. The local check
is complete, while production-shaped data/hardware, migration-duration/lock
planning and an acceptable maintenance-window/latency/backlog budget remain
required before an authorized deployment.

## Frozen artifacts and acceptance evidence

`office-I6-build-F3.json` records the successful F3 build with source
`d6ac92adeb97249c1a792e2586c358f63da20790283c5481fee6baafa8cae1b8`, R2 image
`sha256:3c491e7a7033a72206fa30cf8d4895a7678908a6781912a66e16cedcbbe435bf`
and compatible R1
`sha256:677a6f41dc672b1658c30e6c048bddfceab3eb770a5ac120bd5aa79ef4c1e092`.
These are frozen F3 identities. The subsequent frontend-only F4 build is
recorded in `office-I6-build-F4.json`: source
`8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`, frontend
`27e20aee4b1a066adfc079d51df9654ca105f5a2e4ac386d14b612107624d5f6`, R2
`sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`
and compatible R1
`sha256:c8083c2153e7d43598fd36b5c50f54667de05e34d62e72f609ae4110e05f9cee`.
F4 has a stable build and actual admin-orders CUA PASS. The final source audit
confirms only `AdminOrdersView` changed as product source from F3; all backend
production files and migrations remain identical.

F1→F2 changed only the two fixed-seven-day service validators to accept
UI `days` and historical `day`. F3 then added the typed, validated frozen snapshot
to the signed resume-token result, preserving anonymous verification's minimal
fields, and corrected the related payment/plan UI. The F2 duration correction
passed service 32 and I5 PG 16 top-level/32 total checks. F3's signed-result handler
selection passed 8 top-level/12 total checks, including malformed/unknown-field
snapshots and anonymous non-disclosure.

F1 actual wallet/metadata/pools/WS/mixed-replica, boundary, Redis-fault,
lost-lease and controlled conversion receipts passed. The long SSE passed after
16m 14s with renewed original owners, client progress and exact 0.00120000 debit.
Real default 3 queue promotion passed with four completed/settled/logged streams,
exact 0.00240000 debit and cleanup of this test's owned slots/tickets; unrelated
account leases remain protected. Final metrics passed with actual
`promote.active.acquired=2` and renewal/loss/cleanup/safety-deadline evidence.
The three machine receipts are `office-I6-long-sse-receipt.json`,
`office-I6-promotion-receipt.json` and `office-I6-metrics-receipt.json`.

Actual R0→R1→R2→compatibleR1 recovery/restart passed independently on F1, F2
and F3. F2/F3 named-image proofs are `I6-F2-image-proof-receipt.md` and
`I6-F3-image-proof-receipt.md`; they include signed paid-order recovery with
issuance/admission disabled, known Claude/GPT exact settlement and stable
restart. Preserved failed attempts are not erased. These results retain their
original source/image/driver identities; the F1 long lease run is not labeled
as a new F3 run. The final source audit documents reuse for unchanged paths.

The coordinator's actual browser results cover manual default/override,
canonical `days` save and frozen purchase despite plan mutation, plus explicit
maintenance/preview/409/reopen/convert/release. F3 paid results show frozen
125.12345678/168h and 86.38 CNY in Chinese and English at 390px; the missing F2
snapshot remains a recorded failure. A separate actual admin-orders route
exposed an unconnected details/refund guard, so F4 changes `AdminOrdersView`.
F4's 22 actual-view/dialog focused tests, lint, typecheck and frontend build passed.
Actual F4 CUA on `/admin/orders` verified COMPLETED rows, absent automatic-refund
actions, manual guidance and the connected frozen-rights detail, with Chinese
desktop and Chinese/English 390px layouts, Return/Escape/focus and no horizontal
overflow. Other refund statuses and response-race guards are automated-test
coverage, not claimed browser runs. `office-I6-browser-receipt.json` records the
scoped F1–F4 acceptance and 15 candidate-bound screenshot hashes.

The coordinator's matrix records 74 functional cases passed using their actual
I1–I6 evidence; it is not 74 fresh F4 runs. The separate PostgreSQL 18.6 rehearsal
is also complete. No local implementation/validation item remains; the concrete
batched commit plan still needs the workflow's one-shot confirmation. Production
inputs and deployment authority remain separate.

Actual image replacement/recovery evidence does not claim that the process-wide
release drain API was exercised in that image driver; the full drain sequence
above remains mandatory for an approved single-instance deployment.

Even a complete local acceptance set does not supply current live inventory,
production-shaped migration timing and operating budget, business allocations
or deployment authority. No production group, user allocation or paid smoke test
is implicitly authorized by this prepared document.
