# I6 isolated PostgreSQL scaling experiment

> This receipt covers the earlier PostgreSQL18.1 experiment. The separately executed exact production-version18.6 full300k results are in I6-PG18.6-rehearsal-receipt.md; do not compare the two environments as a causal version improvement.

Evidence collection and the measured low-risk admission/backlog optimizations are complete. All correctness assertions passed. No quantitative latency/throughput SLO was specified, so this receipt does not invent a pass/fail threshold. It does establish a remaining history-dependent settlement cost and a measured original GPT throughput reduction under concurrent load; it cannot support a negligible-impact claim or unrestricted production capacity. The full ledger consistency check is deliberately retained.

## Before optimization

Initial run completed 2026-10-20 09:28:33 +08.

```sh
cd backend
CI=true GOMAXPROCS=3 go test -p 2 -tags=integration ./internal/repository -run '^TestSubscriptionQuotaPerformanceI6_' -count=1 -timeout=20m -json
```

The original TestMain creates disposable PostgreSQL 18.1 and Redis containers. No production connection, external provider, payments, or user data are involved. One test passed, zero failed/skipped; package elapsed 48.759 s. Each scale uses 96 fresh original GPT `UsageBillingRepository.Apply` commands alone, then 96 GPT and 96 Claude commands concurrently, two workers per lane and separate labelled connection pools. Throughput uses each lane's own completion time. Actual admission and durable receipt encoding prepare measured commands outside timed Apply. Twenty calls measure real `BillingBacklog`; independent advisory-lock probes and sampled `pg_stat_activity` observe contention. No deadlocks, failed commands, or ledger/counter disagreement occurred.

Cold historical rows copy a valid settled receipt's opaque payload to preserve width and SQL constraints; they are not receipt-replay correctness fixtures. Their request IDs, fingerprint fields and ledger event keys are distinct, and card used quota equals the sum of all ledger rows. Measured requests use freshly encoded real receipts. This uses the candidate's original GPT path as the control, not an R0 image comparison; one ordered run is insufficient for confidence intervals or a production SLO.

| Cold history | Actual request/ledger rows before measurement | DB bytes | Claude admission p95 ms | Claude Apply p95 ms | Backlog p95 ms | Same-scope wait p95 ms | GPT control/mixed requests/s | GPT control/mixed p95 ms |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 0 | 1 | 19,338,943 | 2.99 | 16.25 | 0.19 | 13.29 | 467.7 / 443.6 | 5.19 / 4.82 |
| 10,000 | 10,097 | 50,419,391 | 4.62 | 20.03 | 2.49 | 17.41 | 518.8 / 411.6 | 4.77 / 6.11 |
| 100,000 | 100,193 | 315,209,407 | 24.51 | 26.14 | 11.69 | 23.70 | 478.3 / 453.5 | 5.33 / 4.86 |
| 300,000 | 300,289 | 901,617,343 | 65.29 | 42.74 | 33.42 | 40.25 | 455.5 / 332.2 | 6.23 / 9.15 |

At 300k, the unrelated scope probe p95 remained 0.68 ms while same-scope p95 reached 40.25 ms. GPT throughput was 27.1% below its same-scale standalone control. The small subsecond GPT window and local Docker CPU/storage contention limit causal attribution, but cannot support a no-impact claim.

`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` identifies three history-dependent queries:

1. Admission reconciliation: `count(*) WHERE term_id=$1 AND (status='reconcile_required' OR last_error_category='ledger_mismatch')` scans 300,289 historical requests and takes 29.966 ms, reading 38,371 shared blocks. A predicate-matching partial index should exclude settled healthy history.
2. Backlog: aggregate FILTER expressions without an outer WHERE scan all requests, taking 34.553 ms / 38,092 read blocks. Restrict to unfinished requests or settled requests lacking a usage log, with a matching partial index; retain all existing counts and age semantics.
3. Apply: while holding scope, `used_usd=COALESCE((SELECT SUM(delta_used_usd) FROM subscription_quota_ledger WHERE term_id=$1),0)` scans all 300,289 ledger rows (12.297 ms). A bounded incremental check/transactionally maintained ledger summary plus independent full reconciliation needs a separate invariant design; simply removing the mismatch fail-closed check is not an acceptable optimization.

