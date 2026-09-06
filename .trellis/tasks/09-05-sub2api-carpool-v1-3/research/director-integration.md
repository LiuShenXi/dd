# Director Integration Record

## Activation

Director session `codex_01a0722c-e17a-7092-8abd-d7776b150f1b` successfully ran `task.py start`, `current --source`, and `validate` on the existing task. Both role manifests contain one real engineering-context reference and validated successfully. Existing core/gateway workers were notified with the absolute Active task path, full-read requirements and latest safety boundary. Both subsequently explicitly confirmed complete manifest/research/task/AGENTS/workflow/role/spec reads and unchanged ownership. The scheduler briefly reported pending_init; follow-up to the same agents resumed their work without respawning or replacing their files.

## Dispatch Ownership

Native agents are explicitly `gpt-5.6-sol`, reasoning `high`, `fork_turns=none`. `/root/core` and `/root/gateway` keep existing work; `/root/frontend` now owns only frontend carpool implementation and frontend verification. Core remains the sole public backend wiring and generated Ent coordinator. Source coordinator alone owns `research/local-test-isolation.md` and database-export/restore evidence. No new user-owned tasks were created by the director.

## Decisions and Review Feedback

1. Scope defaults to global carpool scope 1, not group ID, so all members share a single special-reset cooldown and pending batch.
2. Takeover bucket balances are final opening balances. Ordinary-balance transfer funds a portion of that opening amount; it must not be added a second time as manual quota. Preview must make both balance-source changes explicit.
3. Core owns admission, durable receipt persistence/recovery service and repository helpers; gateway owns existing billing command/Apply integration and all real forwarding paths. Do not build duplicate receipt services.
4. Settled receipts cannot transition back to usage_known on replay. Debit lock ordering must agree with cycle closure and receipt settlement.
5. Every actual HTTP request/WS turn requires an ingress-owned unique intent. Failover within that request reuses its snapshot; receipt replay only settles, never re-forwards. Reused client IDs cannot enable free calls or use an expired cycle.
6. WS passthrough does not always invoke BeforeTurn. Turn-level admission must cover its real pre-forward hook too. SimpleMode and unsupported ordinary-balance hold paths must not silently bypass carpool billing.
7. Plan updates are versioned and never recompute terms. Imported unknown usage must not be described as complete zero history.

These are implementation decisions and actionable review notes. No feature or test is accepted by this record alone.

## Copied User Fixtures

Latest instruction: only in the sealed local copy, a copied user's test term uses `users.created_at` as start and exactly 30 days thereafter as expiry. Historical/future dates are preserved; no renewal or backfill to make an expired user appear active. Plan mapping must be confirmed independently of balance, otherwise the user remains pending mapping. Synthetic identities cover all plans and active/future/expired transitions; reports and screenshots never contain copied personal data. Core/frontend/gateway were notified, and future check workers must load this rule. Product opening behavior remains unchanged.

The source coordinator's aggregate-only inspection found no verifiable carpool tier mapping in the old database: only standard billing and no subscription-plan association. Accordingly, do not create copied-user carpool terms or convert their standard group. The copy serves migration and legacy balance/usage compatibility checks; full carpool behavior uses synthetic fixtures. Frontend and core explicitly acknowledged this boundary. Source owns the detailed isolation evidence and must approve application access separately.

## Resumed Integration Status (2026-09-06)

The same three workers remain running. Coordinated Ent generation succeeded; core reports domain/repository compilation, with services and user/admin handlers written. Providers, routes, Wire and lifecycle are still being integrated. Gateway HTTP admission and transactional billing are written; WS turn/passthrough admission, usage-log SQL and unsupported balance-hold paths remain under active work. Frontend reports the user slice and five-tab admin console written; the earlier 4-file/8-test result does not validate the latest changes.

The next bounded milestone is a full backend build and registered routes (core estimate 30-45 minutes at this update). Gateway's complete focused-test milestone is estimated at 45-75 minutes, frontend typecheck/tests/DTO alignment at about 30 minutes. These are worker estimates, not acceptance or a promised final delivery time. Reset/observer/announcement implementation still needs the next available worker, and end-to-end acceptance has not passed.

