# Sub2API Carpool v1.4 PRD

## Executive Summary

Implement the approved 2026-09-06 revision: new 28-day terms with four weekly cycles, two boosts by default, visible independent spendable quota, and successful reset annotations on the member time bar. Preserve the complete ledger/gateway/admin/reset/announcement implementation and legacy contracts. Delivery requires an integrated local application and reproducible isolated validation. The existing OpenSpec directory name remains stable.

## Problem and Evidence

Operators need accurate term rules without conflating ordinary balances and carpool quota. Members need to see credits they received and actual reset times. Verified bug: `frontend/src/views/user/DashboardView.vue:6` reads ordinary balance for a carpool member, hiding credited quota; a synthetic account shows ordinary0 despite carpool838. Business authority is architecture v1.4 at `C:/WORK-SPACE/monasapi/sub2api-carpool-architecture.md` and the user's annotated time-bar screenshot. Section8.5 exact48h/22:00 scheduling remains unchanged.

## Users and Context

The member may inspect only their own permitted details and claim two boosts by default per new term. The administrator opens/renews/terminates terms, records CNY payments/refunds, inspects the ledger, previews takeover, adjusts quota with reasons and manages reset qualifications. Existing auth, ordinary balances, subscriptions and scheduling remain intact.

## Success Measures

All architecture section 12 scenarios must have reproducible evidence. No duplicate grants/debits, ordinary-balance fallback, cross-user data access or production mutations are acceptable. Backend build and relevant unit/transaction/concurrency/regression tests, frontend typecheck/build/tests, and desktop/narrow-screen real-page verification must pass or be explicitly reported incomplete. No invented business-growth target is used.

## Scope and Stories

1. As an administrator, I can open/renew a membership with the new fixed contract. Given a configured carpool group and user, when I submit an idempotent opening, then exactly one nonoverlapping 28-day term and four snapshotted weekly cycles exist, with only the current cycle activated.
2. As a member, I can use my administrator-authorized key without spending my ordinary balance. Given valid quota at final upstream admission, when an HTTP request or WS turn completes, then actual cost is durably settled exactly once against the frozen original cycle.
3. As a member, I can claim my allowance reliably. Given an active new term, when four concurrent distinct claims arrive, then at most two succeed and replay returns its original result across boundaries. Dashboard/header show refreshed server-authoritative carpool quota without changing ordinary money.
4. As a member, I can understand timing and replenishment. Details show available quota, lifetime net consumption, this-term reset count, four equal time segments and confirmed relevant windows. Successful own-term resets are marked at actual time with `已加满至 $700` or localized equivalent and Shanghai date/time together ABOVE the rail. The latest visual follow-up replaces only this widget, not surrounding page sections; screenshot red markup and numbered badges are not the intended visual style. Zero-grant successes count; scheduled/failed/foreign events do not. Markers remain legible on narrow screens and never alter time progress.
5. As an administrator, I can keep financial history accurate. Given a term, when I record a payment/refund, adjustment, renewal, termination or explicitly confirmed takeover, then immutable audit records preserve money-source separation and snapshot history.
6. As a member, I receive a dependable qualified reset announcement. Given complete synthetic upstream observations or an administrator-confirmed qualification, when it is scheduled, then one scope batch and an idempotent announcement promise the earliest valid Shanghai 22:00 slot after the 48-hour cooldown.
7. As an operator, I can recover from downtime without false accounting. Given delayed billing or missed scheduler execution, when the application restarts, then persisted receipts retry, unknown usage remains visible, and missed reset minutes reschedule with corrected announcements.

## Fixed Rules

| Plan | CNY list price | Weekly USD | Four-cycle base total USD | Boost USD | Boost count |
| --- | ---: | ---: | ---: | ---: | ---: |
| Four-seat | 330 | 550 | 2200 | 55 | 3 |
| Three-seat | 420 | 700 | 2800 | 70 | 3 |
| Two-seat | 655 | 1100 | 4400 | 110 | 3 |

New terms last exactly28*24h; cycles last7/7/7/7 days with half-open intervals. No fifth cycle is created. Boosts use weekly*0.10, two per term by default rather than per calendar month. Store eight decimal places and immutable snapshots. Net availability is max(0,base+boost+manual); consume base,boost,manual and retain overuse as base debt. Grants offset debt through paired transfers. No carryover or boost-count refresh on weekly/special reset. Renewal creates a future term; omitted plan_id defaults to the latest same-tier rules, while an explicit administrator choice may use another latest enabled tier without changing the current term.

Append plan versions preserving custom latest price/weekly quota/name/enabled settings. Existing30-day/five-cycle/two-boost snapshots, grants, payments, terms and history remain immutable and display their actual shape; never shorten purchased terms. User details expose only net quota and successful reset target/time fields, not internal IDs, bucket balances or raw ledgers. `已加满至` means the cycle base target, not credited delta/current total. Loading/errors/account changes must not substitute ordinary balance or stale foreign quota.

## Revision Acceptance

The active Trellis PRD A1-A7 is the current verification map:28-day boundaries/four-cycle totals, two default atomic boost slots, migration/legacy conservation, real dashboard/header refresh, authorized reset projections including zero grants with desktop/mobile screenshots, Go/PG/frontend gates, and source-bound local delivery. Historical v1.3 results in tasks.md are not acceptance of this revision.

## Out of Scope and Safety

Latest user authorization is recorded by the active Trellis task at `.trellis/tasks/09-05-sub2api-carpool-v1-3`: the source coordinator exclusively owns read-only production database export and isolated local-copy testing. This locally supersedes the original blanket ban below only for that coordinator operation. Business workers still do not access production or connect an application to the copy before network-isolation/neutralization approval. No real data goes into git, reports, screenshots, logs or agent messages. All other safety limits remain in force.

No production SSH, deployment, upstream card consumption, real refunds/balance changes, live-data takeover, credential inspection, pushing or publishing. Never access or scan `C:/WORK-SPACE/my-sub2api`; it contains real PostgreSQL/Redis data. Only edit the specified architecture file in the dirty monasapi workspace. Do not change Codex connections. Preserve worktrees/history. Strict pre-reservation billing and automatic mid-term proration remain out of scope. The authorized neutralized copy already exists; no further remote export is required.

## Dependencies and Risks

Base commit: `36266f512776d78d4f1645a75ae0e84816f8a0a7`, from the clean `sub2api-v0.2.1-refundfix` worktree. New branch: `codex/sub2api-carpool-v1.3`. Worktree: `C:/WORK-SPACE/sub2api-carpool-v1.3`. Existing repository conventions are Go/Gin/Ent/PostgreSQL/Redis/Vue/pnpm and OpenSpec. System Go 1.21.1 does not meet go.mod 1.27.0; use safe isolated toolchains/containers. Upstream snapshot compatibility is only validated with synthetic samples; polling cannot prove unseen between-poll events or official campaign identity. A crash before actual usage is saved cannot reconstruct exact charges and must remain reconcilable.

## Decisions

The architecture's section 2.3 defaults are accepted for this implementation. Section 8.5 uses one precise slot/due algorithm, never a whole-hour cooldown shortcut. Unknown capabilities are investigated locally, not treated as verified. Production adoption remains separately authorized.