Full initial execution plans and raw Go JSON remain private in `/tmp/claude-i6-performance-before.jsonl` (SHA256 `81c9568e8eff8bda2893e039f89b9115e6eb590e68718a934a28a7f0f60b0f83`). Compact measurements and plan summaries are in `I6-performance-before-results.json`. Test source at this run: SHA256 `8689e5610b928866281e4ed9615af0f18e74eddf43e7c2a8685d5f78e0992159`.

## After predicate and index changes

The primary worker added migration 254 with predicate/covering indexes and a backlog WHERE clause. The performance test now EXPLAINs the actual shared production `subscriptionQuotaBacklogSQL` constant. No ledger summary or extra mutable accounting state was introduced, and Apply still performs the full ledger SUM under the scope lock.

Primary-worker rerun completed 2026-10-20 09:35:25 +08, package 44.778 s: four top-level tests passed, zero failed/skipped. In addition to this scaling experiment it ran `HistoryIndexesI6_PendingCountsRetainMeaning`, `HistoryIndexesI6_RecoversRealInvalidIndex`, and `HistoryIndexesI6_FailureRestoresSessionTimeouts`. The scaling test itself took 32.4 s. This receipt parses the actual completed JSON; it does not claim a separate rerun by its author.

| Cold history | Claude admission p95 ms | Claude Apply p95 ms | Backlog p95 ms | Same-scope wait p95 ms | GPT control/mixed requests/s | GPT control/mixed p95 ms |
|---:|---:|---:|---:|---:|---:|---:|
| 0 | 3.04 | 18.09 | 0.23 | 15.09 | 464.5 / 439.5 | 5.68 / 5.32 |
| 10,000 | 3.96 | 17.35 | 0.35 | 14.75 | 433.0 / 443.3 | 7.26 / 4.83 |
| 100,000 | 5.99 | 69.25 | 0.36 | 66.64 | 452.9 / 320.3 | 5.52 / 8.85 |
| 300,000 | 4.23 | 50.45 | 0.64 | 47.15 | 456.3 / 319.5 | 5.91 / 9.66 |

At 300k, reconciliation EXPLAIN fell to 0.016 ms / 2 hit blocks and backlog to 0.482 ms / 126 hit + 74 read blocks, confirming the two history scans were removed. Full ledger SUM remained 16.421 ms / 4,008 hit blocks. Same-scope wait p95 was 47.15 ms against 0.75 ms for the independent scope, and mixed Claude completion throughput was 58.9 requests/s. GPT throughput was 30.0% below its standalone control. The 100k tail was worse than 300k (69.25 ms vs 50.45 ms), illustrating local scheduling/cache noise and the need to retain measured results instead of fitting a false monotonic capacity curve. Zero deadlocks, command errors, or ledger/counter disagreement occurred.

These results support retaining the full consistency invariant rather than adding a second mutable accounting state merely to improve a local experiment. They do **not** remove the settlement bottleneck. The evidence supports this operational boundary:

- A 300k-history card with two settlement workers is a tested scale, not a certified production limit. Its approximately 59 completions/s under this synthetic mixed burst is an observation, not a safe configured arrival rate; independent production hardware, upstream request shape and other traffic can change it substantially.
- Scope locking serializes the same user's card. Sustained arrivals approaching that scope's measured service capacity can accumulate `usage_known` backlog even while admission itself remains fast. Do not infer unlimited concurrency from the now-fast admission query, and do not increase workers to solve a lock-held SUM bottleneck.
- Enable a limited set of groups first and compare each deployment's original GPT latency/throughput baseline, known-usage backlog age/count, reconciliation counts, settlement latency and PostgreSQL lock/I/O pressure. Expansion should use an explicit operational latency/error budget agreed for that deployment, since this task supplies no numeric SLO. A rising backlog or unacceptable GPT regression is a reason to stop new admission while keeping recovery available.
- If higher histories or sustained concurrent traffic exceed that deployment's budget, reproduce the workload on its hardware first. A ledger-summary/checkpoint redesign needs independent corruption/replay/fault-injection proof and a documented reconciliation invariant; silently dropping the SUM is not justified by this result. No historical ledger deletion or production tuning is performed by these tests.

Indexed raw Go JSON: `/tmp/claude-i6-performance-indexed-v2.jsonl`, SHA256 `b4abcac12839b592c06069bd7090a895c4f385c22284d55d730f7be2421ec87c`. Compact after-results and plan summaries: `I6-performance-indexed-results.json`.
