# I5 frontend implementation receipt

Date: 2026-10-20 (Asia/Shanghai). Branch: `codex/claude-weekly-quota`.
Baseline: `203047eda`. This receipt covers local source and frontend checks;
it does not claim final-image browser acceptance, payment-provider acceptance,
production conversion, or deployment.

## Boundary and implementation

- Plan configuration derives term mode from its selected group. Term allowance
  uses strict positive decimal strings and the existing BigInt-based quota
  validation/formatting utilities. No floating-point conversion of allowance
  is introduced. Term payloads use exactly `7` / `days`; sale price, currency
  conversion, and native windows validity remain separate. Invalid term
  allowance is rejected before submission. Existing default values load
  without precision loss, including `123.45678901`.
- The admin plan table exposes the default USD allowance. Public plan cards and
  purchase confirmation show fixed 168-hour validity and the allowance instead
  of interpreting missing native day/week/month limits as unlimited. Invalid
  term defaults disable purchase. Native windows cards retain their old fields.
- Manual open/renew offers valid plans for the selected group, including plans
  not for sale. Selection copies the default once; explicit overrides, reverting
  to the selected default, and manual-only issuance are supported. Payloads
  retain `plan_id` and the exact allowance. Failed plan loading does not block
  manual entry. Plan/form changes invalidate preview and operation intent;
  exact failed-network retries retain the same copied payload and operation key.
- Order/create-response types expose the backend's versioned
  `subscription_quota_snapshot`. A shared display consumes only the immutable
  order snapshot in payment progress/results, user order history, QR success,
  and admin order details. Purchase submission sends the plan identity and sale
  amount, never a client-authored allowance. A component test deliberately
  differs the selected plan's allowance from the returned order snapshot and
  verifies the returned snapshot is displayed.
- Term orders no longer describe the payment amount as credited wallet balance.
  Their legacy automatic-refund actions are hidden; the admin detail explains
  separate cash-refund/card-cancellation handling. Backend enforcement remains
  authoritative. Legacy orders with no snapshot retain prior behavior.
- Chinese/English strings, labels, keyboard-native plan selection, fixed-duration
  copy, narrow-screen wrapping, and explicit error roles are included. Plan
  editor save callbacks guard close/reopen and replacement identity.

## Regression found while implementing

Directly replacing a user in the existing manual modal could queue `groupId`'s
reset watcher after the next group lookup had captured its identity, invalidating
that new lookup and leaving loading stuck. Scope reset now runs synchronously.
The source-backed frontend state spec records this invariant. Tests cover direct
user replacement, delayed old plan responses, existing close/reopen late success
and failure, exact-intent retries, revision conflicts, and the existing mutation
mutex. No GPT carpool behavior was changed.

## Executed verification

- `pnpm typecheck`: passed, exit 0.
- `pnpm lint:check`: passed, exit 0 (final rerun recorded by the implementing agent).
- Focused suites exercised plan editing, manual term modal, public plan card,
  purchase confirmation, snapshot rendering, order currency display, existing
  payment result/QR/status flows, validity, payment API, and locale completeness.
  Initial test-only mock/selector issues were repaired; the direct user-switch
  production race above was also repaired and retested.
- Final `pnpm test:run`: **325 files, 2467 tests passed**, exit 0; duration 40.35s.
  This includes existing GPT/windows/car-pool and unrelated frontend regressions.
- Final `pnpm build`: passed, exit 0; locale completeness **3/3**, `vue-tsc -b`,
  and Vite production build **15.12s**. Existing Browserslist age/chunk-size
  warnings remain; no warning was suppressed.
- `git diff --check -- frontend`: passed.

Local full logs: `/tmp/dd-i5-frontend-tests.log` and
`/tmp/dd-i5-frontend-build.log`. They are temporary verification logs, not release
artifacts. No backend source, database, credentials, deployment, commit, or push
was performed by this frontend owner.

## Remaining final-image browser gate (owned by root)

- Verify the exact rebuilt candidate at desktop and 390px width: plan form,
  fixed allowance and separate sale price, public confirmation, frozen order
  details, manual default/override, readable dates, keyboard controls and no
  overflow.
- Exercise selected-plan edits/deletion against an already-created order,
  renewal scheduling, user/modal switches with held network responses, exact
  retries, and revision conflict with an override preserved.
- Verify the real fake-provider-backed purchase/callback flow together with the
  backend/PG receipt. Unit/DOM success is not substituted for this gate.

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