The source coordinator is reviewing the sanitizer. No application may connect to the copied database before explicit clearance. Web port 38088 is reserved for this task; 18080 is an unrelated existing service and must not be touched. The copied-user fixture restrictions above remain unchanged. No goal has been created; the existing Trellis task remains active.

## Director Code Review: Corrections Requested

The following findings were observed in the actual current source and sent to the owning worker. They remain open until corrected code and regression evidence are inspected.

1. Serialize operation-key replay before looking it up, and persist the original response with business writes. Concurrent same-key opens, boosts, payments and plan versions must return the original result rather than overlap/exhaustion/unique errors.
2. Fix adjustment lock order to term then cycle; namespace request/event identity by operation kind and actor, not only the raw retry key.
3. Preserve positive reversal references, reject duplicate reversals and include consumption reversals in cumulative net usage without counting non-consumption grants.
4. Details and boost eligibility need the same current-cycle activation fallback, truthful pending/expired/terminated states, and successful reset-target counting including zero grants.
5. Bounded maintenance scans must not repeatedly process only the earliest 200 active terms and starve later due terms.
6. Commit expiry lifecycle transitions before returning unavailable. The current early error rolls back the expired status update and leaves ended active cycles unclosed.
7. Freeze admission after database-lock waiting with a database-authoritative, canonical microsecond timestamp. PostgreSQL timestamp roundtrips must not invalidate a snapshot through nanosecond inequality during settlement.

No director edits have been made to workers' owned implementation files. Focused tests and independent acceptance will verify each correction.

## Continued Review and Reconciliation Decision

The director resumed the same core/frontend/gateway workers. Core reports routes, Wire and lifecycle written; its latest recovery and exception projection changes still require a new compile. Frontend is saving final typecheck/lint/focused-test/build evidence. Gateway's post-fix handler unit run passed, while service and further focused regressions remain running. None is an integrated acceptance result.

Further observed review requirements sent to the owners: quantize takeover inputs individually before arithmetic; reject quantized-zero adjustments; recheck boost validity after term locks; preserve usage_known recovery on settlement failure; separate invalid durable-receipt quarantine; fail closed on missing gateway snapshots; fix WS snapshot shadow and frozen pricing; maintain cycle-before-key locking across admission and debit; clear stale frontend announcement/modal state; keep renewal preview anchored at old expiry and support source-bucket reversals.

The director approved the minimal administrator reconciliation endpoint now recorded in api-contract.md. Only explicit confirmed no-cost or verified positive actual-cost resolution of unresolved reconcile_required records is permitted. Operation replay/audit and common usage-dedup/debit/settlement share one transaction. Unknown auxiliary usage is not reconstructed and late durable receipts cannot be silently overwritten. Core owns the new repository/handler implementation, coordinating package-local billing helpers with gateway. Regression evidence remains required.

Source coordinator added architecture section 13 with the already agreed registration-based copied-user dates, no guessed tier mapping, sealed local-only testing and original-record preservation. The director read that section and both isolation documents. This does not alter product opening semantics or authorize application startup before code-ready. The source preparation evidence exists, but backend/frontend/runtime acceptance is still pending.

## Frontend Milestone and Reset Dispatch

Director inspected `frontend-typecheck.txt`, `frontend-affected-lint.txt`, `frontend-focused-tests.txt`, `frontend-build.txt`, `frontend-full-lint.txt`, and `frontend-full-tests.txt`. All contain exit 0. The focused suite passed 11 files/40 tests; the full suite passed 264 files/1887 tests. Affected ESLint covered 46 files; production build completed with existing large-chunk warnings. Director `git diff --check -- frontend openspec/changes/add-carpool-v1-3 .trellis/tasks/09-05-sub2api-carpool-v1-3` exited 0. Browser acceptance is pending.

The same `/root/frontend` worker has now received explicit package D ownership from `research/reset-handoff.md`: new reset domain/repository/service/admin-handler/schema sources, migrations 237 onward, existing announcement and quota-observation hook sources. Core retains all public wiring/lifecycle and generated Ent. No replacement or recursively delegated worker was created. Existing frontend follow-through remains with that worker, including minimal billing-exception UI.

