# Synthetic OpenAI Upstream

This directory contains a deterministic, local-only OpenAI-compatible upstream for carpool billing acceptance. It has no outbound client, does not read production files or credentials, and never stores prompts or authorization headers.

## Build and run

Use the repository's existing backend module and pinned Go toolchain. No second dependency module is needed.

```powershell
Set-Location C:\WORK-SPACE\sub2api-carpool-v1.3\backend
$go = 'C:\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.27.0.windows-amd64\bin\go.exe'
& $go test ..\validation\source-runtime\mock\main.go ..\validation\source-runtime\mock\main_test.go
& $go build -o "$env:TEMP\sub2api-mock-upstream.exe" ..\validation\source-runtime\mock\main.go
```

Tests use `httptest` and do not leave a listener running. The task runtime starts the static `/app/mock-upstream` binary; it requires no arguments and listens on `:8090` by default.

Within the task's isolated application network, configure the synthetic OpenAI API-key account with base URL `http://carpool-mock:8090`. The mock supports both default paths and `/v1` compatibility paths. Do not publish or host-map this service, and never attach it to production.

## OpenAI-compatible endpoints

- `GET /v1/models`
- `POST /v1/chat/completions` and `POST /chat/completions`
- `POST /v1/responses` and `POST /responses`
- `GET /v1/responses` and `GET /responses` with a WebSocket upgrade

JSON and SSE responses include deterministic text and usage. Defaults are `1000` input tokens and `100` output tokens for `gpt-4o-mini`. Every request has a unique `X-Request-ID`; every inference has a unique `chatcmpl_mock_*` or `resp_mock_*` response ID. Responses WebSocket connections accept multiple `response.create` turns and emit `response.created`, output events, done events, and `response.completed` with usage.

Example HTTP request from inside the isolated task network:

```powershell
Invoke-RestMethod -Method Post http://carpool-mock:8090/v1/responses `
  -Headers @{ Authorization = 'Bearer synthetic-only' } `
  -ContentType application/json `
  -Body '{"model":"gpt-4o-mini","input":"synthetic acceptance","stream":false}'
```

## Control cases

`POST /__control` is accepted only from loopback or private-network peers. Request bodies are limited to 16 KiB. `delay_ms` is capped at 10 seconds, `failures` at 100, token counts at 10,000,000, and output at 4 KiB.

A named case is selected by `X-Mock-Case` first, then by a matching request `model`:

```powershell
Invoke-RestMethod -Method Post http://carpool-mock:8090/__control `
  -ContentType application/json `
  -Body '{"mode":"named","name":"retry-429","input_tokens":1200,"output_tokens":80,"output":"synthetic retry result","delay_ms":25,"status":429,"failures":2}'
```

The first two matching requests fail with `429`; later matches succeed with the configured usage and output. `status` is the configured failure status and must be `400..599`.

A next case applies to the next otherwise-unmatched requests. It produces the configured number of failures followed by one success, then is removed:

```powershell
Invoke-RestMethod -Method Post http://carpool-mock:8090/__control `
  -ContentType application/json `
  -Body '{"mode":"next","name":"one-retry","status":503,"failures":1,"input_tokens":1000,"output_tokens":100}'
```

Explicit named selection takes precedence over the queued next case. WebSocket failures are represented by `response.failed`, while HTTP failures use the configured status and a bounded JSON error.

## Aggregate statistics

`GET /__stats` returns counts by endpoint, transport, case, and status plus WebSocket connection count. It never returns request bodies, prompts, authorization headers, or arbitrary model names.

```powershell
Invoke-RestMethod http://carpool-mock:8090/__stats
Invoke-RestMethod 'http://carpool-mock:8090/__stats?reset=1'
Invoke-RestMethod -Method Delete http://carpool-mock:8090/__stats
```

The reset operation clears aggregates but does not reset the ID sequence, so IDs remain unique for the server lifetime.
