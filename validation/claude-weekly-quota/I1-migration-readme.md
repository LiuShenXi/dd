# I1 PostgreSQL 18 migration acceptance

Final frozen-source F3 run `20261019T133930Z-pg`: **PASS**, 18 top-level tests,
zero skips/failures. This includes eight migration tests, all five protected-group
tests, both coexistence tests, and three original GPT regressions. Source SHA-256
`e2d31f2525ccf72a74eb03c308611785246ce45d0e75198ef4d06fd6d7651693`
was unchanged before/after. The original 297 migrations upgraded to 302, every original
column in 115 legacy tables retained its fingerprint, and old/new migration
replay preserved all migration records. The five separate SQL NaN rejection
subtests (`quota`, `used`, `actual`, `ledger_delta`, `plan`) all passed.
Migration files 245-249 stayed byte-identical throughout the run.

Evidence: [`environment-I1-F3-20261019T133930Z-pg-receipt.json`](environment-I1-F3-20261019T133930Z-pg-receipt.json)
and [`environment-I1-F3-20261019T133930Z-pg.jsonl`](environment-I1-F3-20261019T133930Z-pg.jsonl).

The dedicated source file is
`backend/internal/repository/subscription_quota_migration_integration_test.go`.
The original Docker-starting `integration_harness_test.go` remains unchanged.
Each acceptance case uses a fresh database on that harness's disposable
PostgreSQL 18.1 container. No environment-supplied DSN, copied user data, or real
upstream connection is used.

Run from the worktree root:

```powershell
pwsh -NoProfile -File validation/claude-weekly-quota/I1-migration-run.ps1
```

The migration-only runner records JSONL events, exact new migration hashes, the test source hash,
and a dated receipt. Acceptance requires all eight exact top-level tests to pass,
zero skips, zero failures, and stable 245-249 migration bytes during execution.
`CI=true` turns a missing Docker engine into failure instead of the harness's
local-development skip. The existing Windows Go 1.27.0 toolchain is selected
directly; the Linux race environment remains separate.

| Test suffix | Evidence provided |
| --- | --- |
| `EmptyDatabaseAndReplay` | Empty public schema, actual full upgrade, both usage FKs/checks validated, concurrent index valid/ready, replay keeps every migration checksum and applied timestamp, no quota granted by schema installation |
| `Legacy297UpgradePreservesAllRows` | Exact original 297 filename/checksum aggregate, real old-schema GPT terms/cycles/ledger/receipts/payment and native windows/plan/usage/order fixtures, all old columns in every legacy table fingerprinted before/after, defaults windows/revision zero/plan allowance NULL, old/new migration replay |
| `GroupModeAndCompositeOwnership` | Term limited to anthropic subscription groups without fallback, foreign user/group/parent/term/request and cross-term reversal rejected by actual FK constraints, issued group identity protected |
| `Exact168HoursAndNumericBoundaries` | 167h/169h/one-microsecond drift rejected, DST distinguishes 168h from seven local calendar days, fixed eight decimal values and tie rounding agree across terms/requests/ledger/plans, positive/min/max/overflow checks, SQL rejects NaN in quota/used/actual/delta/plan, in-flight overshoot storage remains possible |
| `ImmutableTermsAndLedger` | Issued ownership/allowance/times/source snapshot immutable, usage/lifecycle fields writable, ledger UPDATE/DELETE rejected, unique debit/reversal, an audited reversal appends and preserves net reconciliation |
| `UnknownUsageAndUsageLogLinks` | Unknown cost stays SQL NULL, fabricated no-charge without evidence rejected, known/settled receipt requirements and object payloads enforced, valid usage-log request/term pair allowed and mismatched/partial pair rejected |
| `OnlineIndexFailureRestoresTimeouts` | A real conflicting table lock causes PostgreSQL 55P03 after the migration's bounded wait; the exact same backend PID retains the original non-default 321ms/17s settings, and no migration success row is written |
| `OnlineInvalidIndexRecovery` | A real failed UNIQUE INDEX CONCURRENTLY leaves an invalid namesake; running 247 drops and replaces its OID, produces the correct nonunique valid/ready index, and retains both pre-existing usage rows |

Original baseline aggregate:

```text
SHA256(concat(sorted filename + NUL + SHA256(TrimSpace(SQL)) + LF))
35a19130f66cce3abb9134b074a611ce52c29c4ee3f88ebd3a87744656637bb3
```

This constant was computed from the 2026-10-19 architecture evidence's exact 297
production filename/checksum records, then separately checked against every
current original SQL file. Historical `108a`, `120a`, `154a`, `234z`, and similar
filenames are retained intact; only leading digits determine the I1 cutoff.

## Initial execution finding

Run `20261019T132116Z` failed in original TestMain before any case started:

```text
validate migration 247_subscription_quota_usage_index_notx.sql:
*_notx.sql must not mix non-CONCURRENTLY SQL statements
```

The new migration's first comment contained a semicolon. The existing migration
splitter splits on semicolons before stripping line comments, so the comment's
trailing text became a non-SQL prefix. The migration owner was notified to remove
that new comment separator while preserving the existing strict migration parser.
The failed receipt remains available; it must not be represented as business
test execution or a successful migration.

Current pass/fail status is determined by the most recent dated receipt, never
by this coverage plan alone. These database checks do not prove gateway routing,
UI correctness, recovery behavior, performance, or production activation.
