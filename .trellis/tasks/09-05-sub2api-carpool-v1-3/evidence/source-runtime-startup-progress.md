# Source Runtime Startup Progress

Status at 2026-09-06 08:19 Asia/Shanghai: V5 passes HTTP 67/67, strict WebSocket 11/11 and coherent pending-merge reset 22/22. The director completed final-image UI; source post-UI safety and additive lifecycle PostgreSQL tests passed. Final test-only source builds an identical V5 application binary. Source work is ready for director acceptance; see `source-runtime-final-handoff.md`. The original V3 first-publication 20-pass/1-fail result remains preserved and is not rewritten as green.

## Reviewed Snapshot

- Director evidence: `director-code-ready-local-start.json`.
- Evidence SHA256: `F3583FB6D1BACF5E7A85DA9849D20E98A2BA15AEEFB0EFE041D5E2BE3AD85C07`.
- Source SHA256: `8496B57ACDDA2D2FFCC1B69B8B80C104E52454CF180A1FC237CAE4F7108C58ED`.
- Embedded frontend SHA256: `FBA89230EA032A8745E817EB09189061A491CF00587990C6146C49BE3F79ACF4`.
- Image: `sha256:3b7be8b1d4efac300e933a4b53008b8b4f482344ba343d054d9432b2b17846a4`.

The startup script verified source and frontend identities before and after compilation and saved `BuildComplete=true` with the immutable image ID. Application writers were then released. Acceptance for this image does not accept subsequent checkout changes.

## Actual Startup

The synthetic bootstrap committed twelve fresh identities and privately published their credentials. The installed config passed hash, owner and mode checks. The application and mock use one internal-only network, nonroot users and read-only roots. Internal `/health` returned `{"status":"ok"}`. Direct-IP outbound probing failed at routing level. No copied credential was used.

The first host-health attempt failed because this Docker engine suppresses host publication for the internal-only network: requested loopback `PortBindings` existed, but actual published ports were empty. Internal health was already passing. Startup now distinguishes internal health from host access, preserving the network isolation. A separately reviewed loopback ingress is required for browser acceptance.

## First HTTP Attempt

Evidence `source-runtime-http-20260905T184804698Z.json` records one pass and one failure. Synthetic admin login passed. Fresh synthetic user creation returned HTTP 423 because the existing administrator compliance middleware had no prerequisite fixture for the newly created test administrator. No carpool feature assertion ran; this is not a feature pass or a new application regression.

The prerequisite must be represented explicitly as synthetic test data, not as an operator's legal consent. No real-user acceptance record may be overwritten and the legal acceptance endpoint is not used on the user's behalf.

Original copied users, keys, usage, deduplication rows, groups and announcements all passed conservation after this attempt.

## Loopback Access And Subsequent Attempts

The reviewed Windows bridge is now bound exclusively to `127.0.0.1:38088`; host `/health` returned 200. The bridge uses shell-free Docker exec against the exact immutable container ID and verifies its identity/network for each connection. A deterministic 2 MB finite-response test verifies the stdout drain, with separate lifecycle and concurrency tests. The application has not been attached to another network.

The synthetic administrator prerequisite was applied with a guarded serializable insert of one new test-only settings key. Its marker explicitly states that it is synthetic fixture state, not operator consent. The original container predates task labels, so the applier verifies the sealed exact PostgreSQL ID, network ID, alias and sanitization evidence instead of assuming a label exists.

HTTP attempt `source-runtime-http-20260905T190055875Z.json` passed 31 assertions and stopped at a zero-result parser mismatch: the API returned `items:null,total:0`. Independent read-only verification confirmed no term had been created. The driver now accepts that representation only in its no-mutation check. The director independently reproduced a blank admin page caused by nullable list items and returned the application array-contract fix to core.

HTTP attempt `source-runtime-http-20260905T190538564Z.json` passed 55 assertions, including exact previews, initial/fifth-cycle grants, boost limits and concurrency, authorization and no ordinary-balance fallback. Gateway forwarding returned 502 because the existing enabled URL allowlist always requires HTTPS, despite the local `allow_insecure_http` flag. A reviewed single-property local configuration transition is being prepared; production configuration and network isolation will remain unchanged.

