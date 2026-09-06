# Rules28 Independent Check

Date: 2026-09-06
Scope: frontend source review and gates, plus read-only confirmation of the backend fixes raised during review.

## Findings (fixed)

- File: `frontend/src/components/admin/user/CarpoolTermModal.vue`
  - Issue: takeover validation and the numeric input capped `boost_used` at the legacy value `2`, so a valid v1.4 term could not import all three consumed boosts.
  - Fix: derive the maximum from the selected plan snapshot, require an integer in range, and cover `boost_used: 3` in the modal test.
- File: `frontend/src/stores/carpool.ts`, `frontend/src/App.vue`, `frontend/src/views/user/CarpoolDetailsView.vue`
  - Issue: the store prevented stale response writes, but callers still applied side effects from stale successful responses and could not distinguish a current request failure from an older failure.
  - Fix: add exact current-response and current-error identity checks and guard announcement polling, boundary timers, and visible error side effects. Deterministic overlap and account-switch tests cover the behavior.
- File: `frontend/src/types/carpool.ts`, `frontend/src/views/user/__tests__/CarpoolDetailsView.spec.ts`
  - Issue: the frontend DTO still admitted the removed `current_30_day_term` basis and a legacy test derived five-cycle behavior from that obsolete discriminator.
  - Fix: narrow the DTO to `current_term`; legacy rendering now follows immutable cycle timestamps and count.
- File: `backend/internal/service/carpool_service.go`, `backend/internal/repository/carpool_repository.go`
  - Issue: preview accepted request shapes that open would reject; renewal replay and tier selection needed to remain stable when plan versions changed.
  - Fix: preview now rejects takeover data in new mode, negative historical usage, and out-of-range boost counts. Renewal operation replay occurs before plan lookup and explicit renewal plans must match the source term tier. Backend ownership supplied the corresponding tests; this reviewer confirmed the final code paths read-only.

## Findings (not fixed)

None in the reviewed frontend scope.

Backend PostgreSQL, browser, copied-runtime, and deployment acceptance are intentionally outside this reviewer's ownership and are not represented as passed here.

Director follow-up: the reported explicit same-tier renewal enforcement was a regression, not an accepted fix. The API defaults to the same tier only when plan_id is omitted; explicit administrator tier changes remain supported and are covered by the preserved four-seat-to-three-seat HTTP test. Backend owner was instructed to remove that restriction and verify explicit upgrade, omitted-plan default and stable replay separately before the runtime gate.

## Verification

- TypeCheck: pass
  - Command: `pnpm typecheck`
  - Exit: 0
  - Log: `evidence/rules28-frontend-typecheck.log`
- Lint: pass
  - Command: `pnpm lint:check`
  - Exit: 0
  - Log: `evidence/rules28-frontend-lint.log`
- Tests: pass
  - Command: `pnpm test:run`
  - Exit: 0
  - Result: 267 files, 1930 tests passed
  - Log: `evidence/rules28-frontend-tests.log`
- Build: pass
  - Command: `pnpm build`
  - Exit: 0
  - Result: Vite built 1051 modules in 31.95 seconds; existing chunk-size and mixed-import warnings remain non-fatal.
  - Log: `evidence/rules28-frontend-build.log`
- Diff hygiene: pass
  - Command: `git diff --check -- frontend/src/App.vue frontend/src/stores/carpool.ts frontend/src/views/user/CarpoolDetailsView.vue frontend/src/components/admin/user/CarpoolTermModal.vue frontend/src/types/carpool.ts frontend/src/stores/__tests__/carpool.spec.ts frontend/src/components/admin/user/__tests__/CarpoolTermModal.spec.ts frontend/src/views/user/__tests__/CarpoolDetailsView.spec.ts`
  - Exit: 0

The full test run emitted pre-existing expected stderr from negative-path tests and the build emitted existing bundle warnings; neither gate failed.