Two later frontend findings remain open: acknowledge announcement versions only after successful full-list refresh and prevent same-session response reordering; cancel or identity-guard the login/popup delay timers and avoid anonymous announcement requests. The saved frontend test milestone predates those corrections and does not accept them.

## Latest Independent Findings

Core: admission must retry its whole transaction if the selected cycle expires after taking a key lock; acquiring a new cycle while holding that key recreates billing's lock inversion. Boost eligibility must recheck database time after cycle-lock waits, not only term-lock waits. Final authoritative admission must preserve generic key expiry/status/quota and user/group restrictions. Exclude terminated future terms from details selection; incomplete takeover statistics default to the actual takeover time. These are sent to core and gateway with PG18 regression requirements.

Gateway: carpool must skip ordinary user-platform quota increments and ordinary-balance notifications; a recovered synchronous task panic must not fall through to asynchronous enqueue. Missing pricing or absent/malformed usage cannot become a known zero-cost receipt. RecordUsage must centrally prefer the frozen carpool admission timestamp for HTTP as well as WS. All findings remain open until corrected code and tests are independently inspected.

Source is preparing a separate empty synthetic PG18 test target/runner. The existing integration TestMain must never be pointed at the copied `carpool_test` database. Source startup now requires director evidence JSON in this task's evidence directory, tied to current HEAD and actual backend/frontend build passes. Source additionally binds the sanitization gate to concrete container/network IDs and is hardening failed-start recovery. No application has been authorized to start yet.

## Source Freeze Gate and Current Review

Source now provides `Get-LocalRuntimeCodeIdentity` in `validation/source-runtime/runtime-common.ps1`. Final code-ready must include its `SourceSha256` and `EmbeddedFrontendSha256` as `source_sha256` and `embedded_frontend_sha256`, after workers freeze and final builds pass. HEAD alone does not identify uncommitted implementation. Startup validates both hashes; any source or embedded-dist change invalidates approval. The director has not generated code-ready.

Independent `go build ./...` failed with the four stale provider calls in `cmd/server/wire_gen.go`; evidence is `director-backend-diagnostic-build.log`. Core owns coordinated regeneration after D interfaces settle. The gateway service focused, handler focused/full, and DTO full evidence files each contain `ok`, independently inspected. These are not a full application acceptance.

Current source inspection confirms admission restarts the whole transaction on post-key-lock cycle boundary changes and checks generic key/user/group restrictions after locks. Boost rechecks database time after cycle locking. Negative takeover boost/manual inputs are now rejected, plan inputs are quantized, and the plan repository projects latest versions including disabled entries. Database concurrency tests and final management DTO alignment remain required.

Still-open reset findings: refresh database time after scope/batch/term/cycle lock waits; recheck the allowed execution minute and actual success time; preserve satisfiable second-level cooldown inside the promised minute; atomically confirm needs-review qualification without replacing another pending batch; capture lifecycle done channels; persist every merged source-event identity across batch completion. Core's exception diagnostics must use static categories, and resolved manual history must remain listable. Takeover/adjustment closure must preserve net balance without manufacturing nonbase debt.

Source HTTP review found preview contract drift. The director aligned `preview.plan.plan_id` with domain/frontend and explicitly classified preview as a read-only POST, exempt from economic-write idempotency and operation persistence. Core must validate the positive path user ID, verify the nondeleted target exists, and reject disabled/superseded plans. Unknown users return 404 and malformed IDs return 400. A preview never freezes the later open transaction's null start time. Source's independent HTTP assertions must enforce these rules; this clarification does not relax financial-write replay requirements.

Source owns repeated HTTP fixture creation through real admin APIs: each run gets a new synthetic batch, never resets the copied database or reuses modified test users. Original-row conservation remains independently enforced. HTTP success does not replace WS, reset, receipt-recovery or browser acceptance.

## Whole-Backend Build Milestone

Source's corrected seventeen-test PG18 run passed; director independently counted the actual private RUN/PASS lines. After missing reset audience/window repository methods were added and core regenerated Wire, director `go build ./...` passed with exit 0 (`director-backend-full-build-second.log`). No application startup approval has been issued: source is still mutable, reset-window service wiring needs its final connection, D transaction tests and latest frontend verification remain open.

