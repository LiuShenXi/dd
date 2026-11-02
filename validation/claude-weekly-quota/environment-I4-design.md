# Reserved isolated I4 design

Status: design only; no I4 provider, application container, browser listener or
credential has been created. I3 tests and image recovery remain the active work.

Retain the resources enumerated in environment-active.json: task-only internal
network 10.247.190.0/24, PostgreSQL18, Redis and Linux Go runner, with zero published
ports and no Docker socket mounts. The runner has8GiB after the approved isolated
limit update in environment-I3-runner-limits.json. No unrelated containers or
networks may be changed.

After I4 authorization, use new uniquely named synthetic database/Redis namespace,
application and deterministic provider containers on the retained internal
network. The provider will emit fixed JSON/SSE usage and implement only test
routes, without Internet egress. Generate fresh synthetic secrets locally; keep
them out of commands, receipts and screenshots. Never reuse production tokens,
accounts, URLs or data.

The browser entry point must publish only127.0.0.1 on a verified free task port.
No host port is allocated by this document. Recheck actual listeners and Docker
port bindings immediately before allocation. Browser access is separate from
the internal provider transport; the provider needs no host listener.

T45 must cover all six generation aliases in both JSON and SSE through the
actual router/auth/Forward/RecordUsage and real PG ledger. Prepare synthetic
admin-issued A100/B300 cards, multiple Keys and interleaved original GPT traffic.
Evidence must retain each request's server UUID, original term/card/user/Key and
account, exact decimal costs, ledger/request/dedup/log acknowledgements and
complete opposite-wallet/foreign-row zero deltas. HTTP200 or a fake billing port
alone is not acceptance of this future end-to-end scope.

The I3 image proves current-receipt recovery only. I5 paid-order schema and
fulfillment recovery require final I6 compatibility verification or another R1
freeze. No deployment or activation is authorized by this design.