The first WebSocket attempt passed seven setup assertions but no functional turn: model mappings lacked the run-specific aliases and the legacy transport eligibility flag was absent. The driver fixture has been corrected without changing application code or mock behavior. No WebSocket billing pass is claimed yet.

## Gateway Progress

The local HTTP-policy transition completed with the same app, mock, image, database, Redis and network IDs. The only semantic configuration change was `security.url_allowlist.enabled:true` to `false`; the network remains internal with routing-level egress denial. Original and intended configuration hashes are `D46343F4730C9E5A4650ACD81DAF0B22F94B75B5B4AD6EE708DCDA2BD48179BB` and `DE0F5E535B4695CE00962D8C7C85A46482D9802ACC637010BE02A0AAEEA53D32`. Exact private archives preserve the original configuration and state. Resume testing uncovered and fixed timestamp byte-normalization and PowerShell path-parameter issues. The coordinator also verified and stopped only its own loopback bridge before Docker restart, then relaunched it and verified host health.

HTTP evidence `source-runtime-http-20260905T192112617Z.json` records **67 passed, 0 failed**, with original-record and announcement conservation plus isolation all passing. This includes real HTTP and SSE forwarding with exact debit checks, future renewal, payments/refund entries, reversible audited adjustment, takeover and immutable plan snapshots.

The second WebSocket attempt passed ten assertions and failed one. Real two-turn usage produced two independent receipts and two debits. A later turn after expiry was refused before upstream forwarding. Same-turn cross-cycle failover used two different synthetic accounts and settled exactly once to the admitted cycle. Missing usage produced one `reconcile_required` record with NULL cost/payload and no debit, but its diagnostic was `usage_persistence_or_settlement_failed` rather than the strict expected missing-receipt category. Connection-close timing and that diagnostic distinction were returned for review. This is not yet a complete WebSocket acceptance pass.

All results above refer to the initial immutable reviewed image; subsequent application corrections require a final image rebuild and repeated acceptance.

## First Final-Image Rebuild

The approved `director-code-ready-final-image.json` rebuilt successfully. The private manifest at `runtime/rebuilds/rebuild-20260905T193629567Z-5d843fe4cddf47019e67ecf1bbf4a967/rebuild-manifest.json` reached `Completed` after verified compilation, image capture, startup, internal health and denied-egress checks.

- Approval SHA256: `D0B72A3EA858F35F99B0FEA76007EE63CD6CF79E43E388170CEA157D01B0068E`.
- Source SHA256: `53EAFD417B380B9AB39DFD220E7C28E31FA7DA9267094E153F4EEEB64B2A2421`.
- Embedded frontend SHA256: `8098E251957CF7D87285196DE2E3570FF09EEC0A90A5A092CB05DDD500EFCF2F`.
- Image: `sha256:253014dc123b4280b96a73b47e4cbf23f1ab966cfd0386941e31a1ff9152a12c`.
- Local config SHA256 remains `DE0F5E535B4695CE00962D8C7C85A46482D9802ACC637010BE02A0AAEEA53D32`.

The coordinator stopped only the independently verified old task bridge before rebuilding, then relaunched the private bridge after health/isolation passed. The new listener was verified at `127.0.0.1:38088`, PID 77468, expected parent 74024 and private executable path; host `/health` returned 200. Database, Redis, copied records, credentials and original conservation baselines were preserved.

Final-image HTTP evidence `source-runtime-http-20260905T194034665Z.json` passed 67/67 with isolation and both conservation postconditions passing. Strict WebSocket evidence `source-runtime-websocket-20260905T194155808Z.json` passed 10/11. The missing-usage persisted row now correctly has `reconcile_required`, `usage_receipt_missing`, NULL cost, no billing payload and zero debit. However, the real admin list re-sanitizes `usage_receipt_missing` into `usage_reconciliation_required`. The director and driver owner independently confirmed this canonical-classification defect; the unchanged strict driver correctly continues to fail it. The persistence-failure canonical category needs the same idempotence protection.