Current reset review confirms scope membership locking, merged event identities, lifecycle done-channel capture, batch-before-outbox locking, separate publication transactions and publication-time relative dates. The grant transaction still needs a final execution-window guard and actual successful timestamp. A synchronous two-minute observer scan could block the five-second due loop; D reports separate scan lifecycle and a slow-scan regression, plus rejecting null credit-list members as complete zero baselines. The explicit admin scan idempotency key was being discarded and requires durable replay behavior. These items remain evidence-gated.

Only source main owns shared E2 execution and cleanup. After one compilation captured a partially written test file, the director coordinated a temporary repository-only freeze across core, D and gateway for source compilation. Source must notify compilation completion so implementation can resume without waiting for binary execution.

## Local-Start Snapshot And Follow-Through

All implementation owners froze after D saved the execution-time boundary guard, final successful timestamp, same-minute cooldown repair, invalid schedule repair and transaction-scoped scan replay lock. Director ran fresh whole-backend build, frontend typecheck, production build and full lint: all exited 0. Frontend focused tests passed 11 files/44 tests; full tests passed 264 files/1891 tests. The affected backend unit selection passed, with explicit no-matching-tests results only for quotaview/middleware. Logs use the `director-local-start-*` prefix.

`evidence/director-code-ready-local-start.json` authorizes only source-owned sealed local startup. HEAD is `36266f512776d78d4f1645a75ae0e84816f8a0a7`, source SHA-256 is `8496B57ACDDA2D2FFCC1B69B8B80C104E52454CF180A1FC237CAE4F7108C58ED`, and embedded frontend SHA-256 is `FBA89230EA032A8745E817EB09189061A491CF00587990C6146C49BE3F79ACF4`. Source identity matched before and after builds; `Assert-DirectorCodeReady` passed. This is not final acceptance. Source has started its Linux embedded build and must confirm immutable image capture before writers resume. Subsequent source edits require a rebuilt, reverified image for final acceptance.

The director explicitly assigned the existing gateway worker all new `backend/internal/repository/carpool_reset_*_integration_test.go` files only, after snapshot capture. Frontend confirmed no competing test files exist. Gateway must not modify D production, schema or wiring, and never runs shared E2 resources. Frontend retains all D production/service tests/frontend; core retains recovery, shared wiring and generated Ent. This divides remaining tests without creating or replacing a worker.

Gateway's independent read-only review found remaining receipt-recovery risks: permanent Apply failures retain usage_known indefinitely without manual resolution eligibility; maintenance and recovery share a timeout that can starve recovery; stale-admission updates can block known-receipt selection. Core owns post-snapshot fixes and direct recovery regressions. These findings are listed as explicit remaining acceptance gaps in the local-start artifact.

## Post-Start Independent Review

Source confirmed the captured application built and internal health passed, then explicitly issued HOSTREADY for `http://127.0.0.1:38088` through its loopback-only bridge. The application remains on its internal network with external egress denied. Director neither starts nor cleans runtime/E2 resources. This captured image predates the following corrections and cannot accept them.

Director logged in with the private synthetic bootstrap administrator and navigated only to the new carpool management page. After closing the ordinary onboarding overlay, the page was blank; its DOM was empty and screenshot `evidence/director-ui-old-image-empty-list.png` confirms the rendered failure. Source independently observed an empty terms response with `items: null,total: 0`. The new Vue page reads `terms.length`; core corrected terms/cycles/ledger/payments to successful empty arrays and added four endpoint JSON-array regressions. Frontend additionally normalizes nullable list responses at its API boundary. No copied-user rows or credentials are in the screenshot or evidence.

Core separated maintenance and recovery work, bounded the stale-admission update with SKIP LOCKED, added permanent/retry-exhausted known-receipt promotion while preserving durable cost/payload, and gave each receipt Apply and promotion a fresh bounded context. Director identified and returned the remaining blocked-first-receipt starvation case before the latter fix. Direct tests now cover that case, parent cancellation, malformed receipt isolation, stale dedup convergence, independent maintenance, lifecycle Stop and query shape. Director ran the focused recovery selection three times, then recovery plus all four empty-list endpoints three times: both exited 0. Actual output is in `evidence/director-recovery-focused-three.log` and `evidence/director-recovery-empty-lists-three.log`. These do not replace real database or updated-image acceptance.

