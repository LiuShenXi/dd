# I3 gateway and frozen pricing owner receipt

Owner: sol_contract_review. No production API, database or model was called. No commit made. This receipt covers the gateway/pricing slice; it is not whole-I3 acceptance.

Current final pre-F1 freeze is the **37-file** `I3-owner-final-pre-F1-source-hashes.json`. After the broad owner regression below, the root authorized two additional test files for T22/T29 and one sender scalar, ExpectedAccountType from the selected Account.Type. The final targeted two-package run `I3-T22-T29-account-type-final-windows-unit.jsonl` passed 6 top-level / 29 including subtests, zero fail/skip; it includes JSON/SSE GPT transport compatibility, repeated-client-ID HTTP, actual sender type forwarding across the supported adapter matrix, and three endpoint type-rejection cases proving zero send. See `I3-T22-T29-account-type-receipt.md`. The older 31/35-file hashes below are historical slices, not the current final freeze. Whole-I3 F1 remains a separate main/environment result.

After the dashboard compatibility additions and force-cache error-path fix, `I3-owner-final-post-dashboard-windows-unit.jsonl` passed 142 top-level / 268 including subtests across four packages, zero fail/skip. `I3-owner-post-dashboard-source-hashes.json` records that source slice. It superseded the earlier three-package run for that slice; additional final source and tests are identified above. The force-cache test freezes true at actual Forward admission, calls the trusted-final-usage error submission helper, and verifies normal known persistence with input=0/cache-read=13/output=5. Chat/Responses term success jobs also capture the same scalar before their asynchronous closure.

Broad owner regression evidence is `I3-owner-final-post-dashboard-windows-unit.jsonl` and its then-current 35-file `I3-owner-post-dashboard-source-hashes.json`. It used Go 1.27.0 Windows `go test -tags=unit -json -count=1` and includes real TCP disconnect, the additional frozen force-cache path, audited resolution units, dashboard publication/retention units, and original pricing/forward/protected-mode regressions. Earlier `I3-gateway-owner-windows-unit.jsonl`, `I3-gateway-owner-final-windows-unit.jsonl` and the old 31-file `I3-owner-source-hashes.json` remain historical debugging evidence; they do not identify the final owner freeze.

## Supported adapters and limits

| Client endpoint | API key | OAuth / setup token | Vertex service account | Bedrock API key / SigV4 |
| --- | --- | --- | --- | --- |
| Messages JSON / SSE | Actual sender tested | Actual sender tested | Actual sender tested | Actual sender tested, including AWS EventStream frames |
| Chat Completions buffered / SSE | Actual sender tested | Shared final sender hook | Shared final sender hook | Explicitly rejected before admission/send; old converter has no signed Bedrock sender |
| Responses buffered / SSE | Actual sender tested | Shared final sender hook | Shared final sender hook | Explicitly rejected before admission/send; old converter has no signed Bedrock sender |

The route tests use production `RegisterGatewayRoutes` and auth middleware for six exact generating aliases. Responses suffixes/WS, ForcePlatform, Google and unrelated generation endpoints fail closed. Kiro has no baseline adapter and is not claimed as implemented.

Native provider server tools, including web search/fetch/code execution and unknown builtins, are rejected before any external call and before compatibility conversion can discard the declaration. Client function/custom tools, local shell, apply_patch, bash/computer/text editor/memory tools remain token-billed. Unexpected upstream server-tool evidence makes usage incomplete. `websearch_emulation` is unsupported for term billing; its text-length estimates and empty usage cannot become a receipt. Existing GPT behavior is unchanged.

Both count_tokens aliases retain authentication as metadata, call the existing input-count sender and create no term admission/ledger/usage record. Production-router tests configure admission disabled, negative ordinary balance and a billing port which would reject no-current/expired cards, then prove 200 input count with zero Admit calls while generation remains 503. `/v1/usage` remains explicitly unavailable in I3; I4 must implement the term remaining-allowance projection.

## Guarantees verified in this slice

- A durable admission succeeds before every supported final DoWithTLS point. All subsequent sends, including same-account internal retries and handler failover, are blocked for this request. Failed admission emits no upstream request. Server IDs cannot be replaced by upstream/client request IDs.
- A request mutex owns cloned snapshots and a forward heartbeat. HTTP exit does not stop the lease when mandatory usage is queued; the worker stops it after its durable transition attempt. Unknown attempts stay held.
- Raw provider usage distinguishes explicit zero from absence/null/invalid integers. message_start input/cache alone cannot prove final output. Cache read and 5m/1h creation are preserved across conversion. Raw models are observed before client model rewrites; conflicting model declarations retain baseline pricing.
- Converter client-write failures continue draining the upstream and retain complete final usage; missing final usage is unknown, and a buffered converter does not fabricate a completed response.
- Mandatory usage is submitted even with a drop-configured worker pool. Its immutable snapshot and context survive HTTP cancellation. `TestSubscriptionQuotaGatewayTCPDisconnectMandatoryKnownPersistence` then uses a real TCP client cancellation and sends final provider usage afterward. It exercises actual Forward → mandatory worker → actual RecordUsage → valid known receipt persistence → Apply/log (synthetic repository boundaries). Its complete 13-input/7-cache/5-output case and missing-final-output unknown/zero-known case both passed. An initial run lacked the fixture's required Apply/log dependencies; after fixing that fixture, the combined three-package run passed.
- Pricing deep-freezes the private ResolvedPricing channel and long-context fields, interval pointers, cache TTL rules, user/group and peak rules, final DB pricing time, tier/effort policy, response candidates, selected account multiplier and independent account-statistics rules. Calculation performs no mutable price lookup.
- Interval-only pricing supports matching intervals and rejects a gap without a base price rather than inventing zero. Account statistics use existing pure precedence rules, separately from account quota and user allowance. Manual verified calculation checks original model scope, counters and overflow and uses the original frozen force-cache rule.

## Deliberately separate evidence

This slice uses synthetic upstream transport/token-cache responses and local HTTP routers. It does not prove live provider eligibility. PostgreSQL admission/known/Apply/recovery/resolve/lifecycle evidence belongs to the environment owner's I3 PG receipt. Audited Resolve service units and the final owner regression, including old pricing/forward behavior, have passed; its repository and lifecycle transitions still require the environment owner's PostgreSQL evidence. No live provider/API or production database verification is claimed.