The director owns that narrow application fix and regression tests. No driver predicate or mock behavior was weakened. Reset HTTP, the next corrected immutable image and final browser acceptance remain separate pending gates.

## Corrected Second Final Image

The canonical diagnostic correction passed the real PostgreSQL projection regression documented in `source-runtime-pg-progress.md`. The director then approved `director-code-ready-final-image-v2.json`; the coordinator independently verified that approval and rebuilt without changing copied database/cache state.

- Approval SHA256: `0C9B6057A4E477649AD8FDF4C45628035CBEFA5EA8A67D16069D5D4A71D871BF`.
- Source SHA256: `9138F61CFDDBF8FA454C050275999E5502CA790C8336222B0697FEF5691129D2`.
- Embedded frontend SHA256: `8098E251957CF7D87285196DE2E3570FF09EEC0A90A5A092CB05DDD500EFCF2F`.
- Image: `sha256:b2fa0308ba3b60d42377b7e95f6a673d23481e96d69b1b7af8fb9c5862fb1fe8`.
- Completed private manifest: `runtime/rebuilds/rebuild-20260905T195245763Z-d3de3608c1cf463baac08ae41b6c47e3/rebuild-manifest.json`.
- Exact application ID: `b2ece468bd5163f3c55b59bc15d61aed5ffef4ce04000044664a4ffbace90dfc`.
- Exact mock ID: `eb031a15d67a6931b8a094bade8158e24dbc00571161ed80e47085fb9435bf83`.

Internal health and denied egress passed. The previous verified task bridge was stopped before rebuild; its replacement was verified at `127.0.0.1:38088`, PID 38224 and parent 63888, with the expected private executable and host `/health` HTTP 200. Local config remains unchanged.

HTTP `source-runtime-http-20260905T195406071Z.json` passed 67/67. The unchanged strict WebSocket driver then passed 11/11 in `source-runtime-websocket-20260905T195600247Z.json`, including the corrected real admin exception projection. Both reports bind the corrected image and approval and pass original-record conservation, original-announcement conservation and runtime isolation. Reset HTTP and browser results must still be collected before final task acceptance.

Reset attempt `source-runtime-reset-20260905T195840727Z.json` passed eight setup/safety assertions and failed while obtaining the initial announcement version. Independent source inspection confirmed that the existing version handler and frontend consumer had no authenticated `GET /announcements/version` route registration. A direct host check confirmed HTTP 404 with `text/plain`, not a JSON response. No qualification or reset batch was created; the new synthetic group and two synthetic terms remain as test data. Zero copied-user terms, both conservation checks and isolation passed. The owner must correct the route and verify an actual router regression; this failed reset run is not accepted as a feature pass.

## V3 Reviewed Runtime

The authenticated announcement-version route and actual-router regression passed before `director-code-ready-final-image-v3.json` approval. V3 also included localized reset/observer statuses and automatic reasons with free-text fallback preserved. The director recorded 265 frontend files / 1902 tests passing, full frontend typecheck/lint/build, backend build, whole unit-tag vet, all unit-tag package compilation, and three repetitions of the related router/server/middleware tests. Package compilation is not full service-suite execution: the five independently reproduced baseline service failures remain disclosed in `director-baseline-service-review.md`.

- Approval SHA256: `DA84F4CEC19A5664C4D6C64D1CFD219956D53091970EC2EB7AC04A1A33A72CE0`.
- Source SHA256: `ED9D2820552B64FD22F96E40EEF94AD80B35538F24BA83AB8BA5AD70FC3F23F2`.
- Embedded frontend SHA256: `E619531AD228CE39642B6C92156F66F971F2A69691488D502A706237D9189BBC`.
- Image: `sha256:a59de764355df829d8c58535358767d95da9fb696eb03a5aa1cb8e8564b62955`.
- Completed private manifest: `runtime/rebuilds/rebuild-20260905T201859216Z-f086fc7177de4a739008e59c3f4382d3/rebuild-manifest.json`.
- Exact application ID: `86951f6e7554ade286af21693079f2e30d3469c0888a50b4819fe90c184660e9`.
- Exact mock ID: `7f65093fe1a94b426c02fe3800119cc02c8c76b79d79c0e0a66de7bab4cf3173`.

