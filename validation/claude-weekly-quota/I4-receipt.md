# I4 backend and v2 concurrency receipt

Status: **I4 accepted** by the coordinator, including actual browser review. Base is
accepted I3 `9005acffad8f9b30975ea3dd27e82095eb9e5520`. The v2 supplement is
authorized additional scope; it does not retroactively relabel v1 acceptance.
All mutations used isolated synthetic fixtures. Production and Codex/SSH
credentials/profiles remain untouched. This iteration stops at I4 commit/push;
I5/I6 and production deployment are outside the latest requested session scope.
Remote publication is verified separately after the commit.

The accepted final F4 source is
`9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`,
image `sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9`.
Windows532/1005, Linux critical race512/968, native PG/Redis1/5 and actual HTTP
metadata1/15 pass; original GPT84 identities remain present. Frontend2440tests,
typecheck/lint/build and real user Chrome acceptance are recorded separately.
Historical F2/F3 runtime tests retain their original source/image identities.
See `coordinator-I4-review.md` for the accepted evidence and reuse boundaries.

The backend implements the published `I4-backend-contract.md`: canonical term
summary on legacy list/active/progress/summary/admin surfaces, term-first usage
metadata with separate Key constraints, sanitized request query and strict
audited Resolve HTTP. Actual Router tests cover both term and windows responses;
windows does not call the new summary reader. Missing Group edges are resolved
from stored display metadata rather than silently treating a term as windows.
Authenticated term-null display remains unavailable and never falls back to a
wallet or native window allowance.

The v2 supplement adds migration251's nullable positive user override, independent
configured default3, Ent/User/admin/auth mappings, snapshot28, per-user updates
and all-Key auth invalidation, independent Redis active/waiting scope, policy
locking and lease lifecycle. No old GPT key, queue, limit, fault policy or startup
cleanup algorithm is replaced. New dual-pool reads use a separately bounded
client and expose explicit null/unavailable states. Input accepts positive int32
only; omission preserves the override and explicit null is rejected. Legacy
batch mutation remains scoped to the original concurrency field.

The local lease safety deadline uses Redis remaining duration minus operation
elapsed time and cancellation margin, rather than comparing host and Redis wall
clocks. Redis operation timeout2s, heartbeat15s and lease60s are engineering
parameters, not a performance guarantee. Client cancellation and lost lease
cancel actual term forwarding; user/account releases remain tied to forwarding
completion. Monetary known/unknown recovery remains independent of slots.

Protected legacy groups now establish a durable marker before user-slot work,
covering queue waiting and the original billing task. Final forwarding rechecks
the same marker and identity. No-send exit is explicit no_charge; an attempted
unknown stays held. Complete mode conversion, all-replica/config/protocol checks
and final-image rollout/rollback remain I6, not claims of the local marker slice.

Actual developer verification so far:

- `go test -tags=unit -run '^$' ./...`: exit0, full repository unit compilation;
  `I4-backend-v2-debug-compile.log`.
- `go vet -tags=unit ./internal/domain ./internal/service ./internal/repository
  ./internal/handler/... ./internal/server/... ./internal/config ./cmd/server`:
  exit0; `I4-backend-v2-debug-vet.log`.
- `go test -tags=unit -json -count=1 -timeout=5m -run
  'TestClaudeSubscriptionConcurrency|TestSubscriptionQuota(Legacy|I4)|TestAdminService_(CreateUser|UpdateUser)|TestAPIKeyAuth|TestUserService_(GetProfile|GetByID)'
  ./internal/service ./internal/handler/... ./internal/server/middleware
  ./internal/server/routes ./internal/repository ./internal/config ./cmd/server`:
  exit0, 84 top-level /141 including subtests, zero failures/individual skips;
  `I4-backend-v2-debug-windows-unit.jsonl`. This84 is this command's selection,
  **not** the separate original GPT84 baseline. Source was still changing;
  this is diagnostic evidence.
- Later focused `^TestClaudeSubscriptionConcurrency` passed all selected tests
  in routes/service/admin/repository/config, including actual self/admin routes,
  null/range rejection, independent Redis availability, cancelled-task ownership,
  clock skew and failed-commit cleanup. Final stable inventory is still pending.
- Full related debug rerun `I4-backend-v2-debug-full2-windows-unit.jsonl` passed
  456top-level/878including subtests, zero failures/skips and all original84GPT
  case names. Its predecessor is retained: a legacy R1 test still classified
  supported GET `/v1/usage` as generation and used an incomplete last-used stub.
  Only that test classification was corrected; separate real usage Router tests
  retain normal term/windows and missing-reader503 assertions.
- PostgreSQL `TestSubscriptionQuotaConcurrencyV2LegacyBulkKeepsClaudePolicy`
  passed using the real repository's three old bulk methods, including original
  legacy concurrency0. Override2, null→default3, foreign7 and all other user
  fields stayed unchanged. See `I4-v2-legacy-bulk-pg-debug-receipt.json`; source
  stability is not asserted for this diagnostic run.
- Real TCP blackhole `TestClaudeSubscriptionConcurrencyBlackholeTransportHasWholeOperationDeadline`
  passed: both new read operations returned within2.00s while the original client
  kept30s/read,20retries and its old context setting. The new client has separate
  notification/maintenance state and a rebuilt bounded dialer. Project Redis
  construction has no custom proxy/dialer; network/TLS/auth semantics are retained.
