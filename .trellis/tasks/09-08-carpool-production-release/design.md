# Design Boundary

The approved release design is `openspec/changes/add-carpool-v1-3/production-release-2026-09-08.md`.

Implement the smallest missing capabilities: independently anchored takeover cycles, immutable special-plan terms and renewal, a controlled request/usage drain, and an idempotent restricted-permission import command. Preserve existing carpool admission/settlement locks and ledger invariants.

Root owns the migration CLI/repository and production deployment. A domain worker owns anchor and renewal handling. A runtime worker owns HTTP/async-usage quiescence and operational control. No worker edits another worker's owned files without coordination. Tests use new isolated resources; production remains untouched until integrated acceptance.

The import transaction creates all terms/current cycles/opening ledger, persistent per-user billing bindings and member access restrictions atomically. The existing standard group, upstream routing and every API key row stay unchanged; administrator keys remain in group 2 and ordinary billing. Each imported member's binding selects the carpool ledger even after term expiry, preventing fallback to the retained ordinary balance. Exact financial data belongs in ignored, permission-restricted artifacts. The restricted migration role cannot UPDATE balance. Inactive application staging must not run new carpool maintenance before takeover; the drain must count accepted async usage work and persistent tasks, not HTTP concurrency alone.

Before ledger activation, old standard-compatible application rollback remains possible with additive schema. After activation, rollback must use an application that retains carpool billing. Keep a tested compatibility image and preserve the ledger; do not restore a whole old database over new usage.