The coordinator preserved database, Redis, private fixtures, configuration and original conservation baselines. Internal health and denied egress passed. The replacement bridge was verified at `127.0.0.1:38088`, PID 63612, parent 47644, with its private executable path and host `/health` HTTP 200. An anonymous announcement-version request now returned HTTP 401 JSON. The unrelated port 18080 was not touched.

HTTP `source-runtime-http-20260905T202004894Z.json` passed **67/67**. The unchanged strict WebSocket driver passed **11/11** in `source-runtime-websocket-20260905T202121496Z.json`. Both bind the exact V3 image and pass isolation plus both conservation postconditions.

## V3 Reset Attempts And Preserved First Publication

Reset attempt `source-runtime-reset-20260905T202157504Z.json` passed eight assertions and failed one before qualification creation. Its driver decoded an unused envelope `code` as an integer, but authorization middleware legitimately returns string codes while business handlers return numeric codes. The driver owner changed only that unused field to `json.RawMessage` and added deterministic string/numeric/malformed/trailing-response tests. No application change was needed.

After that driver correction, `source-runtime-reset-20260905T202723333Z.json` passed **20 assertions and failed one**. Passing checks include authorization, missing-key refusal, confirmed qualification and exact replay/conflict behavior, precise Shanghai scheduling and exact replay/conflict behavior, early execution refusal with zero targets/grant preserved, and publication within 20 seconds using the exact source identity. The failing assertion is `announcement.version_audience`.

That first-publication run created synthetic batch 1 and announcement 10. They are preserved. The private run directory is `runtime/acceptance-reset-e66c7a37c2a8452bae0b324fa6157ec3`; its synthetic member/future-member/outsider IDs are 118/119/120. No mark-read step was reached. The report does not retain pre-publication version/count values, so those values cannot be reconstructed or assumed to be zero.

The coordinator's subsequent read-only version/list/version observation found stable versions and unread counts for all three run-owned identities. Member 118 and future member 119 each had 10 visible unread announcements, including announcement 10. Outsider 120 had nine visible unread announcements, excluding announcement 10. There are nine pre-existing visible announcements. This observation supports current audience correctness, but does not convert the earlier failed assertion into a pass or independently prove its historical before/after version transition.

The driver owner is reviewing whether publication between its original version-first and list-second requests produced an incoherent snapshot. The approved continuation is a bounded coherent-snapshot driver correction, preserving exact audience/permission assertions, followed by a fresh `pending_merge` run. No database reset, scope deletion, clock change, recreated first publication or alternative fresh copied database is allowed. Any final first-publication evidence must be explicitly described as composed from the original passing assertions, truthful read-only continuation and the separate real-PostgreSQL publication tests in `source-runtime-pg-progress.md`.

## Restricted V3 Browser Progress

The director used only synthetic HTTP identities. Fresh V3 three-seat boost changed remaining claims from two to one while ordinary balance stayed zero and the time-only rail retained 7:7:7:7:2 proportions. Desktop 1440 and details 390 checks showed no horizontal overflow; `director-v3-user-*.png` contains the director's screenshots. Mobile dashboard boost text was squeezed beside the button. Only that frontend layout and its regression test were reopened; no V4 runtime rebuild is authorized until the next reviewed approval. No administrator/reset/global announcement mutation was part of this restricted browser pass.

## Snapshot Driver Correction

The driver owner reproduced the observation race with deterministic HTTP responses: publication between the first version and list calls made the old implementation combine different states. The corrected implementation accepts only matching version/list/version reads with a matching projected unread count, retries incoherent reads under a bounded child context, and fails malformed responses. The 20-second publication context is also checked before success. Exact first-publication and pending-merge audience predicates remain unchanged.