- Actual `/auth/me` now uses the display-profile projection; global GetByID does
  not gain Redis reads. Actual registered routes prove default3, override2 and
  independent nullable stats; the original AuthCurrentUser compatibility test
  also passed. Two materialized Key L1/L2 caches are invalidated by actual
  AdminService→APIKeyService update and reload override3 with GPT5 unchanged.
- Root review found a nil-input check ordering regression in user repository
  creation. The new override check was moved after the original nil guard;
  actual Create/CreateWithEmailAliasGuard nil-input regression test passed.
- Safe Ent generation completed with8 changed generated artifacts, and Wire
  generation succeeded. `go build ./cmd/server` succeeded before candidate
  execution. Embedded SPA alias regression passed with `-tags=embed`;
  only already registered root API aliases bypass the SPA.

Separate environment and gateway-owner receipts contain real PG/Redis and
synthetic HTTP execution. Their debug results are not silently promoted to final
acceptance here. Final Windows F2 is complete:
`I4-windows-F2-20261019T175936Z-receipt.json` records529top-level/992including
subtests passed, zero failed/individual-skipped tests and one no-test helper
package skip. Inventory529 exactly matches529top-level passes. Every original
GPT84 Package::Test identity is present; additional existing WS/live ingress,
lease cancellation and Kiro reasoning sanitization tests are separately included
in the recorded regex. They are unit compatibility coverage, not a claim of
actual paid-provider live calls. All-repository unit compilation and selected
package vet both exited0. Before/after source hashes are identical:
`aa71d6226b7beab62cc04b5a9c584619d7fda2989e93155d3dbf69740e36f3ba`.
An earlier55a126candidate was rejected by the script's preflight hash check while
environment-owned test-fixture edits finished; it executed no tests and is not
passed evidence. F1 remains superseded.

F2 Linux race subsequently exposed an existing cleanup test-stub race: async
`RecomputeRange` incremented an integer while two `Eventually` assertions read
it. The file's Git blob was identical in production baseline8d869d, HEAD9005 and
the pre-fix working tree (`I4-preexisting-cleanup-test-race.json`). The failed
race run is retained. The coordinator approved only replacing that counter with
`atomic.Int64`, `Add(1)` and `Load()`; both assertions and their deadlines remain.
F3 source is `394913132389c24e38234d4949907d6ac2a4e67afb68a96189516e19f92543b3`.
`I4-F2-to-F3-test-only-diff.json` records exactly one changed `_test.go`; business,
frontend, provider and embedded assets are unchanged. Stable Windows cleanup
supplement `I4-windows-F3-cleanup-20261019T181105Z-receipt.json` passed31/31 with
zero failures/skips and exit0. The environment owner's full F3 Linux race rerun
`I4-unit-race-F3-20261019T181120Z-receipt.json` exited0 with stable394913source;
the main independently parsed its JSONL as509top-level/955including subtests
passed, zero failed/individual-skipped tests and inventory509. F2's529/992
Windows and actual-image receipts retain their original source identities and
need an explicit coordinator test-only compatibility mapping, not relabeling.

`I4-backend-business-freeze.json` captures75changed business files before final
whole-source freeze: aggregate
`afcd0c9e634cc79db164b12104172b0cd059c59c904e870390f6db61b3cab33c`.
It supersedes the earlier `c6d823...` boundary after the real browser exposed
the API Key auth User Select omission. `I4-F4-key-projection-receipt.md` records
the native projection fix, version28 invalidation and limited audit. Stable F4
Windows532/1005, full unit compile and selected vet all pass; original GPT84
identities remain complete. Native actual PG/Redis cold/L1/L2/v27 reload passes
1/5 on the same source:
`9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`.
Actual F4 server metadata and browser validation remain pending; earlier
F2/F3 image evidence is not relabeled as passing this changed business code.

The `c6d823...` boundary superseded `1172e41...` after root review found that
admin List performed an unbounded old-client batch read before the new bounded
display projection. The wrapper now reuses `LegacyCurrentConcurrency`, whose
reader preserves the old ordinary+live key count and Redis TIME/TTL semantics.
Its integer wrapper remains0 on error; explicit nullable/status fields remain
unknown. Two registered Admin Router tests passed, including a real TCP
blackhole that returned200 with unknown statistics in2.0002425s. The original
client30s/retries20/context setting is unchanged. See
`I4-v2-admin-list-blackhole-debug.jsonl`. F1 evidence is superseded, not final.

The prior boundary superseded `d5a65b...` after real PG exposed that
SQL COALESCE attempted to coerce invalid default2147483648 into int32 before
considering an override. The new helper reads nullable override first, then
validates the effective Go integer. Two real PG tests now pass: old bulk
preservation and manual-issue policy rejection/valid-override/successful replay.
The original failure log is retained; no existing GPT policy code was changed.
This is a review boundary, not final acceptance or the complete-source test hash.

The coordinator has completed I4 runtime/browser consolidation and review. The
overall project remains unfinished. I5 must add plan-default selection/override,
plan configuration, immutable order snapshots and paid-order recovery. I6 must
complete maintenance conversion, high-cardinality performance, final deployment
capability checks and compatible rollback. The initial explicit manual quota
UI does not waive the final plan-default obligation.
The coordinator-approved v2 scope operation-counter slice is explicitly retained
in `I6-pending-obligations.md`; financial metrics and instantaneous stats do not
complete that obligation or its final traceability assertions.