Gateway saved fourteen reset repository integration tests, including same-minute cooldown correction from 22:00:10 to 22:00:20, final term/cycle/minute boundary rollback, zero-grant cycle five, observer baseline/replacement/dedup, announcement audience/correction, and scan replay/final-response failure. Core and D explicitly acknowledged a backend compile-only freeze. Source received the exact regex `^(TestCarpoolReset(Execution_|Observation_|Announcements_|ScanOperation_)|TestCarpoolAdjustmentClosure_)`, matching fourteen reset tests plus the unresolved adjustment-closure invariant; source adds its isolation harness. Writers must be released immediately on source COMPILED, not after database execution.

Remaining review requests include actual SQL publication failure with durable retry, publication-relative dates and completion suppression; current announcement failure coverage only injects a business-state error. The remaining new admin modal actions also require the same stale-response identity protection already added to payment. Final whole-source freeze, rebuilt image identity, HTTP/WS completion and desktop/narrow UI acceptance remain open.

## Final-Image Verification In Progress

Director independently verified source PG18 run `20260905T192309Z-87c0a6e5`: 20 top-level tests passed, including the added SQL announcement retry/date/suppression, strict adjustment closure and real SKIP LOCKED recovery cases. Details are appended to `evidence/director-pg18-intermediate.md`. Reset service and recovery/empty-list focused tests also passed three repetitions in the director logs.

Frontend completed all modal stale-response guards and same-tier latest-enabled renewal selection, and froze its source. Director fresh `pnpm typecheck`, `pnpm lint:check`, `pnpm build` and `pnpm test:run` all exited 0; the full run passed 265 files/1901 tests. Logs are `evidence/director-final-image-frontend-{typecheck,lint,build,full-tests}.log`. Embedded frontend SHA-256 is `8098E251957CF7D87285196DE2E3570FF09EEC0A90A5A092CB05DDD500EFCF2F`. Existing Vite large-chunk and test-stub warnings do not change the exit status.

The old image passed source HTTP 67/67, but WS remains 10/11 because missing usage is classified as persistence failure despite zero debit and manual-reconcile state. Source independently inspected the exact synthetic receipt and pinpointed `ErrCarpoolUsageUnknown` handling in WS AfterTurn. Only gateway is narrowly reopened for this diagnostic correction and two-branch regression; source keeps its strict missing-receipt assertion. Core/frontend remain frozen. A fresh backend build, post-fix hashes and new code-ready artifact are still required before source rebuilds.

Source owns the final-image HTTP, WS and additional synthetic admin reset/announcement API acceptance. The reset API suite must use real manual qualification/schedule/replay/permission paths and audience list/version/read/mark-read, without wall-clock/SQL overrides or provider consumption. The director owns desktop and narrow-screen actual UI/screenshot acceptance. Old-image screenshots are diagnostic evidence only. No final acceptance is granted by this record.

## Final Immutable Image Captured

Gateway corrected the sentinel classification and froze three handler files. Director inspected the helper and real omitted-usage WS regression, then ran a fresh whole-backend build (exit 0) and the seven-package carpool-focused selection three times (all pass). The post-fix source hash matched before and after these commands. `director-code-ready-final-image.json` passed `Assert-DirectorCodeReady`, with evidence SHA-256 `D0B72A3EA858F35F99B0FEA76007EE63CD6CF79E43E388170CEA157D01B0068E`.

Source completed preservation-safe rebuilding. Director independently read selected nonsecret fields of private `runtime/rebuilds/rebuild-20260905T193629567Z-5d843fe4cddf47019e67ecf1bbf4a967/rebuild-manifest.json`: Phase `Completed`, new image `sha256:253014dc123b4280b96a73b47e4cbf23f1ab966cfd0386941e31a1ff9152a12c`, source `53EAFD417B380B9AB39DFD220E7C28E31FA7DA9267094E153F4EEEB64B2A2421`, embedded frontend `8098E251957CF7D87285196DE2E3570FF09EEC0A90A5A092CB05DDD500EFCF2F`, matching code evidence hash, and passed copied-record/announcement conservation. Config and app-environment hashes are captured by that manifest and must remain stable during acceptance.

