# Director v1.4 Local Acceptance

Date: 2026-09-06, Asia/Shanghai. Result: A1-A7 accepted for local delivery, not production deployment.

## Delivered Behavior

- New openings and renewals use exactly 28 days, four equal seven-day cycles and three boosts per term. Each boost is 10% of full weekly quota: 55/70/110 USD. Prices and full weekly quotas are unchanged.
- Existing 30-day/five-cycle/two-boost contracts remain immutable and retain their actual presentation. Migration239 appends forward plan versions while preserving custom price, quota, name and enabled state.
- Dashboard/header consume authoritative carpool available quota, separately from ordinary money. A successful boost refreshes this projection; stale responses and account changes cannot replace it with another account's data.
- Successful own-term resets annotate the time rail with `已加满至 $700` and actual Shanghai timestamps below. The amount is the immutable base target, not the grant delta. Zero-grant successes are included; pending, failed and foreign targets are excluded.
- Exact >=48h cooldown and the Shanghai 22:00 execution minute remain unchanged. No qualification means no automatic reset. Explicit administrator renewal tier changes remain supported; only omitted plan_id defaults to the latest same-code tier.

## Evidence Identity

Worktree: `C:/WORK-SPACE/sub2api-carpool-v1.3`, branch `codex/sub2api-carpool-v1.3`, HEAD `36266f512776d78d4f1645a75ae0e84816f8a0a7`.

| Item | SHA-256 |
| --- | --- |
| Immutable local-start gate, director-code-ready-v14.json | EBAD973BAD5CC5FF0401EBFEC60D9C29FB52EEA94D75BD4E789852513D639060 |
| Backend/frontend source | 03C390BE535CDDA2F0967CC63DBA88126ED1C211B7FCE4A5908AD1A89E9BFAF6 |
| Embedded frontend | CB46406A6786FCF952D3D5E84E29167CCE91F547ECAC059C752CC40CBBD783E5 |
| Running image | 8a3edbcd4dc3a09846f23834c33ccf7322d0f07fd4dbb42900a8de1ad2caec25 |

Private rebuild manifest: `runtime/rebuilds/rebuild-20260906T031626925Z-681e29d10c1241b797d6c610dce11895/rebuild-manifest.json`. The previous image and original runtime data remain available. The immutable local-start gate still says final_acceptance=false by design; this separate report records subsequent acceptance without rewriting that gate.

## Executed Gates

| Gate | Result and evidence |
| --- | --- |
| Backend build and vet | Passed whole build/vet; private rules28-ready-backend-build and rules28-final-backend-vet logs |
| Backend focused units | Passed Carpool selection and actual router/server/middleware tests; private rules28-ready-backend-focused and rules28-director-router-server-middleware logs |
| PostgreSQL18 | 11 distinct selected tests passed across the three runs below; not a claim that every run passed |
| Frontend | Typecheck, full lint, build and 267 files / 1930 tests passed; rules28-frontend-*.log |
| Exact-image HTTP | 71/71, source-runtime-http-20260906T031759170Z.json |
| Exact-image WebSocket | 11/11, source-runtime-websocket-20260906T031926011Z.json |
| Trellis manifests | task.py validate passed, two real entries each in implement.jsonl/check.jsonl |

PostgreSQL evidence remains outside git in `PrivateTests/sub2api-carpool-v1.3-20260905/repository-tests/runs/`:

- `20260906T030646Z-d35444b9`: five passes (lock-boundary reselection, missed-cycle maintenance, boost replay after expiry/renewal, preview validation, migration conservation/replay). Renewal test failed on decimal representation; that failed run remains failed and retained.
- `20260906T030823Z-f99f8c99`: five passes (four claims yield exactly three boosts, cross-renewal details, transactional reset execution including zero grant and legacy cycle five, successful-zero-grant projection, inactive quota null).
- `20260906T031418Z-b3748017`: corrected fixed-rule plan/renewal test passed, including explicit administrator tier upgrade. The correction uses numeric equality rather than decimal internal representation equality.

The independent review and its fixes are in `rules28-independent-check.md`. The director rejected the reviewer-introduced explicit same-tier restriction; final code and HTTP acceptance preserve explicit upgrades.

## Actual Browser Acceptance

All identities below are synthetic. No copied-user identity or credential appears in screenshots or this report.

- Preserved reporter user122: ordinary balance0, authoritative carpool quota838 shown in dashboard/header/details; actual legacy five-cycle contract preserved.
- Fresh user158/term113: 28 days, four cycles, three boosts. Actual browser click changed available quota698 -> 768 and remaining3/3 -> 2/3. Reload retained768. Selected database verification found ordinary balance0, boost_used1, four cycles, exactly one boost ledger row and ledger net768.
- Ordinary synthetic user: original100USD balance retained in dashboard/header, no carpool badge, unavailable boost. Switching identities did not leave the preceding account's quota in the accepted loaded state.
- Desktop1440 and narrow390 screenshots were captured and viewed. The boost control remains readable and does not overflow. Viewport capability was reset afterward. The final browser remains on the new-rule user's dashboard with768USD and two boosts available.

