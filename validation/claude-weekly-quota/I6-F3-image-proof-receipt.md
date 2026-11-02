# F3 actual image upgrade / rollback receipt

Status: **PASS**, 2026-10-20 02:37:09–02:37:22 UTC. `TestSubscriptionQuotaI6ImageUpgradeAndRollback` passed in 9.78 seconds against actual F3 server images. One top-level pass, zero failures, zero skips; runner elapsed 12.63 seconds.

| Input | Verified identity |
|---|---|
| Build receipt | `office-I6-build-F3.json`, SHA256 `e4823e4abc6e065647f2b8a52aedd95bdfc12a24e1fa68cd40b093a8b437194c` |
| F3 R2 image | `sha256:3c491e7a7033a72206fa30cf8d4895a7678908a6781912a66e16cedcbbe435bf` |
| F3 compatible R1 image | `sha256:677a6f41dc672b1658c30e6c048bddfceab3eb770a5ac120bd5aa79ef4c1e092` |
| F3 source label | `d6ac92adeb97249c1a792e2586c358f63da20790283c5481fee6baafa8cae1b8` |
| Synthetic provider image | `sha256:f85f5e8b5b5d5929df9753cd9aca20e59ce96c65a0efe0334e99b8312d383044` |
| Unchanged corrected native driver, reused from F1 | `f73265ba2226adc1a0b41894513477b041528fe3423eac76d02ea77d24799abd` |
| Unchanged upgrade/rollback test source | `c1ba713a83b178c23c0c18af129fb409710b07a58d04603c40a3ac579e7d6dde` |
| F3-specific runner | `bfcfc4cd434c3493867694441136d3710741c0c66ad86b4384c57ac5944296f5` |

Machine execution receipt: `office-F3-rollback-20261020T023709Z.json`. Raw log: `/Users/shenxi/.codex/private/claude-weekly-quota/office-f3-rollback-20261020T023709Z/rollback.log`, SHA256 `68466c864703240cf0df9cc6c1a51e66b69be84f70fde4c9ebdb55ae34ca2bae`.

Actual command:

```sh
python3 /Users/shenxi/.codex/private/claude-weekly-quota/office/rollback-f3-runner.py \
  --build-receipt /Users/shenxi/Desktop/WORK-SPACE/dd/validation/claude-weekly-quota/office-I6-build-F3.json
```

The runner explicitly identifies this as F3, checks image IDs/source/stage labels, and supplies F3 inputs to the unchanged corrected native driver. It sets `CI=true` and invokes only `^TestSubscriptionQuotaI6ImageUpgradeAndRollback$`, with `-test.parallel=1 -test.timeout=15m`. Driver, test source and input receipt hashes remained stable. No compilation or backend/frontend edits were performed in this run.

The complete corrected sequence from `I6-image-proof-receipt.md` ran again: verified R0 baseline → actual F3 compatible R1 startup and migration → actual F3 R2 enabled → correctly signed HTTP callback with durable payment and injected rights failure → stop R2, then seed known Claude/GPT receipts → actual F3 R1 with admission/issue disabled and recovery enabled, retaining the latest database → callback replay and receipt recovery → another F3 R1 restart with stable protected business fingerprints.

The assertions require immutable payment snapshot enforcement and one frozen `125.12345678` USD / 604800-second entitlement despite the changed/unpublished plan, exactly one rights card/audit, Claude debit `0.50000000`, original GPT-cycle debit `0.25000000`, no repeated effects after restart, and real HTTP 503 `TERM_BILLING_UNAVAILABLE` while admission is disabled. R1/R2 filesystem layers must match. The PostgreSQL harness in this run is 18.1; separate PostgreSQL 18.6 provenance/rehearsal evidence must not be inferred from this image test.

The test used fresh disposable resources on the available internal proof subnet `10.247.192.0/28`, with no production rows or external processor/upstream. `office-F3-rollback-cleanup-20261020T023709Z.json` verifies zero remaining proof containers/networks and all pre-existing networks still present. Main `10.247.193.0/28`, browser database and canonical F1 receipt were untouched; canonical F1 SHA256 remains `01b892385a18bd69aa3df0425ddac7198c542bfcac902b1ffe8e10fcffdd5da9`. Previous F1/F2 evidence remains preserved under its original identities.

Scope: this is actual F3 image upgrade/rollback and recovery evidence, using the already-validated F1 fixture helpers. It is not a new full F3 Go regression run and does not relabel earlier F1 runtime, browser, concurrency or long-stream tests as F3.
