# Execution Plan

## Two-Boost Default Follow-up

- [x] Reuse boost_count as a2-or3 plan parameter; default new plans to2 without altering old contracts.
- [x] Enable the existing admin field and cover changed submission.
- [x] Verify migration replay, concurrent two-boost exhaustion, old-three entitlement and renewal in fresh PostgreSQL.
- [x] Update the local image and verify administrator plan projection, retaining user testing data.

Evidence: `evidence/director-two-boost-acceptance.md`. Original copied records pass conservation; the historical synthetic baseline is explicitly no longer equal after the pre-existing22:00 scheduled reset executed. No baseline overwrite or rollback.

## Timeline-Only Visual Follow-up

- [x] Record newest scope: widget only, timestamps above, no red/numbered badge design.
- [x] Implement refined timeline and collision-safe paired annotations, preserving surrounding UI and business data.
- [x] Independently review focused tests/diff and actual desktop/mobile visual output.
- [x] Rebuild local image safely, verify preserved data and complete U1-U3 evidence.

Final visual evidence: `evidence/director-timeline-ui-final-acceptance.md`. Includes dense-history and skewed desktop packing regressions,13 focused/1934 total frontend tests, final-image desktop/mobile captures and conservation against the original baselines.

Design authority: `research/timeline-ui-redesign.md`. The v1.4 evidence below is preserved history, not visual acceptance of this follow-up.

## Current v1.4 Revision

- [x] Inspect reported balance defect and existing runtime/accounting boundaries.
- [x] Update PRD, original architecture, OpenSpec and agreed user DTOs; task context manifests validate with two real entries each.
- [x] Backend: append plan upgrade, snapshot-aware four-cycle generation, three boosts, quota and actual reset-event projections, regressions.
- [x] Frontend: shared carpool balance display/refresh, four-cycle defaults, reset markers/copy/times, localized responsive tests.
- [x] Validation: adapt fresh synthetic acceptance to new rules; verify migration conservation and legacy compatibility.
- [x] Director: independently review diffs; run focused Go/real-PG and frontend gates; review new source/runtime identities.
- [x] Rebuild local-only image and restart the owned app/bridge recoverably; verify HTTP flows and desktop/mobile screenshots, including quota after boost and actual reset-event projection.
- [x] Record v1.4 evidence and deliver URL without commit, archive, push or deployment.

Current local acceptance is recorded in `evidence/director-v14-final-acceptance.md`; all seven PRD criteria are evidence-backed. Preserve the immutable code-ready gate and historical V5/failed reports. Service remains at http://127.0.0.1:38088; task status stays in_progress for separate commit/archive handling.

Current ownership and DTO contract are in task design.md. Existing V5 acceptance below is historical and cannot complete the new checklist. Copied-user fixture expiry for newly created memberships is now registration+28days, not30; already-created contracts remain immutable. Preserve existing synthetic reporter/history and pending reset batch; no clock changes in the copied runtime.

## Historical v1.3 Execution

This is adoption of approved work, not a new implementation. Preserve the two existing workers, OpenSpec artifacts, source branch, and all code. The full feature/evidence checklist remains `openspec/changes/add-carpool-v1-3/tasks.md`.

- [x] Read approved architecture and latest precise reset algorithm.
- [x] Preserve independent worktree and register existing core/gateway ownership.
- [x] Create Trellis PRD/design/implementation plan and explicit role model settings.
- [x] Validate manifests and activate the same task in the director session (session:codex_01a0722c-e17a-7092-8abd-d7776b150f1b; start/current/validate all exited 0).
- [x] Send existing workers the task path and context; core and gateway both explicitly confirmed complete task/manifest/research/spec reads and unchanged file ownership after director activation.
- [x] Finish core and gateway contracts and integrated billing implementation.
- [x] Complete frontend and reset/announcement packages against agreed contracts; V5 fixed-enum presentation correction passed real-page verification.
- [x] Source coordinator exports authorized production database read-only, restores to fresh restricted local storage, and disables external effects before app startup.
- [x] Run focused tests, migrations, and independent cross-owner review; return fixes to their owners.
- [x] Start an isolated local backend/frontend and verify source requirements through real API/UI workflows with safe test identities; V5 image UI and final post-UI conservation passed.
- [x] Director inspects actual diffs, builds/tests, screenshots and accounting invariants; record evidence and unresolved risks in evidence/director-final-acceptance.md. Additive lifecycle PG and full1911-test frontend results close the last coverage gaps without changing product bytes.
- [x] Deliver local URL, worktree/baseline and verification summary without push or production deployment. Commit/archive require separate user follow-through; task remains in_progress.

## Shared Validation Commands

Backend commands run from backend with a compatible Go toolchain: `go build ./...`, `go test -tags=unit ./...`, `go test -tags=integration ./...`, and relevant `golangci-lint` checks. Start with affected packages. The director verified a cached Go 1.27.0 toolchain; do not weaken go.mod for the older host Go. Only the coordinated owner runs Ent generation.

Frontend commands run from frontend: `pnpm install --frozen-lockfile`, `pnpm typecheck`, `pnpm lint:check`, `pnpm test:run`, `pnpm build`. Avoid unscoped autofixing lint during other owners' edits.

Real tests must include current architecture section 12 requirements: new 28-day/four-cycle/three-boost terms, immutable legacy five-cycle/two-boost contracts, concurrency/idempotency, permissions, cross-cycle HTTP/WS identity, retained debt/history, exactly 48h and seconds-sensitive 22:00 scheduling, incomplete observations, restart/recovery, announcement correction and audience, authoritative quota and successful-reset timeline, and desktop/narrow-screen interactions. Capture commands and outputs, not claims from workers. Never treat skipped or blocked checks as passed.

## Historical v1.3 Local Copy Fixture Rule (Superseded for New Terms)

The following 30-day rule records the prior iteration only. The current PRD and design require newly created copied-user fixtures to start at registration and expire after 28 days with four cycles; existing contracts are not rewritten.

Only for memberships created to test copied users: `starts_at = users.created_at` and `expires_at = starts_at + 30*24h`, with cycles 7/7/7/7/2. Preserve historical/future instants. Users older than 30 days remain expired; never silently substitute now, auto-renew, or issue missed historical cycles. Source coordinator preserves registration timestamps, ledger associations and numeric values during sanitization. The director assigns local API/fixture generation after functionality and isolation gates are ready.

Choose a copied user's plan only from an existing confirmed association or clearly verifiable mapping. Never infer a tier from the current balance. Unmapped users stay explicitly pending mapping and receive no invented membership. Separate synthetic users cover all three plans and current/future/expired/cross-cycle scenarios. This fixture rule does not alter production data or the product's administrator-specified start time / null-start transaction-now behavior. Do not put copied user identities or records in evidence, screenshots or prompts.

Trellis process checks: `python .trellis/scripts/task.py validate .trellis/tasks/09-05-sub2api-carpool-v1-3`, `task.py start`, and `task.py current --source`. Use real curated manifest entries, not allow-empty-context. Generated empty guideline templates are not validated project conventions; use the curated research and actual target source, then fill relevant source-backed specs as work progresses.
