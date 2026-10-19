# I3 coordinator review — accepted on frozen F3

Production remains unchanged. All implementation and tests use the isolated
`codex/claude-weekly-quota` worktree and synthetic identities.

## Evidence boundary

- I2 accepted commit: `0d61e956e2a9d86fbe7fb9a24adbdb47e687c486`.
- The early I3 PostgreSQL runs are debugging evidence only. Their source
  manifests changed during execution and they are not acceptance receipts.
- `I3-pg-debug-20261019T143447Z-receipt.json`: the initial admission SQL failed
  because PostgreSQL could not infer a consistent timestamp type for `$7`.
- `I3-pg-debug-20261019T143811Z-receipt.json`: the targeted admission and JSONB
  round-trip checks passed after the explicit timestamp cast.
- `I3-pg-debug-20261019T144255Z-receipt.json`: 19 top-level PostgreSQL tests
  passed, no failures or skips; source was still changing. This does not close
  T13–T44 or authorize I4.
- The coordinator independently parsed `I3-gateway-owner-windows-unit.jsonl`:
  24 top-level tests / 106 including subtests passed across service, handler and
  the actual router packages, with no failure or skip. This owner-slice run
  predates the Resolve/lifecycle/RecordUsage integration and is not a final
  frozen-source receipt.
- The expanded owner run `I3-gateway-owner-final-windows-unit.jsonl` was also
  independently parsed: 124 top-level tests / 246 including subtests passed
  across the same three packages, with zero failure or skip. Dashboard and
  repository integration were still changing, so this is not the I3 gate.
- The post-dashboard owner run
  `I3-owner-final-post-dashboard-windows-unit.jsonl` was independently parsed:
  142 top-level / 268 including subtests, four packages, zero failure or skip.
- PostgreSQL debug run `20261019T152250Z` passed all 31 then-present top-level
  cases, including the corrected real two-worker lock ordering and retained
  historical dashboard base. Source was still changing.
- Targeted PostgreSQL debug runs `20261019T152902Z` and `20261019T153303Z`
  passed the actual cross-partition ctid collision cleanup and durable
  ledger-mismatch hold respectively. They remain debug evidence pending the
  full final source freeze.

## Review findings and required closure

1. Generate a server UUID for every real request; client Request-ID cannot
   identify a money event. Verify repeated client IDs produce separate charges.
2. Canonicalize JSON numbers using exact decimal values before hashing. Real
   PostgreSQL JSONB rewrites number spellings such as `1.0` and exponent forms.
3. Previously admitted usage must settle against original Key/account rows even
   after soft deletion, without reviving those identities.
4. Conflicting late usage must preserve settled/no-charge terminal state and the
   first authoritative receipt; record a conflict for audit instead of rewriting
   the financial outcome.
5. Validate the prompt-free typed statistics payload against the saved snapshot,
   receipt and exact cost before persisting known usage.
6. Term money, counter costs, receipt fingerprints and SQL writes must not round
   trip through float64. Include maximum valid NUMERIC(20,8), one atomic unit,
   and rounding boundaries in real PostgreSQL checks.
7. Existing usage_logs NUMERIC(20,10) cost columns cannot contain the complete
   term integer range. Use a new widening-only migration preserving historical
   values and ten fractional digits; never edit a prior migration.
8. Recovery uses bounded independent child contexts of the worker root; Stop
   cancels ongoing work. Claims release request locks before waiting for scope.
9. Audited manual resolution and late known usage race under the same lock and
   revision protocol. Only unknown unresolved requests can be resolved.
10. Closing/naturally ended cards converge after the last unresolved request;
    immutable card times and the parent scheduling revision remain unchanged.
11. Unacknowledged statistics insertion is protected from cleanup; acknowledged
    history removed by normal retention must not be recreated or rebilled.
12. Final production Wire starts recovery, injects the same durable repository,
    and stops the worker before closing its infrastructure dependencies.
13. Exercise real concurrent recovery, including foreign-key locks during
    statistics INSERT. A database deadlock was observed during expanded debug
    tests; a larger test timeout is not a fix. Verify the actual wait graph and
    the final writer lock order alongside financial Apply.
14. Protect all retention paths, including scheduled batch cleanup and physical
    partition drops. Preserve the existing DROP path for partitions without
    unresolved term requests; immutable admitted_at must protect requests whose
    log has not yet been written.
15. A stale publication worker must not recreate a log after another worker
    acknowledged it and retention deleted it. The acknowledgement is permanent.
16. Late statistics must preserve historical aggregate bases whose raw logs
    were already retained/cleaned. Verify exact, idempotent publication together
    with normal dashboard aggregation; do not clear a whole historical bucket
    and rebuild it from incomplete raw data.
17. Batch retention must match both tableoid and ctid on partitioned usage_logs,
    and repeat the acknowledgement predicate at deletion. A ctid alone can
    select a protected row in a different partition.
18. Detect a ledger/used mismatch distinctly, roll back the provisional dedup
    write and keep all money unchanged while recording a durable card hold.
    Preserve terminal states and first receipts; a later contradictory receipt
    must not erase the hold, and another/new card must remain independent.
19. Final admission must compare the actual sender's selected account type with
    the locked database account type. The administration API can change type
    while admission waits; otherwise the builder may use API-key credentials
    while the snapshot records OAuth and omits the original account quota.
    Refuse the mismatch before admission/send, and prove the lock-wait race.

The actual two-worker failure was PostgreSQL `40P01`: financial Apply held the
parent row and waited for api_keys, while statistics INSERT held an implicit
foreign-key lock and waited for user_subscriptions. The candidate correction
discovers immutable request routing without a lock, then acquires the dashboard
writer lock and scope -> parent -> term -> request before INSERT. A final
concurrent PostgreSQL run must prove the correction; increasing the timeout
does not resolve this finding.

