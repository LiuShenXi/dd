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
