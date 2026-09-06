# Sub2API Carpool Handoff

## Status

This branch contains the cross-layer carpool implementation and local acceptance tooling. The source builds, but the latest copied-data balance import is not accepted and must be corrected before any release decision.

Do not treat the current local runtime balance display as production-accurate. Do not deploy this branch to production until the issue below is resolved and the copied-data acceptance is repeated.

## Source Checkout

- Source: `C:\WORK-SPACE\sub2api-carpool-v1.3`
- Original branch: `codex/sub2api-carpool-v1.3`
- Original base: `36266f512776d78d4f1645a75ae0e84816f8a0a7`
- Product contract: `openspec/changes/add-carpool-v1-3/`
- Trellis task: `.trellis/tasks/09-05-sub2api-carpool-v1-3/`
- Architecture authority outside this checkout: `C:\WORK-SPACE\monasapi\sub2api-carpool-architecture.md`

The implementation includes backend domain, repository, service, handlers, routing, Ent schemas and generated code, migrations 235-241, frontend user/admin flows, tests, and isolated validation tools.

## Current Business Rules

- Standard terms last 28 days and contain four contiguous 7-day cycles.
- Current standard plans default to two 10% boost claims per term.
- Operator-confirmed legacy custom tiers are represented by disabled legacy plan versions.
- Operator-confirmed weekly legacy terms last 7 days, contain one cycle, allow zero boosts, and must not be renewed.
- Imported terms start at the copied user's original `users.created_at`; no term starts at import time.
- Ordinary balance and carpool balance are independent after takeover.
- No payment record may be invented during copied-data import.

## Blocking Balance Import Defect

The local copied-data import script is outside Git at:

`C:\WORK-SPACE\sub2api-carpool-local-realdata\apply-confirmed-carpool-mapping.sql`

Its current balance logic is wrong for acceptance:

1. It copies the existing ordinary `users.balance` into the active carpool base balance.
2. It imports historical usage with an equal positive source entry and negative usage entry.
3. Those historical entries net to zero, so the user-facing carpool balance can remain at the full weekly quota even when another administrator projection shows a lower remaining amount.

This is an import-fixture defect, not yet proven to be a product billing defect. Determine the authoritative production meaning of each balance/usage projection before changing source or data. Do not guess a formula from a single account.

The import script also previously stopped creating cycles after the active cycle. The local script was corrected to create all cycles, and the current local database was patched to contain the expected future cycles. That correction is not a substitute for re-running the final import after the balance rule is fixed.

## Local Runtime

- Runtime directory: `C:\WORK-SPACE\sub2api-carpool-local-realdata`
- URL: `http://127.0.0.1:38080`
- Current image: `sub2api-carpool-local:confirmed-20260907`
- A pre-import PostgreSQL backup exists under the runtime's `snapshot/` directory.

The runtime contains copied production data and local-only credentials. It is intentionally outside Git. Never add that directory, its SQL mapping, database files, snapshots, compose secrets, screenshots containing user identities, or test passwords to this repository.

The current runtime has local-only login password resets for manual testing. They do not change production passwords and must not be copied into source, documentation, chat history, or CI variables.

## Verified So Far

- The Docker image completed the frontend type check/build and Go server build.
- Focused frontend lint passed for the latest carpool admin view change.
- Focused domain tests passed for 28-day/four-cycle terms and 7-day/one-cycle/zero-boost legacy terms.
- Migration 241 applied locally and accepts 7-day duration plus zero boost count without changing standard defaults.
- Local health endpoint and temporary user/admin login checks passed.
- Exact tier, start-time, duration, boost-count, and cycle-count mapping checks passed after the cycle repair.
- The copied-data balance result did not pass acceptance because of the defect above.

Historical test evidence is under `.trellis/tasks/09-05-sub2api-carpool-v1-3/evidence/`. It proves only the scope named by each evidence file. It does not prove that the latest copied-data balance import is correct.

## Recommended Next Steps

1. Keep the current local runtime read-only while diagnosing the discrepancy between the administrator user-list amount and `GET /api/v1/user/carpool/details`.
2. Trace the administrator user-list amount from storage through repository/service/DTO/frontend. Identify whether it is ordinary balance, remaining quota, subscription quota, or another projection.
3. Define the authoritative takeover equation for existing balance and historical usage. Confirm it across several copied users and edge cases, not one screenshot.
4. Update only the outside-Git local import script once the equation is confirmed. Keep real identities and values outside Git.
5. Create a fresh task-specific database volume from the pre-import backup rather than mutating production or attaching unrelated local volumes.
6. Re-run the mapping transaction, invalidate local Redis caches, and restart only the isolated runtime.
7. Verify ledger conservation, ordinary-balance zeroing, current available quota, future cycles, weekly zero-boost behavior, and zero invented payments.
8. Compare administrator and user views for the same accounts. Any unexplained difference blocks release.
9. Rebuild from the exact source commit and record image/source hashes before considering a blue-green deployment.

## Safety Boundary

- No production database writes are authorized by this handoff.
- No real charge, refund, provider/card request, notification, or reset execution is authorized.
- Do not push copied data, credentials, tokens, cookies, API keys, or local environment files.
- Preserve existing worktrees and do not rewrite the upstream repository history.
- The task remains in progress; do not archive it as completed while the balance import defect is open.
