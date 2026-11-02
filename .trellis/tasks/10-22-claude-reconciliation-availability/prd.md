# Claude unknown-usage reconciliation availability

User request (2026-10-22): clear the affected user's hold and permanently stop
blocking card use for this class of interrupted requests.

Production diagnosis: user16 / key98 / group7 / term3, request441 was cancelled
at 2026-10-21 23:26:02 CST without complete usage. `usage_incomplete` left a
NULL-cost reconciliation record that blocked the whole card.

## Scope

- Resolve request441 via the authenticated, revision-checked and idempotent
  admin API as a user-authorized fee waiver, preserving the original request.
- Distinguish unknown-use reviews from blocking known-money conflicts in the
  shared repository projection. Rename the internal count to communicate this.
- Continue card use and show the card active while unknown usage awaits review.
- Preserve known-conflict/ledger protection, pending-cost quota, expiry and
  late-usage idempotence. No automatic zero-price settlement or schema migration.
- Test, commit, build and deploy using the established blue-green controller,
  preserving live requests and shared PostgreSQL/Redis processes.

## Acceptance

- Request441 has audited `no_charge`, revision2, zero waived fee; issued quota,
  existing settled usage and expiry stay unchanged by the waiver.
- Incomplete usage, stale admission and missing pricing without a known cost
  leave reviewable NULL-cost rows while a second Key can admit on the same card.
- Pending known cost still exhausts the card. Late trusted usage settles once.
- Known receipt conflicts and ledger mismatches still reject new admission.
- Both production domains serve the new immutable image; prior slot retires
  naturally and new background leadership activates normally.

## Session notes

`trellis-start` was read; this checkout has no `.trellis/scripts`, current-task
pointer or journal. Task metadata and receipts are maintained directly.
Initial working tree was clean at ce26558fe on codex/claude-weekly-quota.
Request441 was resolved by admin API at 2026-10-22 09:51:35 CST. Private server
receipt: /root/sub2api-ops/claude-reconciliation-20261022/.

## Completion

All acceptance criteria passed; production blue .3 verified at 10:09 CST.
Final receipt: validation/claude-weekly-quota/reconciliation-20261022/release.md.
No schema migration; current task metadata recorded manually because Trellis scripts and journal are absent.
