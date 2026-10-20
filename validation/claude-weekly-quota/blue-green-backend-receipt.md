# Backend blue-green coexistence receipt

Date: 2026-10-20. Scope: additional release protections after I5/I6, based on the
live-deployment request. This receipt does not claim production rollout success;
production operations and the isolated nginx/image proof have separate owners.

## Implemented behavior

- Explicit `GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE=preserve` prevents the entire
  legacy startup cleanup (including the one-time wait sweep) from removing
  another process's account/user slots and wait counts. Existing score/TTL
  expiry remains intact. Default `exclusive` preserves the single-process path.
- The same preserve mode skips backup startup recovery. A newly started process
  cannot infer that another process's persisted running backup/restore crashed,
  mark it failed, or delete its uploaded objects.
- `GATEWAY_BACKGROUND_JOB_MODE=coordinated` and
  `GATEWAY_BACKGROUND_JOBS_START_HELD=true` introduce an explicit held candidate
  for scheduled account tests, channel monitor probes, batch image TTL cleanup,
  CN provider balance/quota checks, and scheduled backups (including upload and
  retention cleanup). Default `legacy` retains normal startup.
- An authenticated and audited exact-instance activation endpoint
  `/api/v1/admin/release/background/activate` acquires a single PostgreSQL
  session advisory lock. It owns the entire autonomous-job role, including all
  accepted work during drain, rather than serializing only individual probes.
- Release status reports `background_jobs.mode/state/active_tasks/instance_id/leader`.
  A normal drain freezes claims and lets accepted work finish. Pool-queued work
  is counted. The last completed task releases a held role. Cancel restores the
  prior state and cannot accidentally activate a previously held candidate.
- Lock/resume also require zero tracked autonomous tasks. Ordinary SIGTERM
  cleanup holds the producers before stopping dependent services. Existing
  usage/billing recovery, paid fulfillment, and invalidation workers retain
  their original recovery protocols and continue during coexistence.
- PostgreSQL acquisition/release has a three-second bound and no second-backend
  election fallback. Uncertain results discard the physical session. Connection
  loss holds new claims and cancels job contexts; the heartbeat runs every two
  seconds with a two-second check timeout. Normal channel-monitor Stop still
  waits for accepted probes to finish, preserving prior single-instance behavior.

## Verification

Passed commands:

```text
GOMAXPROCS=3 go test -p=1 -ldflags='-s -w' ./internal/pkg/backgroundjobs ./internal/pkg/releasedrain ./internal/config -run 'Test(Gate|PostgresLeadership|Release|BlueGreen)' -count=1
GOMAXPROCS=3 go test -p=1 -tags=unit -ldflags='-s -w' ./internal/service ./internal/server ./internal/server/middleware -run 'Test(ProvideConcurrencyPreserves|AutonomousWorkers|Release|CleanupStaleProcessSlots|ChannelMonitorRunner|CNProviderBalance|ScheduledTestRunner|BatchImageCleanup|RecoverStaleRecords|BackupService_(PreserveStartup|RecoverStaleRecords))' -count=1
GOMAXPROCS=3 go test -p=1 -tags=unit -ldflags='-s -w' ./internal/service -run 'Test(ProvideConcurrencyPreserves|AutonomousWorkers|Schedule_|Unschedule_|RunOne_|Start_LoadsAllEnabledMonitors|Start_SkipsDecryptFailedMonitor|Stop_DrainsAllGoroutines|Stop_WaitsForInFlightCheck|InFlight_|CNProviderBalance|BatchImageCleanup|RecoverStaleRecords|BackupService_(PreserveStartup|RecoverStaleRecords|RunScheduledBackup_LeaderElection))' -count=1 -json
GOMAXPROCS=3 go test -race -p=1 ./internal/pkg/backgroundjobs ./internal/pkg/releasedrain -count=1
GOMAXPROCS=3 go run -p=1 github.com/google/wire/cmd/wire ./cmd/server
```

The expanded service regression passed **31 top-level cases, 53 test events
including subcases, 0 failures and 0 skips**. Private raw log:
`office/bluegreen-backend-service-tests.jsonl`. This includes existing monitor
natural completion/cancellation, CN probes, batch cleanup, and old backup
recovery, plus the new held-worker and preserve-mode cases. The server command
passed authenticated exact-instance activation and existing full-handler SSE/WS
release tests; its middleware package had no matching tests in that selection.
The race run passed both new concurrent gate logic and release-controller tests.
Wire regenerated the checked-in dependency graph successfully.

An initial service compile attempt referenced a helper defined only under the
`unit` tag. The new regression was corrected to use a self-contained stub, and
the actual existing service regressions were rerun with `-tags=unit`. No failed
compile is counted as passed evidence.

Real PostgreSQL/Redis verification **passed** with the original disposable
PostgreSQL 18.1 / Redis 8.4 TestMain (not relabeled as PostgreSQL 18.6):