Useful screenshots: `rules28-new-before-boost-desktop.jpg`, `rules28-new-after-boost-desktop.jpg`, `rules28-new-after-reload-390.jpg`, `rules28-four-cycles-390.jpg`, `rules28-ordinary-100-loaded-desktop.jpg`.

### Successful Reset Markers

The create-only historical fixture importer was independently reviewed and tested in disposable migrated PostgreSQL18, including replay refusal, before the isolated runtime import. Its private result is `runtime/reset-marker-fixture.json`. It created synthetic user172/term127 in new scope2147480914; existing rows and scope1 were not edited. This fixture only demonstrates historical event projection and UI, not live scheduler execution or boost eligibility in the alternate scope.

The term is 2026-09-01T04:00:00Z through 2026-09-29T04:00:00Z with four cycles and700USD availability. Both actual markers show `已加满至 $700`:

| Event | Shanghai display | Historical grant | Display target |
| --- | --- | ---: | ---: |
| 1 | 2026/09/02 22:00:20 | 163 | 700 |
| 2 | 2026/09/04 22:00:20 | 0 | 700 |

`rules28-reset-markers-desktop.jpg` and `rules28-reset-markers-390.jpg` were successfully captured and visually inspected. DOM geometry independently confirms four equal tracks, two distinct nonoverlapping target labels and both timestamps below the rail:

- Desktop: viewport/scroll width1440; track width277 each; rail bottom486.5; timestamp top494.5.
- Mobile: viewport/scroll width390; track width86.5 each; rail bottom718.5; timestamp rows top726.5 and748.5. Label rectangles end before the rail and do not intersect.

The browser capture tool occasionally timed out. One mobile full-page capture repeats a page tile below its first viewport; the inspected first viewport contains the complete rail, both target labels and both timestamps. The supplemental `rules28-reset-markers-390-viewport.jpg` is a scaled complete-page capture, not a native-pixel screenshot. These capture artifacts are retained and are not attributed to an application rendering defect. Actual due execution, all-or-nothing writes and zero-grant accounting are proven by the separate PostgreSQL tests, not these imported history images.

## Final Conservation And Runtime

The director reran the following after UI acceptance, without recapturing any baseline or rerunning the mutating reset HTTP suite:

- `check-copied-records.ps1`: passed; original users, keys, usage, dedup and groups unchanged.
- `check-copied-announcements.ps1`: passed; original rows unchanged, migration defaults empty.
- `check-preserved-synthetic.ps1 -CodeReadyEvidence .../director-code-ready-v14.json`: all21 hash/count comparisons passed, preserving users122/127, terms78/94, key64, batch1, announcement10 and three qualifications.
- `Assert-DirectorCodeReady` against current source and embedded frontend: matched the immutable approved hashes. Running image matches startup binding; running=true, restart_count=0, loopback `/health` HTTP200.
- Isolation: one internal-only network; database/Redis unpublished; application only publishes127.0.0.1:38088->8080, no privileged mode/engine socket. Owned loopback listener PID64620. An initial ad-hoc check incorrectly expected no application port and failed; comparison against the existing start/rebuild contract corrected that diagnostic assumption and passed. No runtime setting was changed.

Local URL: http://127.0.0.1:38088 . Detached service intentionally remains running. No foreground test/exec session remains pending.

## Residual Limits And Handoff

- The full backend service suite is not green: previously reproduced Windows plugin/moderation baseline failures remain documented in `director-baseline-service-review.md`. Focused passing gates do not erase them.
- One ordinary-dashboard fetch timeout recovered on a read-only reload. Occasional browser click/screenshot timeouts also recovered after inspecting state. Their precise causes remain unproven; no speculative production fix was made.
- Copied users have no verified tier mapping and therefore no invented membership. For a newly mapped copied-user fixture, start=original registration and expiry=start+28days. Existing contracts are not rewritten.
- No production writes, new export, live upstream/card use, real charges/refunds, external notifications, clock changes, port18080 operations, push or deployment. No commit or archive. Trellis remains in_progress because its commit/archive workflow requires a separate authorized handoff; its local acceptance metadata and A1-A7 now record completion.
- Original architecture v1.4, OpenSpec PRD/contracts and Trellis agree. Historical V5 acceptance and all failed evidence remain intact. Product and tooling writers remain frozen; reopening code changes requires a new source/image gate and proportional revalidation.
