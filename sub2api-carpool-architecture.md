# Sub2API Carpool Architecture

## 本次规则摘要

- 会员有效期固定为开通后 28 天，不随任何重置延长。
- 开通时发放一份完整额度，首次自然补满在 7 天后。
- 特别重置成功后补满基础额度，下次自然补满改为本次生效时间的 7 天后，旧日期取消。
- 余额已满但重置成功也顺延；失败、延期、重复请求不会再次顺延。
- 不再承诺或预生成四个固定周周期；内部保留实际账本周期，保障流式 API 请求的延迟结算。
- 会员到期优先。到期前不足 7 天但确实发生自然补满时，仍给完整额度，到期清理。
- 加油次数不增加，加油及人工余额随当前周期的调整后截止时间失效，最晚为会员到期。
- 页面保留连续的会员时间进度和历史重置标记，分别显示会员到期和下次自然补满。
- 本次只开发并验证本地代码，使用 Sol 子代理协作，不运行 Trellis，不修改线上数据。

## Current Decision: Rolling Refill, 2026-09-07

This repository-local document is the current architecture authority. The earlier
document referenced `C:/WORK-SPACE/monasapi/sub2api-carpool-architecture.md`, which
is not available in this Mac checkout. Existing OpenSpec implementation and API
documents remain supporting contracts; their fixed-four-cycle requirements are
superseded by this revision.

The user confirmed that carpool has not launched and there are no real carpool
cycle records to migrate. Implement locally with GPT-5.6 Sol workers and director
integration/acceptance. Do not run Trellis workflow, create Trellis tasks, restart
historical workers, deploy, modify production, or migrate ordinary balances.
Preserve the existing uncommitted ordinary-balance-read-only takeover changes.

## Membership and Refill Rules

1. Standard membership lasts exactly 28 * 24 hours from its start. Its expiry is
   fixed and is never extended by a refill, special reset, boost, or retry.
2. Opening grants one full configured weekly base quota. The initial natural
   refill deadline is opening + 7 * 24 hours, subject to membership expiry.
3. Natural refill closes the previous accounting period and starts the next
   period with the full configured base quota. Its next deadline is seven days
   after that boundary. Do not promise or pre-create four future weekly grants.
4. A successful qualified special reset tops the current base balance up to its
   configured quota and moves its natural refill deadline to effective reset
   time + 7 * 24 hours. It replaces the old deadline. It does not add another full
   quota on top of the remaining base balance.
5. Zero-increment successful participation also moves the deadline. Failed,
   pending, delayed or rolled-back resets never move it. Idempotent replay never
   grants again or moves the deadline relative to replay time.
6. Membership expiry wins over natural or special refill. Period coverage ends at
   min(natural deadline, membership expiry). If the next natural deadline is at
   or after expiry, expose no next natural refill. Never grant at or after expiry.
7. A natural refill occurring before expiry grants a full base quota even when
   less than seven days remain. Remaining quota expires with the membership;
   do not prorate the last rolling period or extend membership to consume it.
8. Boost allowance remains two per term by default, with existing administrator
   2-or-3 configuration, at 10% of the configured full weekly base quota. Special
   reset neither restores boost slots nor clears boost/manual balances. These
   balances follow the current accounting period's adjusted expiry, capped by
   membership expiry. Ordinary user balance remains independent and read-only
   during takeover.
9. Existing special-reset qualification, exact 48-hour cooldown, Shanghai 22:00
   execution window and announcements retain their behavior. Banked upstream
   coupons, upstream coupon consumption and local member refill are distinct
   events. This change does not introduce automatic upstream coupon consumption.

Example: a member starts at day 0, expiry is day 28. Successful special resets at
days 5 and 17 produce refill opportunities at days 0, 5, 12, 17 and 24. The old
days 7, 14 and 21 are no longer promised refill dates. At day 28 service ends.

## Accounting and Concurrency

Retain `carpool_cycles` as internal accounting periods and keep historical IDs,
ledger rows and billing receipts. A special reset extends the active period's end
inside the existing reset transaction; it must not change its start or ID.
Natural expiry creates subsequent periods as needed. Maintenance, user reads and
gateway admission must share the same period-advancement logic.

