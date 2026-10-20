New isolated proxy: `http://127.0.0.1:54551`, forwarding only to the existing synthetic browser candidate `http://127.0.0.1:49441`. The direct candidate and old59160 proxy remain running. New proxy receipt: `environment-I4-ui-proxy-20261019T230958Z.json`. No browser was controlled by the environment agent.

Control is `GET /__cwq/control` and `POST /__cwq/control` with JSON. Every rule has a unique `rule_id` and matches exactly one path/method. Rules are single-use. Before a new scenario, inspect all rules and release existing held responses. Never arm generation/model routes.

Independent slow preview gates, with actual backend preview responses:

```json
{"action":"arm","rule_id":"preview_A_1","path":"/api/v1/admin/users/2/subscription-quota/terms/preview","method":"POST","phase":"after_backend_response","effect":"hold"}
{"action":"arm","rule_id":"preview_B_1","path":"/api/v1/admin/users/3/subscription-quota/terms/preview","method":"POST","phase":"after_backend_response","effect":"hold"}
```

A/B already have cards: use renew previews. Observe each rule's `held:true`. Close A/open B in the browser; while B is still held, release A only:

```json
{"action":"release","rule_id":"preview_A_1"}
```

Inspect B still `held:true` and its DOM still pending. Then release B. To deliver a deliberately late A preview error, release A with `"status":503`; this is explicitly a test-proxy error, after the real preview response. Use fresh rule IDs for each scenario. Early release is safe: its closed gate remains available when the backend response arrives.

One committed C open/renew response loss:

```json
{"action":"arm","rule_id":"C_commit_lost_1","path":"/api/v1/admin/users/4/subscription-quota/terms","method":"POST","phase":"after_backend_response","effect":"drop"}
```

The proxy waits for a real2xx envelope `code:0` with positive `data.term.id` and explicit boolean `data.replayed`, reads it fully, then closes the client TCP socket with reset. Only that valid successful response consumes the drop; unsuccessful backend responses pass unchanged and leave the rule unconsumed. `valid_successful_term_response:true`, `dropped:1`, and safe metadata identify the actual commit. This supports open or renew; it never creates or edits a payload.

Before a **manual** retry, arm another hold on the same C path to capture the real replay response:

```json
{"action":"arm","rule_id":"C_manual_retry_1","path":"/api/v1/admin/users/4/subscription-quota/terms","method":"POST","phase":"after_backend_response","effect":"hold"}
```

Click Confirm again without editing amount/notes/operation or requesting another preview. Compare original/retry `body_sha256`, `idempotency_key_sha256`, `term_id`; the successful retry must return actual `replayed:true`. Release its hold. If the browser automatically retries transport instead of exposing an error, record that actual behavior and do not label it a manual second click. The Go smoke uses fresh connections because Go automatically retries replayable POSTs carrying Idempotency-Key on reused failed sockets.

One genuine **proxy-side before-upstream**409, with no backend RoundTrip:

```json
{"action":"arm","rule_id":"C_before409_1","path":"/api/v1/admin/users/4/subscription-quota/terms","method":"POST","phase":"before_upstream","effect":"reject","status":409}
```

It returns HTTP409 with integer `code:409`, reason `QUOTA_REVISION_CONFLICT` and a clearly synthetic message. `upstream_round_trips:0`, `rejected_before_upstream:1`, and `upstream_status:null` must hold. Independently compare C's terms/operations before/after when executing the UI scenario. This is a controlled transport-layer conflict for UI behavior, not a claim that the real backend rejected a stale revision. Do not implement409 by committing a card and altering the resulting success status.

Unused rules may be disarmed:

```json
{"action":"disarm","rule_id":"unused_rule"}
```

Control observations contain only bodySHA256, idempotency-keySHA256, upstreamstatus, termID and replayed; no authorization header, raw idempotency key, password, API Key or raw payload is logged. Counts/rule paths are control metadata. The actual application image/business/embedded frontend are unchanged. Non-business httptest smoke covers independent gates, success-only exactly-once reset+retry hashes, and zero-RoundTrip409; it does not claim browser DOM acceptance.
