# I3 — durable Claude admission, settlement and recovery

Status: accepted by the coordinator on frozen F3. Final Windows, PostgreSQL,
Linux race and actual local R1 recovery/restart verification passed. The added
T16 case proves an already-issued
Claude card is unchanged by actual GPT term
termination and can still settle its own consumption. F1/F2 evidence is
retained but superseded. This is not a release or production activation receipt.
Baseline stages are I0 `311d11718`, I1
`92bd96e6` and I2 `0d61e956e2a9d86fbe7fb9a24adbdb47e687c486`.

No production card, consumption, migration, account, Key or deployment action
was performed. All requests, database fixtures, faults and images are isolated.

## Implemented monetary boundary

- Final actual sender preparation resolves detached pricing rules, then the
  repository admits with its final database clock under scope → parent → terms
  by ID → late identity locks. Waiting across a card boundary reselects once;
  the final request receives its own `subquota:UUID`, immutable original card,
  Key/user/account identity and complete pricing envelope/hash.
- Admission requires the actual sender's selected account type. The final
  database account lock must return that exact type; an administrative type
  change during admission wait refuses before request insertion or upstream
  send instead of freezing a different account-counter policy.
- Multiple Keys share the user's original card. Known pending costs count
  against new admission. Unknown consumption blocks that card without becoming
  zero. GPT, native windows, ordinary/frozen balance and other users' cards are
  not alternate sources.
- `RecordUsage` branches on its explicit immutable term snapshot before the
  legacy calculator, simple-mode path or any legacy fallback. Complete trusted
  provider usage, including explicit zero, is priced with frozen rules and
  durably recorded before atomic Apply. Partial input/cache observations with
  unknown output remain unknown and held.
- New receipt version 1 uses strict eight-decimal strings and exact decimal
  counter fields throughout JSONB replay, fingerprint, ledger and SQL bindings.
  Invalid/mixed raw sources are rejected before normalization. Existing GPT
  normalization order and old carpool version-1 fingerprints are unchanged.
- Pricing JSON canonicalization normalizes exact JSON numbers without float
  conversion (`1e-7` and `0.0000001`, `1.0` and `1`). JSONB round trips retain
  the original pricing hash and receipt identity. Returned/context snapshots
  detach raw JSON buffers.
- Apply verifies persistent identity, pricing hash, first known receipt,
  amount, dedup/archive and original debit. Post-charge overage stays on the
  original card. Later cancellation, expiry, Key/account suspension or soft
  deletion does not waive admitted charges or redirect them to another source.
- A proven ledger/used discrepancy rolls back the attempt to a savepoint while
  retaining its scope lock, then records a durable original-card hold. New
  known requests become reconcile-required; terminal requests keep their
  original terminal facts and a ledger-mismatch diagnostic. Dedup, money and
  ledger remain unchanged. Admission recognizes that card's hold without a new
  per-admission ledger scan; an old-card hold does not block a new card.
- Late contradictory usage preserves the first known amount/payload. Settled
  and no-charge terminal results remain terminal, with an appended conflict
  diagnostic instead of disappearing from user settlement history.

## Recovery, audited resolution and publication

- Workers commit short `SKIP LOCKED` claims before scope locks. Each item gets
  a fresh worker-root child timeout; one timeout does not poison later items,
  and Stop cancels the root. Retries use five-second exponential backoff capped
  at five minutes. Issuance/admission switches do not disable recovery.
- Actual forwarding has no global maximum duration. A ten-second heartbeat
  extends a thirty-second admission lease through forwarding and mandatory
  usage persistence. Stale recovery requires both lease expiry and the idle
  grace, and rechecks them after locks. This maps the architecture's maximum
  duration-plus-grace requirement to observed liveness without changing
  entitlement duration or granting a timeout refund.
- Audited Resolve requires an active admin, expected request revision,
  idempotency, reason and evidence. It only resolves genuinely unknown requests
  with verified usage through the frozen calculator or explicit no-charge
  evidence. It accepts no arbitrary amount, replacement command or new price.
  Known facts/ledger and late usage win through the same scope protocol.
- Closing cards converge to closed after their last unfinished request ends.
  Natural state maintenance uses database time, independent scope contexts and
  the original issued interval. Usage and maintenance never increment parent
  schedule revision.
- Fresh Apply durably emits original-Key auth invalidation and original-account
  scheduler/last-used facts in its transaction. Rate-limit cache invalidation
  runs before statistics acknowledgement in both the live provider and recovery;
  a failure leaves publication pending for retry. No legacy finalize function
  queues parent subscription or balance increments for term requests.
- Typed prompt-free statistics payloads validate request/card/user/Key/account,
  model/tokens and exact actual amount before known persistence. Statistics use
  the durable receipt, seven exact cost SQL arguments and independent frozen
  account-statistics rules. Account cost is not added to user allowance use.