Every HTTP request or WebSocket model turn retains its persisted admission
identity and original cycle ID. A model response arriving after a reset, natural
boundary or membership expiry settles exactly once against that original period.
Closing periods wait for unresolved receipts before final balance cleanup. Never
move historical usage to the latest period or mutate ordinary balances.

Keep existing lock ordering, post-lock database-time checks and final execution
boundary checks. Credit, deadline movement, successful reset target, ledger and
batch completion commit atomically. Natural and special reset racing at a shared
boundary cannot create two usable balances or duplicate grants. Downtime catch-up
must not issue accumulated missed weekly quota or restart seven days from an
arbitrary late browser visit.

Mark the rule in the frozen plan snapshot (`reset_mode: "rolling"` for new
standard 28-day terms). Missing/legacy mode retains old fixed semantics so old
fixtures and recorded history remain readable. Do not add a production migration
workflow for live contracts that do not exist. Append migrations if schema or
constraints change; do not rewrite applied migrations or unrelated history.

Historical takeover retains administrator-supplied opening balances. An optional
`takeover.next_natural_reset_at` can explicitly preserve the next deadline; it
must be a future valid deadline no more than seven days after calculation time.
Clamp period coverage at membership expiry. If omitted, use the existing
start-anchored, no-special-reset baseline for current-period selection; do not
invent historical special-reset events. Preview and opening must agree on this
contract, including failed validation and expired/future starts.

## API and User Interface

- `term.starts_at` and `term.expires_at` remain membership-time authority.
- User term, administrator term and opening preview expose
  `next_natural_reset_at: RFC3339 | null`. It is null when no natural refill can
  occur before expiry, or when the membership is expired/terminated.
  A pending/future opening or renewal exposes its known starts_at + seven-day
  deadline when strictly before expiry; its available quota remains null until
  activation.
- User term exposes `reset_mode`; administrator term and preview expose it in
  the plan snapshot. Legacy missing mode means fixed.
- `reset_window` remains the qualified special-reset schedule; do not reuse it
  for natural refill. Keep `reset_events`, server time and server-owned available
  quota projections. Internal accounting periods are historical/current records,
  not a promise of four future grants.
- The membership timeline is continuous, without fixed four-week divisions.
  Preserve current visual treatment and successful reset annotations. Show next
  natural refill separately from membership expiry. Successful reset updates the
  deadline while membership progress and expiry stay fixed.
- Admin opening/renewal preview shows membership dates, quota, initial action and
  next natural refill. Keep ledger and period history. Remove fixed four/five
  cycle and guaranteed four-times-quota claims from active product copy.
- Header/dashboard balance, authorization, request-generation guards, localization
  and existing unrelated UI remain intact.

## Acceptance

- Consecutive special resets move the next deadline and never the 28-day expiry.
- Crossing an obsolete fixed-week boundary grants nothing.
- Zero-increment success shifts once; replay/failure/delay does not shift again.
- Natural/special reset races, concurrent admission and delayed settlement
  preserve ledger equality and exactly-once debit/grant behavior.
- Day 27 special reset has no day 34 refill; expiry itself never grants. A valid
  last short natural period receives full base quota, without proration.
- Downtime catch-up, membership renewal, takeover and old fixed snapshots remain
  deterministic. Natural/term expiry handles boost/manual balances consistently.
- Focused unit and real PostgreSQL integration tests pass, followed by relevant
  existing backend regressions and frontend typecheck/tests/build.
- A fresh isolated local runtime and desktop/mobile browser acceptance show
  correct quota, moving natural deadline, fixed membership expiry and usable
  admin workflows. Synthetic data only. No production changes are authorized.

## Ownership

- Director: this architecture, supporting docs/API contract, integration review,
  acceptance decisions and final reporting.
- Backend Sol worker: domain, repository, reset transaction, migration, DTO and
  related backend tests. One owner for shared accounting files.
- Frontend Sol worker: frontend API types, store/view consumers, admin preview,
  localization and focused UI tests.
- Validation Sol worker: isolated local runtime preparation, independent checks,
  browser/HTTP acceptance support and evidence under `validation/rolling-reset/`.
