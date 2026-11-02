# I1 — Additive schema and compatibility refusal candidate

Date: 2026-10-19 (Asia/Shanghai). Workspace/branch: the isolated `claude-weekly-quota/dd` checkout on `codex/claude-weekly-quota`. Baseline/I0 commit: `311d11718` (production source starts at `8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`). No production migration, card issue, model request, user modification or deployment was performed.

## Implemented boundary

- Added migrations 245–248 for group mode, parent schedule revision, exact plan allowance text, order snapshot, four quota-domain tables and nullable usage links. The usage index has its own online migration and later FK validation. Existing 297 migration files remain intact; the true PostgreSQL tests bind their original aggregate checksum.
- Added explicit numeric NaN rejection, positive quotas, fixed 168-hour term duration, composite identity FKs, append-only ledger enforcement and immutable term identity/allowance fields. The exact existing monetary scale and old receipts are retained.
- Added mode to Ent/service/admin/output DTOs and auth snapshots. Legacy absent mode means windows; unknown modes fail closed. Snapshot version is 26, rejecting prior L1/L2 entries. No group or user is automatically converted.
- The GPT binding projection and open/renew Key check permit only an identity-consistent anthropic/subscription/term Key as an additional group. A missing/expired Claude card does not change mode recognition. Ordinary/frozen opening balance requirements, binding persistence and other-group refusal remain intact. The GPT transaction does not acquire Claude scope/parent/term/request locks.
- Normal and Google auth, simple mode and billing eligibility explicitly reject unsupported term generation before native-window or balance paths. Real router aliases are covered. R1 `/v1/usage` refuses until a proper term projection is implemented; it never labels ordinary balance as a card allowance. Discovery may retain metadata access.
- Old assign/bulk, extend, reset, revoke, restore, redeem, automatic full/partial refund and payment fulfillment paths reject term mode. Refund checks include `force`, `deduct=false`, independent query/finalization and rollback. Native-window activation/reset/increment and unified old subscription debit have database mode predicates; a blocked term reset is not reported as successful maintenance. Old expiry maintenance/reminders exclude term parents.
- General admin update cannot silently convert an existing group to term; it reports `TERM_TRANSITION_REQUIRED`. New term groups are validated against platform/type/no-fallback constraints. Simple-mode request sanitizers reject an explicit term request before removing advanced fields; duplication retains the source mode.
- Added default-disabled issue/admission config, separate recovery and protected-group settings, actual Viper defaults/env mapping, and startup refusal if R1 is configured to enable issuance/admission. Wire injects the protected-group repository/service. Candidate Compose/env files explicitly keep new issuance and generation off.
- Migration 249 and the protected-group submodule add a final database check plus durable per-request in-flight records while the group is still standard/windows. Only adapted endpoints are allowed for configured groups; stale platform/type, forced vendor routes, simple mode and unsupported paths reject. Async billing completion, missing/ambiguous usage, failures and crashed requests cannot falsely drain the count. See `I1-protected-groups-receipt.md` for the independent module evidence.

No I2 card-opening domain/API implementation or new quota consumption is included. The shared I2 port documents are design coordination only.

## Generation and migration engineering

Direct Windows Ent generation encountered a mapped-file write failure and left incomplete generated output. Only that attempt's generated changes were restored. `ent-codegen/main.go` now generates the full result into a fresh temporary directory with the original import package, then copies only changed successful artifacts. It produced 28 necessary generated artifacts without unrelated entity churn. Five edited existing Ent schemas and SQL remain aligned. Wire generation succeeded.

The existing nontransactional parser accepts only concurrent index statements. The new online migration uses a narrowly scoped exact whitelist for its two bounded timeout SET statements and RESETs. It runs on the same migration connection, restores prior session values through an independent bounded context on success/failure, and discards an actual SQL connection if restoration fails. Failed concurrent indexes are detected and rebuilt before retry. No arbitrary session setting or general parser relaxation was introduced.

## Developer-executed validation

Pinned executable: `C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe`. Commands below ran from `backend/`; all named final commands exited 0.