The owner verified focused tests, vet, twenty repeated snapshot-test runs and Linux compilation. The coordinator independently inspected the changed snapshot/polling logic, reran all focused driver tests successfully and matched the frozen hashes:

- Driver source SHA256: `1CBB6D7ABB28F43AEA6E05CF887C23ACE3F7A119778952D74C24385C1D0698C6`.
- Driver tests SHA256: `340927AE7984ADB9C3D4639FF716701F9782B124F9A8BA928FF9A350F0FF0062`.
- Driver README SHA256: `C5143A35E44A27DAF616428885A9D99F58E3F102D3958B94B90ED10C54F18C70`.

This proves the driver defect and its correction, not the exact historical interleaving in the failed live run whose baseline values were not retained. No application, copied database or source announcement was changed for this fix.

## Final V4 Rebuild

The director froze all application writers and approved `director-code-ready-final-image-v4.json` after the sole mobile boost wrapping/alignment correction and regression test. Complete frontend typecheck/lint/build and 265 files / 1903 tests passed; a fresh whole-backend build passed. Backend business behavior is unchanged from V3.

- Approval SHA256: `6CA4C8B23F69CD87A32B9AFE667A1D8F1DDAC80247D69C240DC5183E5258D490`.
- Source SHA256: `E27EC3B41A1DDC3FE78520B33E93CA811821C9F5D8DCEA72D81E2F8F1B2812D4`.
- Embedded frontend SHA256: `2B88660EFE8991B65B380713D96DDBFCC9E67BE523080393BB27563EFCED9508`.
- Image: `sha256:0ad4aa72ab397139643253a22f8b1643f7593edc8fef9624a0db6ac9a3408ef8`.
- Completed private manifest: `runtime/rebuilds/rebuild-20260905T204707568Z-064fa7f9588c409e9c58c4fbbaad2581/rebuild-manifest.json`.
- Exact application ID: `50efe839322a9af6b5a67bd3d3995f21c7cdcb686f5fc0c23ad021f5c837be71`.
- Exact mock ID: `53e5b346a7c574f43ee95d9a6c81ad8b129aa3cd628e9666b6e4402776745ea7`.

Before replacement, the coordinator independently verified the old bridge executable, PID 63612, parent 47644 and sole loopback listener, then stopped only that bridge. The preservation-safe rebuild completed with database, Redis, configuration, fixtures, original baselines and synthetic batch 1 / announcement 10 retained. Internal health and denied egress passed. The new private bridge was independently verified at `127.0.0.1:38088`, PID 41152, parent 54780; host `/health` returned 200. Configuration/environment/network hashes remain unchanged from V3.

V4 HTTP `source-runtime-http-20260905T204812429Z.json` passed **67/67**, with isolation and both conservation checks passing. Its fresh synthetic fixture is under private `runtime/acceptance-http-227b1e77eafd407d92e7d7d9f554a960/results/batch-fixtures.json`. WebSocket, pending-merge and final browser gates remain separate until their actual results are recorded.

## V4 Completed API Acceptance

Strict WebSocket `source-runtime-websocket-20260905T204926926Z.json` passed **11/11**. Coherent reset `source-runtime-reset-20260905T205045597Z.json` passed **22/22**, scenario `pending_merge`. Both bind the exact V4 image and approval, with isolation and both original-record conservation checks passing.

The new qualification joined the preserved batch 1 without changing its schedule or source announcement 10. The real APIs verified missing-key rejection, exact registration and schedule replays, conflicting-payload rejection, out-of-window execution refusal and zero targets/grant preservation. Coherent reads proved no duplicate publication, active/future audience visibility and outsider exclusion. The outsider could not mark the announcement read; the member's read changed only its own version/unread state, and the future member remained unread.

The private reset fixture is `runtime/acceptance-reset-90bdd02d20f7421c87bd61f9b150d0a5/results/batch-fixtures.json`. It contains only fresh synthetic member 136, future member 137 and outsider 138, with group 19 and terms 92/93, plus the synthetic bootstrap administrator. No copied-user term exists. Runtime/API mutations are now frozen while the director performs synthetic UI workflows.

