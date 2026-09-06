# Timeline-Only Visual Acceptance, 2026-09-06

Status: U1-U3 accepted locally. This report supersedes only the prior timeline presentation, not historical v1.4 functional evidence. No commit, archive, push or deployment.

## Scope

Product edits are limited to `frontend/src/views/user/CarpoolDetailsView.vue` and its focused spec. The slim duration-proportional rail, exact quiet reset nodes, cycle indices, paired Shanghai timestamps/full-target annotations above, and bounded dense-history disclosure replace the rejected red numbered presentation. Header, statistics, cycle detail list, reset-window section, other pages, stores, API and business behavior remain unchanged.

Trellis PRD/design/spec, OpenSpec and the original architecture reflect the latest above-rail and widget-only requirement. Original checked v1.4 criteria and old below-rail captures remain historical evidence, not the visual authority.

## Final Gates

- Final code-ready artifact: `director-code-ready-timeline-ui-final.json`, SHA-256 `14290500B545353B87570B6C26E01773DE34F6DA815C8F94C510A2404C2CA9CB`.
- Source SHA-256: `4B79950D81E1EECCBFA5A95E2A607B322D4F97CEA9C120BCB4A52588AE5ADA01`.
- Embedded frontend SHA-256: `C2DC29D8E9D97DD72770180F355916B23CC820884F32473538C3307609F3734F`.
- Final image: `sha256:bf4602af5a9104f2ccdd3bf0a2b71596a8b62126120501d459a39ab45f868660`.
- Final full frontend suite: 267 files / 1934 tests passed, session68115,49.19s, exit0. Focused view suite13/13; scoped ESLint and typecheck passed. Full lint passed before the scoped followups; only the two scoped files changed afterward.
- Final `pnpm build`: session19382, `vue-tsc -b` and Vite build passed. Fresh backend `go build ./...` passed earlier in this visual revision; backend source remained unchanged. `git diff --check` passed.
- Existing nonfatal build/test warnings remain: Browserslist age, large chunks/mixed imports, and unrelated Vue test-stub diagnostics. No all-backend-suite-green claim.

## Independent Review And Regressions

`timeline_ui` implemented; `rules28_check` reviewed read-only; both retain gpt-5.6-sol/high. The director inspected actual screenshots and found an additional skewed-history edge issue after the first build. Both identified findings are fixed:

- Dense valid48h histories previously produced seven label lanes. Default display now retains the latest two paired annotations, all14 exact nodes, and an accessible expandable chronological list with bounded scrolling above the rail.
- Blanket overflow subtraction could push an early label negative when late events clustered. Backward constraint packing fixes this. A ResizeObserver-driven1120px regression with event days0/20/22/24/26 yields offsets0/338/536/734/932 with no overlap or overflow. Normal two-event positioning is unchanged.

Focused tests also retain no-event/pending, legacy five-cycle durations, edges, long amounts, translations and authoritative clock/refresh guards. Dense/legacy/skewed states are component-test evidence, not claims of extra database fixtures or live scheduler execution.

## Actual Browser Evidence

Same local synthetic identity: `carpool-reset-marker-v14@example.invalid`. Its existing two historical resets, Shanghai2026/09/02 and09/04 at22:00:20, both display full target700, including the zero-grant participation. No fixture or balance writes occurred during this visual revision.

- `timeline-ui-final-desktop-zh-light.png`:1440x1000 viewport, page width1440. Rail1120px atx288/y502.5. Labelsx288/486,width188; both wholly above the rail.
- `timeline-ui-final-mobile-zh-light.png`:390x844 viewport, page width390. Rail358px atx16/y734.5. Labelsx16/194,width168; both wholly above with no overlap.
- `timeline-ui-final-desktop-en-dark.png`: final image, English/dark at1440px; actual screenshot reviewed.
- `timeline-ui-final-widget-mobile-en-dark.png`: final image, English/dark at390px. Labelsx16/194,width168, no text overflow, both above rail y754.5. This is a crop of actual first-viewport pixels from `timeline-ui-final-mobile-en-dark.png`; the browser full-page capture repeats a tile below844px, so that lower capture area is not used as page-layout evidence. One capture timeout was retried successfully.
- `timeline-ui-final-widget-desktop.png` and `timeline-ui-final-widget-mobile.png` are crop-only derivatives of the final Chinese captures. No fabricated UI pixels or source mockups are used.

## Runtime And Conservation

URL: http://127.0.0.1:38088/carpool. Final health response200. Existing controlled rebuild workflow replaced only owned app/mock containers; old images and rebuild archives remain recoverable. Database, Redis, volumes, fixtures, configuration and internal-only network retained their bindings. Startup health and denied-egress gates passed.

Copied-record and announcement checks passed before/after rebuild. Original21-row hash-only synthetic baseline remains unchanged, compared through `timeline-ui-final-preserved-after.stdout.log`; its manifest was not rebound or overwritten. Marker term/cycles/ledger/targets/batches match the original five-row `timeline-ui-marker-before.stdout.log`, via `timeline-ui-final-marker-after.stdout.log`. These files remain private and contain only hashes/counts. Source/code gate was revalidated against the final runtime.

No new export/import, production operation, copied-user disclosure, boost, business write, clock change, unrelated port18080 operation or external provider action. The existing test account remains available. The task stays in_progress solely for separate commit/archive handling.
