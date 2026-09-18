# BWH read-only release preflight — 2026-09-19 00:37–00:43 Asia/Shanghai

This preflight performed remote reads only. It did not drain, lock, resume,
restart, stop, create a container, export a database, change a setting or issue a
billable gateway request. Production deployment is coordinated by the main task.

## Current deployment

- SSH alias: `bwh`; deployment root: `/home/linuxuser/apps/sub2api`.
- Active slot is `green`. `sub2api-blue` does not exist.
- Green image: `sub2api:carpool-imagefix-20260915-gpt55`, ID
  `sha256:133537d6b33be5578a6e825fa74e7254de655ddf14b07ab7221790922477e4d0`.
- Image revision label is `dfb612ae3676c45ed7f2b334d7844d751bc0c292+image-main-gpt55`;
  its version label remains `0.2.1-refundfix`. This label is not an upstream-version
  determination; recovered executable/source provenance is stronger evidence.
- Green started `2026-09-16T03:48:07.786098249Z`; restart count 0, no OOM flag.
- Executable SHA256 remains
  `7c8ac352ca44541b770c13bdcce913586cf138a75013b6477866e30af92b0bc1`.
  `/app/carpool-release` is present, SHA256
  `49b6aa0c0165d9327c53b064fdd7584f46d7142b650a46da09130579d0c00b41`.
- Green, router, PostgreSQL 18 and Redis 8 report healthy. Direct green
  `127.0.0.1:28080`, router `127.0.0.1:8080` and the public HTTPS host all returned
  `/health` 200 and unauthenticated `/v1/models` 401. Blue port 18080 is closed.
  The installed read-only `release-guard.py monitor` also passed.
- Running and merged Compose flags are `RELEASE_DRAIN_START_HELD=false`,
  `CARPOOL_BACKGROUND_ENABLED=false`, `BATCH_IMAGE_QUEUE_ENABLED=false`.
  Both slots share the same production database, Redis, network and bind-mounted
  `/home/linuxuser/apps/sub2api/data`; an inactive slot is not isolated.

## Capacity and backups

| Item | Live observation |
| --- | --- |
| RAM total / available | 1,043,536 / 418,428 KiB, approximately 1019 / 409 MiB |
| Swap total / free | 2,655,224 / 2,337,340 KiB |
| Green memory | 122.4 MiB resident accounting; 384 MiB limit, 768 MiB memory+swap limit |
| PostgreSQL memory | 144.4 MiB; 256 MiB limit |
| Redis / router memory | 7.586 / 1.562 MiB |
| Other running apps | Two CPA containers, approximately 9.133 and 16.12 MiB |
| Filesystem available | 5,660,404 KiB, approximately 5.40 GiB |
| Database size | 1,434,154,687 bytes, approximately 1.34 GiB |
| App-data directory | 38,720 KiB |
| Latest existing automated backup | `20260918T122156Z`, 101,276 KiB, checksum manifest exists |
| `usage_logs` metadata | Estimated 193,622 rows; table 105,119,744 bytes; including indexes 258,113,536 bytes |

The existing `deploy.sh` requires at least **700 MiB MemAvailable** and 2 GiB
free disk. The current memory fails its first gate. Do not silently weaken or
bypass that check, and do not claim safe overlapping app slots from the observed
steady-state consumption. A serial maintenance release that stops the drained
old process before starting the candidate avoids overlapping application memory;
the released memory and migration headroom must still be rechecked at execution.
No production-volume migration duration or peak-memory measurement was made.
The historical backup's presence is not a fresh restore validation.

## Database and deployment consistency

Production has **291** migration records, ending with the custom files through
`244_carpool_balance_preserving_release.sql`. The ordered filename/checksum
fingerprint is
`8cc40780a6d19f2d489f82ba88b3b40ac0c2ba2354b04ed103a63a51cbc2db33`;
all entries exactly match the earlier recovered production baseline. Candidate
acceptance uses 297 records. Only migration metadata and relation-size estimates
were read, not user records.

The current Compose YAML, common/deploy/switch/backup scripts, release guard and
README exactly match their recovered copies by SHA256. `deploy.sh` takes a
deployment file lock, runs backup, pulls an image, starts the inactive slot,
switches routing and stops the old slot after router connections drain. It does
**not** call the app's release drain/lock API. Therefore it cannot be used as-is
to implement the requested quiescent schema upgrade, even apart from its memory
gate. `backup.sh` performs writes (Redis SAVE, temporary DB validation file,
private archives and retention); it was read but not executed.

