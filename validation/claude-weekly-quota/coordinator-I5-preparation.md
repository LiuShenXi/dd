# I5 read-only preparation

Prepared during I4; no I5 implementation or acceptance is claimed. I5 starts
only after the I4 gate. Basis: architecture integration-contracts.md and the
actual payment_fulfillment.go / payment_order.go entry points.

- Keep notification verification, paid-state handling, the five-minute lease
  and its updated_at version. Dispatch valid frozen term orders before the old
  native assignment path or any mutable/deleted plan dependency.
- FulfillPaidTerm locks the order first, checks paid/non-refunded state and
  the current lease version, then scope/operation/parent/terms. The card,
  operation, existing SUBSCRIPTION_ASSIGNED audit and outbox commit together.
  Distribution and markCompleted remain the existing recoverable later steps.
- A new issue/sales flag must not prevent an already-paid order from receiving
  its immutable entitlement. Recovery and final compatible R1 must preserve
  this behavior even with new admission disabled.
- Snapshot validation must distinguish nil legacy orders from malformed or
  unsupported snapshots. A legacy order whose group became term is an explicit
  manual-handling failure, never a current-plan/default quota guess.
- I4 intentionally implements manual explicit amounts first. I5 must add
  administrator plan/default selection with an override, the positive default
  quota product UI, immutable order display and fake-provider fulfillment.
  Price charged by the payment provider remains distinct from usage quota USD.
- Exercise concurrent callbacks, lease takeover, rollback before issuance and
  failure after card/audit commit. A unique payment_order_id is necessary but
  is not enough without atomic audit/lease checks and recovery proof.
- Preserve explicit old full/partial refund and redeem rejection for term.
  No cash refund is converted to an ordinary-balance recharge, and no old
  parent-day subtraction can revoke or reset a card.

The I3 receipt-compatible image is not the final rollback target after I5.
Rebuild/revalidate the paid-order-capable compatible R1 in I6 with the final
schema and retain the latest database throughout the rollback rehearsal.
