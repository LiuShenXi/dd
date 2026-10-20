# Recover background ownership after a completed release

`scripts/background-reconcile.py` is an optional oneshot for the completed host
release described in [HOST_RELEASE.md](HOST_RELEASE.md). It imports the existing
controller; it does not change its release state machine or start containers.
Install it beside `host-release.py` and `release-guard.py`.

## Admission and state boundaries

The helper holds the same nonblocking `state_file.lock` as every release command
from state inspection through token rotation and background activation. A busy
lock or any phase other than `active`, or a non-null transition, is a successful
skip. It will not recover an interrupted cutover, retirement or activation.

An active phase must have a completed cutover, an old retirement record with
`phase=stopped`, matching recorded/cutover identity, and the exact old container
ID, image and start time still stopped with `restart=no`. The candidate must
have preserve/coordinated/start-held background flags and open HTTP startup.
These checks precede authenticated network activity. After authentication it
skips any non-open HTTP gate, verifies direct readiness and both stable route
instance IDs, and checks identities again before activation. It never changes
an HTTP gate, reloads Nginx, starts/restarts a container or resumes a migration.

Already-active background ownership returns without a POST or controller-state
write. A held candidate is passed to the existing `Controller.activate()`, which
revalidates the exact live instance and durably records activation. This also
handles an otherwise valid preserve-mode candidate that has restarted with a
new background instance ID. Unknown evidence fails closed.

## Private token pair

Create a dedicated mode-0600 file from a normal administrator login response.
Do not synthesize a JWT, reset credentials, copy browser session storage, or put
tokens in command arguments. Use the candidate's direct loopback login endpoint
and the same host Python urllib default User-Agent as this helper. When session
binding is enabled, a login from another IP/User-Agent can invalidate refresh.

```json
{
  "version": 1,
  "access_token": "VALUE_FROM_NORMAL_LOGIN",
  "refresh_token": "rt_VALUE_FROM_NORMAL_LOGIN",
  "expires_at": "2026-10-20T12:00:00+00:00"
}
```

Compute `expires_at` in UTC from the request start time plus the returned
`expires_in`. This separate file is authoritative; do not reuse the controller's
plain `admin_token_file` path for it. Preserve the login-issued refresh token as
well as the access token. Missing, malformed, nonprivate or symlinked pair files
are errors, not permission to obtain credentials another way.

The helper refreshes when expiry is within 120 seconds or an authenticated
release request returns 401. At most one refresh occurs per invocation. It only
POSTs the refresh token to the active slot's configured loopback
`/api/v1/auth/refresh`, with redirects and proxy environment disabled. It never
sends a refresh token to public routes. Responses and credentials are not logged.

Before the request, it durably marks the pair `refresh_pending=true`. A normal
200 response must contain valid access/refresh tokens, Bearer type and a positive
integer lifetime. It atomically writes the complete new pair first, then the
active slot's plain access-token file, both mode 0600. A crash between these
writes is repaired from the pair on the next invocation without another refresh.
The active slot is the only token file synchronized; the original configuration
may intentionally share that file between slots.

An error, malformed response, lost response or failure to persist the rotated
pair leaves `refresh_pending=true` and refuses further automatic refresh. The
server may already have consumed the old refresh token. Replace the entire pair
using a new normal login while holding the same controller lock; do not merely
clear the marker or replay the previous refresh token. Authentication failures
preserve HTTP service and leave background activation untouched. If activation
itself was interrupted after intent was persisted, reconcile that controller
phase explicitly; the timer deliberately skips it.

## Commands and units

```sh
python3 background-reconcile.py \
  --config /opt/sub2api2/blue-green/private/config.json \
  --token-pair /opt/sub2api2/blue-green/private/token-pair.json reconcile

python3 background-reconcile.py \
  --config /opt/sub2api2/blue-green/private/config.json \
  --token-pair /opt/sub2api2/blue-green/private/token-pair.json refresh
```

The `refresh` command applies the same completed-release gates and readiness
checks but never activates background work. It rotates only if due or on 401;
otherwise it reports `token_current`. It is not a bypass for an in-progress
release. Exit 0 reports an action or a safe skip; exit 1 reports a sanitized
failure. Alert on failed unit runs and prolonged held ownership rather than
automatically weakening these checks.

Review the provided `systemd/sub2api-background-reconcile.service` and `.timer`
paths, then run `systemd-analyze verify` on the deployment host before installing
and enabling the timer. The root oneshot runs after Docker/Nginx, has a 60-second
timeout, and the timer uses `OnBootSec=90s` and `OnUnitActiveSec=60s`. Enable the
timer only after the first release has been verified, the login pair installed,
and a manual helper run succeeds. This repository change does not install or
enable host services.

Validation: `python3 scripts/test_background_reconcile.py -v` exercises exact old
identity, late identity change, restarted candidates, shared real flock, all
non-active phases, migration gates, both-domain identity, no-op active state,
401 rotation, malformed responses, lost responses, both persistence boundaries,
no secret output and a real loopback HTTP POST/redirect boundary. Host systemd
verification and actual candidate activation/refresh remain deployment gates;
unit tests do not claim they happened.
