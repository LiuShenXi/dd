# Coordinator I1 review

Review date: 2026-10-19. **I1 accepted for entry into I2** on the frozen F3 source below. This is acceptance of the preliminary compatibility/refusal iteration only; it does not authorize production activation.

## Independently reviewed implementation

- Incremental migrations 245–249 and the narrow nontransactional migration runner extension. Existing 297 production migrations remain identical to the live verified baseline. Money CHECK constraints explicitly reject PostgreSQL numeric NaN.
- Explicit `windows`/`term` mode throughout Ent, group queries, auth projections and snapshot version 26; fail-closed auth runs before simple mode or legacy balance fallback. The GPT coexistence exception is limited to an identity-consistent Anthropic subscription term group.
- Legacy subscription mutation and maintenance predicates, payment/refund guards including force/deduct=false, pending finalization and compensation, and the separate negative-day redemption path. Normal GPT billing fingerprints and amount algorithms remain unchanged.
- Protected Claude standard/windows groups perform the final DB gate immediately before forwarding. Lock order is user, group, Key, followed by a fresh DB clock. Durable unfinished records include asynchronous billing work; unknown usage and failures cannot disappear on HTTP exit or by timeout.
- CountTokens explicitly records no charge and cannot be moved back to reconciliation by its deferred completion. Protected old billing requires the atomic repository and uses the mandatory queue.
- The actual route test registers the existing gateway router. New term generation remains unavailable, including forced-provider, unsupported and WebSocket routes.

## Evidence inspected

The migration receipts and JSONL demonstrate eight real PostgreSQL scenarios, including unchanged legacy rows, exact 168-hour/decimal constraints, immutable history, lock-timeout restoration on the same backend PID, and repair of a genuinely invalid concurrent index. Protected-group database tests cover state changes while waiting, refreshed Key expiry checks, persistent drain and concurrent actual GPT preparation.

The F2 combined run was **not accepted**: 14 of 15 top-level tests passed, and the final old-sink immutability assertion compared nanosecond input against PostgreSQL microsecond storage. All sink refusal subtests passed. F3 normalizes only the synthetic timestamp before writing and retains exact equality; its fresh results must be inspected before stage acceptance.

The 99-test GPT/new-mode focused result has zero failures/skips. Expanded unit verification required two fixture updates for intentional new fields and a process-local shell PATH for existing pg_dump tests; failures are retained in the evidence. Full compilation, domain tests and vet are reported by the developer and are to be read with the final I1 receipt.

## Final frozen-source acceptance

- Frozen source: `e2d31f2525ccf72a74eb03c308611785246ce45d0e75198ef4d06fd6d7651693`; embedded frontend: `b657870fcdf990281b559656e5bd671843e0f626608a9bfd3e7dcc0d26d96542`.
- Read the final F3 PostgreSQL receipt and JSONL: 18 top-level tests, zero failures or skips, unchanged source before/after. Includes all eight migration scenarios, five protection scenarios, two coexistence/legacy-write scenarios and three original GPT baseline tests.
- Read the F3 Linux race receipt and JSONL: 15 top-level tests across four packages, zero failures/skips, unchanged source. The earlier 1 GiB temporary-filesystem build failure remains recorded; the successful run used the task's own output volume for compiler temporary files.
- Read the successful local image build receipt and independently inspected the built image. Tag `sub2api:claude-weekly-i1-r1-e2d31f2525cc`, image ID `sha256:459dd4757b05983d0fcf79cec26d91fcca45edacb640f32056623f9b242d36ae`. The image explicitly identifies a dirty frozen I1 tree, disables activation and requires I3 recovery refreezing.
- Build receipt records the embedded server SHA-256 `29e0609d4b73bc805bc9f08665794cc5ff836b23d81eca0457d52bb061d7ab00`, successful no-network/non-root version smoke, matching in-image binary hashes and PostgreSQL 18.1 client. This establishes buildability, not a running business-server or production acceptance.

Exact evidence: `environment-I1-F3-20261019T133930Z-{pg,race,build}-receipt.json` and their referenced logs/manifests. The build uses the documented local runtime recipe and Linux Go 1.27.2; final deployment equivalence remains I6.

## Explicit remaining boundaries

- I1 is a preliminary compatibility rejection version. It is not an activation candidate and not the final rollback image after receipts exist. I3 must freeze a version capable of current-receipt recovery.
- I6 must serialize old entitlement/order mutations against maintenance and mode conversion, enforce all-replica capability checks, and prove exact drain/cutover. Early preflight guards alone do not prove that final conversion protocol.
- No production issuance, consumption, migrations, configuration changes or deployment were performed. Local synthetic fixtures are not production activation evidence.
- No T01–T62 row is promoted to passed solely because this review or a planned test exists.
