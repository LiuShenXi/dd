# Controlled source-runtime rebuild

`rebuild-runtime.ps1` replaces only the isolated application and mock
containers after a director-approved code change. It preserves the copied
PostgreSQL and Redis containers, application data volume, installed private
configuration, synthetic fixture file, network, and original runtime nonce.

The normal command is:

```powershell
pwsh -NoProfile -File validation/source-runtime/rebuild-runtime.ps1 `
  -CodeReadyEvidence <new-director-approved-evidence-file>
```

The script first validates the healthy current runtime and both conservation
checks. It builds the Linux application, mock, bootstrap, and a uniquely tagged
image while the old application remains running. Before any stop, it writes a
private create-only archive and durable rebuild manifest under
`runtime/rebuilds/`. Archived resource metadata is retained only as SHA-256
digests; raw container environment metadata is not written to the archive.

Before any application restart or rebuild stop, the coordinator must first stop
the verified task-owned loopback bridge. Docker validates the requested
`127.0.0.1:38088` binding when the replacement application container starts
even though the internal-only network suppresses actual host publication.
Never stop an arbitrary process merely because it listens on port 38088.
Confirm the bridge's exact PID, its executable path under the private runtime
directory, and its expected parent process before stopping it.

Only the exact app and mock container IDs recorded in the old startup state may
be gracefully stopped and removed. The script never removes volumes, the
database, Redis, fixtures, archives, or unrelated containers. It promotes the
new image tag only after the old containers are gone, commits an explicit
unhealthy continuation state, and delegates exact recreation, health, and
egress checks to `start-runtime.ps1 -Resume`.

If interruption occurs after the durable manifest is written, use the exact
manifest path reported in the private runtime directory:

```powershell
pwsh -NoProfile -File validation/source-runtime/rebuild-runtime.ps1 `
  -CodeReadyEvidence <same-new-evidence-file> `
  -ResumeRebuild <private-rebuild-manifest.json>
```

Resume revalidates the approved code, archive hashes, prepared image, config,
environment, tooling, network, original IDs, and current phase. It may remove a
remaining old container only when its exact ID and role still match. Once the
continuation state is committed, resume delegates only to the existing startup
recovery path. A completed manifest refuses replay. Rebuild manifest
parent-directory lookup intentionally uses `[IO.Path]::GetDirectoryName`;
preserve this compatibility fix and do not replace it with the earlier
path-parent lookup.

Relaunch the loopback bridge only after the rebuilt application passes both
internal health and denied-egress checks. The new bridge invocation must read
the fresh startup state and capture the replacement application container,
image, and network IDs rather than retaining identities from the old runtime.

Source-level tests do not invoke Docker or the runtime:

```powershell
pwsh -NoProfile -File validation/source-runtime/rebuild/rebuild-lib.Tests.ps1
```
