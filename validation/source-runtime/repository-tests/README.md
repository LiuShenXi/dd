# Isolated repository integration runner

This runner executes the real `backend/internal/repository` integration package against a new empty synthetic PostgreSQL 18 database and a new empty Redis. It must never connect to the copied `carpool_test` database, the existing `carpool-db` alias, host databases, or application Redis.

## Safety model

- Required existing network: `carpool-v13-test-net`, verified as Docker `Internal=true`.
- PostgreSQL container: `carpool-v13-integration-postgres`, alias `carpool-integration-db`, image `postgres:18-alpine`.
- Redis container and alias: `carpool-v13-integration-redis`, image `redis:8-alpine`.
- Ephemeral runner: `carpool-v13-integration-runner`, image `alpine:3.20`.
- Fresh PostgreSQL volume: `carpool-v13-integration-pgdata`, mounted at the PostgreSQL 18 data root `/var/lib/postgresql`.
- Fixed synthetic database: `sub2api_carpool_integration_20260905`.
- Every resource is labeled `com.codex.local-task=09-05-sub2api-carpool-v1-3` and `com.codex.test-role=repository-tests`.
- No container publishes a host port. The runner has no Docker socket or host bind mount, drops all capabilities, and uses a read-only root filesystem. The host streams only the static test binary into an executable runner tmpfs before invoking it as UID/GID 65534.

The script refuses to start if any exact container, volume, or active manifest already exists. Before creating resources, it records a private active-run manifest and updates it immediately after each container ID is captured. Each successful invocation creates a fresh volume and containers, then removes only resources whose captured IDs, task/role labels, and run ID still match. It never reuses or clears an existing database or Redis. A random local password exists only in access-restricted per-run env files and is deleted during cleanup; it is never printed or committed.

## Harness mechanism

`prepare-overlay.go` parses the current `integration_harness_test.go` with Go AST, removes exactly its one Docker-starting `TestMain`, and removes only imports dedicated to that function. All original transaction, Ent, Redis namespace, cleanup, and assertion helpers remain unchanged.

The Go overlay adds `source_runtime_testmain_test.go` as a virtual repository test file. Its replacement `TestMain` requires the exact synthetic DNS aliases and database name, verifies PostgreSQL has zero public tables and Redis DB 0 has zero keys, calls the real `ApplyMigrations`, initializes UTC, Ent and Redis, and then calls `m.Run()` without skipping tests. Any setup failure exits nonzero.

Compilation uses the repository's pinned Go 1.27 module with `GOOS=linux`, `GOARCH=amd64`, and `CGO_ENABLED=0`. Generated overlays, binaries, logs, and summaries stay under:

```text
C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905/repository-tests/runs/<run-id>/
```

## Commands

Run the harness-only smoke after confirming no other E2 run owns the exact resources:

```powershell
pwsh -NoProfile -File validation/source-runtime/repository-tests/run-repository-tests.ps1 `
  -RunPattern '^TestSourceRuntimeHarnessIsolation$'
```

For business tests, obtain the exact current test names from the core/gateway/reset owner and pass a bounded regex. Do not use a broad pattern as proof that specific carpool tests ran. Example shape:

```powershell
pwsh -NoProfile -File validation/source-runtime/repository-tests/run-repository-tests.ps1 `
  -RunPattern '^(TestExactCarpoolRepositoryCaseOne|TestExactCarpoolRepositoryCaseTwo)$'
```

The command compiles the entire repository integration package before starting resources, so compile errors are reported without creating a database. It then stores test stdout, stderr, and `summary.json` privately. A zero exit is accepted only when verbose output proves at least one test actually started, so a stale or misspelled regex cannot produce false-positive evidence. The script always attempts bounded cleanup in `finally`.

## Current review evidence

- Both PowerShell files parse with zero errors, and the overlay generator unit test passes with the pinned Go 1.27 toolchain.
- A reviewed source snapshot cross-compiled the Linux repository integration binary successfully: 118199338 bytes, SHA-256 `BA0C83B12849EAEE5F880BD3F3404C4CC30ED4F2C0DA8CE0CDFE749236A88F2E`.
- The first coordinated resource start exposed that PowerShell rejected Redis's required empty `--save` argument before launching Redis. The process wrappers now explicitly allow empty strings, and a child-process probe confirmed the argument remains a distinct empty argv element. That interrupted partial start left no task container, volume, or active manifest, exercising the manifest-bound `finally` cleanup path.
- The next coordinated attempt stopped at the compile-before-resources gate because the concurrent application source referenced a missing `CarpoolRepository.GetPlanByCode`. It created no Docker resource and left no active manifest.
- After that application mismatch was fixed, real isolated starts established that Docker Desktop could not expose the ACL-restricted private binary through either a file or directory bind. No test started. The runner now has no host mount: it starts with a read-only root and executable `/work` tmpfs, then receives the binary through `docker exec --interactive` standard input. A bounded probe verified the resulting file as `nobody:nobody` mode `0500` and executed it. Each failed launch still cleaned all three exact containers, the fresh volume, secret env files, and active manifest.
- Focused run `20260905T175942Z-0a86f184` passed all 10 names requested at dispatch time against real isolated PostgreSQL and Redis. Its summary records exit code 0 and 10 verbose run events; the executed binary SHA-256 is `24ABA1E32032345F2D895381886FE27840DDB88266E268CE3080FE1276BBD007`. Post-run inspection found all three containers, the PostgreSQL volume, active manifest, and both secret env files absent.

Two additional reconciliation tests appeared in the concurrent source after that run and are not covered by its evidence. At the current source names, a follow-up run covering the harness and all 11 Carpool repository tests should use this exact anchored pattern:

```powershell
pwsh -NoProfile -File validation/source-runtime/repository-tests/run-repository-tests.ps1 `
  -RunPattern '^(TestSourceRuntimeHarnessIsolation|TestCarpoolBillingReconciliation_(ActualCostIsAtomicAndAuditable|NoCostCreatesNoUsageLedger|ReplayAndResolvedConflicts|RollsBackDedupDebitAndOperation|ParallelResolutionsApplyOnce|ParallelSameKeyReplaysExactResponse|FinalOperationFailureRollsBackAllEffects|DelayedKnownReceiptCannotOverwriteManualAmount)|TestUsageBillingRepositoryApply_Carpool(ConcurrentSettlementAppliesOnce|ReplayDoesNotDebitAgain|FailureRollsBackAllEffects))$'
```

Only after an interrupted process leaves `active-run.json`, inspect the recorded IDs and use:

```powershell
pwsh -NoProfile -File validation/source-runtime/repository-tests/cleanup-repository-tests.ps1
```

The cleanup script refuses unmanifested, relabeled, renamed, externally networked, host-published, or Docker-socket-mounted resources. It never targets the copied-runtime containers or volumes.