The historical dashboard correction is deliberately limited to first durable
term-log insertion: exact SQL increments preserve an already-published base,
and normal dashboard writers share a lock acquired before reading source rows.
Explicitly rebuilding a historic interval after its raw rows were purged keeps
the prior incomplete-source limitation; this feature does not invoke that
operation as part of recovery.

The first expanded migration run
`I3-pg-debug-20261019T150418Z.jsonl` failed before any business test because
PostgreSQL forbade widening actual_cost while the existing UPDATE trigger's
WHEN condition referenced its type. The new migration recreates the same
trigger condition in its transaction; final real PostgreSQL proof is required.

## Route/adapter facts to verify in the final receipt

- Exact generating routes: Messages, Chat Completions and Responses including
  the actual router aliases; arbitrary suffixes and WebSocket remain closed.
- Both count_tokens aliases are authenticated non-consuming metadata. They do
  not create a term admission or grant generation rights. `/v1/usage` term
  projection is an I4 obligation.
- OAuth/setup, API passthrough, native Bedrock and Vertex have separate adapter
  evidence. The production baseline has no implemented Kiro forward path.
- The existing Chat/Responses path has no Bedrock signed builder; this
  combination must refuse before sending rather than silently use another pool.
- First-version term attempts allow one real upstream send after admission.
  Server-side billable tools and web-search emulation without complete metering
  refuse before sending. These limitations must be explicit in the handoff.
- Complete final provider usage survives client disconnection. Partial usage
  remains NULL/unknown and cannot become a zero-cost bill.
- A 10-second heartbeat extends the 30-second active-admission lease through
  forwarding and mandatory usage persistence. Stale recovery requires an expired
  lease and the inactivity grace; a healthy long stream must not become stale.

## Later acceptance obligations retained

- I4 final candidate browser: A 100/B 300, multiple Keys, GPT interleaving,
  authoritative tables, asynchronous identity protection and expiry display.
- I5 paid-order recovery must be present in the final compatible R1; an I3
  receipt-compatible image alone is not the final all-business rollback image.
- I6: benchmark high-ledger cards (Apply currently checks the ledger sum),
  demonstrate old-writer/conversion lock races, recheck production drift, and
  prove R0 → R1 → R2 → R1 in the isolated deployment.

## Final F3 acceptance

Accepted by the coordinator on 2026-10-19 UTC (2026-10-20 Shanghai), using
source SHA-256 `c9d25ce07053fba7642a8eb4473288b88877d4bec62472df8cf2180ef4b7eebe`.
The coordinator independently read the receipts and counted the actual JSONL:

| Layer | Top-level / including subtests | Result |
| --- | --- | --- |
| Windows focused unit | 356 / 692 | pass; no individual skip/failure |
| Isolated PostgreSQL 18.1 | 87 / 234 | pass; no skip/failure |
| Linux unit concurrency race | 293 / 636 | pass; no skip/failure |
| Actual compatible R1 server recovery | 1 / 1 | pass; no skip/failure |

All four runs and the build have identical before/after F3 source hashes.
Windows also completed all-package compilation and related vet with exit 0;
all 84 original GPT test names were present. The Linux race run is a unit
concurrency check, not PostgreSQL integration race evidence. Real PostgreSQL
concurrency is separately exercised in the 87-case run.

The original 297 production migration checksums remain unchanged and the
verified production revision is an ancestor. Migration 250 was executed in the
real PostgreSQL harness, including preservation of historical ten-place costs
and the original group-rollup trigger behavior.

F1/F2 and earlier debugging runs remain historical evidence, not the final
candidate. The only source difference from F1 to F3 is the integration-test
file: T16 now issues Claude first, terminates GPT through the real service,
proves Claude/parent/Keys/wallet unchanged, and settles against the original
Claude card. F3 additionally executes all 13 original automatic-reset tests.

Final evidence:

- `I3-windows-F3-20261019T160855Z-receipt.json`
- `I3-pg-F3-20261019T160927Z-receipt.json`
- `I3-pg-race-F3-20261019T160927Z-receipt.json`
- `I3-pg-F3-20261019T161328Z-build-receipt.json`
- `I3-pg-F3-20261019T161410Z-receipt.json`
- `coordinator-I3-independent-counts.json`
- `I3-test-trace-map.md` and `I3-pg-traceability.md` for exact assertion layers.

The actual R1 image is
`sub2api:claude-weekly-i3-receipt-r1-c9d25ce07053`, image ID
`sha256:c17ee70e59572a06cb4b44d7334739120f0425d71cb2978e705ae36943ea0897`.
Its server binary SHA-256 is
`95d023b13826888098b90ebb4f6f91590411c715798d7844af1ac330b6430820`.
Normal Wire/server startup recovered Claude cost 0.50000000/account cost
0.10000000 and an original GPT receipt of 0.25000000. Restart preserved the
complete wallet/receipt/ledger/statistics state. An actual term HTTP request
returned 503 with new admission disabled. This ran only in an internal
synthetic network, with no host ports or external provider calls.

T01–T44 are accepted within their documented layers; HTTP/SSE/WS transport,
database money and crash fault injection are not misrepresented as one live
provider end-to-end test. I4 must still prove the final UI and candidate HTTP
flows. I5 payment fulfillment and the final I6 compatible rollback image,
conversion gate and performance/release rehearsal remain required.

Acceptance status: **I3 accepted; I4 authorized**. Production remains disabled
and unchanged. This is not completion of the user's I0–I6 request.
