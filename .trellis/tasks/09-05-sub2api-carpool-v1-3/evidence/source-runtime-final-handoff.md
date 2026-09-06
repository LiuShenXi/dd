# Source Runtime Final Handoff

Source-owned implementation support, runtime and verification are complete. The director owns final integrated acceptance and task/checklist state. No commit, push, archive or production deployment was performed.

## Local Runtime

- URL: `http://127.0.0.1:38088`; host health returned 200 after final UI.
- Worktree: `C:/WORK-SPACE/sub2api-carpool-v1.3`, branch `codex/sub2api-carpool-v1.3`.
- Baseline HEAD: `36266f512776d78d4f1645a75ae0e84816f8a0a7`; local changes remain uncommitted.
- Image: `sha256:6d569031eb1b5017fe7c3a7f5f14ed6a863442d9f49d7b2dc372d586b3c7adfd`.
- Immutable approval: `director-code-ready-final-image-v5.json`, SHA256 `2F8841CEB2659B384ABAF5642F56A725B26B35986AD556115ACFD4290FC8A422`.
- Private bridge PID 76160, parent 30284; only `127.0.0.1:38088` is bound. PostgreSQL/Redis have no host ports; app/mock use the internal-only network with denied egress.

## Passed Evidence

| Check | Result | Evidence |
| --- | --- | --- |
| Final HTTP | 67/67 | `source-runtime-http-20260905T235449813Z.json` |
| Strict WebSocket | 11/11 | `source-runtime-websocket-20260905T235608621Z.json` |
| Coherent reset pending merge | 22/22 | `source-runtime-reset-20260905T235712249Z.json` |
| Accounting/reset/recovery/lifecycle PostgreSQL | Passed named actual runs | `source-runtime-pg-progress.md` |
| Final UI safety and original-data preservation | Passed | `source-runtime-v5-post-ui-20260906T001713694Z.json` |
| Final test-only checkout produces identical V5 product | Passed | `source-runtime-v5-final-equivalence-20260906T001844573Z.json` |
| Real desktop/390px browser evidence | Director-owned | `director-ui-acceptance.md` |

Final additive lifecycle run `20260906T001828Z-46e6ee52` passed four business tests, one harness test and four nested unresolved-state cases. Its isolated synthetic resources and active manifest were cleaned. All source foreground sessions are complete; the local app/DB/cache/mock and hidden loopback bridge intentionally remain available.

## Data Boundaries

The authorized remote read-only export was restored into restricted local storage outside Git and sanitized before startup. Original encrypted archive and private logs remain restricted. Original 13 users, 18 keys, 41154 usage rows and 41154 dedup rows retain their conservation baselines; registration timestamps, ordinary balances and associations were preserved. No production writes, copied credential usage, upstream/card use, real charges/refunds or external notifications occurred.

Copied-user local membership starts must equal original `users.created_at` and expire exactly 30 days later. No reliable plan mapping was found, so no copied-user memberships were invented. Synthetic identities cover all three plans and lifecycle cases. The UI-created synthetic user 127 retains balance 100 USD and unchanged key 64; it has exactly one term 94/grant 550 USD, fifth target 157 USD and no payment record.

Synthetic batch 1 remains scheduled for 2026-09-06 22:00 Shanghai with no grant yet. Announcement 10 and the original qualification rows were preserved. Later API runs only merged their own qualification once; final count is three. The runtime must remain isolated when left running.

## Honest Limits

First-publication evidence is composed: V3 had 20 passes and one version-audience failure with an unretained baseline; the original report remains failed. Deterministic driver-race reproduction, stable read-only audience, final coherent merge/read-state checks and real PostgreSQL publication tests provide separate evidence. Actual due-time execution is covered by PostgreSQL tests, not claimed as live daytime HTTP execution.

The full backend service suite remains non-green due to independently reproduced baseline Windows plugin rename failures and intermittent moderation timeout. One V4 post-opening refresh timed out and recovered with read-only retry; successful single creation is proven, but the specific client/bridge cause is unknown. No speculative bridge fix was made.