## Release gate and credential sources

The old recovered and current candidate `release.go` and gate controller have
identical hashes. The live unauthenticated status endpoint returns 401. The
`settings` table has no configured `admin_api_key`, so authenticated gate counters
were **not obtained** in this read-only preflight. Fast `/v1/models` 401 shows
ordinary request admission is currently working; it is not a substitute for an
authenticated zero-inflight status snapshot.

Supported authentication is `Authorization: Bearer <valid administrator JWT>` or
`x-api-key: <configured global admin key>`. Existing `/home/linuxuser/apps/sub2api/.env`
contains nonempty administrator email/password fields and JWT secret; this report
does not print or copy them. The bootstrap password's presence does not prove it
still matches the account. This read-only preflight did not authenticate. The
release owner must record the actual authorized authentication path separately,
keep operator credentials private and preserve existing user authentication.
Other sensitive configuration sources are
`data/config.yaml` and `blue-green/slot-images.env`; no contents were exported.

| Endpoint under `/api/v1/admin/release` | Contract |
| --- | --- |
| `GET /status` | State, operation ID, `active_http`, `pending_usage`, `queued_http`; no state mutation |
| `POST /drain` | From open to draining; returns a new operation ID; new ordinary requests queue |
| `POST /lock` | Body `operation_id`; requires draining and zero active HTTP + pending usage; enters migrating |
| `POST /cancel` | Body `operation_id`; only works while draining; migrating returns conflict |
| `POST /resume` | Body operation ID plus distinct positive `user_ids` and `group_ids` (1–500 each); migrating only; refreshes scoped auth caches and opens only on success |

The gate counts complete HTTP handlers, including upgraded WebSocket lifetime,
and pending usage tasks. It does not suspend arbitrary background writers or
other applications. New ordinary requests wait up to 120 seconds before 503 with
Retry-After; queued HTTP is reported but is not itself a lock precondition.
Do not assume drain cancels long-lived WS sessions. `RELEASE_DRAIN_START_HELD`
is process-local startup policy; restart retains an existing container's env.
The guard deliberately rejects held containers, and health 200 alone cannot
prove readiness. The monitoring timer never resumes a held gate automatically.

## Execution sequence for the deployment owner

1. Finish image transfer/immutable checks and private legitimate admin
   authentication before holding traffic. Preserve old image, slot reference,
   router config and deployment files. Acquire the existing deployment lock.
2. Apply an explicitly controlled maintenance ingress policy so fresh ordinary
   traffic stops accumulating. Drain the actual serving green process and retain
   its returned operation ID. Poll authenticated status until active HTTP and
   pending usage are zero; observe queued requests reach zero before stopping the
   process. If WS remains active, wait or make a deliberate maintenance decision;
   do not label a forced interruption a graceful drain.
3. Lock with that operation ID, verify migrating and zero activity again, then
   stop the old serving process for the chosen serial release. This eliminates
   its background writers as well. Recheck host resources and take the fresh,
   private consistent backup; validate the archive and record checksums. Do not
   run new schema writes while the old process still serves traffic.
4. Start only the candidate with start-held **false**, in the unexposed slot,
   while maintenance ingress remains in force. Startup applies the reviewed
   291-to-297 migration path. Keep ticket collection off initially. Verify exact
   image/executable identity, migration count/checksums, direct health 200 and
   unauthenticated models 401, then normal admin and bounded read-only business
   checks. Root Dockerfile packaging and release CLI are separately validated.
5. Switch router through the validated switch operation, verify stable
   local/public routing, update active-slot atomically, then remove maintenance
   ingress. Do not call the old process's operation ID on a new process: gate
   state and operation IDs are in-memory and do not transfer across restarts.
6. On candidate failure while maintenance is held, stop candidate before
   restarting the exact old image; the tested compatibility bridge preserves old
   model-config columns. Verify old image direct/business readiness, route to it,
   then release maintenance. If data/schema restoration is necessary, stop all
   writers first and use the freshly verified backup. After public candidate
   traffic has resumed, do not blindly restore that snapshot and erase new
   usage/financial records; hold traffic and reconcile the actual write interval.

This is an operational sequence for the authorized deployment owner, not a
claim that any of these mutations were performed by this preflight.
