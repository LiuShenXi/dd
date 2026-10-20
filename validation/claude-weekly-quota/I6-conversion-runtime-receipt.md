# I6 actual-image controlled conversion receipt

Status: **PASS**, 2026-10-20 02:17:21–02:18:45 UTC. `TestSubscriptionQuotaI6RuntimeControlledConversion` passed in 77.92 seconds against two actual application containers, PostgreSQL, Redis and the synthetic provider. One top-level pass, zero failures, zero skips. Preparation and second-replica startup also passed independently with zero skips; real synthetic-admin login and the application's compliance GET/POST flow completed before the proof.

This result covers the frozen image below. It does **not** cover the subsequent `days` / `day` product-validation changes being prepared after this run. Those require their own validation and source-delta accounting.

| Artifact | Identity |
|---|---|
| R2 image | `sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483` |
| Image source label | `2ceb119eb44d2d8d9ba68e37e0cfe4646b51cc067628cc34175734607daaa55d` |
| Provider image | `sha256:2d20848a025bd39adeb1f732ba3b2216dc2f7502ffd9a7d61091008c3b718826` |
| Native test driver | `c4d79f3b55521a19b4d35ab7172c7321e780ae737bebe506bca447822c91dabb` |
| Conversion test source | `57e86d0083f1e527f9ced0d86bc4171784d2e766ed3a67877f6207f7057e01f1` |
| Independent V4 runner | `2e54af80cbd5352a42c019efb0b1834521b02288f55b05736102ed55a8fccf82` |
| Passing raw log | `6ed39c5e54b53937fa463dde492b37566e04fd274d5a5279d39958b6cd93b421` |

Machine records are `office-I6-conversion-v4-conversion-20261020T021721Z.json`, `office-I6-conversion-v4-observed.json`, `office-I6-conversion-v4-compliance.json`, and the V4 prepare/second/environment receipts. Raw logs and synthetic fixture credentials remain private under `/Users/shenxi/.codex/private/claude-weekly-quota/office-conversion-v4/`; no credentials appear in public receipts. Both driver and test-source hashes remained stable throughout the passing run. Images retain the original build labels from `office-I6-build-receipt.json`; only test assertions were corrected for this proof.

## Actual execution

The native driver was compiled separately from the main runtime driver. There were two scoped compilations, for V3 and then the corrected V4 proof; neither overwrote `repository-final.test`.

```sh
cd backend
GOMAXPROCS=3 go test -c -p=1 -tags=integration \
  -o /Users/shenxi/.codex/private/claude-weekly-quota/office/repository-conversion-final.test ./internal/repository
python3 /Users/shenxi/.codex/private/claude-weekly-quota/office/conversion-v4-runner.py prepare
python3 /Users/shenxi/.codex/private/claude-weekly-quota/office/conversion-v4-runner.py second
# Keep each loopback bridge in its own foreground session; then authenticate
# the synthetic admin and complete the real compliance GET/POST flow.
python3 /Users/shenxi/.codex/private/claude-weekly-quota/office/conversion-v4-runner.py conversion
```

The runner sets `CI=true`, loads exact image/provider/source inputs from the frozen build receipt, and invokes only the selected test with `-test.parallel=1 -test.timeout=10m`. The conversion command uses `-test.run=^TestSubscriptionQuotaI6RuntimeControlledConversion$`. The fresh V4 fixture uses internal network `cwq-env-i6-conversion-v4-20261020`, subnet `10.247.196.0/28`, and run ID `cwq-i4-4ccc74df858445fa`. The main fixture on `10.247.193.0/28` was untouched.

The complete test proved:

- Five admitted Claude requests and one queued pre-slot marker block preview. Cancelling the waiting request removes only its own marker and produces one no-charge result; the five admitted streams and asynchronous legacy settlement drain during maintenance.
- Explicit conversion creates exactly one `75.12345678` USD card. Replaying the same conversion idempotency key returns the existing result, and maintenance is then released through the real API.
- Real GPT HTTP and WebSocket streams hold their original leases across conversion. Both complete with `response.completed`; the two settled requests debit exactly their actual-cost sum from their original cycle. The original user-slot members and WebSocket ingress member survive conversion and stale-cache rejection.
- After both real auth-invalidation passes and the real L1 TTL, injecting an authentic old windows-mode L2 snapshot produces `/v1/usage` mode `quota_limited`. Generation then returns HTTP 503 with the generic unavailable message and a decoded application-log error identifying the protected-source mismatch. A calibrated, fenced Redis MONITOR observes zero legacy acquire/wait attempts, and provider call count is unchanged.
- Once the stale L1 expires, the same key succeeds and the new card records exactly `0.00060000` USD usage.

Independent post-test database and provider inspection, before cleanup, confirmed:

| Observation | Actual value |
|---|---|
| Converted group | `term`, `active` |
| New card quota / usage | `75.12345678` / `0.00060000` |
| Protected completed / no-charge / pending | `5` / `1` / `0` |
| Legacy / term usage logs | `5` / `1` |
| Ordinary balance after the five legacy requests | `77.12700000` |
| Settled GPT requests / cost sum | `2` / `0.00003500` |
| Original GPT cycle remaining | `549.99996500` |
| Provider total / active / cancelled | `8` / `0` / `0` |

## Preserved failures and fixture corrections

The main fixture's original conversion attempt remains a failure in `office-I6-conversion-receipt.json`. Its private raw log SHA256 remains `7c1318ad47f0bbf3751fcc5c4ebe26a95a750733f87761da811eca1c9d78cf63`. The test waited only 15 seconds for an empty auth outbox, but production deliberately retains that row for a second invalidation after `authInvalidationSafetyDelay=30s`. The assertion now waits 45 seconds, covering that real delay and worker polling; production behavior is unchanged.

The independent V3 attempt remains a failure in `office-I6-conversion-v3-conversion-20261020T021317Z.json`, raw log SHA256 `3e5b405458841e8a6442784f178dd7946486dc4185709524d077b95a2b0401fb`. It passed the corrected outbox wait and real stale-cache HTTP rejection, then compared an unescaped error string with raw JSON logs. The actual error was present with JSON-escaped quotes. The assertion now decodes each structured log's `error` field before comparing it. This changes evidence parsing only; it does not weaken the required error reason or HTTP/Redis/provider assertions. A completely fresh V4 fixture reran the entire conversion proof after this correction.

Both V3 and V4 were cleaned by exact task ownership checks, preserving their private records and public receipts. V4 cleanup removed its five containers, its PostgreSQL volume, its internal network and its two foreground bridges; application logs were captured and hashed before removal. `office-I6-conversion-v4-cleanup.json` records all identities and `main_fixture_modified=false`. No production data, external processor, live upstream or unrelated runtime fixture was used or modified.
