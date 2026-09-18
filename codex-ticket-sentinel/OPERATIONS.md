# Operations

## Boundaries

Use a separate Compose project, `sub2api-ticket-sentinel`. Do not merge this
service into the application Compose project or give it Docker socket, DB,
Redis, OAuth, administrator token, or application `.env` access. Joining the
existing bridge network provides reachability, not a firewall between peers.
The controller uses only the restricted token-authenticated Sub2API HTTP API.

Do not start an inactive production slot merely to test this feature: both slots share the
production database. Installing the integration requires a new compatible
Sub2API image and the existing controlled release procedure. Preserve carpool,
billing, migrations, branding and all existing release guards.

## Layout and permissions

Recommended server root: `/home/linuxuser/apps/sub2api/codex-ticket-sentinel/`.
Keep immutable source/deployment releases under `releases/<date>-<source-sha>/`,
private files under `private/`, and durable SQLite/status files under `state/`.
The state path stays constant across image releases. Never run two controllers
against one state database or reset state to clear cooldowns.

Before starting the service, run the included Linux helper as root:

```sh
sudo sh deploy/init-state.sh
```

It creates only the dedicated state directory, UID/GID 10001, mode 0700. It does
not recursively change files. If restoring state, retain ownership 10001 and
keep SQLite's database and WAL/SHM files together; prefer an SQLite online backup
or a cleanly stopped controller when making a copy.

Create a cryptographically random dedicated token in a private file without
printing it or placing it on a command line. Keep its host parent directory mode
0700. Sub2API rejects a token file with any group/other permission bits. Store
two copies of the same random token, each mode 0400: one owned by Sub2API's
actual UID (1000 here), the other by controller UID 10001. Mount each file
read-only into its respective container. Compose file-backed secrets do not
reliably remap host ownership. Verify both runtime UIDs can read their own token;
do not assume Docker's configured User reflects entrypoint privilege dropping.

Copy `deploy/config.example.json` to the private config path; it contains no
credential, only the token filename. Do not put token contents in Compose or
environment files. Keep all private files out of source archives and images.

## Stage and release

1. Run the controller and integration acceptance tests. Build the sidecar from
   this directory with `docker build -f deploy/Dockerfile -t <immutable-image> .`.
   The build context allowlist includes only `sentinel.py`. Record source SHA,
   image ID/digest and the Python base image digest in the release receipt.
2. Build and verify the new compatible Sub2API image separately. Stage the
   integration's token mount and these environment names on the new slot:
   `CODEX_SENTINEL_TOKEN_FILE=/run/secrets/codex-sentinel-token`,
   `CODEX_SENTINEL_ACCOUNT_IDS=2`, and
   `CODEX_SENTINEL_MODELS=gpt-6-astra,gpt-5.6-sol`.
   Mount the shared dedicated token file read-only at the configured path.
   Maintain the existing US-static-car8 harvest proxy in Sub2API; the sidecar
   config does not replace or supply proxy credentials.
3. Use the existing release backup, drain, readiness and routing procedure.
   Keep `RELEASE_DRAIN_START_HELD=false` in normal slot configuration. Verify
   `/health` HTTP 200 plus unauthenticated `/v1/models` exactly HTTP 401, both
   before and after routing. Do not replay the guard installer's old backups.
4. Initialize state and private files, then set only nonsecret path/image
   variables `SENTINEL_IMAGE`, `SENTINEL_CONFIG_FILE`, `SENTINEL_TOKEN_FILE`,
   `SENTINEL_STATE_DIR` in the operator shell or a restricted deployment env file.
   Paths must be absolute. Copy the compose example to the release or reference
   it in place. Check syntax without dumping config:

   ```sh
   docker compose -f deploy/compose.example.yml config --quiet
   docker compose -f deploy/compose.example.yml up -d --no-build --pull never sentinel
   ```

5. Verify the new Sub2API interface returns protocol version 1, expected target
   allowlists and eligible metadata. Use a private helper that reads the token
   file; do not use `curl -H` with a token expanded into process arguments.
   Start with one bounded, authorized refresh through Sub2API. Confirm a new
   persisted version and readiness without printing ticket values. For this
   bounded operator check, `reason=expiry` accepts a current matching version
   even before its expiry window; the server does not infer genuine expiry from
   that label. Collection or database-write failures retain the old valid ticket.
   If persistence succeeds but cache verification fails, the database may already
   hold the replacement; the response reports propagation uncertainty.

The example has no public ports, a read-only root, dropped capabilities,
`no-new-privileges`, bounded logs, 64 MiB memory and 0.25 CPU. The host is small:
measure RSS/restarts/OOM before changing limits. It needs no pip packages.

## Observe

```sh
docker compose -f deploy/compose.example.yml ps
docker compose -f deploy/compose.example.yml exec -T sentinel \
  python /app/sentinel.py --healthcheck /state/status.json --max-age 30
python3 /home/linuxuser/apps/sub2api/blue-green/scripts/release-guard.py monitor
```

Health proves recent successful control-plane polling, not successful ticket
refresh, upstream model identity or quality. Check sanitized status for queued
jobs, bounded attempts, cooldown/backoff, event gaps, eligibility and persistence
results. A refresh can take up to 40 seconds; a temporarily old poll timestamp
must not trigger application restarts or route changes. Docker health status
alone does not automatically restart this service. Never log raw tokens,
tickets, prompts, response text, HTTP bodies or environment dumps.

Defaults: poll every 2 seconds; refresh 600 seconds before expiry; 60-second
cooldown; backoff capped at 900 seconds; at most 6 attempts per hour under the
controller's budget. Sub2API applies its own cooldown/singleflight/version guard.
The live global ticket switch takes precedence over controller jobs.

## Stop and rollback

```sh
docker compose -f deploy/compose.example.yml stop sentinel
```

Stopping the sidecar disables the event sentinel only. **The native Sub2API
collector remains active. Disable the Sub2API global ticket switch to stop all
ticket mechanisms.** An already running bounded harvest may finish; stopping
the controller does not revoke a server operation already admitted.

For a sidecar-only rollback, retain its state and private token, select the
previous compatible immutable image and restart only `sentinel`. Verify state
schema compatibility first. Do not delete cooldown/job state or restore the
production database. Roll back a Sub2API integration image only through the
normal release workflow, checking migrations and shared-database compatibility;
an older image without this protocol requires the sidecar to remain stopped.

## Existing release assets

On BWH, `/home/linuxuser/apps/sub2api/blue-green/scripts/` contains `deploy.sh`,
`backup.sh`, `switch-slot.sh`, `common.sh`, and `release-guard.py`. Base services
use `/home/linuxuser/apps/sub2api/docker-compose.local.yml`; slots/router use
`blue-green/docker-compose.yml`. `run/active-slot` records the selected slot.
Do not run these scripts merely to install or restart the sidecar.

The checked-in guard kit is `../deploy/blue-green/`; its README and
`../.trellis/spec/backend/release-safety.md` define the current safety contract.
Previous compatible build/deployment receipts live on BWH under
`/home/linuxuser/apps/sub2api/releases/compatible-0.2.6-20260919/`. They are
historical evidence, not scripts to replay without reviewing the new release.
