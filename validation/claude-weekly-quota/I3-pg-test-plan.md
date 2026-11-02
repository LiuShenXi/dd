# I3 PostgreSQL/fault/recovery test plan

Status: F3 execution passed on source
`c9d25ce07053fba7642a8eb4473288b88877d4bec62472df8cf2180ef4b7eebe`.
The billing inventory now has37 top-level cases. The prior expanded31-case run
passed completely; the remaining added partition/wallet/issued-card/mismatch/
account-type/image-fixture cases passed targeted diagnostics. The real worker
deadlock and scheduled-cleanup defects were fixed and retested. Formal F3 runs87
real PG cases (37 billing,29 retained I1/I2/GPT cases,8 additional old GPT lifecycle/
recovery cases and13 original automatic-reset execution cases), separate Linux critical unit concurrency race, and a rebuilt
image with an independent actual-server recovery/restart proof. The final87 PG,
293 Linux unit race,356 Windows unit and actual R1 image proof all passed with
stable source. See I3-pg-F3-readme.md and I3-pg-F3-final-receipt.json; coordinator
acceptance is separate from execution. Diagnostic passes do not replace them.

F1 passed74 real PG cases,293 Linux unit race cases and an additional13 actual
automatic-reset PG cases, and built an image. F2 was interrupted after review
required the already-issued Claude card to precede actual GPT termination.
F3 supersedes both earlier freezes. Only that test fixture changed; all final
tests and the image must bind the new F3 hash.

Owner: `sol_test_environment`; proposed exclusive file:
`backend/internal/repository/subscription_quota_billing_integration_test.go`.
Implementation owners must freeze actual domain/receipt/repository types before
the tests bind to them. The implementation files, SQL migrations, gateway
parsers and HTTP helpers remain owned by their respective implementers.

The isolated environment and original unmodified repository TestMain from I1/I2
remain the execution boundary. Use new PostgreSQL 18 databases and synthetic
users/accounts/Keys only. Upstream calls are simulated by the gateway owner;
this file tests actual durable repository/service behavior and database faults.

## Proposed executable scenarios

| Matrix | Database/fault assertions to implement |
| --- | --- |
| T13-T23, T59 | Interleave actual GPT and Claude admission/settlement; compare exact per-wallet deltas and unchanged opposite ledgers, native windows, balance/frozen balance. Preserve old GPT failures, existing legitimate Claude Keys, feature-off rejection, and no new repo calls on GPT paths. Run the existing GPT HTTP/SSE/WS unit regressions separately. |
| T24, T30 | Malformed/partial source fields and all mixed-source combinations, including zero-cost term receipts, must fail before durable effects. Cross-user/group/parent/term/Key identity substitutions must reject. |
| T25-T28 | Same durable known receipt concurrently applied many times gives one debit/used increment/Key-account effect; archive dedup remains authoritative; contradictory amount/fingerprint cannot replace the original fact. Explicit zero settles with a complete source and no wallet write. |
| T29 | Repeated client request identity yields two server-generated consumption IDs and two distinct durable charges. This must use the actual production identity generator, not test-generated replacements. |
| T31-T33 | Block a selected original term or parent across an actual PG clock boundary. Admission must reselect a valid term; known usage after expiry/cancellation remains attributed to the admitted original term and never rolls into the next card. |
| T34-T38 | Unknown admission remains SQL NULL and becomes audited reconciliation instead of zero. Known is independently committed before Apply and survives simulated process replacement. A late trusted receipt and manual resolution race preserve the first authoritative fact. Current unknown reconciliation blocks only its current card, not the next card or GPT. Client disconnect with known usage remains billable. |
| T39-T40 | Two real workers claim using short SKIP LOCKED leases with no duplicate business effect. Expired leases can be reclaimed. A first item blocked past its bounded timeout does not cancel the next item's independent context. A worker waiting on a scope lock must not hold the request row lock. |
| T41-T42 | Include raw calculation 0.000078125, exact eight-place storage and separate account cost; term used, ledger, request, dedup-linked command, Key/rate-limit counters and usage log agree exactly. Mutating plan/default/multiplier after admission does not reprice the stored snapshot or recovery receipt. |
| T43 | Inject real PG trigger failures in usage-log insertion/marking; successful ledger remains exactly once and unconfirmed logs can be repaired. Once the write marker is set, ordinary cleanup deletes history without recovery recreating it or recharging. |
| T44, T02 billing | Two Keys share one user's card. Several admitted calls may overshoot, but all known pending costs already count against new admission. Applying them records the true overshoot on the original card; the next card remains unused. |

Additional review requirements are part of the same scope: actual PostgreSQL
JSONB round trips for `1e-7`, `0.0000001`, `1.0`, `1` and nested numbers;
NUMERIC(20,8) maximum/last-unit values through exact counters and authoritative
usage-log payloads; post-admission disabled/deleted Key/account rows still
settle without resurrection; late conflicts never reopen settled/no_charge;
audited manual resolution versus late usage, and closing-to-closed maintenance
without changing the issued 168-hour interval or parent schedule revision.

## Diagnostic evidence so far