| Command | Result/evidence |
| --- | --- |
| `go generate ./cmd/server` | PASS; actual generated constructor includes the protected repository/service (`I1/wire-generate.log`). |
| `go test -tags=unit -run '^$' ./...` | Final all-package compile PASS (`I1/go-unit-compile-final.log`); compile is not business execution. |
| `go test -tags=unit ./internal/service ./internal/handler/... ./internal/server/... ./internal/repository` | Service 189.077s, handler/admin/dto/quotaview and middleware/routes passed in the first full run. Two changed-schema/JSON test fixtures required updates; repository also required the existing Git `sh` on process PATH (`I1/go-unit-related.log`). |
| `go test -tags=unit ./internal/repository ./internal/server/...` with process PATH including `C:/Program Files/Git/usr/bin` | All affected packages PASS after the fixture fixes (`I1/go-unit-fixture-recheck.log`). Repository 6.760s; server 3.487s; middleware/routes PASS. No test skip or timeout alteration was used. |
| `go test -tags=unit -json -run 'SubscriptionQuota\|Carpool\|UsageBilling\|SubscriptionAppliesRateMultiplier\|APIKeyAuthSnapshotPreservesPerUserBillingIsolation' ./internal/service ./internal/handler/... ./internal/server/... ./internal/repository` (regex alternation pipes are unescaped in the shell command) | 99 top-level tests PASS, zero failed/skipped test events (`I1/gpt-and-quota-unit.jsonl`). Counts: service 61, handler 15, admin 3, DTO 1, repository 18, actual routes 1. This includes all original 84 focused GPT tests and 15 new quota/protection tests; the actual-router case executes 50 term-route/run-mode subcases. |
| `go test ./internal/domain/...` | PASS (`I1/go-domain.log`). |
| `go test -tags=unit -json -run 'DuplicateGroup\|Group.*SimpleMode\|SimpleMode.*Group\|SubscriptionQuota\|Subscription.*(Reset\|Renew\|Window\|Bulk\|Revoke\|Restore)\|Refund\|Redeem' ./internal/service ./internal/handler/admin ./internal/server/middleware` (unescaped shell alternation) | Additional final-state legacy/sanitizer/duplication regression PASS: 117 top-level tests (service 84, admin 33), zero failed/skipped (`I1/mode-and-legacy-guards-final.jsonl`). Middleware had no matching tests in this supplemental command. |
| `go vet ./...` | PASS, no diagnostics (`I1/go-vet.log`). |
| `git diff --check -- backend/internal backend/ent/schema deploy` | PASS for business changes. Original copied architecture/evidence is preserved rather than cosmetically rewritten. |

The changed old lock fixture now supplies the added `quota_revision` column; the exact API-contract fixture expects the additive `subscription_quota_mode: windows` field. The backup tests were unchanged and pass with the existing shell available on process PATH. Initial failures remain recorded.

## Independent final acceptance

The environment owner runs fresh PostgreSQL 18 and Redis through the original repository harness with `CI=true`, preventing Docker absence from counting as a skipped success. Final scope is eight migration tests, five protected-group lock/drain tests, two coexistence/legacy-sink tests and the three original GPT PostgreSQL baseline tests. Linux race and a local R1 candidate image are separate evidence layers.

F2's legacy-sink test initially compared Ent's nanosecond create value to PostgreSQL's microsecond read value after all eight mutation-refusal assertions passed. The fixture now truncates its input to microseconds and retains exact comparison; it does not use a time tolerance. Failed F2 evidence is retained. F3 supersedes F2 and must bind a stable source manifest.

F3 PostgreSQL completed: `environment-I1-F3-20261019T133930Z-pg-receipt.json` records all 18 top-level tests passing, zero skipped/failed, exit 0 and stable source SHA-256 `e2d31f2525ccf72a74eb03c308611785246ce45d0e75198ef4d06fd6d7651693`. It includes the original three GPT tests and their eight opening-conflict subcases. Exact arguments and individual test names are in that receipt and its JSONL.

F3 Linux race completed: `environment-I1-F3-20261019T133930Z-race-receipt.json` records all 15 targeted top-level tests passing, zero skipped/failed, exit 0 and the same stable source SHA-256. The exact command is `go test -p=2 -tags=unit -race -json -count=1 -timeout=10m -run '^TestSubscriptionQuota' ./internal/service ./internal/handler ./internal/repository ./internal/server/routes` in the task's Linux Go/CGO runner. F2 race's initial cold build filled only the runner's temporary filesystem; the environment owner moved temporary build output to its dedicated task volume before F3. No source/test skip or concurrency assertion was removed.

F3 local image completed: `environment-I1-F3-20261019T133930Z-build-receipt.json` records exit 0 for both Linux Go builds, local Docker packaging, no-network/read-only/non-root version smoke, in-image binary hashes and PostgreSQL 18.1 client verification. Image `sub2api:claude-weekly-i1-r1-e2d31f2525cc` has ID `sha256:459dd4757b05983d0fcf79cec26d91fcca45edacb640f32056623f9b242d36ae`. It binds the same stable F3 source and explicitly records a dirty frozen I1 revision. The embedded server hash is `29e0609d4b73bc805bc9f08665794cc5ff836b23d81eca0457d52bb061d7ab00`. It is a local packaging/build proof; full business-server operation and production-equivalent I6 packaging remain unverified.

The coordinator independently inspected and accepted all three layers in `coordinator-I1-review.md`; their combined receipt is `environment-I1-F3-20261019T133930Z-final-receipt.json`. No T01–T62 status is inferred from this document.

## Rollback and limits

This is the preliminary I1 R1 compatibility/refusal candidate. It cannot be the final post-activation rollback image until I3's receipt codec/recovery component is included and its image digest frozen. There is no DOWN migration or old-database restore step; new rows must be retained.

Production activation is forbidden in this task. In particular, the I6 all-instance release barrier, final conversion transaction, maintenance/legacy-write serialization, exact drain procedure and explicit per-user allowances remain unimplemented here. Database mode predicates back stale legacy callers, but they do not replace I6's transition locking. No protected group IDs were applied to production. No performance, final UI, real provider route or billing consumption acceptance is claimed by I1.

The later full implementation continues to use post-charge semantics: already admitted costs may exceed the original card; those costs belong only to that original card. Strict zero overshoot, transfers, rollover and early use of a future card are outside the accepted design.