```text
CI=true GOMAXPROCS=3 go test -p=1 -tags=integration -ldflags='-s -w' ./internal/repository -run 'TestBackgroundJobLeadershipRealPostgresHandoffAndLoss|TestConcurrencyCacheSuite/TestBlueGreenPreserveStartupKeepsLiveOwnersAndWaits' -count=1 -json
```

Two top-level cases plus one suite subcase passed, with zero failures/skips in
6.338 seconds. The PostgreSQL case held the role across drain, rejected the
other process until the final task finished, transferred leadership, and then
terminated the actual owner session to verify cancellation and held state. The
Redis case used real Lua/Redis data with live blue account/user/WS owners,
indexed and unindexed waits, and a missing one-time sweep marker; green startup
preserved every value and could not acquire a full account/user scope. Private
log: `office/bluegreen-backend-pg-redis-tests.jsonl`.

The final scheduled-backup integration into the same role passed a separate
focused service/server run on the final source:

```text
GOMAXPROCS=3 go test -p=1 -tags=unit -ldflags='-s -w' ./internal/service ./internal/server -run 'Test(AutonomousWorkers|Release|BackupService_(ScheduledRole|PreserveStartup|RecoverStaleRecords|RunScheduledBackup_LeaderElection))' -count=1 -json
```

**17 top-level cases, 31 test events including subcases, 0 failures and 0 skips**;
service 1.406 seconds and server 0.488 seconds. A blocked scheduled dump stayed
counted through hold, was not cancelled, and completed upload/retention before
the counter became zero. Held cron reached no dependency. The same final source
passed the release authentication/identity, SSE/async-usage, and upgraded-WS
cases. Wire was regenerated again after the fifth producer was added. No Go
work remains running from this backend task.

Private raw-log SHA-256 values:

| Log under private `office/` | SHA-256 |
| --- | --- |
| `bluegreen-backend-service-tests.jsonl` | `b837427ec6be22c2a6869e8902cd6159eb9b80377fdcfcb7a49081f09d2c7fe0` |
| `bluegreen-backend-pg-redis-tests.jsonl` | `eadeb1d5fea3cb095495e6343017e67048d93536028e98f7b5cab1e51b5ddbde` |
| `bluegreen-backend-backup-final-tests.jsonl` | `93ebb6b9d597908bb0da2ad2ffaf142a3c6e64cbf3b0395f2834c52edb9a132b` |

The above suites overlap; their case counts must not be added together as a
single set of unique acceptance cases. Exact-image/nginx/runtime acceptance is
separate evidence and is not inferred from these package tests.

## Deliberate limits and retirement evidence

This change is not a general HA certification. The five autonomous producers
are the new role's exact scope. The still-running R0 does not know the role lock,
so first activation is allowed only after R0 retirement and a fresh inventory
showing the uncoordinated producer conditions are safe, including disabled
`backup_schedule.enabled` on R0. Never restart unpatched
R0 alongside live newer-process slots; preserve-mode rollback or a full drain
is required after R0 has stopped.

The release counters do not include every detached operation. Before retiring
**any** version, also establish the relevant extra work is absent or complete:

- Async image records are Redis `image_task:*`, private JSON with status
  `processing/completed/failed`. After old ingress stops and old HTTP count is
  zero, a private SCAN/parse must show no processing records. Do not print result
  payloads/owner identities, invent a process owner, expire a record manually,
  or equate age with successful completion. A global count can conservatively
  wait for new-instance image work too.
- Batch images have `batch_image_jobs` status plus configurable Redis ready,
  delayed and active queues (defaults `batch_image:queue:{ready,delayed,active}`).
  Current empty-job proof must be refreshed near retirement.
- Persisted `backup_records` must have no `status=running` or
  `restore_status=running`. Their detached goroutines are outside usage counts.
- No generic `uncoordinated_probes=0` metric exists. Establish the individual
  facts: enabled scheduled-test plans, enabled active-v1 monitors, batch jobs,
  active CN platform targets (`kimi/deepseek/zhipu/minimax`), and the
  `backup_schedule` setting’s `enabled` field. CN polling is
  enabled by default every ten minutes. Management must not enable or trigger
  producers during the maintenance observation window.

Plugin startup reconciles per-instance runtimes and does not sweep the shared
root. Shared plugin installation/upgrade/deletion must remain frozen during
coexistence; enabled plugin health/artifacts need independent verification.
BPS opaque storage has no startup deletion, uses content hashes and atomic
rename, and must remain shared so prior conversation references remain valid.
Its local-only budget mutex is not a distributed storage quota guarantee.

Old HTTP/SSE/WS connections must finish before SIGTERM. The host controller must
establish the old nginx worker/connection drain before closing old backend
admission, then check all local counts and these extra work conditions. No
finite forced-kill deadline can also promise uninterrupted indefinitely active
WebSocket sessions. PostgreSQL loss cancellation cannot prove an uncertain
external provider call had no side effect.
