# Claude weekly quota: retained local test environment

Verified on 2026-10-19. This environment contains synthetic data only. It does not
connect to production, an existing application database, or an upstream model.
The original repository integration harness was not modified.

## Verified capabilities

| Capability | Actual result |
| --- | --- |
| Docker Desktop | 4.27.2.137060; Linux engine/client 25.0.3, API 1.44 |
| WSL | Existing WSL2 Docker distributions; WSL 2.0.14.0/kernel 5.15.133.1-1 |
| Windows Go | Existing pinned Go 1.27.0 at `C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe` |
| Linux Go | Official `golang:1.27-alpine` resolved to Go 1.27.2/linux/amd64; base digest is pinned in the Dockerfile |
| Linux CGO/race | GCC 15.2.0; `go test -race -count=1 ./internal/pkg/timezone` passed |
| Linux compilation | `go build -buildvcs=false -o /output/timezone.a ./internal/pkg/timezone` passed; archive size 79,928 bytes |
| PostgreSQL | 18.1, UTC, fresh database `claude_weekly_synthetic`, zero public tables at creation |
| Redis | 8.4.6; PING=PONG, DBSIZE=0 at creation |
| Frontend dependencies | Node 20.14.0/pnpm 9.5.0; `pnpm install --frozen-lockfile --offline` passed in 47.8 seconds |
| Frontend build | `pnpm run build` passed: three i18n assertions, Vue type build, Vite build; existing large-chunk warning remains |

These environment checks do not establish Claude billing behavior, full backend
build correctness, migration correctness, or production readiness.

## Retained resources

The source of truth for exact immutable container/network IDs and image IDs is
[`environment-active.json`](environment-active.json). Run ID:
`20261019T131000Z-e4f68a1d`. Resource name prefix:
`cwq-env-20261019t131000z-e4f68a1d-`.

| Suffix | Type | Limit or purpose |
| --- | --- | --- |
| `postgres` | Container | 1 CPU, 512 MiB, 128 PIDs; dedicated PG18 volume |
| `redis` | Container | 0.5 CPU, 128 MiB, 64 PIDs; persistence disabled, tmpfs data |
| `runner` | Container | 4 CPUs, 4 GiB, 512 PIDs; Go/CGO/race compilation |
| `pgdata` | Volume | Dedicated PostgreSQL 18 data root `/var/lib/postgresql` |
| `gocache` | Volume | Linux Go build cache |
| `gomodcache` | Volume | Linux module cache, separate from Windows module cache |
| `output` | Volume | Linux build artifacts |
| `net` | Internal network | `10.247.190.0/24`; no external egress |

No host ports are published. No Docker socket is mounted. Repository source and
the existing Windows module download cache are mounted read-only in the runner.
`GOPROXY=file:///module-proxy`, `GOSUMDB=off` uses that offline cache; existing
`go.sum` still verifies cached module content. The runner has a read-only root,
drops capabilities, and has writable tmpfs/cache/output volumes only.

PostgreSQL trust authentication is restricted to this new internal network and
uses no real credentials. The dedicated aliases are `cwq-postgres` and
`cwq-redis`. Containers have restart policy `no`. The PG volume is retained;
Redis intentionally has no persisted state.

## Commands

From this worktree, obtain the recorded resource IDs before use:

```powershell
$docker = 'C:/Program Files/Docker/Docker/resources/bin/docker.exe'
$state = Get-Content validation/claude-weekly-quota/environment-active.json -Raw | ConvertFrom-Json
$runner = ($state.containers | Where-Object role -eq 'runner').id
$postgres = ($state.containers | Where-Object role -eq 'postgres').id
& $docker --host npipe:////./pipe/docker_engine exec $runner go test -race -count=1 ./internal/pkg/timezone
& $docker --host npipe:////./pipe/docker_engine exec $postgres psql -X -U postgres -d claude_weekly_synthetic -At -c 'SHOW server_version'
```

Run the unchanged repository integration harness from Windows Go, where its
Docker CLI and client can access the local pipe. It starts its own new synthetic
PostgreSQL and Redis containers; it does not consume the retained environment DB:

```powershell
$env:PATH = 'C:/Program Files/Docker/Docker/resources/bin;' + $env:PATH
$env:DOCKER_HOST = 'npipe:////./pipe/docker_engine'
Remove-Item Env:DOCKER_CONTEXT -ErrorAction SilentlyContinue
$env:CI = 'true' # Missing Docker must fail, never silently skip.
$env:GOTOOLCHAIN = 'local'
$env:GOMAXPROCS = '4'
Set-Location backend
& 'C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe' test -tags=integration -count=1 -run '<exact anchored test names>' ./internal/repository
```

The retained Linux runner intentionally has no Docker control socket or Docker
CLI. It supports Linux builds and race unit tests. A repository integration
binary inside that runner cannot run the original Docker-starting TestMain as-is;
use the Windows host invocation above. No harness replacement or skip overlay is
used for this environment.

Frontend commands, already executed successfully:

```powershell
Set-Location frontend
pnpm install --frozen-lockfile --offline
pnpm run build
```

The build writes the existing ignored `backend/internal/web/dist` output. Neither
`frontend/package.json` nor `frontend/pnpm-lock.yaml` changed.

To recreate after cleanup, first build the task-only image, then start:

```powershell
& $docker --host npipe:////./pipe/docker_engine build --pull=false -f validation/claude-weekly-quota/environment-runner.Dockerfile -t claude-weekly-quota-go:1.27-alpine-gcc validation/claude-weekly-quota
pwsh -NoProfile -File validation/claude-weekly-quota/environment-start.ps1
```

The startup script refuses an existing active manifest. The explicit subnet was
checked against existing Docker networks and Windows IPv4 routes. Docker rejects
overlap with any Docker network; recheck host routes if network setup changes.

Resources are deliberately retained for I1-I6. When authorized to remove them:

```powershell
pwsh -NoProfile -File validation/claude-weekly-quota/environment-stop.ps1 -RemoveVolumes
```

That script validates every captured ID/name, task/run label, internal network,
membership, port state, and absence of a Docker socket before the first removal.
It never selects resources by a broad name filter. Its syntax and full read-only
resource validation passed (`-ValidateOnly`); cleanup has not been executed
because resources must remain available.

## Initial blockers and recovery evidence

1. `docker version` initially found no `docker_engine` pipe. Existing service
   `com.docker.service` was stopped. `Start-Service` failed because it could not
   open the service in this process context.
2. Starting the existing `Docker Desktop.exe` with `Start-Process -WindowStyle
   Hidden` succeeded. Both local pipe endpoints then reported engine 25.0.3.
3. Docker Desktop automatically restarted existing local containers according to
   their pre-existing restart policies. No explicit start/stop/update command
   targeted an existing container; those containers were neither used nor cleaned.
4. New network creation using Docker defaults failed because its available
   default IPv4 pools were exhausted. The failed run created no task resources.
   Its manifest and receipt are retained. Creating this task's explicit internal
   `10.247.190.0/24` network succeeded, without deleting old networks.
5. All required PG18/Redis/Ryuk images were cached. The official Linux Go image
   was pulled once and its GCC/musl layer was added only to this task's new image.

See `environment-evidence.json`, the dated start receipt, frontend logs, and
`environment-linux-race.log` for commands/results. Application test receipts from
the main development agent remain separate.