The capture freeze was explicitly released to all existing workers after this acknowledgment. No further implementation is requested; any concrete runtime/UI finding must reopen its owner and require another rebuilt snapshot if source changes. Browser remains paused until source finishes serial HTTP/WS/reset suites and issues SAFEUI. The image is locally ready, not finally accepted.

Director applied the project `trellis-update-spec` skill and added source-backed backend `carpool-contracts.md` and frontend `carpool-state-contracts.md`, linked from their layer indexes. They document exact transaction/error/async boundaries and required tests; they do not duplicate the business specification or change application source.

## Second Immutable Image And Runtime Follow-Through

The first final-image HTTP suite passed 67/67, but strict WS passed 10/11. Its durable missing-usage row was correct; the real administrator list re-sanitized the canonical category into a generic value. Core added only the two exact canonical self-mappings and all-alias/unit plus real-list PG regressions. `research/ws-diagnostic-review.md` records the independent diagnosis and prevention. Director reran full backend build and the seven-package focused selection three times: all exit 0. Real PG run `20260905T194859Z-4843a365` passed both top-level tests and both canonical subtests.

The new immutable `director-code-ready-final-image-v2.json` has approval SHA-256 `0C9B6057A4E477649AD8FDF4C45628035CBEFA5EA8A67D16069D5D4A71D871BF`, source `9138F61CFDDBF8FA454C050275999E5502CA790C8336222B0697FEF5691129D2`, and unchanged frontend `8098E251957CF7D87285196DE2E3570FF09EEC0A90A5A092CB05DDD500EFCF2F`. Director independently inspected private rebuild manifest `rebuild-20260905T195245763Z-d3de3608c1cf463baac08ae41b6c47e3`: Phase Completed, image `sha256:b2fa0308ba3b60d42377b7e95f6a673d23481e96d69b1b7af8fb9c5862fb1fe8`, matching identities and passed conservation. Core capture freeze was released with no further work requested.

Director independently read v2 HTTP evidence `source-runtime-http-20260905T195406071Z.json`: 67 actual pass assertions, zero failures, matching image/approval/config/environment hashes, passed isolation and copied-record/announcement conservation. V2 WS/reset are still source-owned and browser remains paused until SAFEUI.

Browser remains the existing Chrome binding and tab `2048470441`. A blank viewport probe was closed without any runtime access. In-app browser is unavailable; official browser capability discovery exposed `viewport`, whose complete supplementary documentation was read. The persistent `directorViewport` binding supports `set({width,height})` and `reset()` for final desktop/narrow acceptance; no override has been set yet. Reset it before handoff. Do not reload/reselect the existing browser or reread its full documentation.

## V2 Runtime Passes And Remaining V3 Corrections

Director independently read `source-runtime-websocket-20260905T195600247Z.json`: 11 pass assertions, zero failures, exact v2 image/approval/config/environment identities and passed isolation/conservation. The missing-usage case now passes both durable SQL and real administrator API projection checks, with no charge. V2 HTTP remains 67/67.

V2 reset report is `source-runtime-reset-20260905T195840727Z.json` (the filename rounds one millisecond after its completed_at). Director inspected its eight passes/one failure, first_publication scenario, zero copied-user terms and passed isolation/conservation. Failure occurred before qualification registration. Source's actual read-only check returned HTTP 404 plain text for `/api/v1/announcements/version`; director verified that `AnnouncementHandler.Version` and its frontend consumer existed but `RegisterUserRoutes` omitted the route. Core added the authenticated route and a real router regression verifying anonymous 401 and authenticated version JSON 200.

Director's additional whole vet also found the cleanup test's stale provider arguments; core added only the two missing nil service arguments in `cmd/server/wire_gen_test.go`. Director all-unit-tag package compile-only completed with exit 0, followed by full cmd/server/routes/middleware tests three times (exit 0) and whole `go vet -tags=unit ./...` (exit 0). Logs use `director-final-image-v3-*`. Compile-only is not a business-test execution claim.

