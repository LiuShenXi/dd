# Loopback host access

This Windows-only bridge restores browser access when Docker Desktop retains
the reviewed `127.0.0.1:38088` port binding in `HostConfig` but suppresses the
actual host listener for an internal-only Docker network.

The bridge is deliberately fixed to `127.0.0.1:38088`. For each accepted TCP
connection it re-reads the private healthy startup state, re-inspects the exact
immutable application container ID through the local Docker named pipe, and
requires the original task label, run nonce, image ID, internal network name,
and network ID. It then starts exactly:

```text
docker.exe exec -i <verified-app-id> /bin/busybox nc 127.0.0.1 8080
```

No command shell is involved. The bridge does not attach a network, publish a
port, mount the Docker socket, read fixtures, or handle credentials. HTTP,
SSE, and WebSocket traffic remain raw TCP bytes.

Start it in the foreground after the source runtime is healthy:

```powershell
pwsh -NoProfile -File validation/source-runtime/host-access/start-host-access.ps1
```

## Restart and rebuild coordination

Before any application restart or rebuild, the coordinator must stop the
verified task-owned bridge. Docker validates the application's requested
`127.0.0.1:38088` binding during container start even though the internal-only
network suppresses actual host publication. Never stop an arbitrary process
merely because it listens on port 38088. Confirm the bridge's exact PID, its
executable path under the private runtime directory, and its expected parent
process before stopping it.

Relaunch the bridge only after the application passes both internal health and
denied-egress checks. The new bridge invocation must read the fresh startup
state and capture the recreated application container, image, and network IDs;
do not retain identities from the stopped runtime. The local-mock transition
and rebuild recovery manifests intentionally derive their parent directories
with `[IO.Path]::GetDirectoryName`. Preserve this compatibility fix and do not
replace it with the earlier path-parent lookup.

`-MaxConnections` defaults to 32 and is restricted to 1 through 128. Excess
connections are closed immediately. Ctrl+C closes the listener and cancels
active Docker CLI processes.

## Limitations

- This is a local validation bridge, not a production reverse proxy. Every TCP
  connection pays one Docker inspection and one `docker exec` startup cost.
- It binds only IPv4 loopback. Remote hosts and IPv6 loopback cannot connect.
- A runtime rebuild changes the captured identity. Existing bridge sessions
  are canceled only when their TCP connection ends, while every new connection
  fails closed until the bridge is restarted against the new healthy state.
- Container identity and network state are inspected immediately before each
  exec. Docker does not provide an atomic inspect-and-exec operation, so a
  privileged local operator could still mutate networking in that narrow
  interval. The exact immutable container ID prevents replacement by name, and
  the exec target remains the application's own loopback address.
- Transport shutdown follows ordinary full-close behavior. Clients that rely
  on unusual TCP half-close semantics should be tested separately.

Source-only verification does not contact Docker or start the runtime:

```powershell
go test ./...
go build -trimpath -o "$env:TEMP/carpool-host-access.exe" .
```
