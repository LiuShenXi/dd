# Frontend v0.2.7 integration review

## Scope and baseline

Authoritative baseline: `2fefcf51f0e00618686ab825d81e65444ee1ccfd` in the completed v0.2.6 plus Sentinel integration history. Work is performed in `C:/WORK-SPACE/sub2api-compatible-0.2.7-20260919`.

The first `dd-update-027` attempt used an obsolete v0.2.1 custom baseline. It was stopped after the baseline correction; its source edits and dependency installation were preserved in that separate worktree and were not copied into this integration.

The actual upstream frontend delta is nine files: Seedance endpoint capabilities in account creation/edit/bulk edit, plugin status API and UI bridge, and the narrow-screen model plaza header entry. Existing custom quota, announcements identity, branding, routes, and dashboard implementation were already correctly reconciled in the completed v0.2.6 baseline and remain intact.

The only additional behavior repair is the bulk key editor's group assignment boundary: single-key editing already treats carpool groups as administrator-assigned, but the existing bulk editor offered group changes for those same keys. The bulk editor now preserves the selected group's metadata, disables group changes for selections containing carpool keys, filters carpool groups from available targets, and validates targets against the filtered list. Other bulk fields and failed-key retry behavior remain available. Two component regressions cover mixed selections and stale target values. English and Chinese copy explain the restriction.

No routes, brand assets, carpool financial logic, modal reset behavior, or production settings are changed by this repair.

## Verification

| Command | Result | Log |
| --- | --- | --- |
| `pnpm install --frozen-lockfile` | Passed, exit 0; lockfile unchanged | `frontend-install.log` |
| `pnpm typecheck` | Passed, exit 0 | `frontend-typecheck.log` |
| `pnpm lint:check` | Passed, exit 0 | `frontend-lint.log` |
| `pnpm test:run --maxWorkers=4 --minWorkers=1` | Passed, exit 0; 310 files, 2365 tests | `frontend-tests.log` |
| `pnpm build` | Passed, exit 0; i18n check, vue-tsc, Vite all passed | `frontend-build.log` |

The first default-concurrency Vitest run and build i18n subprocess were deliberately stopped to reduce contention with concurrent Go checks. Their logs are preserved as `frontend-tests-interrupted.log` and `frontend-build-interrupted.log`; these were not accepted as successful runs. The complete rerun limits workers to four, and the build uses process-local `VITEST_MAX_FORKS=4`, `VITEST_MIN_FORKS=1`, `VITEST_MAX_THREADS=4`, `VITEST_MIN_THREADS=1` (supported by the installed Vitest version). No tests or build stages are skipped.

Key preserved coverage passed: CarpoolView asynchronous modals (22), CarpoolTermModal (20), CarpoolDetailsView (23), carpool store (8), dashboard carpool presentation (11), header carpool presentation (6), announcement mark-all identity/partial outcomes (5), and BulkEditKeysModal (13, including the two new compatibility regressions). The complete suite also includes announcement identity/version ordering and the upstream Seedance capability cases.

The build produced its configured embedded assets under `backend/internal/web/dist`; these are generated, ignored output, not edited backend source. Warnings were limited to the existing stale Browserslist dataset and large output chunks, plus expected negative-path test stderr. No dependency upgrades were performed. `git diff --check HEAD -- frontend` passed.

## Source review

- `App.vue`, `AppSidebar.vue`, `UserDashboardStats.vue`, carpool state and views are unchanged from the completed baseline.
- `AppHeader.vue` changes only the model plaza entry; separate quota/money rendering remains intact.
- `announcements.ts` retains request sequence, identity generation, partial mark-read success, and stale failure guards, including the previous regression coverage.
- `KeysView.vue` retains carpool group filtering, provider counts from selectable groups, and administrator-assigned read-only group rendering.
- `utils/branding.ts` still defines the default site name as `水上列车`; custom logo assets and route entries remain in place.
- Seedance is opt-in: the absent/empty capability fallback remains chat completions and embeddings, while explicit two-capability sets containing Seedance are serialized.
- Plugin status retains existing bridge session/source/origin validation and request deduplication; the new read-only status request does not use the step-up mutation path.

## Limits

Unit/component tests and a production build cannot establish screenshot or deployed-runtime acceptance. This task does not deploy or change production data.
