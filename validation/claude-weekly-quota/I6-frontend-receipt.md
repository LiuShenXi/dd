# I6 controlled-conversion frontend receipt

Date: 2026-10-20 (Asia/Shanghai). Branch: `codex/claude-weekly-quota`.
Baseline: `203047eda`. This receipt covers local frontend source and automated
checks. Actual PostgreSQL conversion, replica/lease enforcement, final-image
browser acceptance and deployment are separate gates owned by the coordinator
and backend/integration owners.

## Implemented boundary

- The admin group table exposes Claude conversion for Anthropic windows groups
  and converted groups still in `quota_maintenance`. GPT and normally active
  term groups do not receive the action. Maintenance status has a readable label.
- The new modal displays authoritative group mode/revision/check time, protected
  pending request count, unfinished order count, current Key owners, and replica
  heartbeat/capability/receipt/lease/default-concurrency/feature/roster evidence.
  Known blocker codes are localized in Chinese and English; unknown codes are
  preserved. A blocked preview cannot authorize conversion.
- Current Key ownership is a review list only. No user or allowance is inferred
  from keys, existing balances, plans, or an earlier server preview. Adding a
  current user fills only their ID; the allowance remains empty. Administrators
  explicitly enter unique safe positive IDs and strict positive decimal-string
  allowances. Quotas never pass through JavaScript Number arithmetic.
- Zero-card conversion is represented by an explicit `allocations: []` payload,
  with a warning and the same explicit review checkbox as populated conversion.
- Maintenance entry and release require an audit reason. Preview shows the
  server's exact allocation set and fixed 168-hour duration. Conversion sends
  the copied allocation payload, revision and token from the same preview.
  Editing allocations invalidates preview, confirmation and operation identity.
- Identical failed-network retries preserve their original payload and operation
  key; changed requests receive a new key. A revision conflict preserves input,
  discards the old preview, refreshes server state and requires a new preview.
- Group/account/modal identity guards protect success, error and finally paths.
  The shared mutation mutex survives close/reopen while a request is in flight;
  reopening rereads authoritative state after the original operation settles.
- Conversion leaves the group in maintenance. Successful conversion displays
  saved allocations and returned card intervals. Releasing maintenance is a
  separate explicit operation using the latest revision; no automatic release
  or additional allocation inference is performed.
- Native labels, buttons, fieldsets, checkbox confirmation, error/status roles,
  wrapping layouts and Chinese/English copy are included. Actual keyboard and
  narrow-screen acceptance still requires the browser gate below.

## Verified API contract

Checked against `backend/internal/server/routes/admin.go`, exported rollout
domain types and admin handler/service inputs:

- `GET /api/v1/admin/groups/:id/subscription-quota/rollout`
- `POST .../rollout/maintenance` with `{enabled, expected_revision, reason}` and
  `Idempotency-Key`.
- `POST .../rollout/preview` with explicit `{allocations}`.
- `POST .../rollout/convert` with `{allocations, expected_revision,
  preview_token}` and `Idempotency-Key`.
- Capability, receipt and lease protocol versions are numbers. Nullable lists
  normalize only at the client API boundary, including nested replica lists.
  Request failures propagate unchanged. The backend is authoritative for final
  allocation validity, state freshness and safe maintenance release.

## Executed verification

- `pnpm lint:check`: passed, exit 0.
- `pnpm typecheck`: passed, exit 0.
- `pnpm test:run`: **327 files, 2485 tests passed**, exit 0; duration **44.61s**.
- `pnpm build`: passed, exit 0; locale completeness **3/3**, `vue-tsc -b`,
  and Vite production build **17.38s**. Existing Browserslist age/chunk-size
  warnings remain; no warning was suppressed.
- New rollout modal suite: **15 tests**, covering no inferred grants, explicit
  empty allocations, strict amounts/IDs, exact retries, changed-intent preview,
  separate release, both locales and unknown blockers, stale conflicts,
  delayed old success/failure after group changes, and close/reopen mutation lock.
