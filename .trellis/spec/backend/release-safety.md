# Release Gate Safety

## 1. Scope / Trigger

Required when changing Sub2API blue-green deployment, release drain/recovery,
Compose environment overrides, readiness checks or operational monitors.
Production and inactive slots share a database; neither is an isolated test target.

## 2. Signatures

- `release-guard.py compose-config`: merged Compose JSON on stdin; exit 0 only for safe slots.
- `release-guard.py container sub2api-{blue,green} --image REF`: inspect running state, startup flag and image ID.
- `release-guard.py probe BASE_URL`: `/health` must be 200 and unauthenticated `/v1/models` exactly 401, each within two seconds.
- `release-guard.py monitor`: check active slot environment/image and direct/router business readiness.
- `remote-release-ops.py clear-stage --workdir RELEASE_DIR`: archive/disarm stage config only; never modifies containers or releases a gate.
- Legacy `prepare-stage`, `auth`, `backup`, `probe` in this one-off script are retired and exit 2 before any side effect.

## 3. Contracts

`RELEASE_DRAIN_START_HELD=false` is explicit in normal Compose slot definitions.
Reject start-held or unrecognized flag values in merged config and running target
containers, even if an operator previously resumed that process in memory.
Docker restart preserves container environment; editing an override file does not
change an existing container. Do not force-recreate an active service merely to
install checks. Monitor independently until the next approved recreation applies
new Docker healthcheck definitions.

Check target readiness before router mutation, then verify stable routes. Every
command inside a shell verification function must propagate failure explicitly:
`check || return 1`. Bash `set -e` is suppressed when a function runs in an `if` or
`!` condition; its final successful command cannot stand in for all prior checks.

Raw stage backups containing start-held flags must be archived with restricted
permissions and verified bytes before runnable backup copies are removed.
Do not emit environment JSON, credentials or HTTP bodies in guard logs.
Monitoring must not auto-resume, restart, or switch slots. A locked migration is
not safely cancellable: the tested lock state returns 409 for `/release/cancel`.
Verify committed import state before resume; do not restore a production snapshot
after traffic has resumed and new billing has occurred.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Health 200, models timeout | Readiness failure; no cutover |
| Models 200/302/403/500/503 | Failure; exact unauthenticated 401 required |
| Merged override sets start-held true | Compose wrapper refuses before mutation |
| Running container has true despite currently open gate | Target rejected before route changes |
| Configured image differs from actual image ID | Target rejected |
| First route probe fails, last legacy health command succeeds | Function still fails |
| Malformed/symlinked stage file | Cleanup refuses; originals remain |
| Cleanup succeeds | Flag false; unrelated settings preserved; backup bytes recoverable |

## 5. Good/Base/Bad Cases

Good: use explicit false startup flags, read-only probes, and an independently
authorized short drain/lock/verify/resume migration. Base: reject a held standby
before nginx reload. Bad: assume `/health` or `docker restart` proves the gate is open.

## 6. Tests Required

Run `python deploy/blue-green/test_release_guards.py -v` on Linux, including
conditional-shell failure propagation and switch refusal before router mutation.
Validate the final Compose merge without printing it. In a fresh isolated exact
image, prove open 401, held timeout, health still 200, guard/healthcheck rejection,
and that restarting a true-configured container remains held. Assert production
start time/image/active slot and business records remain unchanged when installing
guards. No real user keys or upstream provider calls are needed for these probes.

## 7. Wrong vs Correct

Wrong: `if ! verify_route; then ...` where `verify_route` runs several commands
and only its last command determines success. Correct:

```bash
verify_route() {
  python3 "$RELEASE_GUARD" probe http://127.0.0.1:8080 || return 1
  python3 "$RELEASE_GUARD" probe https://sub2api.monasapi.com || return 1
}
```

Wrong: automatically clear a gate after sixty seconds. Correct: bound the
pre-lock drain, stop before importing if its budget is exceeded, and require
explicit state-aware recovery after a lock/commit rather than silently opening.

## Backend coexistence and autonomous job handoff (2026-10-20)

Normal single-process defaults remain `gateway.concurrency_startup_cleanup_mode=exclusive`
and `gateway.background_job_mode=legacy`. Explicit overlapping deployments require:

- `GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE=preserve` skips the **entire** legacy
  account/user startup cleanup, including the one-time wait-counter sweep. It also
  skips backup startup recovery, which otherwise marks all persisted running
  backup/restore records failed and can delete their remote objects. Normal
  score/TTL cleanup remains enabled. No other process's live member is deleted.
