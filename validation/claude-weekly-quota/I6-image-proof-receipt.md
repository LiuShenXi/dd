# I6 actual image upgrade / rollback proof

Status: **actual final-image execution passed**, 2026-10-20 02:08:51–02:09:04 UTC. `TestSubscriptionQuotaI6ImageUpgradeAndRollback` passed in 9.58 s; independent runner elapsed 13.33 s. One top-level pass, zero failures and zero skips. This result came from running the frozen server images against the original disposable PostgreSQL/Redis harness, not from compilation or repository-only tests.

Machine receipt: `office-I6-rollback-corrected-20261020T020851Z.json`. Private raw log: `/Users/shenxi/.codex/private/claude-weekly-quota/office-rollback-20261020T020851Z/rollback.log`, SHA256 `bd70c9f67b9104bbfade85e49c1791b1fe10697730f157f1cb68bd83d7881880`.

| Frozen artifact | Verified identity |
|---|---|
| R0 | `sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677` |
| R2 | `sub2api:claude-weekly-i6-r2-2ceb119eb44d`, `sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483` |
| Compatible R1 | `sub2api:claude-weekly-i6-r1-2ceb119eb44d`, `sha256:7d78d912b323875ca61a4c9c716b0018716f361a7146a1443bd94c6882b3523a` |
| Source label | `2ceb119eb44d2d8d9ba68e37e0cfe4646b51cc067628cc34175734607daaa55d` |
| Synthetic provider | `sha256:2d20848a025bd39adeb1f732ba3b2216dc2f7502ffd9a7d61091008c3b718826` |
| Corrected test driver | `f73265ba2226adc1a0b41894513477b041528fe3423eac76d02ea77d24799abd` |
| Corrected test source | `c1ba713a83b178c23c0c18af129fb409710b07a58d04603c40a3ac579e7d6dde` |

The image source labels remain those of `office-I6-build-receipt.json`. Only the test driver fixture was corrected after that build; this run did not rebuild or change either product image. Both the driver binary and test source hashes remained stable throughout the passing run. The coordinator records the separate test-only source delta.

Entry point: `backend/internal/repository/subscription_quota_upgrade_rollback_i6_integration_test.go`, `TestSubscriptionQuotaI6ImageUpgradeAndRollback`.

Required inputs from the successful `office-build-I6.py` receipt:

| Environment variable | Receipt value |
|---|---|
| `CWQ_I6_R1_IMAGE` | `r1_image` |
| `CWQ_I6_R2_IMAGE` | `r2_image` |
| `CWQ_I6_SOURCE_SHA256` | `source_sha256` |
| `CWQ_I6_PROVIDER_IMAGE` | `provider_tag` |

Actual compilation and execution used a separate driver, leaving the main runtime driver's binary untouched:

```sh
cd backend
GOMAXPROCS=3 go test -c -p=1 -tags=integration \
  -o /Users/shenxi/.codex/private/claude-weekly-quota/office/repository-rollback-final.test ./internal/repository
python3 /Users/shenxi/.codex/private/claude-weekly-quota/office/rollback-runner.py
```

The independent runner sets `CI=true`, loads all four image/source inputs from the frozen build receipt, invokes only `^TestSubscriptionQuotaI6ImageUpgradeAndRollback$` with `-test.parallel=1 -test.timeout=15m`, and writes a fresh timestamped log and receipt. Runner SHA256: `0064d4fa2cc957d81453b21b8b18ad23a52072c4569514d2afc542ec97f9f707`.

All four variables must be set. With none set this opt-in test skips; any partial configuration fails. R1/R2 labels must match the exact frozen source and stages `I6-compatible-R1` / `I6-isolated-candidate`; their filesystem layer lists must match. The R0 reference is the separately provenance-verified local `sub2api:2.8.13-monas-bps.13`; the test never pulls or deploys an image.

The proof executes this sequence on one disposable PostgreSQL 18 database owned by original TestMain:

1. Apply the exact, checksum-bound 297-file R0 baseline and seed synthetic users with distinct balances plus a native subscription with fractional counters/windows. Boot actual R0 and compare selected legacy financial rows/columns.
2. Replace the server with actual compatible R1. Its normal startup applies all current migrations; assert the quota schema and retain a byte-exact fingerprint of selected old financial columns. No current migration runner is called by the test at this step.
3. Replace with actual R2, issue/admission enabled. Use actual login, normal payment order HTTP, the real EasyPay HTTP client, and an egress-free synthetic provider. The provider validates the outgoing signature; the test requires one verified creation. Assert the order is pending with a frozen canonical snapshot, reject a snapshot UPDATE through the real immutability trigger, and modify/unpublish the plan.
4. Add a synthetic assignment-audit failure trigger. Submit a correctly signed HTTP callback through the actual server. Require HTTP 500, durable `paid_at`, failed order and zero paid rights; never fake an order into a paid state. R2 has recovery enabled as required by its admission configuration. Stop R2, then persist fresh Claude and original GPT `usage_known` receipts through real admission/codec/repository helpers. Both receipts are asserted known before R1 starts, so no R2 worker can consume them ahead of the rollback proof.
5. Remove only the synthetic fault and replace with R1, issue/admission disabled and recovery enabled, retaining the same database and latest migration records. Replay the signed HTTP callback. Require completed paid order with exactly one card/audit and original quota/duration; require one Claude debit/log and one original GPT debit. Ordinary/frozen user money and unrelated native subscription rows must remain exact.
6. Verify real generation HTTP 503 (`TERM_BILLING_UNAVAILABLE`), then replace/restart the same R1 again. After multiple background intervals, compare rights, orders, audit, billing receipts, dedup, usage logs and money fingerprints to prove no additional effects. Runtime replica heartbeat rows are intentionally excluded from that business-data fingerprint.

Isolation: random task container names, internal Docker network `10.247.192.0/28` (overlap fails), no published ports, read-only non-root application containers, existing TestMain-only PG/Redis identified by their exact ephemeral ports, synthetic credentials, no external HTTP or real payment/refund. Cleanup removes only this test's app/provider/network and detaches its connections. The synthetic provider never sends a callback or any outbound network request.

Scope limits: this proves actual image/database compatibility and recovery of receipts generated by real in-process admission/codec helpers. It does not claim real upstream generation, browser interaction, real processor sandbox acceptance, a production migration, or rollback to R0 after term receipts exist. Those are distinct gates. Provider signature tests in `environment-I4-provider/main_test.go` are also separate from the actual image run.

Observed final effects include one Claude debit of `0.50000000`, one account-side amount of `0.10000000`, and one each of its ledger, dedup and usage-log rows; the original GPT known receipt debits its original cycle by `0.25000000`. The paid order creates one card for its frozen `125.12345678` USD / 604800-second entitlement despite the changed/unpublished plan, with exactly one assignment audit. Ordinary/frozen user money and unrelated native windows remain exact; the final R1 restart preserves all protected business fingerprints.

Earlier failures remain preserved. `office-I6-rollback-receipt.json` and its original `office-runtime-20261020-v2/rollback.log` record the invalid test configuration with admission enabled and recovery disabled. The corrected attempts at `20261020T020609Z` and `20261020T020737Z` exposed a second fixture issue: feeding `nc` an immediately exhausted reader closed the connection before authentication completed, causing Redis operations to receive `context canceled` and the login limiter to fail closed. The helper now keeps its stdin pipe open until the complete HTTP response has been read. The `20261020T020715Z` attempt mistakenly started the previous driver while compilation was completing; its `driver_stable=false` receipt is explicitly invalid and failed, not acceptance. All timestamped receipts/logs are retained. No product behavior was changed or bypassed to make the proof pass.

Provider unit validation completed 2026-10-20 09:36:26 +08:

```sh
cd backend
CI=true GOMAXPROCS=3 go test -p 2 ../validation/claude-weekly-quota/environment-I4-provider/main.go ../validation/claude-weekly-quota/environment-I4-provider/main_test.go -count=1 -json
```

One top-level test plus six cases passed, zero failed/skipped, 0.596 s. Cases cover missing configured key, wrong method, invalid signature, missing order, oversized body and valid signature. Only the valid synthetic request increments the provider creation counter; the response never includes its signing key. Raw log `/tmp/claude-i6-provider-unit.jsonl` SHA256 `23f677a0ef445750d1349a8aaccc6c2e8bf122a79689372cf565d8b15bd09a4a`. This is not a substitute for running the final images.