- New API suite: **2 tests**, covering real route strings, exact money/IDs,
  revision/token/key transport, explicit empty arrays, nullable normalization and
  failure propagation. Group view suite: **8 tests**, including Claude-only
  action visibility and correct selected group. Locale completeness: **3 tests**.
- `git diff --check -- frontend`: passed.

Local full logs: `/tmp/dd-i6-frontend-tests.log` and
`/tmp/dd-i6-frontend-build.log`. They are temporary logs, not release artifacts.
No backend source, production database, credentials, deployment, commit or push
was changed by this frontend owner.

## Remaining final-image browser gate (owned by coordinator)

Use the exact rebuilt combined candidate at desktop and 390px width. Verify
keyboard operation, long user/replica names, no horizontal overflow, readable
blocker counts and separate maintenance/conversion/release controls. Exercise
the real maintenance → explicit user/amount preview → conversion → explicit
release sequence on authorized test groups, including an explicit zero-card
set, stale preview, protected pending/unfinished order/replica mismatch blockers,
and a held response across close/reopen. Verify GPT has no conversion action.
Do not infer production recipients or amounts from these test fixtures.
DOM and unit tests do not establish this browser or real PostgreSQL acceptance.

## Follow-up: historical terminal-order boundary

After the independent review, the coordinator retained the T53 manual-review
boundary for late payment of historical orders. The backend's authoritative
`legacy_terminal_orders: number` counts orders in this group with no entitlement
snapshot and status `CANCELLED` or `EXPIRED`; it is separate from pending-order
blockers. The frontend DTO and fixture now include this field.

When the count is positive, state review and the current conversion preview
display its count and a Chinese/English warning: later payment requires manual
review, and the system will not infer an allowance or automatically issue a
card. Zero count shows no warning. The warning does not disable a valid explicit
allocation set or populate one. Tests prove an explicitly reviewed empty set
still submits `[]` despite historical terminal orders.

Follow-up verification: targeted ESLint passed; `pnpm typecheck` passed; four
focused suites passed **30 tests** (rollout modal **17**, API **2**, group view
**8**, locale completeness **3**) in **4.89s**. `git diff --check -- frontend`
passed. The preceding full-suite result above remains the last full-suite run;
this small warning addition was checked with focused regressions as requested.
Final copy avoids claiming every historical order is non-blocking: a recently
expired order may also belong to the server's pending grace window. The warning
itself does not add a blocker; the server's existing blockers remain authoritative.
After that wording refinement, the 17-test modal suite and targeted locale lint
passed again. Final `pnpm build` passed (locale **3/3**, `vue-tsc -b`, Vite
**17.43s**); log: `/tmp/dd-i6-frontend-terminal-warning-build.log`. Frontend source
is frozen after this build, pending the coordinator's final-image browser review.

## Follow-up: duplicate runtime replica identities

Coordinator review found that the backend intentionally returns multiple live
instances with the same `replica_id` when reporting `REPLICA_DUPLICATE`. The
replica list now keys each displayed occurrence by `replica_id` plus list index,
so duplicate identities cannot reuse a Vue list key. The added component test
shows both instances with different heartbeat minutes and default concurrency
values, retains the duplicate blocker after preview, and rejects conversion.

Targeted ESLint passed; the rollout modal suite passed **18 tests** (**1.49s**).
Final `pnpm build` passed, including locale completeness **3/3**, `vue-tsc -b`
and Vite **17.37s**. `git diff --check -- frontend` passed. Build log:
`/tmp/dd-i6-frontend-replica-key-build.log`. Frontend is frozen again after this
correction, awaiting the exact-image browser gate.

## Follow-up: isolated browser fixture (2026-10-20)

The coordinator reviewed and authorized `office-browser-I6.py`. Syntax/help,
private-file mode, SQL target isolation, exact callback amount, and rejection of
foreign users/plans/providers passed isolated helper checks. Preparation then
succeeded on its first run. Persistent foreground exec session **79015** keeps
the existing native loopback bridge alive at **http://127.0.0.1:59645**.

