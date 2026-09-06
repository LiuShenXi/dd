# Local mock HTTP configuration transition

`enable-local-mock-http.ps1` changes one property in the private local runtime
configuration:

```text
security.url_allowlist.enabled: true -> false
```

It requires `allow_insecure_http=true`, `allow_private_hosts=true`, and every
configured URL-policy host list to contain only `carpool-mock`. The isolated
Docker network, sanitized copied database, disabled real accounts, synthetic
mock-only accounts, and denied egress remain the safety boundary. This is not
a production configuration change and does not modify application source.

Before this transition restarts the application, the coordinator must stop the
verified task-owned loopback bridge. Docker validates the requested
`127.0.0.1:38088` binding when the application container starts even though the
internal-only network suppresses actual host publication. Never stop an
arbitrary process merely because it listens on port 38088. Confirm the bridge's
exact PID, its executable path under the private runtime directory, and its
expected parent process before stopping it.

Before stopping anything, the script validates the exact healthy startup
state, sealed config/environment/fixture hashes, immutable image, app/mock
roles, database/Redis binding, and original code-ready evidence. It archives
the exact config, resource binding, startup state, and ready state into a
unique private directory, together with precomputed intended replacements and
a durable phase manifest.

Only the exact application container ID is gracefully stopped. The database,
Redis, mock, volumes, image, fixtures, environment, baselines, secrets, ports,
and networks are not changed. The script updates the config hash in the sealed
binding and startup state, clears only `ConfigInstalled` and `Healthy`, and
delegates config installation and health/isolation verification to the
existing `start-runtime.ps1 -Resume` path. The same app, mock, image, network,
volume, fixtures, and run nonce are required after resume.

Normal invocation:

```powershell
pwsh -NoProfile -File validation/source-runtime/enable-local-mock-http.ps1 `
  -CodeReadyEvidence <original-code-ready-evidence.json>
```

If a post-archive transition fails, use the exact recovery command reported by
the script. Recovery accepts only the archived original or intended file
hashes and phases; it never requires manual state editing. Completed manifests
refuse replay. Manifest parent-directory lookup intentionally uses
`[IO.Path]::GetDirectoryName`; preserve this compatibility fix and do not
replace it with the earlier path-parent lookup.

Relaunch the loopback bridge only after the resumed application passes both
internal health and denied-egress checks. The new bridge invocation must read
the fresh startup state and capture the recreated application container, image,
and network IDs rather than retaining identities from the stopped application.

Source-only checks do not invoke Docker or the runtime:

```powershell
pwsh -NoProfile -File validation/source-runtime/local-mock-http/transition-lib.Tests.ps1
```
