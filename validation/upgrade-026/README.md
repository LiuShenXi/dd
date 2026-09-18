# Local 0.2.6 compatibility validation

This directory validates the candidate against synthetic local resources only.
It does not read production connection files or connect to production services.
The source baseline, decisions and rollout boundaries are in
[`docs/UPGRADE_0_2_6.md`](../../docs/UPGRADE_0_2_6.md).

## Repository integration suite

`repository-tests/run-repository-tests.ps1` is the tested Windows/Docker Desktop
entrypoint. It is pinned to this checkout, the locally cached Go 1.27 toolchain,
and the local Docker named pipe. It refuses existing containers, volumes or an
unfinished run manifest instead of overwriting them. Inspect those conditions
before any cleanup; the script's `finally` cleanup removes only its recorded
container IDs and matching run labels.

The dedicated `sub2api-upgrade026-test-net` network must exist and be internal:

```powershell
docker network inspect sub2api-upgrade026-test-net
# Only when absent:
docker network create --internal sub2api-upgrade026-test-net
```

The local image cache must contain `postgres:18-alpine`, `redis:8-alpine`, and
`alpine:3.20`. The runner does not pull images. No database or Redis host port is
published. Each run creates a new database volume, verifies empty initial
state, runs the real repository tests, and removes the owned test resources.

From this checkout:

```powershell
pwsh -NoProfile -File validation/upgrade-026/repository-tests/run-repository-tests.ps1 -RunPattern '^(TestSourceRuntimeHarnessIsolation|TestGroupModelAllowlistCompatibility_.*|TestUpgrade026ProductionMigrations_.*|Test.*Carpool.*|Test.*WithdrawAll.*|TestUsageSummary.*|Test.*GroupUsage.*)$'
```

The compiler overlay replaces only TestMain to attach to the disposable
services. The AST overlay generator has its own regression test and leaves all
repository test cases, production code and SQL intact. `timetzdata` makes the
Go test binary independent of the minimal runner image's timezone files.

The successful run `20260918T155600Z-09e0022e` contains 100 top-level / 180 total
passing tests, no failed or skipped tests. The exact production migration
manifest contains filenames/checksums only; its test proves 291 → 297 migrations
with synthetic financial records, old/new writer compatibility and replay.

## Unit and frontend checks

From `backend`, with Git's `bin` on this process's PATH for the upstream shell
fixtures:

```powershell
$env:PATH = 'C:\Program Files\Git\bin;' + $env:PATH
$env:CGO_ENABLED = '0'
go test -p 4 -tags=unit ./...
go vet -p 4 ./...
```

From `frontend`:

```powershell
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint:check
pnpm test:run
pnpm build
```

Final unit evidence records 20147 passing test results including subtests and
19 conditional skips, with no failures. The frontend has 310 passing files /
2361 passing tests. Skipped external-service tests are not runtime acceptance.

## Full application runtime and evidence

`runtime/` builds the Linux embed candidate and runs an independent fresh
PostgreSQL/Redis/application/mock stack. The app's network is internal; only
the Nginx ingress publishes `127.0.0.1:38626`. All business data is synthetic.
The runtime script records source input hashes so a binary built from a dirty
tree is not mistaken for an exact clean-commit build.

Public summaries and source hashes are under `runtime/evidence/`. Passwords,
tokens, binaries and raw runtime logs stay under
`%LOCALAPPDATA%\Codex\PrivateTests\sub2api-upgrade-026-20260918` outside Git.
Larger test logs and independent audit reports are in
`C:\WORK-SPACE\sub2api-upgrade-0.2.6-evidence-20260918`.

`evidence/` retains compact final unit/PostgreSQL summaries, immutable migration
counts and the independent backend cross-review. Detailed logs remain external
to avoid committing tens of megabytes of duplicated test output.

The isolated runtime and old-binary rollback check demonstrate local
compatibility. They do not prove production performance, real OAuth ticket
harvesting, third-party payment or every provider's current behavior.