The browser application uses the exact final image
`sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483`
and source `2ceb119eb44d2d8d9ba68e37e0cfe4646b51cc067628cc34175734607daaa55d`.
A consistent copy of the synthetic database was restored as
`cwq_browser_i6_4d8579907c00`; only the copied replica registry was cleared.
Separate Redis and payment-provider containers prevent shared Redis pause or
Pub/Sub effects. The main applications, main database, and main Redis were not
modified. No production resources, product code, build, commit or push changed.

Actual HTTP preparation created buyer/manual users **7/8**, term group **7**,
conversion group **8**, sale plan **1** (sale USD **12.34**, allowance USD
**125.12345678**) and unsold manual plan **2** (default USD **300.00000000**).
The conversion group is maintained, declares only `browser-1`, and returned an
explicit empty-allocation preview with no blockers and **1** historical terminal
order. No conversion was pre-applied. Sample order **2** was created through the
regular purchase API and completed by a valid signed synthetic provider webhook;
its frozen quota snapshot stayed **125.12345678**.

Public provenance/IDs are in `office-I6-browser-environment.json`; credentials,
raw responses and diagnostics are only in the task's private browser directory.
`office-browser-I6-instructions.md` gives UI paths, purchase callback command,
manual default/override and conversion review steps. The frontend owner did not
open CUA. Final desktop/narrow-browser interaction acceptance remains with the
coordinator; frontend source stays frozen.

## CUA follow-up: plan duration contract defect

Coordinator CUA found the synthetic plans displaying `payment.admin.day`.
The fixture source now uses the existing form/locale value `days`. An attempted
actual admin `PUT /api/v1/admin/payment/plans/1` with only
`{"validity_unit":"days"}` returned HTTP 400 / `TERM_PLAN_DURATION_INVALID`
("Claude quota plans must have exactly seven days"). Neither plan was modified
by that failed request, and no order/snapshot mutation was requested.

This is a real cross-layer defect in candidate `feee80...`, not merely a fixture
rendering problem: `PlanEditDialog.vue` submits `days` through the unchanged API
wrapper, while `validatePlanQuota` in `payment_config_plans.go` accepts only
`day`. Product source was left unchanged by this fixture correction. The
coordinator/backend owner were notified; correction and candidate revalidation
are required before declaring final browser acceptance. Private failure evidence
is retained in `validity-unit-correction-failure.log` and the adjacent HTTP log.

## F2 browser upgrade and duration retest

The backend owner fixed the `day`/`days` contract and the coordinator supplied
successful `office-I6-build-F2.json`. The explicit `upgrade` command passed
syntax/help checks, validated the exact image/source labels and build receipt,
and replaced only the browser application on 2026-10-20. No SQL was performed
by the upgrade. The old browser container is stopped and retained; the main
runtime, independent browser database, Redis and provider were preserved.

Current browser image:
`sha256:3e18edda6b675cb73d108c92acf139dfe1f6e45b92e5abc8aef1594d2eec39f3`;
source: `56f189a7e2c4c32a96ea439d15687e809f9839670f63181e5312851642b757a4`.
The unchanged URL is **http://127.0.0.1:59645**, new bridge PID **1637**, and
persistent exec session **98126**. The previous browser heartbeat expired
naturally, leaving one `browser-1`; the existing completed sample order's status
and frozen snapshot were checked unchanged after restart.

On F2, actual admin PUTs of only `validity_unit: "days"` succeeded for synthetic
plans **1/2**. Every other returned plan field was equal before/after (excluding
`updated_at`), and sample order **2** retained its completed frozen snapshot.
These requests finished just before the coordinator's message reserving sale
plan 1 for CUA arrived; this timing was reported immediately. No further plan
API mutations will be performed by this owner during the coordinator's CUA
flow. Private evidence: `validity-unit-correction-F2.json` and adjacent HTTP logs.

