# F2 actual image upgrade / rollback receipt

Status: **PASS**, 2026-10-20 02:23:59–02:24:12 UTC. `TestSubscriptionQuotaI6ImageUpgradeAndRollback` passed in 9.57 seconds against the actual F2 images. One top-level pass, zero failures, zero skips; runner elapsed 12.69 seconds.

The authoritative input was the separate `office-I6-build-F2.json`, SHA256 `46aa55cc9a3f47ed2910cf3465d0d0b538b87c2707f2fd650b5aacb7a6287306`. This run did not replace or edit canonical F1 build/validation receipts.

| Artifact | Verified identity |
|---|---|
| F2 R2 | `sha256:3e18edda6b675cb73d108c92acf139dfe1f6e45b92e5abc8aef1594d2eec39f3` |
| F2 compatible R1 | `sha256:9940143ccd871a797893e37b685d211996ce4b75e930747f84d7a099be9e6dc8` |
| F2 source label | `56f189a7e2c4c32a96ea439d15687e809f9839670f63181e5312851642b757a4` |
| F2 synthetic provider | `sha256:628bb0e61febcd1f5fd94c0ae89bf3f5f3e24fe8f25c3a6019a5f98b4af7893f` |
| Pre-existing verified R0 | `sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677` |
| Unchanged corrected native driver, reused from F1 | `f73265ba2226adc1a0b41894513477b041528fe3423eac76d02ea77d24799abd` |
| Unchanged upgrade/rollback test source | `c1ba713a83b178c23c0c18af129fb409710b07a58d04603c40a3ac579e7d6dde` |
| F2-specific runner | `033be88b75d5cae0bb699fcde64f67c220f53f43034aa484c586066c9a8481a3` |

Machine execution receipt: `office-F2-rollback-20261020T022359Z.json`. Private raw log: `/Users/shenxi/.codex/private/claude-weekly-quota/office-f2-rollback-20261020T022359Z/rollback.log`, SHA256 `47682cee7fdbd5fe8f674610f3d8279cbfae71b10150044044fa2fb1e8ff7906`. Driver, test source, F2 input receipt and canonical F1 input receipt remained unchanged during execution.

Actual command:

```sh
python3 /Users/shenxi/.codex/private/claude-weekly-quota/office/rollback-f2-runner.py \
  --build-receipt /Users/shenxi/Desktop/WORK-SPACE/dd/validation/claude-weekly-quota/office-I6-build-F2.json
```

The runner sets `CI=true`, verifies the exact image IDs and source/stage labels, confirms the isolated proof subnet is unused, then invokes the existing driver with `-test.v -test.parallel=1 -test.timeout=15m -test.run=^TestSubscriptionQuotaI6ImageUpgradeAndRollback$`. No Go compilation or backend/frontend source edit was performed for this F2 proof. The native driver includes the already-validated F1 fixture helpers; the actual application containers running the tested product behavior are F2. This receipt does not represent a new full Go unit/repository regression run.

The complete sequence is the same corrected actual-image proof documented in `I6-image-proof-receipt.md`: verified R0 baseline with 297 migrations → actual compatible F2 R1 startup applying current migrations → actual F2 R2 with enabled admission/issue/recovery → correctly signed real HTTP payment callback failing after durable payment but before rights issuance → stop R2 and seed known Claude/GPT receipts → actual F2 R1 with admission/issue disabled and recovery enabled, preserving the latest database → signed callback replay and receipt recovery → another F2 R1 restart proving no duplicate effects.

Assertions require the frozen paid entitlement of `125.12345678` USD / 604800 seconds despite a modified/unpublished plan; one rights card and assignment audit; snapshot UPDATE rejected by the real database trigger; Claude debit `0.50000000`, original GPT-cycle debit `0.25000000`, and no extra effects after restart. Selected legacy money/window fingerprints remain exact. Disabled admission returns the real HTTP 503 `TERM_BILLING_UNAVAILABLE` response. R1/R2 filesystem layers are verified equal. No real payment or external upstream is used.

Isolation used the existing corrected test's unused internal subnet `10.247.192.0/28`, with fresh task-owned application/provider containers and disposable TestMain PostgreSQL/Redis. The coordinator explicitly retained this subnet to reuse the unchanged driver. The main long-stream fixture on `10.247.193.0/28` and the independent browser database were untouched.

Post-run verification in `office-F2-rollback-cleanup-20261020T022359Z.json` found zero proof containers and no network occupying the proof subnet, while every pre-existing network remained present. Private logs remain available. Canonical F1 build receipt SHA256 remained `01b892385a18bd69aa3df0425ddac7198c542bfcac902b1ffe8e10fcffdd5da9`; earlier failed and successful F1 evidence was preserved.

Scope: this proves F2 actual-image upgrade compatibility, paid-order fulfillment recovery, known Claude/GPT receipt recovery and disabled-admission behavior after rollback. F1 runtime conversion, concurrency, browser and long-stream records remain separately identified by their own image/source identities and are not relabelled as F2.