- `GATEWAY_BACKGROUND_JOB_MODE=coordinated` and
  `GATEWAY_BACKGROUND_JOBS_START_HELD=true` keep five autonomous producers held:
  scheduled account tests, channel monitor probes, batch-image TTL cleanup, and
  CN provider balance/quota checks, and scheduled backups through upload/retention
  cleanup. This is independent of the HTTP drain gate;
  normal candidates still use `RELEASE_DRAIN_START_HELD=false`.
- `POST /api/v1/admin/release/background/activate` accepts only
  `{ "instance_id": "<status.background_jobs.instance_id>" }`. It remains behind
  administrator authentication and auditing, requires HTTP state `open`, and
  must be called only after the prior process has retired. A restarted process
  has a new ID and deliberately returns to held; no automatic takeover occurs.
- Release status adds `background_jobs` with `mode`, `state`, `active_tasks`,
  `instance_id`, and `leader`. Queued and running work are counted before pool
  submission. A coordinated process retains one PostgreSQL advisory-lock
  session for its entire active tenure and for all work accepted before hold.
  This prevents two timers from taking turns duplicating the same probe.
- Drain freezes new claims without cancelling accepted work. Once the final
  task finishes, the held process unlocks and releases its database session.
  Cancel restores the previous held/active state; it must not activate a
  previously held candidate. Lock/resume also require zero autonomous tasks.
  Activation/restore cannot obtain leadership while a peer owns the lock.
- Database acquisition/release is bounded at three seconds, uses one
  authoritative backend, and has no Redis/PostgreSQL fallback election.
  Uncertain lock outcomes discard the physical connection instead of returning
  a potentially locked session to the pool. A two-second connection heartbeat
  with a two-second timeout freezes new claims and cancels job contexts on loss.
  An uncertain external provider response is not proof that its side effect did
  not occur; this protocol does not promise exactly-once external side effects.

Keep durable billing recovery, paid fulfillment, auth invalidation, and their
existing leases/idempotency running during coexistence. The new counter covers
these five producers only, not every detached task. Before retiring an older
binary, prove no running backup/restore, detached image job, or uncoordinated
probe remains. In particular R0 predates this gate: a new PG lock cannot stop
R0's cron jobs. Inventory and quiesce them before activating the new role.

For live request retirement, switch ingress first and establish that old nginx
workers/connections no longer introduce work before draining the old backend.
Wait for `active_http`, `queued_http`, `pending_usage`, and
`background_jobs.active_tasks` to reach zero. Existing SSE and upgraded WS
handlers remain counted until completion. Busy WS sessions have no finite
natural-retirement guarantee; never add a forced-kill timeout while claiming
uninterrupted release. The server's ordinary SIGTERM shutdown timeout is five
seconds, so SIGTERM is only the final step after this proof.

During first R0 coexistence, route rollback may target the **still running** R0
(after cancelling its drain if necessary). Once stopped, its unconditional
startup cleanup makes restarting it unsafe while any newer process owns live
slots. Use a compatible preserve-mode rollback binary or fully drain all traffic
before an unpatched restart. After term-mode conversion, the existing R0 schema
and business rollback prohibition still applies.

Plugin runtimes reconcile per instance, but installation/upgrade/deletion must
remain frozen during coexistence on a shared plugin directory. Existing BPS
opaque records must remain shared and intact; their startup does not sweep data.
Do not interpret these narrow checks as a general HA certification.

Required backend regressions: actual config environment parsing and invalid-mode
rejection; preserve startup with live account/user/WS members plus indexed and
unindexed wait keys and no legacy-sweep marker; leader retention during drain;
second activation rejection; last-task handoff; real PostgreSQL session loss;
held producers reaching no repositories/providers; authenticated exact-instance
activation; backup startup preserving another process's running records/objects;
and existing SSE/WS and channel-monitor natural-completion tests.

## Host Nginx controller contract (2026-10-20)

### 1. Scope

`deploy/blue-green/scripts/host-release.py` owns the two host sites and one shared
upstream file. The older router-Compose `deploy.sh` now exits 2 before side
effects because its drain deadline previously fell through to an unconditional
stop. See [the host runbook](../../../deploy/blue-green/HOST_RELEASE.md) for the
private configuration, legacy evidence and exact command sequence.

### 2. Signatures

