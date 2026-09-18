# Local candidate runtime acceptance

This environment contains synthetic fixtures only. It does not read, restore or
write a production database. It publishes only `http://127.0.0.1:38626`; PostgreSQL,
Redis, candidate and mock belong to an internal Docker network. Nginx is the only
container on both the internal network and its separate ingress network.
The candidate disables the URL allowlist only in this isolated fixture and
enables insecure HTTP so it can contact the deterministic HTTP mock. Strict
allowlist mode always requires HTTPS in the existing product. Isolation is
enforced by the application's internal network, not a production config change.

Run from the integration repository root with Python, Docker Desktop and the
cached Go 1.27 toolchain available. `build` embeds the existing final
`backend/internal/web/dist` and records exact input hashes. `start` deliberately
requires unused names, networks, volumes and host port; it never replaces an
existing environment.

```powershell
python validation/upgrade-026/runtime/runtime.py build
python validation/upgrade-026/runtime/runtime.py start
python validation/upgrade-026/runtime/verify_http.py settings
python validation/upgrade-026/runtime/verify_http.py seed
python validation/upgrade-026/runtime/verify_http.py redeem
```

All randomly generated passwords, API keys, environment files, executable
artifacts and raw process logs stay outside the repository in:

`%LOCALAPPDATA%/Codex/PrivateTests/sub2api-upgrade-026-20260918/runtime`

`app-credentials.private.json` contains the synthetic administrator login.
`browser-fixtures.private.json` contains active, future, expired carpool and
ordinary-user logins. Do not paste these files into a report. Committed evidence
contains IDs, result flags, checksums and synthetic balances only. The local
onboarding compliance acknowledgement is exercised solely inside this disposable
database. Do not use these scripts with a real database or public address.

The ticket setting test verifies its initial off state, enable/disable visibility
without restart, masked proxy round trip and invalid scheme rejection. There are
no OAuth accounts: no real ticket collection or injection is claimed. Carpool
fixtures use the latest rolling plan and explicitly assert 28-day terms, equal
preview/open reset boundaries, quota API consistency and ordinary balance
isolation. Pagination uses three synthetic concurrency redemptions so the
ordinary balance remains unchanged. `mock/` separately tests actual Responses
HTTP and WebSocket routes against a deterministic internal upstream.

After the mock probe has completed, and before browser acceptance, run:

```powershell
python validation/upgrade-026/runtime/verify_rollback.py
```

It verifies the recovered production executable checksum, builds a separate old
image, stops the candidate, runs the old executable with the same synthetic
database/data/config, tests startup, login, carpool snapshot/key quota and old
group config writes, then stops it and restores the candidate. Nginx restarts at
each switch to refresh its static upstream DNS. Both applications never run
together. The old binary is read from the sibling recovered baseline directory.
The candidate is restored in `finally` even if an old-version check fails. The
report is written only if all checks, including the return to candidate, pass.

Resource lifecycle commands:

```powershell
python validation/upgrade-026/runtime/runtime.py register
python validation/upgrade-026/runtime/runtime.py check
# Stop this synthetic environment when its review is finished:
python validation/upgrade-026/runtime/runtime.py stop
# Explicitly discard its synthetic database and app-data volumes:
python validation/upgrade-026/runtime/runtime.py cleanup
```

Every lifecycle operation checks exact allowed names, the task label, recorded
container/network IDs, volume creation identity, network endpoints and volume
references before changing resources. Any foreign association or replaced
identity aborts the operation. Register newly created `mock`, `probe` and
`rollback` containers before lifecycle operations. Cleanup removes only the
recorded prefixed containers/networks/volumes; it retains build images and private
files. The initial candidate and mock inherited two unused anonymous PostgreSQL
volumes from the base image. Their exact identity and sole-container attachment
are recorded and verified, but cleanup deliberately retains these unlabelled
volumes. Updated launch commands mask that inherited volume with a tiny tmpfs.
No broad `docker prune`, container-name wildcard deletion or filesystem recursion
is used. Cleanup archives the registry so a later fresh run can register new IDs.
Container and network mutations target the verified immutable IDs, so a later
same-name replacement is not selected. Python optimized mode (`-O` or
`PYTHONOPTIMIZE`) is rejected before lifecycle checks or mutations.

The acceptance environment is left running for local browser review; stop and
cleanup were provided, not executed as part of acceptance. Evidence describes
local compatibility only. Production rollout, real OAuth tickets, external model
quality and production-volume performance are outside this run.