Frontend read-only final review identified raw reset/observation states in the Chinese admin screen. Director authorized only known reset/health/automatic-delay translation keys, raw fallback for unknown values and unchanged operator text, plus focused regression. No restyling or billing change is authorized. These frontend edits and the new route require a combined v3 source/dist snapshot and source-owned rebuild before final HTTP/WS/reset and browser acceptance. V2 runtime stays healthy and no source suite is currently running. No archive or commit is intended without required user confirmation.

## V3 Immutable Capture

Frontend completed the bounded localization correction with separate batchStatus/healthStatus/delayReason namespaces; reset.health remains the scalar heading. Director independently confirmed the fresh full test log: 265 files, 1902 tests, exit 0. Fresh typecheck, full lint, frontend build, whole backend build, server/routes/middleware tests three times, and whole unit-tag vet passed. All unit-tag packages also compiled; that compile-only run did not execute their business assertions. The backend build emitted no compiler output and exited 0. No unrelated full-suite baseline failure is reclassified as passing.

Source hash remained ED9D2820552B64FD22F96E40EEF94AD80B35538F24BA83AB8BA5AD70FC3F23F2 before/after verification. Embedded frontend is E619531AD228CE39642B6C92156F66F971F2A69691488D502A706237D9189BBC. New immutable evidence director-code-ready-final-image-v3.json passed Assert-DirectorCodeReady with SHA-256 DA84F4CEC19A5664C4D6C64D1CFD219956D53091970EC2EB7AC04A1A33A72CE0; final_acceptance remains false.

Source performed the preservation-safe rebuild. Director independently inspected selected nonsecret fields of private manifest runtime/rebuilds/rebuild-20260905T201859216Z-f086fc7177de4a739008e59c3f4382d3/rebuild-manifest.json: Phase Completed, matching source/dist, image sha256:a59de764355df829d8c58535358767d95da9fb696eb03a5aa1cb8e8564b62955, and both copied-record/announcement conservation checks passed. Source confirmed internal health and denied egress. All existing owners received capture-freeze release with no additional source work requested. Strict HTTP/WS/reset first-publication and pending-merge are source-owned and serial; director browser remains paused until SAFEUI.

## V3 Runtime Review And Driver Diagnostics

Director independently inspected V3 HTTP source-runtime-http-20260905T202004894Z.json (67 pass assertions, zero failures) and WS source-runtime-websocket-20260905T202121496Z.json (11 pass assertions, zero failures). Both bind the exact V3 image/approval, unchanged config DE0F5E535B4695CE00962D8C7C85A46482D9802ACC637010BE02A0AAEEA53D32 and environment 243CE4441380C7E49B7E09BF37EFA839BE86CB6E3F0F1B394C894F6909F40B27, and passed isolation plus both conservation checks. Director also reran the seven-package unit-tag Carpool/NextCarpoolReset selection three times on V3; all passed in director-final-image-v3-backend-focused-three.log.

Reset report source-runtime-reset-20260905T202157504Z.json failed before qualification: eight passes, one response-decode failure at admin authorization. The corrected version route worked. Director source inspection and source live response agreed that existing middleware sends a string code while the driver declared an unused integer Code field. Source-owned tooling correction changed only Code to json.RawMessage and added deterministic string401/403, numeric business code, malformed and trailing JSON regressions. Director independently ran those APIClient tests three times from backend; all passed in director-v3-reset-driver-envelope.log. An initial invocation from the validation directory failed module lookup before tests; the corrected backend invocation uses the existing module. No application change or image rebuild was required.

The next reset report source-runtime-reset-20260905T202723333Z.json has twenty passes and one failure at announcement.version_audience. Actual authorization, registration/replay/conflict, schedule/replay/conflict, exact next Shanghai22:00, early-execution rejection/preserved state, and source-linked first announcement publication within20seconds passed. Synthetic batch1 and announcement10 remain; original copied records/announcements and isolation passed. No failed report is rewritten as passing.

Director inspected announcementSnapshot reading version before list, then waitForAnnouncement returning on new list IDs without version/list coherence. Publication between those requests can produce a mixed snapshot; source/driver owner are verifying this hypothesis with selected synthetic identities. Preserve the existing batch and announcement, never reset scope/clock/SQL to fabricate a fresh first publication. Any stable continuation and fresh pending-merge run must be labeled separately from the initial failed whole run. Application source remains unchanged pending concrete evidence.

