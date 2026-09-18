# Sub2API sentinel control protocol v1

Requests use `Authorization: Bearer <dedicated token>`. Token values, ticket values, credentials and prompts never appear in responses or logs. Endpoints are default-disabled without explicit environment configuration; reject non-allowlisted account/model pairs and inactive/unschedulable/non-OAuth accounts. No existing administrator API credential is reused.

`GET /internal/codex-sentinel/status?after=SEQ` returns direct JSON (no envelope):

```json
{"protocol_version":1,"instance_id":"random-instance-id","enabled":true,"latest_seq":12,"oldest_seq":1,"events":[{"seq":12,"account_id":2,"model":"gpt-6-astra","kind":"model_mismatch","expected_ticket_version":"sha256-of-ticket","actual_model":"gpt-5.6-luna","observed_at":"2026-09-19T00:00:00Z"}],"targets":[{"account_id":2,"model":"gpt-6-astra","eligible":true,"ready":true,"ticket_version":"sha256-of-ticket","expires_at":"2026-09-19T01:00:00Z","captured_at":"2026-09-19T00:00:00Z","remaining_seconds":3600}],"dropped_events":0}
```

`enabled` reflects the live ticket master switch. `eligible` includes configured allowlist, active/schedulable OAuth account and configured harvest proxy. Event kinds are `turn_state_312` and `model_mismatch`; the latter requires the **actual original upstream `response.completed`** model, compared to the actual outbound model before client-facing mapping. Sequence is monotonic within instance, ring capacity 1024. Targets include all configured account/model pairs; ineligible targets have no actionable ticket. `ticket_version` is an empty string when no usable ticket is present. Missing dates may be omitted. `oldest_seq` is the oldest retained event or `latest_seq+1` if empty. Events newer than the supplied cursor are returned. A sidecar encountering a new instance re-fetches from after=0 before accepting a high cursor from the old instance.

`POST /internal/codex-sentinel/refresh` accepts bounded JSON:

```json
{"account_id":2,"model":"gpt-6-astra","reason":"model_mismatch","expected_ticket_version":"sha256-of-ticket","event_id":"instance:12"}
```

Reasons: `turn_state_312`, `model_mismatch`, `expiry`, `missing`. HTTP request timeout 40 seconds, native probe bounded to 25 seconds. Successful response is returned only after operation completion:

```json
{"status":"refreshed","account_id":2,"model":"gpt-6-astra","ticket_version":"new-sha256","persisted":true,"ready":true}
```

If a new native harvest returns the same valid blob, preserve the native
collector's renewal semantics and return `status=renewed` after verifying the
new capture/expiry generation in both database and cache. This does not claim
the blob changed. The response additionally contains `previous_ticket_version`
(equal to `ticket_version`) and integer `captured_at_unix_ms` and
`previous_captured_at_unix_ms`, with capture time strictly advancing. The
controller requires the new capture within 120 seconds of its clock, all target
and persistence checks, and an unchanged expected hash. An empty expected hash
is permitted only for a missing/expired job; the returned previous hash still
must equal the reissued blob hash. The old blob can have expired locally before
this successful reissue. A local TTL remains a local policy, not an upstream
expiry guarantee. Later observations of an identical blob remain queued and
are checked against their real send timestamp by Sub2API.

`status=stale` (HTTP 200) means the expected version no longer matches or the observation predates the current capture generation, and no collection was performed; `status=disabled`/`ineligible` (HTTP 200) means no collection was performed. `status=cooldown` (HTTP 429) includes integer `retry_after_seconds`. Failed collection or an unverified capture generation returns `status=failed`, `reason` as a fixed safe code, and `retry_after_seconds` (HTTP 503). Native singleflight also deduplicates regular collector probes. A server-side cooldown of at least 30 seconds applies independently of the sidecar. Preserve existing valid tickets on all unsuccessful attempts. Never replay business requests. On successful save use native account repository merge/cache propagation and verify persisted version; report propagation uncertainty honestly.

Proposed Sub2API configuration: `CODEX_SENTINEL_TOKEN_FILE`, `CODEX_SENTINEL_ACCOUNT_IDS=2`, `CODEX_SENTINEL_MODELS=gpt-6-astra,gpt-5.6-sol`. Presence of a valid private token file and nonempty allowlists enables the integration; the live global ticket switch still controls behavior. Routes return 404 when integration is unconfigured. The sidecar does not need a listener; it polls the authenticated interface and persists jobs/cursor in SQLite.