Coordinator-reported CUA evidence on the preceding image: user **8** / group **7**
received the plan-2 default **300.00000000**, then a scheduled renewal with
explicit override **75.12345678** saved using keyboard Return. The first card
remained **300**, the next card showed **75.12345678**, with consecutive exact
7-day periods. Screenshot: `office-browser-screenshots/i5-manual-default-override.png`.
This frontend owner records the coordinator's observations and does not operate
or share control of its CUA browser. F2 plan/purchase and final conversion/browser
results are tracked separately in `office-I6-browser-receipt.json` as they arrive.

## F3 targeted frontend corrections and source freeze

CUA exposed two additional display defects. The admin plan duration cell built
a locale key directly from historical `day`; it now uses the existing fixed
168-hour translation for term rows and `planValiditySuffix` for windows rows,
including singular/plural day/week/month values. No plan or card amount is
derived from this display change.

The signed resume-token API was typed as the minimal anonymous order lookup.
It now uses a separate accurate `PublicOrderResult`, retaining the immutable
quota snapshot and excluding user/provider identities. The backend owner owns
the corresponding response DTO fix and signed-token handler tests. The payment
result view now calls a term purchase's base amount its sale price and offers
"Back to plans" / "返回套餐". Existing balance result wording and the intentionally
minimal anonymous lookup stay intact.

The component regression exercises the signed-resume branch without a local
recovery snapshot, with an edited/unlisted plan offering 999, and confirms the
result displays the purchased **125.12345678 USD / 168 hours**, never reads the
current plan, and shows neither balance credit nor recharge wording. The admin
view regression uses both term `day`/`days` and the actual existing formatter for
windows day/week/month variants.

Verification: targeted ESLint passed; `pnpm typecheck` passed; **5 focused suites /
29 tests** passed (result view 15, admin plan view 2, validity formatter 6, payment
API 3, locale completeness 3). `pnpm build` passed, including locale checks and
`vue-tsc -b`, with Vite **17.75s**; log `/tmp/dd-f3-frontend-build.log`.
`git diff --check -- frontend` passed. Frontend source/dist are frozen for the
coordinator's F3 build. No extra production image or backend code was built or
changed by this owner.

Browser receipt separation is explicit: F1 rollout/manual observations and F2
purchase/plan-change/signed-callback evidence are retained for their exact
candidates; F2 completed-result rendering is not marked passed. F3 requires
actual admin-duration and signed-resume-result CUA checks. The unchanged rollout
and manual issuance interfaces are not claimed to have been fully rerun on F3.

## F3 browser application upgrade

The coordinator supplied successful `office-I6-build-F3.json` and authorized the
existing upgrade helper. It replaced only the browser app and bridge, retaining
the old stopped container and the same local URL. Current image is
`sha256:3c491e7a7033a72206fa30cf8d4895a7678908a6781912a66e16cedcbbe435bf`,
source `d6ac92adeb97249c1a792e2586c358f63da20790283c5481fee6baafa8cae1b8`.
Persistent bridge exec session is **13569**, PID **6022**; URL remains
**http://127.0.0.1:59645**. Product source remained frozen.

Actual pre/post-upgrade API comparisons confirm order **3** is still COMPLETED
with the same full **125.12345678** snapshot. Sale plan **1** is unchanged at
**999.00000000**, not for sale. User **8** / group **7** still has exactly the
**300.00000000** and **75.12345678** cards with the same IDs, time intervals and
revisions. No upgrade SQL, main-runtime mutation, Redis replacement or provider
replacement occurred. `office-I6-browser-receipt.json` records this separately
from the pending coordinator CUA checks of the F3 changed pages.

## F4 actual routed admin orders correction and freeze

Coordinator CUA found that `/admin/orders` mounts `AdminOrdersView.vue` with the
shared `OrderTable`, rather than the previously changed standalone
`AdminOrderTable` / `AdminOrderDetail` components. Their earlier tests did not
establish coverage of this live route. The actual view now hides refund,
approve-refund, retry-refund, and query-refund actions for snapshot cards and
shows the existing manual refund guidance. Its actual detail dialog renders the
immutable snapshot and labels the base amount as sale price. Pending-order
cancellation and failed-fulfillment retry remain available; the latter is not a
refund operation and is required for paid issuance recovery.

