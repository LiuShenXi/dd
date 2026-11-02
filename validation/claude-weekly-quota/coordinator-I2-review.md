# Coordinator I2 review

Review date: 2026-10-19. **I2 accepted for entry into I3** on final F2. This accepts card operations and read/Key boundaries; it does not authorize production activation.

## Reviewed behavior

- Strict, positive decimal input follows the OpenAPI grammar and normalizes to eight fractional digits. Per-user card quota and source metadata are immutable. A selected same-group fixed-seven-day plan may provide the default even when not publicly for sale; public sale eligibility belongs to I5.
- Administrative idempotency is scoped by actor, target user, group and open/renew operation. Replay uses the original source snapshot before consulting current issuance flags, plan values or revision. Successful replay after a plan edit/deletion does not create another card.
- Writes use the dedicated scope, parent and ordered card locks, followed by final entity checks and a fresh database clock. Creation/cancellation, schedule revision, parent projection and the existing invalidation outbox commit together. No GPT, ordinary balance or Key-accounting write is part of issuance.
- Cancellation preserves future gaps. Parent start retains the earliest historical card; end follows the latest uncancelled card or the historical end when all are cancelled. Open after cancellation uses the current schedule rather than retaining a cancelled far-future tail.
- Reads use a repeatable-read transaction and a single per-card projection for used and pending values. Current access uses the card's half-open interval; the parent's future expiry cannot authorize a gap. Remaining available quota is zero on scheduled/expired/cancelled cards without erasing their historical quota or usage.
- Nine actual admin/user routes use strict DTOs, authenticated identity, stable cursor pagination and explicit public projections. Foreign/missing usage ownership has the same 404 response. Current-card Key entitlement permits an exhausted current card for Key management but does not grant generation or future-only access. Pure GPT queries do not invoke the new quota repository.

## Review corrections

- Fixed last-term selection to use the actual end time (the earlier of cancellation and expiry), with a stable ID tie-breaker. Maximum card sequence alone can select an earlier-cancelled future card over a more recently expired card.
- Fixed legacy group deletion before it can touch a term parent. The cascade path takes the group lock and rejects term immediately; the direct delete has an atomic windows-mode predicate. This prevents both forbidden parent soft deletion and the group-to-parent inversion against renewal/cancellation.
- The unused concrete subscription DeleteByGroupID method is recorded for the I6 legacy-write audit; current production callers were not found. It is not evidence of a presently reachable bypass.

## Evidence boundaries

- The 40-test HTTP/DTO/Key unit receipt uses actual route registration and controlled authentication/service dependencies. It does not substitute for PostgreSQL concurrency.
- Earlier debug/F1 database runs are retained but superseded. Debug source changed during execution; F1 predates the last-term and group-deletion fixes. They are not final acceptance evidence.
- I1's real PostgreSQL composite-FK tests establish rejection of cross-user/group/term tuples; I2 adds public DTO and ownership-read boundaries. Billing receipts and debit tampering remain I3.
- Generation, charging, recovery, UI, payment fulfillment and release conversion remain later iterations. There has been no production issuance, consumption, migration or deployment.

## Final acceptance

- Frozen source SHA-256: `4dd98708a8cab51b34a46f00fec7263a969025b60aa3ac7198a0256c49aeaa97`. Both PostgreSQL and Linux race runs have identical before/after hashes; the environment owner also independently rehashed the final source.
- Independently counted the final JSONL: PostgreSQL 16 top-level passes, zero failures/skips (11 domain, two prior coexistence/legacy sinks and three original GPT cases); Linux race 61 top-level passes, zero failures/skips across six packages.
- Independently counted the Windows final JSONL: 167 top-level passes, zero failing or skipped individual tests. Go reports one package-level skip for the helper package with no tests; it is not a skipped business case. Full compile and final vet passed; their logs and developer receipt are retained.
- The new real PostgreSQL deletion test holds the Claude parent, runs actual renewal/cancellation waiters, checks all three deletion entries reject before accessing that parent, and runs the original GPT preparation with a 500ms lock timeout. Parent and group history remain intact; the GPT preparation is rolled back and the Claude mutations complete after release.
- Re-ran the coordinator baseline verifier: production revision remains the ancestor and all 297 existing migration checksums are unchanged.
- Final evidence: `I2-domain-F2-final-receipt.json`, `I2-domain-F2-20261019T140738Z-receipt.json`, `I2-domain-race-F2-20261019T140748Z-receipt.json`, `I2-final-windows-unit-F2.jsonl`, `I2-receipt.md` and `I2-http-key-receipt.md`.
- T01 and T03–T12 are accepted for the I2 card-operation scope; T24 combines the accepted I1 PostgreSQL composite-FK evidence with the I2 HTTP/ownership results. T02 remains partial until actual multi-Key charging is proven in I3/I4. T59 gains specific open/renew/cancel versus GPT lock evidence, but settlement concurrency remains I3.
