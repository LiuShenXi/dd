# Carpool Asynchronous UI Contracts

## 1. Scope / Trigger

Apply to carpool financial/admin modals, user DTOs, renewal choices and announcement polling. Follow the existing Vue/Pinia components and the authoritative OpenSpec API contract.

## 2. Signatures

`useAnnouncementStore` exposes `fetchAnnouncements(force)`, `checkForUpdates()` and `setIdentity(userId)`. `GET /announcements/version` returns `{version, unread_count}`; list and read-state routes use the same audience rule.

`CarpoolView.vue` captures `modalGeneration`, the target ID, a request payload copy and a stable operation key before awaiting each modal action. `CarpoolTermModal.vue` guards preview/submission against changed modal identity.

## 3. Contracts

- Opening/closing/replacing a modal invalidates prior actions. Old success, failure and finally callbacks cannot close the new modal, replace its errors or clear its submitting state.
- Reuse an operation key for an exact retry; do not reuse it with edited payloads.
- Renewal selects the latest enabled version of the same tier code. A disabled tier cannot silently become another tier.
- Normalize nullable list responses at the API boundary while retaining the backend's `items: []` contract.
- Announcement generation handles account changes; request sequences handle response reordering within one session. A version is acknowledged only after its required full-list refresh succeeds.
- Logout clears identity, timers, popups and pending responses. Cycle segments remain time-based; v1.4 explicitly permits net available quota and successful reset target/time annotations, but no administrator bucket/ledger fields.

## 4. Validation & Error Matrix

| Condition | Required UI behavior |
| --- | --- |
| Old modal request resolves after a new modal opens | Ignore stale result and stale finally state |
| Version changed but list refresh fails | Do not acknowledge version; allow retry |
| Older list/version response arrives last | Ignore by request sequence |
| Latest same-tier plan disabled | No guessed renewal alternative |
| No reset qualification | No invented countdown |
| Countdown reaches zero | Refetch authoritative state; do not assume reset succeeded |

## 5. Good/Base/Bad Cases

- Good: close payment A, open payment B, then A fails; B retains its own form and state.
- Base: current request resolves while identity matches; display result and refresh affected data.
- Bad: acknowledge a new announcement version before a failing list request, hiding its correction until another version appears.

## 6. Tests Required

`CarpoolView.async.spec.ts`, `CarpoolTermModal.spec.ts` and `announcements.identity.spec.ts` must force late success/failure and account/modal transitions. Test both empty and populated lists, immutable old terms with new plan versions, and disabled same-tier choices.

Run `pnpm typecheck`, `pnpm lint:check`, `pnpm test:run` and `pnpm build`. Capture real desktop and narrow-screen interactions/screenshots against the exact rebuilt image. DOM success does not substitute for successful screenshots, and old-image captures do not accept new source.

`CarpoolDetailsView.spec.ts` must exercise the actual Pinia store with mocked API responses. Fake `performance` together with Date/setInterval/clearInterval because `useServerClock` advances from a monotonic clock. Hold the boundary fetch unresolved and assert reset_count, total_used_usd and boosts remain unchanged; only the resolved server response may replace details. Also assert cycle expiry triggers a fetch, absent qualification renders no countdown, and a reset outside term coverage remains ineligible. Advancing Date alone does not exercise the production clock.

## 7. Wrong vs Correct

Wrong: apply an awaited result to whichever modal happens to be open.

Correct, using the existing action pattern:

```ts
const requestGeneration = modalGeneration
const target = adjustmentTarget.value
await adminAPI.carpool.adjustCycle(target.id, request, requestKey)
if (requestGeneration === modalGeneration && adjustmentTarget.value?.id === target.id) {
  closeAdjustment()
  await loadActive()
}
```

Apply equivalent guards in catch/finally; success-only protection is incomplete.

## Fixed Enum Presentation

CarpoolTermModal preview initial_action and CarpoolView ledger event_type/bucket are protocol values, not display copy. Translate known values through separate Chinese/English namespaces, retaining the exact unknown value as a forward-compatible fallback. Keep free-form audit reason/reference/notes and stored plan/group names unchanged. Never translate request payloads or stored ledger values.

Tests must exercise both locales and unknown values in real component rendering, and verify that translated labels do not alter submitted enum values. Wrong: display cycle_initial or scheduled directly as Chinese UI action text, or translate arbitrary operator remarks. Correct: translate known fixed enums only and preserve audit data. Verify populated ledger, current four-cycle preview and legacy five-cycle details on the exact rebuilt image, not only mocked-key assertions.

## v1.4 Quota and Reset Projection

### 1. Scope / Trigger

Applies to shared user-header/dashboard balances, carpool details, boost refresh and time-bar annotations.

### 2. Signatures

