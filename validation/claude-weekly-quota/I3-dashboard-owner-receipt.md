# I3 dashboard publication and retention owner receipt

Owner: sol_contract_review. This is a separate slice from the earlier gateway/pricing receipt. No production API/database/model calls and no commit.

The combined owner regression `I3-owner-final-post-dashboard-windows-unit.jsonl` additionally passed 142 top-level / 268 including subtests across service, handler, server/routes and repository, zero fail/skip. Its 35-file hashes are historical; the current final 37-file owner freeze (including subsequent T22/T29 tests and sender type scalar) is `I3-owner-final-pre-F1-source-hashes.json`. Dashboard source did not change after its recorded tests. Owner code is frozen pending the root's I3 acceptance and the environment owner's actual PostgreSQL tests.

The final scheduled batch DELETE uses tableoid+ctid at victim selection and deletion, with the acknowledgement predicate repeated at DELETE. It therefore cannot remove an unacknowledged term row through an equal ctid in another partition. The environment owner's real-partition test is required separately from the SQL-mock evidence.

`I3-dashboard-owner-windows-unit.jsonl` is an actual Go 1.27.0 Windows run: 17 top-level / 21 including subtests, zero fail/skip, repository package passed. It includes the existing dashboard/group-rollup tests and the new protected-retention, writer-order and historical-publication tests. `I3-dashboard-owner-source-hashes.json` identifies the four owner files. PostgreSQL durability/concurrency results belong to the environment owner's separately recorded execution; no PG result is inferred from these SQL-mock tests.

Both scheduled usage-log batch-delete implementations now use the existing term acknowledgement predicate. An unacknowledged term log cannot be selected. Old partition retention keeps its original DROP fast path for an unprotected month. Under the existing group-rollup lock it first checks immutable admitted-at requests in that UTC month: admitted/usage_known/reconcile_required, or settled but unacknowledged, as well as unacknowledged term logs. A protected month is skipped whole; checking requests also protects known-to-log publication before a raw row exists. Original 222/223 late-insert invalidation triggers are unchanged.

First term log INSERT and its historical hour/day contribution are one transaction. The main owner's writer takes the dashboard writer lock before the financial scope/parent/term/request locks and INSERT; the INSERT's group-rollup trigger therefore cannot invert the dashboard/group order. Only inserted=true calls the helper. INSERT conflict/replay and already-acknowledged rows do not add twice. The helper uses exact inserted NUMERIC columns, including independent account-statistics cost and its multiplier. Hourly and daily user sets use INSERT ON CONFLICT DO NOTHING; only a newly inserted identity increments active_users.

An earlier published watermark or an existing hourly/daily bucket proves historical publication. The helper appends the known contribution and preserves retained base values; a missing bucket contains only the known increment and does not claim reconstruction of purged source. A recent unpublished bucket follows normal scheduling. A recent already-published bucket may receive the immediate increment; a later normal lookback rebuild from its complete raw source yields the same total. No new queue, migration, detached retry or acknowledgement-time range rebuild is used.

AggregateRange and RecomputeRange acquire the same transaction-level dashboard writer lock before their source snapshot. Aggregate retention takes it across all four bucket/user-set deletions, preventing partial cleanup from interleaving active-user publication. Ordinary GPT log INSERT and financial Apply do not acquire this lock. Main's live/recovery publication remains retryable by its durable unacknowledged request when log INSERT or its transaction fails; financial settlement remains authoritative and independently durable.

The root explicitly retained the existing limitation of a user-requested RecomputeRange over an old interval whose raw source has already been purged: that explicit rebuild can reconstruct only remaining source. This change does not introduce sealed buckets or synthetic base offsets, and does not advertise an incomplete historical rebuild as complete. Ledger/requests remain authoritative. Normal scheduled aggregation uses its current watermark/lookback; the environment owner must separately prove that concurrent normal scheduling and old term publication preserve the contribution.

Exact unit command:

```text
go test -tags=unit -json -count=1 -timeout=120s -run '^Test(SubscriptionQuotaDashboard|DashboardAggregationRepository)' ./internal/repository
```
