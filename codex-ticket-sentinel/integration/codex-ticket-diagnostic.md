# One-shot native ticket diagnostic

This is a temporary separate executable. It has no production route, no collector loop, no persistence, no token provider, and no DB/Redis client. Normal builds exclude all diagnostic files via `sentineldiagnostic` build tags. The executable does nothing without `CODEX_TICKET_DIAGNOSTIC_FILE`.

Build from the sentinel integration worktree backend:

```powershell
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go build -tags sentineldiagnostic -trimpath -ldflags '-s -w' -o /chosen/output/codex-ticket-diagnostic ./cmd/codex-ticket-diagnostic
```

Input must be a regular nonsymlink file, owned by root, permission 0600 or 0400, at most 32 KiB. The executable itself must run as UID 0. Generate the file only on BWH, never transmit its contents to the workstation. Only the following JSON fields are accepted:

- `account_id`: must be 2
- `model`: `gpt-6-astra` or `gpt-5.6-sol`
- `access_token`: current account token
- `chatgpt_account_id`: current account credential value
- `chatgpt_account_is_fedramp`: boolean matching the account credential
- `account_concurrency`: positive integer matching account 2
- `harvest_proxy_url`: current dedicated harvest proxy URL (required; never default to direct)
- `old_ticket_sha256`: 64 hex characters computed server-side from the existing model ticket state
- `response_header_timeout_seconds`: current Gateway.ResponseHeaderTimeout (normally 600; independent hard probe deadline 25 seconds)
- `url_allowlist_enabled`, `url_allowlist_allow_private_hosts`: current security URL allowlist booleans
- `disable_codex_identity_enforcement`: current gateway identity-enforcement config boolean

Use the already available production runtime image for CA certificates, override its entrypoint with this read-only mounted executable, and mount only this one input file. Use a separate disposable container on ordinary egress bridge networking, not the production DB/Redis network. No production configuration or container restart is required. Suggested container isolation: `--rm --read-only --cap-drop ALL --security-opt no-new-privileges --memory 128m --pids-limit 32 --user 0:0`. Set `CODEX_TICKET_DIAGNOSTIC_FILE` to the mounted private file. Run exactly once for the chosen account/model.

The shim invokes production `fireOpenAICodexTicketProbe` exactly once through `repository.NewHTTPUpstream(cfg)`. Native `openai_harvest` selects HTTP/1.1 with connection reuse disabled and the standard production TLS/proxy implementation. It sends the same synthetic ping, closes the response body, and does not consume ordinary user traffic. Native redirect behavior is unchanged; no application retry is added.

Stdout contains one safe JSON record: `http_status`, `ticket_length`, `ticket_prefix_ok`, `same_as_old_ticket`, `error_category`, `duration_ms`. Error categories are fixed strings: `new_ticket`, `same_ticket`, `turn_state_312`, `http_non_200`, `missing_ticket`, `unexpected_ticket_length`, `invalid_ticket_prefix`, `timeout`, `canceled`, `dns`, `tls_certificate`, `network`, `transport`, or preflight failure categories. No raw error, response body, header, ticket/hash, account credential, token, or proxy URL is printed. Transport log output is discarded in this diagnostic process.

A successful diagnostic does not update ticket timestamps or stored ticket contents. Remove the temporary container, executable, and private input after collecting the safe report.
