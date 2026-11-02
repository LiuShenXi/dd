# I4 isolated candidate and real HTTP evidence

Current status: frozen F2 actual image plus coordinator-approved F3 test-only
driver bridge. Actual runtime and Linux unit-race results are indexed in
`I4-runtime-F2-F3-readme.md` and `environment-I4-final-resource-index.json`.
Final-image T68 passed with actual held16m13.08s; real browser UI remains pending. The diagnostic
history and original execution design below are retained and are not current
acceptance claims. Main owns backend implementation; reviewer owns frontend;
this owner changes only dedicated HTTP/Redis fixtures and validation tooling.

## Current actual results

The I3 accepted commit is9005acffad8f9b30975ea3dd27e82095eb9e5520. Its final
receipt/R1 image remain distinct and are not retagged as I4 or final I6 rollback.

| Run | Actual result | Interpretation |
| --- | --- | --- |
| environment-I4-debug-20261019T162842Z-build | Server compile failed on unfinished usageSubscriptionQuota wiring | Owner had not declared build-ready. Diagnostic intermediate source only; no server ran. |
| environment-I4-debug-20261019T163455Z-build | Actual I4 image and synthetic provider built | Old embedded frontend reused. Source still changing, no final acceptance. |
| I4-pg-debug-20261019T163632Z | Seed/PG18 dump clone and internal containers created; container health healthy; outer loopback unavailable | Docker25 recorded127.0.0.1 PortBindings but NetworkSettings.Ports was empty. No business HTTP request reached the provider. The receipt script's empty-pass-array reporting bug was fixed separately. |
| environment-I4-debug-loopback-health | Outer127.0.0.1 GET health returned200 | Hidden host TCP byte bridge connects only the exact synthetic container's localhost8080 through Docker exec nc. No external network or in-container socket mount was added. |
| environment-I4-debug-login | Actual synthetic admin/A/B logins returned200 and tokens | Password/APIkeys/JWT remain only in the private fixture, never receipts. |
| I4-pg-debug-20261019T164709Z | First two Claude JSON cases settled0.00060000 each with ledger/dedup/logack and full ownership guards | /v1/messages and /v1/chat/completions passed; /chat/completions returned200 HTML SPA instead of JSON and stopped the matrix. Reported to implementation owner. Two real synthetic charges remain; no full-matrix claim. |

Docker Desktop exited during the local application crash. Only the existing
Desktop was restarted Hidden, and only the four verified task I4 containers were
explicitly resumed. Existing unrelated containers may auto-start under their
own restart policies; none are explicitly changed or used. Recovery is recorded
in environment-I4-docker-recovery.json.

Current diagnostic environment is indexed in environment-I4-active.json. Its
URL and private fixture path are public metadata, while credentials remain in
LOCALAPPDATA/Codex/PrivateTests/claude-weekly-quota. The old dist is not final UI.

## T45–T49 execution design

The original unmodified TestMain creates a new database with all real migrations.
Real services issue A100/B300 cards and open an original GPT term. A PG18 dump is
streamed into a separate uniquely named persistent task PG container. It shares
only the task internal network with new Redis/provider/candidate containers.
No PG/provider port is published. The loopback bridge streams raw real HTTP/SSE,
without changing request/response content or logging authorization headers.

The six aliases are enumerated from actual RegisterGatewayRoutes:
/v1/messages, /v1/chat/completions, /chat/completions, /v1/responses, /responses,
/backend-api/codex/responses. The matrix executes each JSON/SSE for both A and B
(24Claude calls), with A's two Keys selecting the same original card, and four
interleaved real GPT calls. It independently expects100input/20output tokens and
0.00060000 under the fixed synthetic Sonnet4.5 price. Each call must have one
known/settled request, debit, dedup and acknowledged log. Prior requests/logs,
foreign cards/Keys/accounts, all ordinary/native/GPT financial rows and identity
fields remain protected by full-row fingerprints. Legal auth Key last-used
metadata is checked separately as monotonic. GPT preserves all Claude rows.

Formal evidence additionally covers unsupported/forced routes with zero provider
calls and no monetary writes, canonical/v1/usage fields, and actual UI behavior.
T47 uses a task-only loopback response-delay proxy, never a business backdoor.
T48 creates a separate synthetic expiry identity just before browser use: both
cards retain168h, independent next term, no A/B ledger mutation. Slow-response,
countdown, locales, keyboard and narrow viewport are not passed by unit mocks or
old frontend screenshots. Final candidate waits for both owners' freeze and a
new embedded frontend hash.

## Concurrency isolation v2: T63–T74

Read contract: concurrency-isolation.md and test-plan T63–T74 from the updated
architecture. These are additional required tests, not covered by the current
single-instance24-call matrix. Actual exported ports/config fields are awaited
from main; no guessed stub is treated as integration evidence.

Use two uniquely named candidate instances with separate loopback bridges and
the same task PG/Redis. The synthetic user legacy limit is5, Claude limit2, and
each fake upstream account capacity is16 so account scarcity cannot hide scope
behavior. Provider now has internal-only hold/release and active-call audit
controls; default behavior is unchanged. It never forwards or logs secrets.

- T63–66: hold5real GPT+2real Claude upstream calls simultaneously, verify actual
  Redis scope keys plus provider active identities, independent full queues and
  same-user multiple Keys/groups/cards sharing the same Claude pool.
- T67/70: real Redis Lua acquisition/enqueue/promotion/renew/release/stats through
  separate clients/instances, cancellation/timeout and duplicate cleanup preserve
  other tickets. Redis TIME/TTL/hash-tag semantics must be observed directly.
- T68: run an actual stream longer than15minutes in parallel with other work.
  Synthetic provider emits10s heartbeat comments while held. A dedicated client
  has a >=17minute context; the ordinary20s matrix helper is not reused. Confirm
  repeated real lease renewal and capacity throughout. A reduced-TTL quick test
  is supplemental only and cannot satisfy the original15minute boundary.
- T69/71/74: actual Router/HTTP controlled failure, cancellation, lost/failed
  renewal and account handoff; no fallback into legacy keys, original scope
  release exactly once, usage preserved/reconciled, existing GPT HTTP/WS/live
  behavior unchanged. Real Redis faults are distinct from fake bool returns.
- T72: final image enable/drain/rollback exercise, never clear original GPT slots
  or permit mixed-version extra capacity. T73 includes actual policy edit/cache
  and frontend unknown states. Final readiness depends on the new schema/ports.

## Later required compatibility/performance evidence

I6 should use an isolated PostgreSQL18.6 image to match the root's read-only live
version check; earlier18.1 evidence remains valid but patch-distinct. Migration250
needs about400000synthetic historical usage rows with original ten-place values,
trigger behavior, elapsed time and lock observations, beyond the small fixtures.
No production rows are copied or queried by this owner. Actual deployment/push
authorization is held and executed by the coordinator, not this test agent.

The host bridge adds IPC overhead. Do not infer production P95/P99 from these
loopback timings; the required R0/candidate comparison must use the same explicit
load/transport and report its limits. I5 payment recovery and final I6 rollback
cannot be replaced by the I3 receipt-only image proof.
