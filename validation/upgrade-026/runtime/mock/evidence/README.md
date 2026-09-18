# Runtime HTTP and WebSocket acceptance

Both paths passed against the built candidate and its actual PostgreSQL/Redis
runtime on 2026-09-19 (Asia/Shanghai). Data and credentials were newly generated
for each run; the upstream was the deterministic local mock. No real account,
production database, or external upstream was used.

| Evidence | Client HTTP route | Upstream HTTP route | WebSocket |
| --- | --- | --- | --- |
| `http-ws-compat-conversion.json` | `/v1/responses` | `/v1/chat/completions`, converted back to Responses | Two Responses turns on one client connection |
| `http-ws-native-responses.json` | `/v1/responses` | `/v1/responses`, explicitly selected for the mock account | Two Responses turns on one client connection |

The native run exited **0**, with **5/5** assertions passing. Its synthetic IDs
were user **9**, group **11**, account **4**, key **8**, term **7**, and cycle
**7**. Each run started with zero usage and ordinary balance **47.25 USD**.

| Checkpoint | Settled receipts | Durable snapshots | Usage ledger rows | Ledger delta | Usage log rows / actual cost | Ordinary balance |
| --- | ---: | ---: | ---: | ---: | --- | ---: |
| Before requests | 0 | 0 | 0 | 0 | 0 / 0 | 47.25 |
| After one HTTP request | 1 | 1 | 1 | -1 | 1 / 1 | 47.25 |
| After two more WebSocket turns | 3 | 3 | 3 | -3 | 3 / 3 | 47.25 |

All three receipts had distinct request IDs and valid persisted admission
snapshots. All usage referenced the expected term, cycle, group, account, user,
and key. The second WebSocket turn supplied the first turn's
`previous_response_id` and received a different completed response ID. Native
upstream endpoint counters increased by exactly three Responses requests and
zero Chat Completions requests.

Two harness corrections preceded acceptance. First, this isolated HTTP mock
needed the established local-only URL-policy configuration: enabled allowlist
validation requires HTTPS, irrespective of `allow_insecure_http`. No product
security code changed. Second, global mock counters include account capability
probes and idle WebSocket pool connections, so the final probe uses the run's
unique model and counter deltas for inference assertions. The original failure
reports remain in the private runtime directory.

Automatic capability probing tests tool-call behavior that this simple mock
does not implement. That explains the successful compatibility-conversion run;
the native run uses the existing account option
`openai_responses_mode=force_responses`. Both results are kept separately to
avoid claiming the conversion run tested native upstream HTTP Responses.

The provenance JSON files record source/binary hashes; the native provenance
also identifies the candidate image and source-input digest. Probe SQL was
read-only. Container lifecycle and the separate real-old-binary rollback were
controlled by the runtime owner. The probe did not restart the candidate, edit
backend code, or modify the other agents' browser fixtures.
