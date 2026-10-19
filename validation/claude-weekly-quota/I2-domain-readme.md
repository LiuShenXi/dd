# I2 synthetic PostgreSQL domain verification

Owner: `sol_test_environment`. The exclusive source file is
`backend/internal/repository/subscription_quota_domain_integration_test.go`.
It uses the unmodified repository `TestMain`, `CI=true`, and fresh disposable
PostgreSQL 18.1 / Redis containers. It never reads a production DSN or calls an
upstream model. The retained environment from I1 remains available for later
iterations; this verification does not rebuild or activate an R1 image.

## Coverage

| Contract | Real test evidence |
| --- | --- |
| T01, T02 identity scope | A=100/B=300, distinct per-user parents/cards, two existing A Keys unchanged and one per-user/group card. This proves shared identity/allowance scope only; actual multi-Key consumption belongs to I3/I4. |
| T03, T05 | Invalid exact money, missing defaults, missing/negative revision and invalid idempotency key; full public-table fingerprints show zero writes |
| T04 | Selected non-sale seven-day plan, precise default copy, explicit override and immutable source snapshot |
| T06, T07 | Concurrent same-key replay, normalized decimal intent, exact original response after plan modification and physical deletion, stale original revision, changed amount/plan/source conflict, user/group/action/actor scope isolation |
| T08, T09 | Eight duplicate concurrent opens create one term/operation; two same-revision renewals yield one success/one conflict; retry creates an adjacent 168-hour card; real SQL range overlap count is zero; stale independent intent makes zero writes |
| T10 | Read a service-issued card from PG; exercise the actual production projection and authorization predicate at start-1us/start/expiry-1us/expiry/expiry+1us; 168 hours exactly |
| T11 | Cancel middle/current/tail/all; later times never shift; gap denies current identity despite distant parent expiry; tail cancellation shrinks parent; all-cancel preserves history/status; reopening starts at fresh DB time |
| T12 | Synthetic previously-used current card is exhausted; renewal stays at queue end, has zero used/available before its start; identity visibility remains separate from generation admission |
| T24 | Foreign/nonexistent term usage has identical not-found response; forged valid cursors cannot bypass user/group filtering; separate groups and identities never share mutation results; invalid target/non-admin actor rejects |
| Read contract | Stable descending term cursors during concurrent insert, non-null empty arrays, current/next projection, no lost schedule revision |
| Rollout guard | Synthetic config only; issue disabled, group not allowlisted and simple mode reject without writes; cancellation remains available after issuance closes |
| Legacy group deletion | Real renew/cancel waits on an independently held parent row lock. Cascade, if-empty and direct deletion reject before touching that parent, for populated and empty term groups. The actual old GPT prepare completes with a 500ms lock timeout in the same window and rolls back. Group/parent deleted_at and all rows remain unchanged until the parent lock releases and the quota mutation succeeds. |

Every successful scenario fingerprints all unrelated public tables, including
ordinary balances, API Keys, accounts, payment/redeem/audit tables, usage and
every GPT carpool term/cycle/ledger/reset/boost/receipt table. Allowed rights
terms/operations, their parent summary, invalidation outbox and explicit plan-fixture
edits are excluded. New quota requests, the new quota ledger and protected requests
are included and must remain unchanged. The native windows parent's full row is separately
fingerprinted. Direct `used_usd` setup in T12 is a synthetic fixture, not a
claim that I3 billing or settlement has been implemented or accepted.

## Reproduction

From the worktree root in a dedicated PowerShell process:

```powershell
& ./validation/claude-weekly-quota/I2-domain-run.ps1 -Freeze debug
```

The script records the exact command, HEAD, complete before/after source
manifests, per-test JSONL events, skip/failure counts and a JSON receipt. It
uses a local named-pipe Docker endpoint, Go 1.27.0 and process-scoped flags.
`debug` always means `acceptance=false`, even on functional success. Final
acceptance requires an explicitly named frozen source, all eleven top-level
domain tests passing with zero skips, and identical before/after source hashes.

The first debug run (`20261019T135904Z`) passed all ten tests. Other owners
were still editing source during that run, so `source_stable=false` and it
is deliberately not final acceptance evidence. Preserve that receipt and
JSONL instead of rewriting it with a later result.

F1 is explicitly superseded in `I2-domain-F1-superseded.json`, even though
the PG receipt originally recorded a stable passing result. Review then
corrected last-card selection and closed the legacy group-delete path.
F2 includes both repairs and the stronger ledger/request zero-write guard.
Its PG scope includes the eleven domain tests, two I1 coexistence tests and
three original GPT regression tests (sixteen total). Linux race includes
the full quota prefix, visibility and existing API Key Create/Update/admin
group-binding cases (sixty-one total across six packages).

## Final F2 result

Accepted test evidence is consolidated in `I2-domain-F2-final-receipt.json`:

- PG16 = domain11 + I1 coexistence2 + original GPT3: **16 passed, 0 skipped, 0 failed**.
- Linux race across domain/service/handler/handler-dto/repository/routes:
  **61 passed, 0 skipped, 0 failed**.
- Both before/after hashes and a final fresh source capture match:
  `4dd98708a8cab51b34a46f00fec7263a969025b60aa3ac7198a0256c49aeaa97`.
- Source HEAD is I1 commit `92bd96e6feaa4426bacb2312eed8e2d162ba1bc7`;
  the tested I2 changes are identified by the frozen tree hash, not that
  parent commit alone.
- Exact PG commands/events are in
  `I2-domain-F2-20261019T140738Z-receipt.json` and matching JSONL.
  Exact Linux race commands/events are in
  `I2-domain-race-F2-20261019T140748Z-receipt.json` and matching JSONL.
  PG used Windows Go 1.27.0; race reused the task-only Linux Go 1.27.2 runner.
- The eleventh PG case passed all twelve deletion subcases while real
  renew/cancel waited on the parent lock. Both old GPT preparation probes
  completed before parent release. No group or parent was soft deleted.

No I2 image was built, no deployment/activation occurred, and no upstream
model was called. I1's preliminary local R1 and the isolated environment
remain retained for subsequent iterations. Multi-Key consumption,
settlement/recovery and the final recoverable R1 still require I3/I4 and
their later acceptance evidence.
