# Director Final Local Acceptance

Reviewed on 2026-09-06. The implemented carpool v1.3 is accepted for isolated local handoff within the evidence boundaries below. This is not production approval, a claim that every backend suite is green, or user approval to commit/archive.

## Handoff

- URL: http://127.0.0.1:38088
- Worktree: `C:/WORK-SPACE/sub2api-carpool-v1.3`
- Branch: `codex/sub2api-carpool-v1.3`
- Base and unchanged HEAD: `36266f512776d78d4f1645a75ae0e84816f8a0a7`
- Application image: `sha256:6d569031eb1b5017fe7c3a7f5f14ed6a863442d9f49d7b2dc372d586b3c7adfd`
- Application includes administrator terms/cycles/ledger/adjustments/payment audit, dedicated-key billing and recovery, user details/boosts, qualified reset scheduling and scoped announcements.
- No production write, real charge/refund/provider/card use, external notification, push, deployment, commit or archive was performed. The existing isolated runtime remains available; integration-test cleanup is SOURCE-owned.

## Executed Verification

- Whole backend build passed. Focused unit regressions across seven affected packages passed three times; actual router/server/middleware tests, unit-tag vet and compile coverage are separately recorded in `research/director-integration.md`. Compile-only commands are not assertion execution.
- Actual isolated PG evidence covers migrations, idempotency/concurrency, lock boundaries, original-cycle settlement, ledger/debt conservation, reset precision/rollback, recovery and announcement publication/audience. See `director-pg18-intermediate.md` and `source-runtime-pg-progress.md`.
- Immutable V5 frontend typecheck, full lint, build and265files/1907tests passed. After four nonbundled countdown/expiry tests, the full suite passed265files/1911tests in `director-final-additive-frontend-full-tests.log`.
- Exact V5 runtime HTTP67/67: `source-runtime-http-20260905T235449813Z.json`.
- Exact V5 strict WS11/11: `source-runtime-websocket-20260905T235608621Z.json`.
- Exact V5 coherent reset pending_merge22/22: `source-runtime-reset-20260905T235712249Z.json`.
- Director actually viewed V5 desktop/390px localized ledger/renewal preview and user dashboard/details screenshots. V4 already exercised one authorized synthetic boost and one opening. V5 UI was read-only, and viewport override was reset. See `director-ui-acceptance.md`.

## Additive Lifecycle Closure

The director independently read complete verbose output and summary for isolated PG run `20260906T001828Z-46e6ee52`: exit0, five top-level passes (four business tests plus isolation harness), four nested unresolved-state passes, zero failures. The exact anchored pattern is retained in the run summary under the private task root's `repository-tests/runs/` directory. Retained executed test binary SHA256: `BCFFA2F680DE0F2B742B91FA8661EF16B30EB129FC25F9B9BFF8DECFF73C5ED7`.

Direct assertions now cover:

1. Two-week downtime skips missed periods, grants only current, does not activate future terms, and repeated maintenance adds no ledger rows.
2. Boost replay returns the original result after expiry and renewal; an expired new claim cannot consume a slot or add a boost ledger entry.
3. Same-user net usage spans old/new terms with linked reversal, incomplete history metadata and reset counts restricted to the selected current term.
4. Admitted, usage_known, settling and reconcile_required receipts hold closing; resolved original-cycle settlement never charges the new cycle and final balance equals ledger net.

Earlier run `20260906T001349Z-d4c3a8cd` remains failed: two cases stopped in test-fixture PostgreSQL parameter inference, while the other two business cases and harness passed. The owner changed only fixture SQL parameter handling, not production behavior or assertions. That failed run was not rewritten.

## Immutable Product And Test Identity

`director-code-ready-final-image-v5.json` remains unchanged, SHA256 `2F8841CEB2659B384ABAF5642F56A725B26B35986AD556115ACFD4290FC8A422`, with its original local-start-only authorization and final_acceptance:false. This report is the separate later local acceptance decision.

`source-runtime-v5-final-equivalence-20260906T001844573Z.json` binds the final test additions without rewriting V5 source identity. Director independently hashed the preserved V5 and final additive binaries; both equal `30BA65BC7633884C71C77F9DEDDAC7CCEF6E86C859759A3AACA4FD81A7946170`. Exact Linux embed flags and toolchain produced unchanged executable bytes; no runtime rebuild was needed.

- V5 original source aggregate: `B696574939DB9A389525041B17FF5B466EAFF3BB87346938D1026BF20A292117`.
- Final additive source aggregate: `05378BFC865B2E4AD5DF9ADC7DAEB369FFF9D66250247473CA6FE8FE72B65C53`.
- Unchanged embedded frontend: `F8466F2E1B567CEE6A27ACCFA1EAD6087D131C9138CB69693152FDC9897D50AC`.
- `backend/internal/repository/carpool_lifecycle_integration_test.go`: `0F8F63C6E45DFAE8A8B2105CD0E556BA65CDA1D236D068A337BB02E70570109F`.
- `frontend/src/views/user/__tests__/CarpoolDetailsView.spec.ts`: `4A398D62C660A137F05FBF904341F2B73FD83E15E9E4182A47B2B045A2606E58`.

## Final Safety And Limits

Director read `source-runtime-v5-post-ui-20260906T001713694Z.json`: health200, exact V5/config/environment/internal-network identity, sole loopback bridge, and copied-record/announcement conservation passed. Synthetic127 retains ordinary100, unchanged key64 row hash, term94/five cycles/one550 grant/fifth157/zero payments. Batch1 and announcement10 are preserved; the expected third qualification came from V5 pending_merge. That earlier safety report correctly retains final_feature_acceptance:false because lifecycle tests were unfinished at its capture time.

- Full backend service suite is not green. Four Windows plugin file-rename failures and an intermittent moderation timeout were reproduced on the exact unchanged base. See `director-baseline-service-review.md`; no unrelated repair was attempted.
- HTTP did not execute an actual due reset at current wall clock. Exact execution is covered by real PG tests. First-publication evidence is composed from preserved original passing assertions, stable audience checks, deterministic driver-race tests, subsequent coherent merge and real PG publication/execution. The original20-pass/1-fail publication report stays failed, and its unretained original baseline is unknown.
- One V4 post-opening GET refresh timeout recovered with read-only Retry. Opening itself returned201 in12ms and was not retried. The exact client dependency/cause remains unproven; no speculative fix was made.
- Copied users had no verified tier mapping, so copied-user terms remain zero. No guessed membership, balance-derived tier or copied-user identity was used in public evidence. All new workflow mutations were synthetic and isolated.
- Task remains in_progress only for user-controlled commit/archive follow-through. This handoff does not authorize deployment or integrate into another checkout.