| Run UTC | Actual result | Interpretation |
| --- | --- | --- |
| `20261019T143447Z` | 0 of 17 top-level tests passed | Shared admission SQL failed on repeated unknown timestamp parameter plus interval. Owner added explicit timestamptz casts. Failure retained. |
| `20261019T143811Z` | 2 passed, 0 skipped/failed | Actual admission and real JSONB pricing/receipt insert-returning round-trip passed. Diagnostic only. |
| `20261019T144014Z` | 4 of 19 passed | Admitted NULL receipt fields exposed a RawMessage scanner failure; owner changed nullable fields to byte-buffer scans. Failure retained. |
| `20261019T144255Z` | 19 passed, 0 skipped/failed | Admission, known/Apply, atomic failure, archive, original-card expiry/cancel, soft deletion, pending overshoot, stale/heartbeat/leases and real GPT interleaving passed. Source was still changing; `acceptance=false`. |
| `20261019T150418Z` | 0 of 28 executed | Real migration250 failed because a legacy trigger definition depended on the altered cost column. Owner fixed the migration to restore the exact trigger in the same transaction. |
| `20261019T151129Z` | 26 of 30 top-level cases passed, 0 skipped | Actual verified-usage resolution after expiry/price edits, independent maintenance scopes and old ten-decimal rows/triggers passed. Scheduled cleanup deleted an unacknowledged log; two-worker item timeout, overly strict diagnostic-evidence comparison and an out-of-range synthetic multiplier were diagnosed separately. |
| `20261019T151503Z` | 3 of 4 targeted cases passed, 0 skipped | Maximum `999999999999.99999999` and minimum `0.00000001` passed through JSONB receipt/counters, original ledger, Key/account counters, usage log and hourly/daily/group aggregates exactly. Terminal status/facts plus appended conflict evidence and startup readiness passed. Normal workers at the default five-second item timeout exposed a real PostgreSQL deadlock in statistics publication; retained as an implementation failure. |
| `20261019T151718Z` | Two-worker failure reproduced, PostgreSQL40P01 | Captured wait graph: Apply waiting on api_keys FOR UPDATE versus statistics INSERT waiting on user_subscriptions FK. Scope/parent/term/request ordering fixed the real cycle; increasing timeout was not the fix. |
| `20261019T152144Z` | 1 targeted worker case passed | Identical actual workers at5s/15s passed after lock-order repair. |
| `20261019T152250Z` | 31 passed, 0 skipped/failed | Full expanded diagnostic suite, including stale publication after ack+cleanup, historical aggregate-base retention and normal scheduler race, passed; source changing so acceptance remains false. |
| `20261019T152742Z` | 2 targeted cases passed | Five one-wallet failure states leave the other wallet functional; real GPT first opening/renewal preserve an already-issued Claude card and every Key. |
| `20261019T152902Z` | Partition cleanup case passed, 3 paths | Real child partitions have collidingctid andid; manual/scheduled batches and original partition DROP preserve unacknowledged logs, then ack permits cleanup and stale publication remains a no-op. |
| `20261019T153303Z` | Ledger-hold case passed, 2 states | Known and settled/archive mismatch hold the original card without changing original money/payload/ledger or claiming hot dedup; a different new card still admits. |
| `20261019T153954Z` | Minimum image-fixture preparation passed | Both original GPT and Claude receipts are durably known with no settlement effects; this alone is not an actual image recovery proof. |
| `20261019T154652Z` | Infrastructure failure before server start | Docker default IPv4 pools were exhausted. No server ran; read-only inventory verified10.247.191.0/28 was free, and only the new task proof network uses that explicit subnet. |
| `20261019T154940Z` | Account-type race case passed | Actual account lock wait, apikey→oauth edit, stale selected type rejected before pricing/admission and all money writes; fresh OAuth freezes untracked account counters. |
| `20261019T155017Z` | Actual debug R1 image proof passed | Real Wire/server with issue/admission off recovered Claude0.5/account0.1 and GPT0.25, restarted with no second debit/log, rejected real term HTTP503, and preserved user/native/parent rows. Debug image source differs from final F1 and will not be reused as final evidence. |
| `20261019T160314Z` | Added termination fixture failed | Real Terminate succeeded, but the new assertion queried cycle status instead of the existing state column. Test-only column-name fix; no business implementation changed. |
| `20261019T160414Z` | Added termination fixture passed | Actual GPT term becomes terminated and current cycle closing; GPT admission rejects with zero writes while Claude original-card Admit/known/Apply succeeds under complete wallet fingerprints. |

Each row points to the matching `I3-pg-debug-<run>-receipt.json` and JSONL.
The later worker/log/terminal cases are not covered by the nineteen-test run.
Future final receipts must report their actual expanded case count and stable
source hashes. Do not relabel a diagnostic receipt as the final freeze.

The maximum-value case tests the exact durable receipt/storage boundary. It uses
bounded synthetic multiplier metadata and typed decimal strings for input/total
statistics components, rather than claiming legacy float pricing can represent
the maximum numeric value. A separate case uses the actual public Gateway price
factory and verified-usage calculator, then real audited resolution and normal
Apply after the original card expires and mutable plan/group rates change.

Ownership fingerprints retain complete foreign card/request/ledger/log/Key/account
rows. Only the admitted identities' counters may move. The original account's
last-used time is checked separately against the maximum admitted-at timestamp of
settled requests; the admitted Key may only make its documented active-to-exhausted
transition. Cache/scheduler publication is restricted to that Key/account. Ordinary
balance/frozen/native-window and every GPT financial row remain protected.

## Evidence and final image

Every run will preserve exact commands, complete JSONL, source before/after
manifests, real pass/skip/fail counts, and isolated fault diagnostics. Debug
runs and superseded freezes remain distinct from final acceptance.

The final I3 freeze must build and inspect an actual local receipt-recovery-compatible
R1 image, with admission disabled and known-receipt recovery still functional.
Version labels must identify the frozen I3 source hash/dirty or committed state.
The earlier I1 preliminary R1 cannot be relabeled as this final image. No image
is activated, published, or used against production by this test task.
This I3 image does not yet include I5 paid-order snapshot/fulfillment recovery.
I6 must validate the final receipt and order schemas and freeze another R1 if
needed; an I3 receipt-capable image is not proof of a complete final rollback.
