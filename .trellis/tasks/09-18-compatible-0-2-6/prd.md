# Goal

Deliver a local progressive upgrade containing every upstream change through preserved Sub2API 0.2.6 while retaining the currently deployed BWH customization and data contracts.

## Confirmed baseline

- User requested fork source recovery, current BWH application recovery, and full compatibility with custom features.
- Upstream fixed snapshot: `8b69738d782ccaa7fd26511e1cca26ba8d1b58db`; release commit parent `49a39b6dc1abed30fd227611e8af1108bc427610`.
- BWH running image: `sub2api:carpool-imagefix-20260915-gpt55`, ID `sha256:133537d6b33be5578a6e825fa74e7254de655ddf14b07ab7221790922477e4d0`.
- Binary source label: `dfb612ae3676c45ed7f2b334d7844d751bc0c292+image-main-gpt55`. Current local custom HEAD adds release-safety documentation at `197422403`.
- Official common base: `578785ee7fb35030b094b69624efe25670a36f5f` (0.2.1). Original working trees contain unrelated in-progress changes and must stay untouched.

## Requirements

1. Pin recovered source and deployed runtime by commit/hash; distinguish recovered build source from files directly downloaded from BWH.
2. Include all upstream changes through 0.2.6, including intermediate releases, with no blanket ours/theirs conflict resolution.
3. Preserve carpool accounting isolation, immutable term snapshots, admission/receipt/ledger transactions, fixed 28-day term expiry with the deployed rolling refill periods, boost/manual carryovers, existing legacy fixed-cycle terms, boosts, reset timing, reconciliation and announcements.
4. Preserve admin/user workflows, identity-safe asynchronous UI, quota presentation, refund concurrency fixes, branding, image compatibility and release gates.
5. Preserve filenames and checksums of applied migrations; test the combined upgrade sequence on an isolated PostgreSQL database. Never connect candidate services or tests to production DB/Redis/upstreams.
6. Keep new Codex ticket collection disabled by default; expose upstream live switches and proxy settings with masking. Confirm disabled mode performs no harvest/injection, and enabled mode does not bypass existing billing/admission controls.
7. Produce reviewable staged commits, build/test evidence, unresolved limitations and deployment/rollback instructions.

## Acceptance

- Fixed fork checkout and local BWH runtime baseline exist with source provenance.
- Combined tree contains upstream features and custom contracts; conflict resolution and migration inventory reviewed.
- Backend compile/unit/vet, frontend typecheck/tests/build, and relevant PostgreSQL transaction/migration tests pass, or exact failures are documented without claiming completion.
- UI and HTTP/WebSocket compatibility are validated against the new build in isolation where tooling permits.
- No production routing, application, database, account or credential changes.

## Scope boundary

This task upgrades local source and prepares a deployable candidate. Production rollout is a later action after the candidate is reviewable. No Codex connection changes, credential exports, or changes to unrelated repositories.