The actual open/submit/query handlers reject snapshot cards, submission rejects
closed or already-submitting dialogs, and the refund target is separate from
order detail selection. A late or closed detail response cannot retarget a
refund. The targeted test mounts the real routed view, shared order table and
quota summary; it covers all five legacy refund-action statuses for term and
windows orders, frozen details, stale buttons/submission, closed-dialog events,
late detail responses, and preserved non-refund actions. **18 view tests + 4
existing refund-dialog tests passed (2 suites / 22 tests).** Targeted ESLint and
`vue-tsc --noEmit` passed. `pnpm build` passed (locale checks, `vue-tsc -b`, Vite
**18.48s**), log `/tmp/dd-f4-frontend-build.log`. `git diff --check -- frontend`
passed. F4 product changes are limited to `AdminOrdersView.vue`; the backend,
rollout/manual and all other payment views remain frozen.

Read-only route audit traced `/purchase` to `PaymentView` and its actual
`SubscriptionPlanCard` / `PaymentStatusPanel` (create-response snapshot, then
order polling hydration), `/orders` to `UserOrdersView` and the shared
`OrderTable` (snapshot display plus refund eligibility guard), `/payment/result`
to the corrected signed-resume view, and `/admin/orders/plans` to the actual
plan view and `PlanEditDialog`. No second unused-component substitution was
found in these entry points. The legacy standalone `/payment/qrcode` still has
no snapshot summary, but source search found only its router definition and no
purchase-flow reference; it was not changed or claimed as newly accepted.

Coordinator reports F3 real result pages passed in English and Chinese at
390px (document width also 390), showing purchased **125.12345678 / 168 hours**
while current plan is **999 and unlisted**. Back to plans works with Return and
the unlisted plan is absent. The actual F3 plan table shows two fixed **168h**
rows, quota **999 / 300**, both unlisted. Screenshot hashes and candidate identity
are in `office-I6-browser-receipt.json`. These F3 observations are retained for
their scope; F4 actual admin list/detail CUA is pending the coordinator's build.
No full runtime/rollback/long-stream gate is claimed repeated for F4.

## Final scoped browser acceptance — F4 admin route passed

Coordinator CUA completed on F4 R2 image
`sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`,
source `8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`
(`office-I6-build-F4.json`). On the actual refreshed `/admin/orders`, both
COMPLETED snapshot rows show only View and the manual-refund explanation. No
legacy refund button remains. Actual order **3** detail shows the original
**125.12345678 USD / 168h**, sale price **$12.34**, and paid **¥86.38** while the
current plan remains **999 and unlisted**.

Chinese desktop, Chinese 390px and English 390px layouts passed; narrow document
scroll width equals the **390px** viewport. Escape closes the dialog, Return on
View opens it, and initial focus lands on Close. The recommended final capture
is `office-browser-screenshots/i5-F4-admin-detail-zh-viewport.png`; the separate
`i5-F4-admin-detail-zh-390.png` is a full-page capture including the background.
The F3 missing-detail-summary screenshot is retained as historical failed
evidence. Every screenshot has its candidate identity and SHA-256 in
`office-I6-browser-receipt.json`.

Only COMPLETED orders were exercised in the final F4 browser check. Other refund
statuses, stale submit/action/detail races, preserved FAILED issuance retry and
windows compatibility are covered by the **18 actual-view + 4 dialog tests**, not
represented as browser observations. Earlier F1 rollout/manual, F2 creation and
frozen callback/user orders, and F3 plan duration/signed-result acceptance remain
attributed to those candidates. Full runtime/rollback/long-stream gates were not
repeated for this final frontend-only admin-route change.

The earlier pending-browser notes are historical and superseded by the scoped
acceptance above and the consolidated browser receipt. Product source remains
frozen; this finalization changed documentation only. Coordinator owns private
diagnostic preservation and synthetic container/database cleanup; the recorded
loopback URL is an acceptance-time address, not a promise of an ongoing service.
