# Host Nginx release controller

`host-release.py` controls a pre-created pair of application slots behind two
host Nginx sites. It does not build, pull, start/restart containers, import data,
restore backups, enable Claude groups, or acquire a migration lock. Prepare and
verify the immutable candidate and fresh backups separately. The old `deploy.sh`
is disabled before any side effects: its deadline could previously stop a live
process. Historical router-Compose instructions are not this host topology.

## Private configuration and prerequisites

Run on the Linux Nginx host with Docker, Python 3 and `/proc` access. Install
`host-release.py` and its unchanged sibling `release-guard.py` together. Config,
admin bearer-token file, durable state and legacy evidence must be regular files
with mode 0600; never put token values in argv, logs or this repository. The
controller uses a nonblocking `flock` next to `state_file` across every command.
Use one config/state for both sites; do not edit Nginx concurrently elsewhere.

Example structure (replace the image placeholders with full immutable IDs):

```json
{
  "version": 1,
  "initial_slot": "blue",
  "state_file": "/opt/sub2api2/release-private/state.json",
  "upstream_file": "/etc/nginx/conf.d/00-sub2api-release-upstream.conf",
  "upstream_name": "sub2api_release",
  "inflight_evidence_command": ["python3", "/opt/sub2api2/release-private/main-server-inflight.py"],
  "nginx_sites": [
    "/etc/nginx/conf.d/sub2api-migrated.conf",
    "/etc/nginx/conf.d/teamsub2.conf"
  ],
  "routes": [
    {"url": "https://sub2api.monasapi.com"},
    {"url": "https://teamsub2.monasapi.com"}
  ],
  "slots": {
    "blue": {
      "container": "sub2api2",
      "port": 29080,
      "image_id": "sha256:REPLACE_WITH_64_HEX_ORIGINAL_IMAGE_ID",
      "admin_token_file": "/opt/sub2api2/release-private/admin-token",
      "preserve_mode_required": false,
      "legacy_background_evidence_file": "/opt/sub2api2/release-private/legacy-background.json"
    },
    "green": {
      "container": "sub2api2-green",
      "port": 29081,
      "image_id": "sha256:REPLACE_WITH_64_HEX_CANDIDATE_IMAGE_ID",
      "admin_token_file": "/opt/sub2api2/release-private/admin-token",
      "preserve_mode_required": true
    }
  }
}
```

The upstream file must be included once inside Nginx `http {}` (the existing
`conf.d/*.conf` include does this). Each site must contain exactly one direct
`proxy_pass http://127.0.0.1:29080;` before installation. Installation changes
only those two directives; it preserves TLS, upgrade, buffering, timeouts and
header inheritance. It atomically replaces each file, persists originals before
mutation, then runs `nginx -t` and a graceful HUP. The two files become active
together on that reload; this is not a multi-file atomic filesystem transaction.
An interruption is resumed from recorded originals/planned replacements.

Both slots must explicitly set `RELEASE_DRAIN_START_HELD=false`. New candidates
must also set:

```text
GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE=preserve
GATEWAY_BACKGROUND_JOB_MODE=coordinated
GATEWAY_BACKGROUND_JOBS_START_HELD=true
```

Only the original slot/process may use the legacy exception. Runtime container
ID, image ID and process start time are pinned. A retired R0 cannot be restarted
as a rollback target. A future replacement in that slot must use preserve mode.

For initial legacy retirement, refresh the evidence file from read-only live DB
and verified configuration checks against this exact process:

```json
{
  "version": 1,
  "container_id": "EXACT_DOCKER_CONTAINER_ID",
  "started_at": "EXACT_DOCKER_STARTED_AT",
  "image_id": "sha256:EXACT_IMAGE_ID",
  "checked_at_utc": "2026-10-20T00:00:00+00:00",
  "scheduled_enabled": 0,
  "channel_enabled": 0,
  "batch_jobs": 0,
  "backup_schedule_enabled": false,
  "cn_active_accounts": 0
}
```

The three job-target counts must be exact integer zero (not bool/null), and the
legacy backup schedule must be explicitly verified disabled. CN work
requires exact `cn_active_accounts: 0` **or** independently verified
`background_balance_enabled: false`. Missing data never means disabled. The
file is checked after natural ingress drain and immediately before stop; default
maximum age is 300 seconds (`legacy_evidence_max_age_seconds` may be 1–900).
Long sessions may require a fresh audit and another `retire` call. Do not invent
zero counts or change a true balance setting to false in the evidence. This is
external legacy evidence, not a claim that R0 exposes in-memory job counters.

Every slot retirement, including a modern coordinated slot, also requires a
live `inflight_evidence_command`. This trusted argv receives `--container-id ID
--started-at TIMESTAMP` after old ingress and HTTP/usage/job counters are quiet.
The collector must actually query shared PostgreSQL/Redis and return JSON:

```json
{
  "version": 1,
  "container_id": "EXACT_DOCKER_CONTAINER_ID",
  "started_at": "EXACT_DOCKER_STARTED_AT",
  "checked_at_utc": "FRESH_UTC_TIMESTAMP_WITH_TIMEZONE",
  "backup_running": 0,
  "restore_running": 0,
  "detached_image_tasks": 0,
  "batch_active": 0
}
```

All four counts must be actual nonnegative integers, and only all-zero permits
stop. The timestamp must be produced by this invocation and no more than 30s
old. Redis `image_task:*` JSON records with `status=processing` represent detached
work outside the release counters. Count every such task, even if it might
belong to the new slot; conservative deferral is safer than guessing ownership.
Count running backup/restore records and active batch work from their actual
stores. No collector, missing/malformed data or stale evidence refuses stop;
positive work returns exit 3. There is no invented `uncoordinated_probes` zero:
the legacy source conditions above are separate evidence. The companion
`scripts/main-server-inflight.py` implements the production read-only collector;
isolated validation must supply real test stores, never a constant-zero stub.