- `host-release.py --config PRIVATE_JSON install-route`
- `host-release.py --config PRIVATE_JSON release SLOT --timeout 900`
- Separate phases: `switch SLOT`, `retire SLOT`, `activate`, `rollback SLOT`, `status`.
- Read-only Linux observation: `host-release.py observe --nginx-pid-file FILE --ports A,B`.

### 3. Durable contracts

Private version-1 JSON declares exactly two container/image-ID/port slots, two
Nginx site paths and stable URLs, a shared upstream name/file, bearer-token file
and state path. Custom argv prefixes/loopback bridges support isolated real
namespace tests without relaxing any readiness check. File mutation is atomic
per file and fsynced; the combined site configuration takes effect on one
validated reload. A durable intent precedes writes, HUP, drain, restart-policy
change, stop and background activation. One nonblocking flock covers commands.

Readiness reuses `release-guard.py`: health exactly 200 and unauthenticated models
exactly 401, no redirects or billable calls. New stable routes must return the
same authenticated background instance ID as the direct candidate. R0 lacks
that response field; its explicit legacy check uses exact managed routing,
process identity and Nginx generation, without claiming instance-level proof.

After cutover, `waiting_ingress` keeps the old gate open until every captured
Nginx worker identity and established old-port connection disappears. Only then
POST drain. Record its returned operation ID; wait for HTTP, usage, queue and
background counts to reach zero and repeat process/socket checks. Revalidate
routes and counts just before mutating the old container. Disable its automatic
restart policy first, verify policy/process, then stop that exact container.
Activate the new background role only after old retirement is confirmed.

Every retirement also invokes the configured read-only
`inflight_evidence_command` after ingress and application counters are quiet.
It receives the exact container ID and start time and returns version-1 JSON
with matching identity, a fresh UTC check time, and exact integer counts for
`backup_running`, `restore_running`, `detached_image_tasks` and `batch_active`.
All four must be zero. Missing, unknown, boolean, negative, stale or foreign
evidence refuses stop; a positive count defers retirement. This gate applies to
new and legacy versions because detached image work and backup/restore records
are not fully represented by release counters. Legacy evidence additionally
requires the verified backup schedule to be disabled alongside its existing
scheduled/channel/batch/CN producer conditions; do not invent a generic
uncoordinated-work count.

### 4. Validation/error matrix

| Condition | Required behavior |
| --- | --- |
| Wrong image, held HTTP startup, exclusive cleanup or uncoordinated candidate | Reject before route writes |
| Effective nonzero `worker_shutdown_timeout` | Reject; Nginx itself could force-close old streams |
| New worker generation not observed or stable instance mismatch | Do not claim cutover; restore previous route while both slots stay alive |
| Old worker/socket remains at deadline | Exit 3, durable `waiting_ingress`, old gate stays open/running |
| Post-drain request/usage/queue/job count remains | Exit 3, retain old process and exact drain operation |
| Drain response lost before operation ID persisted | No automatic adoption/cancel; preserve and reconcile ownership |
| Missing/stale/foreign legacy background evidence | Refuse old stop; missing/bool/null counts are not zero |
| Missing/invalid common in-flight evidence or any positive external count | Refuse/defer stop before restart-policy mutation; preserve the running old process |
| Old container stopped/restarted or foreign drain | Reject rollback; never start a container |
| Candidate restarts held after old stopped | Explicit `activate` revalidates new process and both routes before exact-instance activation |

### 5. Good/base/bad cases

Good: old HTTP/2, SSE and WS finish under open ingress while new connections use
the candidate; timeout preserves old service. Base: admin authentication failure
aborts before a switch. Bad: close the old gate immediately after HUP, infer
quiescence from an empty shutting-down-worker grep, or stop on a 900-second timer.

### 6. Required tests

`test_host_release.py` covers real file/state writes with injected command/API
failures: partial-install/cutover resume, route identity mismatch compensation,
old ingress lifetime, independent busy counters, stale/strict legacy evidence,
late usage, strict common external-work evidence, lost drain responses, exact-operation rollback, restart disarm and
explicit post-restart activation. Run existing `test_release_guards.py` too.
These are controller tests, not proof of real Nginx stream preservation. The
separate isolated runtime test must exercise real HUP, SSE, WS, HTTP/2, two Host
routes, post-HUP identity failure, timeout preservation and eventual retirement.

### 7. Wrong versus correct

Wrong: `reload → POST drain → wait 900s → stop`.
Correct: `verify/reload → verify instance on both routes → keep old open until
old ingress vanishes → exact-operation drain → all counters/sockets/workers and fresh external-work evidence zero
→ disable old restart → stop → exact-instance background activation`.
