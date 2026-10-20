# I4 frontend owner receipt — stable owner candidate, browser acceptance pending

Base accepted I3 commit: `9005acffad8f9b30975ea3dd27e82095eb9e5520`. This owner implements frontend only for the original I4 slice; the subsequently authorized v2 gateway files are separately recorded in I4-v2-concurrency-gateway-notes.md. Main owns backend DTO/routes and commits. Environment owns actual candidate/PG/Redis evidence; root owns browser acceptance and any authorized deployment/push.

## Implemented original I4 slice

- Dedicated decimal-string DTO/API and normalized windows/term discriminated projection. Legacy missing mode normalizes to windows; term plus null summary is explicitly unavailable.
- Shared user/group card in user subscriptions, compact subscription header and KeyUsage; settled/pending/remainder are server values. Per-card history/usage uses opaque cursors and term ID. Key restrictions stay separate. Windows/GPT branches and their operation paths remain intact.
- Manual open/renew preview and actual-result interval, copied payload, exact network-retry operation key, conflict invalidation with original input preserved. Cancellation uses parent revision/reason and a separate key. Same-scope open/cancel writes are mutually exclusive, including after edits; closing does not claim server cancellation.
- Sanitized administrator request list and audited resolution form. No-charge omits verified_usage. Verified mode requires all five safe-integer counters and optional scoped model evidence. HTTP acceptance is not labeled settlement.
- Async result/error/finally effects check generation, authenticated identity, target and payload. The reusable helper also invalidates on synchronous A → B → A. Store caller markers protect errors/clock effects in addition to internal state. Countdown uses performance-based server calibration and only refetches at boundaries; held responses do not clear amounts or promote next cards.
- Matching Chinese/English known statuses and unknown enum fallback. Financial strings preserve eight-place amounts and one atomic unit through formatting; no float conversion creates a balance.

## Actual checks so far

**Current final owner frontend freeze, including Key-page bootstrap:** `I4-ui-20261019T174120Z-receipt.json`, SHA256 `0fe0b15e7a2876ab1f258ea88fc35750794a225242a99859e95e53d4e20765d0`, source_stable=true. Typecheck, lint:check, full test:run and build all exit0. All 324 files / 2440 tests passed, with zero failed/skipped; the existing build precheck also passed 3 tests. New embedded backend/internal/web/dist was produced at 17:45 UTC. Exact before/after manifests, logs and Vitest JSON are retained. browser_acceptance=false remains correct. No frontend source changed during or after this run.

The prior stable owner frontend slice is `I4-ui-20261019T172651Z-receipt.json`, SHA256 `21594dbe7a01ed9ab019ee823b78ba10f8507b39f270888de1597e4c2bea68cc`, source_stable=true. Typecheck, lint:check, full test:run and build all exit0; 323 files / 2433 tests passed, zero failed/skipped. The build wrote the embedded backend/internal/web/dist and its existing prebuild check also passed 3 tests. Root subsequently authorized the Key-page bootstrap fix described below; the next stable four-step receipt must include it. browser_acceptance=false is deliberate: real browser acceptance is still root ownership.

The preceding `I4-ui-20261019T171924Z-receipt.json` was also a stable successful v2 run (323 / 2433), independently parsed by root. Afterwards the English/Chinese create hint removed its hardcoded default number, because the actual independent default is server configuration. The 172651Z run includes that copy correction; all displayed existing-user limits still come from the server projection.

## Root-authorized Key-page bootstrap correction

Root found the actual initial-login chain had no statistics: login stores its response and starts the auth store's first 60-second refresh; KeysView mounted previously performed no profile read. Main fixed the display-only /auth/me endpoint while retaining a Redis-free global GetByID/JWT path. This owner adds only KeysView plus useKeyPageConcurrency and tests. Mounted/session-change reads use the existing /user/profile endpoint and store an allowlisted local concurrency projection, never calling global auth.refreshUser or writing auth/localStorage. Synchronous session generation invalidates success/catch/finally even across A → B → A; local results are scoped to user ID and token epoch, mismatched returned user IDs are rejected, unmount invalidates requests, and read failure masks counts as null/unavailable. A same-session auth.user replacement supersedes local data and invalidates any older bootstrap so the regular 60-second server update cannot be shadowed permanently.

The targeted test result is two files / 32 passing tests, including the actual KeysView mount/DOM and six deferred composable cases (initial read without polling, local/global separation, late success/error/finally, session reversal, later auth refresh, ownership and unmount). The final 174120Z stable four-step run above includes all of these changes. No I5 source is included.

`I4-ui-20261019T164811Z-receipt.json` records typecheck, lint:check, full test:run and build all exit0. Its full test JSON/log contains 319 files / 2414 passing tests, zero failed/skipped tests. Its source_stable is deliberately false: while the run was active, the A → B → A identity fix and its two tests were added. Those current paths separately passed the five-file / 22-test targeted run. Therefore this receipt is a successful intermediate slice, not final-source acceptance. After v2 source is complete, rerun all four checks under one stable frontend hash.

Earlier full test run I4-ui-tests.log retained a real 3-file / 18-case regression: unopened new modal setup required Pinia in old windows fixtures. The production mount was corrected to instantiate the new modal only when opened. All original 18 failing cases passed the targeted rerun; none were skipped or replaced by a plan.

The new original-I4 tests cover decimal limits, public/admin HTTP payload boundaries, store ordering and auth changes, modal late preview/submission success/error/finally, retry keys, 409 input retention, mutation exclusion, monotonic boundary refetch, card-scoped usage cursors, sanitized request review and multi-Key shared card display. Vitest does not prove real layout or full HTTP→PG billing.

## Required remaining work

V2 frontend is implemented: independent default3 and explicit positive per-user override; server-projected counts/caps for both pools on user subscriptions, Keys, KeyUsage and administrator user editing. Legacy current count/status has its own nullable server projection and overrides the old admin-only field; a declared outage cannot fall back to a stale zero. Claude policy never comes from the GPT limit. A blank create override is omitted, a blank edit keeps the current policy, and null/zero/negative are not sent. No unsupported override-clear control was added.

Create/edit mutation mutexes survive dialog unmounts and keep in-flight same-scope writes locked through input changes. A → B → A responses and step-up retries check captured identity/data; old finally releases only its captured mutex. Term issuance/cancellation uses the same pattern for one user/group scope. New focused mutex/projection tests passed 5 files / 28 tests before the full stable run.

Gateway acceptance is recorded separately in I4-v2-concurrency-gateway-notes.md. T63–T74 real Redis/PG/long-stream/actual-candidate and real browser acceptance are environment/root responsibilities and remain outside the frontend Vitest claim. Plan default selection/override, products and order snapshot UI remain required I5 work; I4 manual issuance currently requires explicit quota because no reliable term-plan enumeration existed at its interface freeze. No I5 business source was modified.