## Commands and resume semantics

```sh
python3 host-release.py --config /PRIVATE/config.json install-route
python3 host-release.py --config /PRIVATE/config.json release green --timeout 900
```

`install-route` keeps the original port serving. The reusable `release` command:

1. Inspects target runtime image and startup flags; direct `/health` must be 200,
   unauthenticated `/v1/models` exactly 401, and admin release status open.
2. Persists the cutover intent and all current Nginx worker identities, updates
   only the shared upstream, syntax-checks, gracefully reloads, and observes a
   new worker generation under the same master.
   Effective Nginx `worker_shutdown_timeout` must be absent or zero; a forced
   worker deadline would interrupt old streams despite keeping the app alive.
3. Probes both public domains without billable calls. The authenticated release
   status `background_jobs.instance_id` must equal the direct target process.
4. Keeps the old application gate **open** while captured old Nginx workers and
   established sockets to the old port remain. It never uses an empty
   `worker process is shutting down` grep as immediate proof after HUP.
5. Once old ingress is gone for two observations, records drain intent and POSTs
   `/api/v1/admin/release/drain`. It stores the returned process operation ID.
6. Requires `active_http`, `pending_usage`, `queued_http` and background
   `active_tasks` all zero; coordinated background state held; captured workers
   gone; old-port established sockets absent. It requires two clean observations
   and repeats the checks after final route verification. The live external-work
   collector must then confirm backup, restore, detached image and batch counts
   are all zero; identity and retirement counters are checked again afterward.
7. Records `restart_disable_requested`, applies `docker update --restart=no` to
   that exact old container and verifies both policy and unchanged process.
   Only then records `stop_requested`, stops it, verifies it stopped and
   activates the new background instance with its exact `instance_id`.

Read-only routes use no proxy environment and no redirects. HTTP responses,
Docker environment and bearer tokens are never printed. Nginx command output is
withheld; inspect private/local service logs separately when needed.

You can run the phases separately:

```sh
python3 host-release.py --config /PRIVATE/config.json switch green
python3 host-release.py --config /PRIVATE/config.json retire blue --timeout 900
python3 host-release.py --config /PRIVATE/config.json activate
python3 host-release.py --config /PRIVATE/config.json status
```

Exit 0 means the command completed. Exit 3 means retirement is **deferred**:
keep both processes running. During `waiting_ingress`, old gate remains open;
after its recorded drain, it remains draining until resumed retirement or an
explicit safe rollback. The active new process continues HTTP service while
its singleton background jobs remain held. Exit 1 means a guard/operation failed;
no unconditional cleanup, restart or stop follows. Do not treat `status` as a
fresh health probe; it reports durable controller phase only.

After a preserve-mode candidate restarts, HTTP remains available because
`RELEASE_DRAIN_START_HELD=false`, while autonomous jobs start held again. Do not
automatically open any migration gate. Once the previous process is confirmed
stopped and restart-disabled, explicitly run `activate`: it re-inspects the new
container/process, verifies open HTTP status and both domains against its new
instance ID, persists that identity, and activates only that exact instance.
An unknown/migrating gate is rejected; `/release/resume` is never used here.

Rerun the same command/config to resume a persisted file/reload/retirement phase.
A lost drain response is deliberately not auto-adopted: a server-generated ID
cannot prove ownership after a crash. The command preserves the process and
requires explicit operator reconciliation of that exact process/operation.
Never use `/release/lock`, migration `/resume`, or a guessed operation ID.

## Rollback boundary

```sh
python3 host-release.py --config /PRIVATE/config.json rollback blue
```

Rollback requires the old process still running with the same immutable image,
container ID and start time. If this controller drained it, cancel only the
recorded matching operation, verify direct readiness, then perform a guarded
route switch. A stopped/restarted R0 is rejected; this tool never restarts it.
Legacy R0 has no new instance-ID response field, so its route verification is
explicitly limited to exact managed configs, new Nginx generation and both route
readiness checks; it does not claim instance-level proof for R0.

The legacy Compose application entry must also be marked retired/removed from
startup management by the deployment owner, preserving its PostgreSQL/Redis
services. Retain the stopped R0 container for diagnostics; do not run its old
Compose `up` command. Disabling its container restart policy prevents Docker
daemon reboot from resurrecting the old startup cleanup against the new slot.

## Isolated validation adapters

The same private schema may set argv arrays `docker_command`, `nginx_command`
and `observer_command` (defaults: Docker, Nginx, this script's Linux `observe`).
These are trusted executable configuration, not shell strings. The Nginx argv
gets `-t` or `-s reload`. The observer gets `--ports P1,P2` and must return actual
`{master:{pid,start_time},workers:[{pid,start_time}],established_ports:[...]}`.
It must observe the same Nginx network/PID namespace; no fake or skipped checks
are accepted as runtime evidence. `slot.direct_url` may point to another host
loopback bridge and `routes[].host` sets Host for real synthetic domain checks.
Neither option relaxes health/status/instance assertions.

Run `python3 scripts/test_host_release.py -v` and
`python3 scripts/test_release_guards.py -v`. The state-machine tests inject
failures, crashes, stale identities, late usage, foreign drains, legacy evidence
and old worker/socket lifetimes. Separate isolated real-Nginx SSE/WS/HTTP
validation must prove old requests survive reload and timeout, new domains reach
the candidate, and retirement occurs only after real ingress completion.
