# Sub2API Carpool: 28-Day Rules and Visible Quota (v1.4)

## Goal

Latest small follow-up: default new plan versions/openings/renewals to two boosts. Reuse the existing administrator `boost_count` field as an editable2-or3 parameter; existing snapshots and claims remain unchanged. The user requested direct implementation without new workers. Migration240 appends new default versions without rewriting migration239 or existing contracts. Accepted locally in `evidence/director-two-boost-acceptance.md`; completed A/U criteria below retain their historical scope.

Latest UI follow-up (2026-09-06): redo ONLY the membership timeline widget, not the whole page. Reset timestamp and refill copy belong together ABOVE the rail; remove red numbered markers and detached labels. See `research/timeline-ui-redesign.md`. This visual revision is accepted locally in `evidence/director-timeline-ui-final-acceptance.md`; prior v1.4 functional acceptance remains historical.

- [x] U1: Timeline-only redesign with paired refill/time annotations above, precise quiet nodes and no numbered badge clutter; surrounding page unchanged.
- [x] U2: Close/dense/edge/no-event and legacy states retain all information without overlap; Chinese/English and light/dark render correctly at390px/desktop.
- [x] U3: Focused/full frontend gates and exact-image browser acceptance pass; runtime data preserved, no business changes or remote operations.

Implement the user's 2026-09-06 approved revision in the existing isolated worktree: show spendable carpool quota after boosts, annotate successful resets on the membership time bar, and change new memberships to 28 days, four weekly cycles and three boosts. This is a business-rule and user-projection revision, not a cosmetic fifth-segment removal.

## Approved Sources

- The full requirements remain in [OpenSpec PRD](../../../openspec/changes/add-carpool-v1-3/proposal.md). Read it, not only this adoption summary.
- Detailed business contracts and section 12 acceptance scenarios remain in `C:/WORK-SPACE/monasapi/sub2api-carpool-architecture.md`, updated to v1.4. Existing task/OpenSpec directory names remain stable for continuity.
- The user explicitly asked to update PRD/architecture and implement. No additional implementation approval is needed. Prior v1.3 evidence remains historical, not proof of this revision.
- Confirmed defect: `frontend/src/views/user/DashboardView.vue:6` supplies ordinary `user.balance` even for carpool members. The synthetic reporter has ordinary balance 0 and carpool available 838 after two 70 USD boosts. Do not double-credit ordinary balance to fix this display.

## Requirements

- New openings and renewals last exactly `28 * 24h`, with four contiguous half-open `7 * 24h` cycles. No fifth cycle is created. Weekly quotas/prices remain 550/330, 700/420, 1100/655 (USD/CNY); base totals are 2200/2800/4400 USD.
- Each new term defaults to two boosts, each 10% of full weekly quota: 55/70/110 USD. Administrator plan versions may set boost_count2 or3; openings freeze that parameter. The allowance belongs to the 28-day term, not a calendar month; weekly/special resets do not replenish it. Concurrency and replay cannot consume extra claims. Quota accounting retains eight decimal places; boosts expire with their cycle.
- Existing terms, snapshots, dates, cycles, claims, ledger and payments remain unchanged. Append plan versions for the new rules; legacy 30-day/five-cycle/two-boost contracts remain faithfully readable and executable. Never shorten an existing term or fabricate historical resets.
- Dashboard and shared user-header balance presentation distinguish carpool quota from ordinary money. Active members see server-authoritative spendable carpool quota; ordinary users retain existing behavior. Refresh after claims, cycle/reset boundaries, explicit refresh and returning to the page. Loading/errors/account changes must not expose another account's quota or represent ordinary balance as carpool quota.
- New terms show four equal time segments. Successful resets add markers at actual execution instants, with localized copy such as `已加满至 $700` and Shanghai date/time ABOVE the bar, paired in a visually linked annotation. Amount means the immutable cycle base target, not reset delta/current balance. Include zero-grant successful participation; exclude pending/failed/cross-user records. Nearby markers remain readable on desktop/mobile; resetting/boosting never moves time progress.
- Special resets require confirmed qualification, at least 48 hours since actual successful execution, and a Shanghai 22:00 slot. Exactly 48 hours is allowed; preserve seconds and the one-minute execution tolerance. No qualification means no automatic reset.
- Ordinary balances and subscriptions retain their behavior. Carpool never silently falls back to ordinary balance. Persist final-admission cycle identity through HTTP/WS turns, retries, failover, and delayed settlement.
- Authorization is server-enforced. User details add only permitted own-user available quota and successful reset-event projections, not internal IDs, bucket breakdown, raw ledger, payments, operator notes or upstream evidence. Chinese/English UI, preview/admin defaults, help and tests agree with new rules; legacy details preserve actual snapshot shape.
- The director controls progress and independently reviews diffs/evidence. All workers use `gpt-5.6-sol` / `high` with explicit context and nonoverlapping ownership. No commit/archive is implied.

