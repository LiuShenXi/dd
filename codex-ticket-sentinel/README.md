# Codex ticket sentinel

生产状态及操作命令见 [DEPLOYMENT.md](DEPLOYMENT.md)。服务已部署，真实续票尚未验证成功；详见其中的验证边界。

A Python standard-library controller that polls a restricted Sub2API interface,
persists its scheduling state in SQLite, and requests targeted ticket refreshes.
Sub2API owns OAuth, harvesting through the configured proxy, ticket storage and
cache propagation. No database, Redis, OAuth or Docker credentials are supplied
to the controller.

The initial scope is OAuth account **2**, models **gpt-6-astra** and
**gpt-5.6-sol**, using Sub2API's existing **US-static-car8** harvest proxy.
The controller reacts to 312-length turn state, terminal upstream model mismatch,
and missing or expiring tickets. Ticket length is an operational signal, not a
quality or revocation guarantee. Refreshes affect subsequent requests; business
requests are never replayed.

For availability, run Sub2API with
`GATEWAY_OPENAI_CODEX_TICKET_FAIL_CLOSED=false`: inject a valid ticket when one
exists, and forward normally when collection has not produced a usable ticket.
This setting does not create a ticket or guarantee upstream quality. A valid
292 observed again with identical bytes renews the native local lease; the
controller requires the newly persisted and cache-visible capture timestamp
before acknowledging a `renewed` result.

This feature requires the companion Sub2API integration patch and a new Sub2API
image. The previously deployed compatible image alone does not expose the full
312/model-event protocol. See [CONTRACT.md](CONTRACT.md) for authentication,
metadata-only events, version checks, rate limits and persistence semantics.

Deployment files are in [deploy/](deploy/); follow [OPERATIONS.md](OPERATIONS.md)
for staging, permissions, verification and rollback. The sidecar uses Python
3.12, runs as UID/GID 10001 with a read-only root filesystem, and has no listener.
The default budget is 64 MiB and 0.25 CPU; verify actual consumption before
expanding its scope.

HTTP connect, TLS, headers and body reads share one request deadline. DNS is
managed by Docker's local resolver in this deployment; an operating-system DNS
lookup is not guaranteed to obey that hard deadline. A process lock on the
local state volume prevents a second controller from resetting live jobs or
overwriting status. Keep that volume shared and run only one configured service;
separate state volumes do not provide distributed exclusion.

Stopping this sidecar stops event-driven sentinel work. Sub2API's native ticket
collector continues to run. Disable Sub2API's global ticket switch to disable
all ticket mechanisms. A healthy controller only proves recent control-plane
polling; it does not guarantee renewal success, model identity or output quality.