- Statistics writer order is dashboard writer → scope → parent → term → request
  → INSERT/FK. The original request-only writer exposed a real PG `40P01`
  parent/Key lock cycle against Apply; its failure and lock graph are retained.
- First historical term-log insertion adds its exact contribution to already
  published hourly/daily buckets in the same transaction, preserving bases
  whose raw rows were already cleaned. Replays do not add twice. Normal
  dashboard writers share the writer lock; ordinary GPT log insertion and
  billing do not acquire it. The existing group-rollup trigger is retained.
- All manual/scheduled batch retention paths protect unacknowledged term logs,
  match `(tableoid,ctid)` across partitions and repeat predicates at deletion.
  Monthly DROP keeps its old fast path unless immutable admitted-month requests
  or unacknowledged logs require protection. After acknowledgement, stale worker
  snapshots cannot reconstruct logs removed by ordinary retention.

## Compatibility and operational boundary

Migration 250 preserves existing ten-decimal statistics values while widening
the seven usage-log costs to NUMERIC(22,10) and published aggregate costs to
NUMERIC(30,10). PostgreSQL required dropping/recreating the existing UPDATE
trigger's same WHEN definition within the migration transaction. The first
failed migration run remains evidence. Original 297 migrations are not edited;
Ent temporary generation produced only the necessary precision metadata change.

Admission startup checks the actual billing repository, current codecs, 245–250
migration checksums, required tables/links, valid online index and monetary
column capability. Missing dependencies/schemas fail closed; no term path uses
legacy money fallback. Config defaults keep issue/admission off and recovery
on; explicit standard mode, enabled group IDs and recovery are required to turn
admission on. Wire drains mandatory usage before stopping recovery, then stops
other workers, billing cache and infrastructure in dependency order.

Process metrics follow the existing snapshot plus structured-log convention:
controlled admission/settlement outcomes, pending known versus reconcile,
oldest known lag, ledger mismatch, missing statistics, retry/failure counts,
exact cumulative realized overage and scope-lock wait. No user/request/Key
credentials or unbounded exception strings are metric labels.

Actual Messages OAuth/setup/API passthrough/Bedrock/Vertex and Chat/Responses
adapters have separate synthetic transport/real local router evidence in
`I3-gateway-pricing-receipt.md`. The baseline contains no Kiro implementation.
Chat/Responses with Bedrock refuse before sending because their existing
adapter has no signed Bedrock implementation. Term requests permit one real
send after admission; no automatic retry/failover is attempted without a proof
of equivalent pricing and no prior consumption. Ordinary GPT retries remain
unchanged. Unsupported server tools and WebSearch emulation refuse before
sending, because estimated display tokens are not trusted billable usage.

Both count_tokens aliases are authenticated non-consuming metadata. They do not
require an active card or create admission/ledger rows, and do not grant
generation rights. `/v1/usage` term projection remains an explicit I4 obligation.

## Evidence and remaining exit work

Final F3 source: `c9d25ce07053fba7642a8eb4473288b88877d4bec62472df8cf2180ef4b7eebe`
(HEAD `0d61e956`, explicitly dirty/frozen). Embedded frontend remains
`b657870fcdf990281b559656e5bd671843e0f626608a9bfd3e7dcc0d26d96542`.

F2 only added a test fixture, and was superseded to assert preservation of an
already-issued Claude card before invoking actual GPT Terminate. No business
implementation changed between these verification freezes. F3 includes the
same test correction and expands final PG scope with all 13 original automatic
reset execution tests. `I3-pg-F3-vs-F1-source-diff.json` confirms the only
changed source is the independent-wallet integration-test file.

Final Windows receipt: `I3-windows-F3-20261019T160855Z-receipt.json`.
Both before/after manifests match F3; all three commands exited zero:

| Command / scope | Actual result |
| --- | --- |
| `I3-windows-final.ps1 -Freeze F3 -ExpectedSource <F3>` functional run, pinned Go 1.27.0 Windows | 356 top-level / 692 including subtests PASS; zero failed or individually skipped tests; one no-test helper package skip. Domain 14, service 226, repository 75, handler 22, admin 5, DTO 3, routes 10, cleanup command 1. The receipt records the exact run regex, packages, discovered inventory and complete JSONL. |
| Original I0 GPT baseline set comparison | All 84 exact `Package::Test` names are among final PASS events; zero missing. Includes original HTTP/WS and billing tests, alongside the new real local-provider JSON/SSE compatibility tests. |
| `go test -tags=unit -run '^$' ./...` | All-package compile PASS (`...-compile.log`); not claimed as business execution. |
| `go vet -tags=unit` on domain/service/repository/handler/server/cmd packages | PASS, empty `...-vet.log`. |