## Acceptance Criteria

- [x] A1: Three plan previews/openings/renewals yield four weekly cycles, exact 28-day expiry, correct totals and three 10% boost slots. Days 7/14/21/28 and expiry are tested.
- [x] A2: Four concurrent unique claims produce exactly three credits; replay, expiry and renewal preserve accounting and ordinary balances.
- [x] A3: Upgrade/replay preserves old terms/snapshots/ledger and custom plan settings; disabled tiers are not accidentally enabled. Existing five-cycle contracts still work.
- [x] A4: Actual local dashboard/header quota increases after a boost and remains correct on reload; ordinary-user behavior is unchanged. Loading/error/stale-response/account-switching tests pass.
- [x] A5: Reset events match own successful targets including zero grants; no pending/failed/cross-user leakage. Desktop/mobile screenshots show four segments, reset copy and times below without overlap.
- [x] A6: Focused backend unit and actual PostgreSQL tests, frontend typecheck/lint/tests/build pass; source/embedded frontend bind to the tested image. Unrelated baseline failures remain explicit.
- [x] A7: Original architecture, OpenSpec and Trellis agree; copied data and prior runtime history are conserved; deliver new evidence and local URL without push/deployment.

Accepted locally on 2026-09-06 after independent source/evidence review. See [v1.4 director acceptance](evidence/director-v14-final-acceptance.md) for the exact image, 71 HTTP / 11 WS assertions, 1930 frontend tests, composed PostgreSQL runs, browser screenshots and residual limits. Imported historical reset markers are UI evidence, not a claim of live scheduler execution. Commit/archive and production adoption remain outside this handoff.

Prior v1.3 results remain in [director acceptance](evidence/director-final-acceptance.md) and the historical section of the OpenSpec checklist. They do not satisfy A1-A7 automatically.

## Out of Scope and Safety

- Latest user instruction authorizes a read-only remote database export and an isolated local copy using knowledge-base deployment details. This copy is for local validation only; no production writes, real refunds/charges, provider/card use, external notifications, pushing, or deployment.
- Never inspect or use `C:/WORK-SPACE/my-sub2api` runtime data. Restore only into fresh task-specific restricted storage. Disable real credentials/external effects before startup; use synthetic identities for test mutations and screenshots. No copied user data may enter git, prompts, reports, screenshots, or logs.
- Preserve existing source worktrees, all agent changes, and the dirty monasapi repository. Only edit the user-named architecture file there.
- Do not alter user-level Codex connections or enable global hooks. Do not copy credentials into task context. No unverified result is treated as evidence.
- The authorized export and neutralized isolated copy already exist; do not fetch another copy. Existing synthetic reporter/acceptance records remain intact; new mutating acceptance uses fresh synthetic identities. Port 18080 is unrelated and must not be touched.
- Copied-user test terms, only when a verified tier mapping exists, start at original `users.created_at` with 28-day expiry. Preserve registration times and expired/missed states; unmapped users receive no invented membership. This does not rewrite any already-created contract.