Actual due execution is explicitly outside this HTTP assertion count and remains the real-PostgreSQL coverage in `source-runtime-pg-progress.md`. First-publication acceptance is composed evidence: original V3 passing publication/source assertions, separately observed stable audience, deterministic driver-race regression, V4 coherent pending-merge/read-state checks and the real-PostgreSQL publication cases. The original 20-pass/1-fail report remains unchanged; its unretained before/after version values are unknown.

## Final UI Continuation Boundary

The coordinator independently inspected the director's V4 announcement, mobile dashboard/help, desktop/mobile user-details and filtered synthetic admin-term screenshots. The boost row is readable, the time-only rail preserves the five-cycle proportions, and the three-seat term shows 768 USD after 700 USD base minus 2 USD usage plus a 70 USD boost. These screenshots use only synthetic identities.

The director is authorized to open exactly one term through the real UI for the fresh HTTP run's synthetic ordinary user 127, group 17 (`carpool-accept-e5ea5e92ed64`) and current enabled four-seat plan 8. No payment, refund, takeover, key/group migration or copied-user operation is authorized. The create-only private `runtime/director-ui-ordinary-127-before.stdout.log` records balance 100 USD, zero terms, active key 64 already in group 17 and full key-row hash `15c8d3b5b03c17a5f5e90ee9c1cca47c`. The coordinator will compare balance and key-row integrity after the director's UI-complete signal. Existing batch 1 / announcement 10 and qualification state remain preserved.

The admin ledger's raw event/bucket localization finding is not accepted as a finished UI result. A further reviewed frontend image and repeated immutable runtime acceptance are required if that correction changes application source. No runtime rebuild is authorized before the next finalized director approval.

## V4 UI Opening And Post-UI Verification

The director submitted the authorized new opening exactly once with a null start, no payment and no takeover. Read-only PostgreSQL verification found one active term 94 for synthetic user 127/group 17/plan 8, from `2026-09-05T21:07:38.418244Z` through exactly 30 days later. It has one ledger grant of 550 USD. Cycle 466 is active at 550 USD; cycles 467-470 remain scheduled with zero balances, and cycle 5's target is 157 USD. There are no payment records. Ordinary balance remains 100 USD and key 64's full-row hash matches the pre-opening snapshot.

The UI then experienced a follow-up loading timeout. The director did not resubmit the opening; one read-only Retry displayed the committed term successfully, and a later renewal preview was not submitted. Structured application logs show the opening POST returned 201 in 12 ms and the initial plans/terms GETs returned 200 in 1/4 ms. No original groups/all request appears in the bounded log interval; the Retry groups/all request returned 200 in 4 ms. This proves successful single creation and recovery, not the precise client timeout cause.

The isolated host-bridge reviewer found the expected PID 41152, parent 54780, sole loopback listener and six established sessions with six Docker children, below the 32-session cap. Its log has only the readiness record. Connection-level verification/exec/copy failures are not traced, so available evidence cannot distinguish browser non-dispatch from pre-application delivery failure. No concrete bridge defect was demonstrated and no speculative bridge patch was made.

`source-runtime-v4-post-ui-20260905T211421967Z.json` records passing exact immutable V4 identity, configuration/environment, isolation, host health, original-record conservation, original-announcement conservation and zero copied-user terms. Batch 1 remains scheduled for `2026-09-06T14:00:00Z`, revision 0, with no targets, reset ledger, effective timestamp or completion timestamp; the two API-created qualifications remain. Announcement 10 remains the active qualification source for batch 1/revision 0. This is a post-UI safety pass, not final acceptance while the frontend localization correction is pending.

## Resumed V5 Final Build

After the user resumed work, the coordinator reverified the preserved V4 image, application/mock identities, bridge ownership, internal network, host health and both copied-data conservation checks. No application mutation was retried. The director completed and froze the six-file frontend-only fixed-enum correction, covering 11 ledger events, three buckets and four preview actions in Chinese and English while preserving unknown values and free-form audit text. Complete frontend typecheck/lint/build and 265 files / 1907 tests passed, with a fresh whole-backend build and stable source/dist identities.