Final PostgreSQL receipt: `I3-pg-F3-20261019T160927Z-receipt.json`.
`I3-pg-final-run.ps1 -Freeze F3` uses pinned Windows Go 1.27.0,
`go test -tags=integration -json -count=1 -timeout=10m -run <frozen regex>
./internal/repository`, original TestMain, `CI=true` and isolated
`postgres:18.1-alpine3.23`. Exit 0; 87 top-level / 234 including subtests PASS,
zero failures/skips. The exact regex and 87-name inventory are recorded in
`I3-pg-F3-inventory.json`: 37 billing cases plus 50 original GPT, I1/I2,
migration, coexistence and automatic reset execution cases. Both source
manifests match F3. The six independent-wallet subcases include actual GPT
termination after Claude issuance, full unchanged Claude/Key/wallet
fingerprints at termination, rejection of GPT admission and successful
settlement against that same original Claude card.

Final Linux race receipt: `I3-pg-race-F3-20261019T160927Z-receipt.json`.
`I3-pg-race.ps1 -Freeze F3` executes Go 1.27.2 linux/amd64 in the isolated
runner, `GOMAXPROCS=2`, `-p=2 -tags=unit -race -json -count=1 -timeout=10m`
over domain, service, handler, DTO, repository and routes with the recorded
critical concurrency/compatibility regex. Exit 0; 293 top-level / 636 including
subtests PASS, zero failures/skips, with the same before/after F3 source hash.
This is a unit race run; the separate PostgreSQL evidence above exercises real
database contention and faults.

Diagnostic evidence is retained under `I3-pg-debug-*`; detailed evolving counts
and failures are in `I3-pg-test-plan.md`. Those runs are not relabeled final.
Superseded raw evidence and duplicate manifests are preserved in the verified
archive described by `I3-evidence-index.md`, with all original files retained
locally. Final F3 receipts and functional logs remain directly readable.
The developer's focused Windows diagnostic command executed 146 top-level
tests with zero failed or individually skipped tests (one no-test helper
package skip), including the original GPT focused cases. Commands were pinned
Go 1.27.0, `-tags=unit -json -count=1`, with regex
`SubscriptionQuota|Carpool|UsageBilling|SubscriptionAppliesRateMultiplier|APIKeyAuthSnapshotPreservesPerUserBillingIsolation|TestProvideCleanup`.
Related compile, vet, codec/readiness/cache retry and cleanup checks also ran.
The F3 Windows evidence above supersedes these diagnostic runs; failures and
superseded diagnostic logs remain retained.

Final local R1 build receipt:
`I3-pg-F3-20261019T161328Z-build-receipt.json`, produced by
`I3-pg-build-R1.ps1 -Freeze F3 -PGReceipt <F3 PG receipt>
-RaceReceipt <F3 race receipt>` using the same source and embedded frontend.
Image `sub2api:claude-weekly-i3-receipt-r1-c9d25ce07053`, ID
`sha256:c17ee70e59572a06cb4b44d7334739120f0425d71cb2978e705ae36943ea0897`.
The server binary SHA256 is
`95d023b13826888098b90ebb4f6f91590411c715798d7844af1ac330b6430820`;
inspect labels and the executable retain the honest dirty-frozen I2 revision
plus complete F3 source hash. No retagging/rebuild is claimed after commit.

`I3-pg-R1-proof.ps1 -BuildReceipt <that receipt>` then ran the actual server
and its background workers in the original integration harness with isolated
PG18/Redis, issue/admission disabled and recovery enabled. Formal result:
`I3-pg-F3-R1-final-receipt.json` and
`I3-pg-F3-20261019T161410Z.jsonl`: one top-level test PASS, zero failed/skipped,
source stable. It recovered Claude `0.50000000` and original GPT `0.25000000`
known charges, retained exact ledger/counters after real restart, returned HTTP
503 to a new term generation request, and preserved parent/native/wallet rows.
The internal `10.247.191.0/28` network had no host ports, external egress or
Docker socket mount. Proof resources were automatically removed; the image
remains local. No upstream or production request was made.

Root acceptance is recorded in `coordinator-I3-review.md` and
`coordinator-I3-independent-counts.json`. I3 R1 does not yet contain I5
paid-order recovery; I6 must freeze the complete final rollback candidate
after I5. Acceptance of I3 does not complete I4–I6 or authorize deployment.

I6 performance obligations include high-ledger cards (Apply validates ledger
sums), high request history (backlog currently scans history), scope wait and
dashboard-writer impact. No unmeasured low-overhead claim is made. Explicit
historical dashboard Recompute after raw retention keeps its prior incomplete
source limitation; recovery does not invoke it. Existing conversion/legacy
writer races and final isolated R0→R1→R2→R1 drill remain I6 work.
