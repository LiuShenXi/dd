# I5 preparation — not implementation or acceptance

Latest session scope is stop after accepted I4 commit/push. The following remains
read-only preparation for company continuation; no I5 business code was started.

Prepared while I4's real-browser/final-environment gates are pending. No I5
business code is authorized to start before the I4 gate is accepted.

Main owns payment configuration/DTO adapters, immutable snapshot domain and
validation, new paid-term repository/service port, manual/paid shared term
creation transaction helper, existing payment creation/fulfillment dispatch and
Wire. Frontend owner owns plan settings, subscription purchasing and completing
the manual modal's optional plan/default/override behavior. Environment owner
owns actual PG/fake-provider/concurrent-callback/crash-window tests. Exact exported
interfaces must be put in source before independent tests are written.

Proposed API extension for all plan list/create/update results:

- `quota_mode: "windows"|"term"`, derived from the current group, never selected
  by a client to evade group validation.
- `default_quota_usd: string|null`, fixed8-decimal projection of the existing
  `subscription_plans.term_quota_usd` column. Term input is a strict positive
  MoneyInput string. An absent patch preserves the default. Term for-sale
  products require a positive default and exactly7days/604800seconds.
- Admin list includes unsold valid term plans for manual issuance; public list
  keeps its existing for-sale filter. Manual quota is copied once on selection
  and remains explicitly editable. Defaults never rewrite issued cards.
- Existing windows day/month/year, limits, sale-price and currency behavior are
  preserved. UI term branch must not interpret null native window limits as an
  unlimited allowance. Sale price and card allowance remain separate values.

The authoritative immutable order snapshot is a separate typed versioned object:
schema_version1, mode term, group_id, platform anthropic, duration_seconds604800,
quota_usd, source_plan_id and plan/product display names. It is saved in existing
`payment_orders.subscription_quota_snapshot` before any provider invocation.
No snapshot field contains a Key, provider credential, bearer token or arbitrary
execution command. The order's original user/group columns must match the object.
Legacy orders with a null snapshot keep their existing windows path; a now-term
group is an explicit manual-review error, never an inferred allowance.

Existing payment creation has an Ent transaction. The new path must lock/reread
the plan and group there before freezing the exact default/display snapshot;
new issue-enabled/allowlist and positive Claude concurrency gates apply to new
orders. The existing provider configuration snapshot is independent and remains
unchanged. Current callback verification, amount/currency checks, grace behavior,
status transition and fulfillment lease acquisition remain the existing code.

Paid fulfillment requires a separate SQL repository transaction, entered before
`ensurePaymentSubscriptionAssigned` and outside any open Ent transaction:

1. Lock payment_orders by its immutable ID first. Verify status recharging,
   updated_at exactly equals the current persisted lease.version, no refund
   status, original identity and strict frozen snapshot.
2. Acquire the user/group scope advisory lock; check the stable
   `payment:order:<id>` operation identity and existing unique payment_order_id.
   Successful replay returns the original card before consulting today's plan,
   issue flag or concurrency default.
3. Lock parent then existing term IDs in ascending ID order. Recheck current
   user/group identity/mode under the existing final entity lock order. Use renew
   scheduling from the locked queue and frozen604800s/quota. No current plan read.
4. Through the same shared term-creation helper used by manual operations, write
   the card with source_kind payment, nullable actor, immutable source snapshot
   and payment_order_id; update only the parent schedule revision/summary.
5. In this transaction write the durable operation, existing
   SUBSCRIPTION_ASSIGNED audit with its original order/action uniqueness, and
   existing auth invalidation outbox. Commit rights and assignment audit together.
6. Continue existing affiliate and conditional markCompleted outside the rights
   transaction. A later failure can mark the leased order retryable but cannot
   remove its card or permit another assignment. Stale workers cannot complete.

Already-paid recovery does not consult today's issue_enabled or concurrency
default; temporary invalid generation policy cannot destroy purchased rights.
Maintenance conversion can defer fulfillment and must retain it for recovery.
The I1 full/partial legacy automatic refund and redeem guards remain enforced;
term cash refunds are not converted into wallet topups or automatic day deduction.

Known source seams: payment_order.go CreateOrder/validateSubOrder/createOrderInTx;
payment_fulfillment.go ExecuteSubscriptionFulfillment/acquirePaymentFulfillmentLease/
doSub/ensurePaymentSubscriptionAssigned/markCompleted; payment_config_plans.go
CreatePlan/UpdatePlan; subscription_quota_repo.go OpenTerm currently contains the
manual creation block to extract. Operations.actor_id is nullable already, but
the manual save helper currently accepts int64 and must preserve its old behavior
when adding the payment-null variant. No historical migration is edited.