The coordinator independently validated `director-code-ready-final-image-v5.json`, stopped only the verified V4 bridge and completed the preservation-safe rebuild:

- Approval SHA256: `2F8841CEB2659B384ABAF5642F56A725B26B35986AD556115ACFD4290FC8A422`.
- Source SHA256: `B696574939DB9A389525041B17FF5B466EAFF3BB87346938D1026BF20A292117`.
- Embedded frontend SHA256: `F8466F2E1B567CEE6A27ACCFA1EAD6087D131C9138CB69693152FDC9897D50AC`.
- Image: `sha256:6d569031eb1b5017fe7c3a7f5f14ed6a863442d9f49d7b2dc372d586b3c7adfd`.
- Completed private manifest: `runtime/rebuilds/rebuild-20260905T235341228Z-f5645141b3254ba59e2305166a80348d/rebuild-manifest.json`.
- Exact application ID: `44a67f2a43ecde5ae1977e01330caabc735ccc60988073e8f69a9b7e10159fa8`.
- Exact mock ID: `15eb0e2d9fd269aabce3757412e6bbb11b915b9c2d07443c0e78210ce7a581dc`.

Database, Redis, configuration/environment, private fixtures and original conservation baselines remain unchanged. Internal health and denied egress passed. The replacement bridge was independently verified at `127.0.0.1:38088`, PID 76160, parent 30284, with its expected private executable; host `/health` returned 200. V5 HTTP `source-runtime-http-20260905T235449813Z.json` passed **67/67**, with isolation and both conservation checks passing. Its fresh private fixture is under `runtime/acceptance-http-128e2db3846f4512a0e9e55eb1c625ec/results/batch-fixtures.json`. No V5 browser acceptance is implied until the director reviews that actual image.

V5 strict WebSocket `source-runtime-websocket-20260905T235608621Z.json` passed **11/11**. Coherent pending-merge reset `source-runtime-reset-20260905T235712249Z.json` passed **22/22**, preserving the existing batch 1 / announcement 10 and adding only its run-owned qualification through the real API. Both reports bind the exact V5 image/approval and pass isolation plus both copied-data conservation checks. Actual due execution remains explicitly excluded from HTTP and covered by separate real PostgreSQL evidence.

The fresh reset fixture is under private `runtime/acceptance-reset-2ff106a49492492da37fb1dafae7afcd/results/batch-fixtures.json`: synthetic member 154, future member 155, outsider 156, group 22 and terms 110/111. The member's mark-read passed without changing the future member or granting outsider access. All foreground suites and temporary runners are complete. The director has full synthetic SAFEUI for final localized ledger/preview inspection, with no additional term opening, renewal, boost, qualification or reset mutation required. Final evidence review may request additional isolated tests; an unchecked criterion is not converted into a pass by source inspection alone.

## Source Final Handoff

The director completed V5 desktop/390px localized ledger and renewal-preview review plus user dashboard/details smoke, with no V5 business mutations. `source-runtime-v5-post-ui-20260906T001713694Z.json` verifies immutable image/config/environment/network and loopback health, both original-data conservation checks, unchanged synthetic user 127 ordinary balance/key and single term/grant, unchanged original two reset qualification rows and announcement 10. The third qualification is the documented V5 pending-merge fixture. Copied-user terms remain zero.

The final evidence audit added only lifecycle/component tests. The director recorded 265 frontend files / 1911 tests passing. The separate final lifecycle PostgreSQL run passed five top-level tests (four business plus harness) and four nested statuses, with cleanup verified; details and retained initial fixture failure are in `source-runtime-pg-progress.md`. Final exact Linux compilation is byte-identical to the V5 image input and embedded frontend is unchanged, recorded in `source-runtime-v5-final-equivalence-20260906T001844573Z.json`. The original V5 source hash remains its historical build snapshot; the final test-only aggregate is separately bound. No runtime restart, copied-data operation, commit, push, archive or deployment was performed for this addition.
