# Candidate HTTP and WebSocket probe

`probe.go` exercises the actual candidate server using only fresh synthetic
fixtures and the existing deterministic upstream at
`validation/source-runtime/mock/main.go`. The mock has no outbound client.

The runtime owner must start both binaries solely on
`sub2api-upgrade026-app-internal` and give the mock the alias `carpool-mock`.
Do not publish the mock or probe ports. The probe allows only the fixed app and
mock URLs and the synthetic `upgrade026` PostgreSQL database. Its SQL connection
sets `default_transaction_read_only=on`; fixture creation goes through the real
admin/user APIs.

Build both Linux executables from the existing backend module (no new module):

```powershell
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:GOTOOLCHAIN = 'go1.27.0'
$probePrivate = Join-Path $env:LOCALAPPDATA 'Codex/PrivateTests/sub2api-upgrade-026-20260918/runtime'
go build -trimpath -o (Join-Path $probePrivate 'carpool-mock') ../validation/source-runtime/mock/main.go
go build -trimpath -o (Join-Path $probePrivate 'carpool-probe') ../validation/upgrade-026/runtime/mock/probe.go
```

The probe expects `app-credentials.private.json` and `postgres.private.env` from
the isolated runtime's bootstrap under `/private`. Run with
`-private-dir /private`; mount that private directory writable and the executable
read-only. Use the existing task label, no external network, and no host ports.
When using the existing PostgreSQL image as a minimal runtime, provide a tmpfs
at `/var/lib/postgresql` so its image-declared volume does not create an unused
anonymous volume. Runtime ownership and cleanup remain with `runtime.py`'s
owner; this probe never creates or stops Docker resources.

The existing gateway permits plain HTTP only when its URL allowlist is disabled;
`allow_insecure_http=true` alone does not override enabled HTTPS validation. The
runtime owner must use the existing synthetic-only configuration contract in
`validation/source-runtime/local-mock-http/README.md`, with external egress
blocked by the internal Docker network. This changes only the disposable
runtime configuration, never production settings or application security code.

Every run creates a new synthetic user with ordinary balance **47.25**, new
standard/carpool groups, mock-backed API-key account, user API key, and carpool
term. It creates the key in the allowed standard group and uses the admin bind
endpoint to select the carpool group. Its unique synthetic model uses a group
price of **1 USD per request**. Its account uses the existing
`openai_responses_mode=force_responses` option: automatic capability probing
requires tool-call behavior beyond this simple mock, so forcing the mock's
known Responses endpoint makes the intended protocol deterministic. Assertions
cover:

- HTTP Responses returns `completed`, then exactly one settled durable receipt,
  one usage ledger debit, and one usage log appear.
- Two turns on one WebSocket, with `previous_response_id` on the second turn,
  have distinct completed response IDs and independently settle.
- The final three receipts match their persisted admission snapshots; ledger
  delta is exactly **-3 USD**, logged actual cost is **3 USD**, and all usage
  references the synthetic carpool term/cycle/group/account.
- Ordinary balance remains exactly **47.25** before and after both transports.
- For the run's unique model, the upstream sees exactly one HTTP inference and
  two WebSocket turns. The client uses one WebSocket connection. Account
  background probes and connection-pool prewarming may add other mock requests
  or idle upstream connections; the report retains these global observations,
  while assertions compare counters for the unique model before and after.
- Native upstream `/v1/responses` has three additional inferences and
  `/v1/chat/completions` has none. The earlier successful automatic-routing run
  is preserved separately under `evidence/http-ws-compat-conversion*.json`; that
  run covered client Responses to upstream Chat Completions conversion.

All generated fixture credentials and diagnostic response bodies are written
only below `/private`. `http-ws-probe-report.json` contains no credentials and can
be copied to the evidence directory after inspection. The probe exits nonzero
on any failure, prints only the failing stage/category, and has a three-minute
total timeout. Synthetic fixture records are retained in the disposable runtime
for rollback inspection; no production data is used.