`GET /user/carpool/details` returns `quota: null | {available_usd: string}` and `term.reset_events: Array<{cycle_no: number, occurred_at: string, target_quota_usd: string}>`; basis is `current_term`. The Pinia carpool store owns this projection; `auth.user.balance` remains ordinary money. Callers use `isCurrentDetails(result)` and `isCurrentDetailsError(error)` before applying response-derived local side effects.

### 3. Contracts

For active covered cycles, label/display the server quota in dashboard/header, including zero. Do not sum with ordinary money or credit it after a boost. Refetch details after successful claims, relevant boundaries and visibility. Failures are distinct from successful null responses. Request generations and identity guards prevent stale updates. Pending/expired/missing terms retain explicit state and ordinary money is never mislabeled as carpool quota.

Ignoring stale writes inside Pinia alone is insufficient: returned responses/errors can still reach component callbacks. Guard clock synchronization, boundary keys, local errors/toasts and announcement polling with the exact current result/error identity. A shared `detailsError` boolean cannot identify which request failed: newer B may fail before stale A, including after an account switch. Starting a newer request or changing identity clears the current-error marker. Takeover boost-used bounds come from the selected plan's `boost_count`, not a hardcoded two or three.

Derive time tracks from actual cycle durations: four equal weeks for new28-day terms; legacy30-day terms retain five tracks. Markers locate actual successful `occurred_at`, target copy describes base quota, and the latest visual requirement pairs the Shanghai timestamp with that copy ABOVE the rail. Nearby labels must remain visibly linked to their actual nodes without overlap; do not use arbitrary half-width timestamp columns or copy red screenshot annotations into production styling. This redesign is limited to the timeline widget, not the surrounding page. No optimistic reset marker at countdown zero.

Annotation packing must remain inside both container edges, including skewed histories with an early event and several late events. After forward collision spacing, constrain positions right-to-left against the right edge and the next label; subtracting one overflow value from every label can push the early label negative. Exercise the measured desktop width through `ResizeObserver`, not only the narrow fallback width. Histories needing more than two annotation lanes retain every exact rail node, show the latest two annotations by default, and expose every date/target in a keyboard-accessible, height-bounded disclosure above the rail. Never grow an unbounded stack of labels for dense history.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Active carpool quota0, ordinary100 | Carpool available0, never100 |
| Older pre-boost read resolves after newer read | Ignore stale result |
| Refresh fails | Visible unavailable/stale state, no money-source substitution |
| Account switches while fetching | Prior quota/events cannot apply |
| Newer B fails before older A | Only B may trigger current-error side effects |
| Takeover of a new term with three boosts used | Preview/open use snapshot limit3; legacy forms use their actual limit |
| Zero-grant successful reset | One marker with full base target and actual timestamp |
| Pending/failed reset | No completed marker |

### 5. Good/Base/Bad Cases

Good: a70USD boost changes server net quota768 to838, and both header/dashboard display838 while ordinary balance remains0. Base: ordinary users keep their balance UI. Bad: refreshing auth.balance after a carpool claim leaves the card at0, or reading scheduled_at invents a successful reset.

### 6. Tests Required

Test real store response ordering/identity/error handling, both quota consumers and unchanged ordinary behavior. Component tests cover four-cycle and legacy-five layouts, zero/pending reset data and timestamp positioning. Real desktop/mobile screenshots on the new image must show before/after boost amounts and reset annotations without overflow; DOM assertions alone do not accept layout.

Force a newer failure before an older failure, both within one identity and after switching accounts. Assert stale caller errors cannot pass `isCurrentDetailsError`, and stale success cannot pass `isCurrentDetails` or regress clock/polling state. Takeover form tests include `boost_used=3` against a three-boost plan and overflow rejection.

### 7. Wrong vs Correct

Wrong: `displayBalance = auth.user.balance + nominalBoost` or hardcoded `7fr 7fr 7fr 7fr 2fr`.

Correct: consume `details.quota.available_usd` under its active-state contract; derive tracks from `cycle.ends_at - cycle.starts_at`; render only backend-projected successful reset events.

## Compact Boost Layout

The existing dashboard is two columns on a390px viewport. Its balance-card inner width can be only133px, so a92px nonshrinking action must wrap before squeezing the copy into single-character lines. In UserDashboardStats.vue, use a wrapping row, copy with min-w-[7rem] flex-1, and a shrink-0 action with ml-auto. This depends on available container width, not font-size scaling or a guessed desktop breakpoint.

UserDashboardStats.carpool.spec.ts asserts those layout contracts. Real-page acceptance must additionally inspect390px and1440px screenshots: readable horizontal title, separate nonoverlapping action, no page overflow, unchanged amount/remaining/disabled behavior. A DOM-only class assertion cannot establish visual acceptance. Wrong: a nonwrapping flex row with min-w-0 text beside a fixed action. Correct: preserve readable copy width and move the action onto its own row when necessary.