## V4 UI And V5 Capture

The driver race correction and V4 composed reset evidence are detailed in source-runtime-startup-progress.md and director-ui-acceptance.md. Original first-publication failed report and unretained baseline remain unchanged/unknown, respectively.

V4 real390px boost retest and desktop/mobile user details passed visual inspection. Director completed all six admin tabs, synthetic-filter empty state, Users opening entry, one authorized new term127/group17/plan8, and read-only renewal preview. Source post-UI report confirms term94 and one550USD grant, ordinary100/key64 hash unchanged, no CNY payments, original copied records/announcements conserved, and preserved batch1/announcement10. One post-open load timeout recovered on read-only retry; precise client dependency remains unproven, with creation201/12ms and fast plans/terms GET200 independently recorded.

Real V4 ledger/preview images exposed remaining fixed protocol enum text. Existing frontend owner corrected only two components, zh/en dictionaries and two tests. Director read the actual changes and both-locale exact-cell regressions, then independently ran full typecheck/lint/build and265files/1907tests, all exit0; whole backend build also passed. SourceB696574939DB9A389525041B17FF5B466EAFF3BB87346938D1026BF20A292117 remained stable across verification; distF8466F2E1B567CEE6A27ACCFA1EAD6087D131C9138CB69693152FDC9897D50AC. Immutable director-code-ready-final-image-v5.json passed the local-start validator with approval2F8841CEB2659B384ABAF5642F56A725B26B35986AD556115ACFD4290FC8A422.

SOURCE rebuilt V5 without deleting/rebaselining data. Director read private manifest runtime/rebuilds/rebuild-20260905T235341228Z-f5645141b3254ba59e2305166a80348d/rebuild-manifest.json: completed image sha256:6d569031eb1b5017fe7c3a7f5f14ed6a863442d9f49d7b2dc372d586b3c7adfd, matching source/dist/approval/config/environment and both conservation passes. Director inspected HTTP source-runtime-http-20260905T235449813Z.json67/67 and strictWS source-runtime-websocket-20260905T235608621Z.json11/11, with matching image/approval/isolation/conservation. Reset and final-image UI remain separate gates.

During final checklist reconciliation the director requested a read-only core evidence audit of multiweek cycle maintenance, cross-expiry boost replay, cross-renewal usage/reset counters and unresolved closing settlement. This audit distinguishes executed assertions from code inspection; no new source change is authorized during V5 capture. Any identified test gaps remain open until resolved or explicitly documented; checklist marks are not proof by themselves.

## Final Local Acceptance

The four audit gaps are closed with test-only additions. Director independently read final PG run20260906T001828Z-46e6ee52 summary/complete verbose output: four lifecycle tests plus harness passed, four nested unresolved-state cases passed, exit0. The preceding two fixture-inference failures remain preserved. Actual executed binary hash is BCFFA2F680DE0F2B742B91FA8661EF16B30EB129FC25F9B9BFF8DECFF73C5ED7. Frontend fake-clock cases passed the fresh full265file/1911test suite.

Director independently hashed final source05378BFC865B2E4AD5DF9ADC7DAEB369FFF9D66250247473CA6FE8FE72B65C53, unchanged distF8466F2E1B567CEE6A27ACCFA1EAD6087D131C9138CB69693152FDC9897D50AC, both additive test files and actual preserved/final Linux executables. Source final equivalence report binds identical executable30BA65BC7633884C71C77F9DEDDAC7CCEF6E86C859759A3AACA4FD81A7946170. No original V5 approval/source hash was rewritten, and no test-only runtime restart was needed.

V5 HTTP67/67, WS11/11 and coherent reset22/22 reports, actual desktop390px UI screenshots, and post-UI safety/conservation were independently inspected. Viewport reset, no V5 business UI writes. evidence/director-final-acceptance.md records local handoff and residual limits, including the non-green exact-base service suite, composed first-publication evidence, unknown transient refresh cause and zero copied memberships without confirmed mapping. Checklists and PRD now reflect evidence-backed completion. Task stays in_progress for separately authorized commit/archive; no production integration or deployment.
