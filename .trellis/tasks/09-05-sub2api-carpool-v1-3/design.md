# Continuation Design (v1.4, 2026-09-06)

## Current Revision and Ownership

Latest direct follow-up: the director owns the two-boost default adjustment, migration240, editable existing admin count field and focused tests. No new workers. This supersedes the prior fixed-three rule below: count is a2-or3 plan parameter, default2 for new versions introduced by migration240; existing three-boost snapshots remain immutable. No other UI or business behavior is reopened.

Latest UI-only follow-up: `research/timeline-ui-redesign.md` overrides the old below-rail/numbered presentation. Only the time-rail widget is reopened, with timestamps and target copy together above it. timeline_ui owns that widget and focused tests; the director owns docs/runtime/screenshots. All prior backend/store and unrelated UI work stays frozen.

The v1.4 PRD supersedes v1.3 rule/display requirements below. Keep the existing branch/task and all prior evidence. The current director is the source-coordination thread; the earlier director task and workers are completed and must not be restarted for this revision.

| Owner | Exclusive responsibility |
| --- | --- |
| rules28_backend | Backend carpool domain/repository/service/handler/DTO, appended plan-rule migration and backend tests |
| rules28_frontend | Frontend types/API/store/dashboard/header/details/admin defaults/locales/tests |
| rules28_validation | validation tooling, isolated acceptance preparation and new evidence; runtime writes only after director gate |
| Main director | PRD/OpenSpec/original architecture, API agreement, integration review, source/runtime gate and browser acceptance |

All workers are native Trellis implement agents configured gpt-5.6-sol/high. Native coordination retains bounded context and explicit file ownership; no redundant channel workers or user-owned tasks are created.

## Rule Upgrade and User Projection

- Append a new migration; do not edit migration235 or old snapshots. Preserve latest tier code/name/price/weekly quota/enabled state while versioning duration=28, cycle_days=7, boost_count=3 and boost_ratio=0.10. New plan creation/versioning uses these fixed rules. Superseded versions cannot open new terms. Old 30-day snapshots retain their fifth-cycle calculation; the new cycle builder follows snapshot duration and never creates an empty fifth cycle.
- `GET /user/carpool/details` adds top-level `quota: null | {available_usd: DecimalString}`. It is non-null only for an active covered term/cycle, including zero availability. The server computes max(0,base+boost+manual), not the frontend or users.balance. Pending/expired/missing terms expose null.
- `term.reset_count_basis` becomes duration-independent `current_term`. `term.reset_events` is an ascending array of `{cycle_no, occurred_at, target_quota_usd}`. Read successful targets joined to completed batches and the selected user's term/cycle; use actual successful execution time, not scheduled_at/detected_at. The immutable cycle base quota supplies target. Zero grants are included. Do not expose batch/term/cycle IDs or internal evidence.
- Frontend consumes this single store projection in dashboard/header, with explicit carpool labeling and unchanged ordinary-balance semantics. Claims refresh details after POST; identity/request generations prevent stale values. Details and balance consumers refresh on relevant boundaries/visibility without an optimistic financial credit.
- Time segments follow actual snapshot cycle times: four equal weeks for new terms, legacy shape when reading old contracts. Reset markers project their absolute execution time into the term interval, but label and timestamp layout may wrap into collision-free rows on narrow screens. The amount is a reset annotation exception to the otherwise time-only segments, not a quota-consumption bar.
- Exact48h/22:00 reset execution, admission/settlement, ledger and payment boundaries remain unchanged. Browser reset examples may use a documented create-only historical synthetic fixture in the sanitized local test DB, entirely within a new test-only scope/user/group/term/cycles/batch/targets/ledger. No existing row, scope1 state or clock is changed, and product APIs gain no alternate-scope override. Fixture credits/debits/targets and actual historical timestamps must be internally consistent and explicitly labeled fixture evidence, never claimed as live due execution; actual execution semantics are proven separately by fresh-PG tests.

## Current Validation Boundary

Prior accepted V5 serves http://127.0.0.1:38088. Preserve its reporter term78/cycle386 and prior synthetic term94, shared reset batch1/announcement10, original sanitized rows and private evidence. Use fresh new-rule fixtures; do not shorten or top up existing test contracts. No shared runtime/system clock changes, production actions, or unrelated port18080 operations. Runtime changes require preflight conservation, reviewed source/build identities and a recoverable existing-image reference.

Read `openspec/changes/add-carpool-v1-3/design.md` and `api-contract.md` in full. They remain the technical contract authority. Full business authority is architecture v1.4 at `C:/WORK-SPACE/monasapi/sub2api-carpool-architecture.md`. The sections below record preserved v1.3 implementation history and safety context, not current worker assignments or new-term rules.

Worktree: `C:/WORK-SPACE/sub2api-carpool-v1.3`; branch: `codex/sub2api-carpool-v1.3`; base: `36266f512776d78d4f1645a75ae0e84816f8a0a7`. Director task: `01a0722c-e17a-7092-8abd-d7776b150f1b`.

## Preserved Ownership

| Existing worker in director task | Ownership |
| --- | --- |
| `/root/core` | New carpool core domain/service/repository/handlers, migration `235_carpool_core.sql`, carpool Ent schemas, terms/cycles/boosts/queries/payments/takeover; shared routes/handler aggregates/providers/Wire/lifecycle and coordinated Ent generation |
| `/root/gateway` | Existing group enum/validation/key authorization, middleware/BillingCache, HTTP/WS admission/usage, billing command/repository, intents/receipts/recovery/tests, group/usage-log schemas; no public wiring or concurrent Ent generation |

Both were already active and explicitly configured `gpt-5.6-sol`, `high`, `fork_turns=none` when Trellis was requested. Retain their sessions and changes; do not restart the implementation. The director assigns frontend, reset, and independent verification packages when dependencies permit, with explicit file ownership and no concurrent shared-file writers.

## Context and Integration

Trellis owns workflow state; existing OpenSpec PRD/design/API/checklist remain in place. Persist further contract agreements under research or the existing API contract. No duplicate acceptance ledger is necessary.

Every dispatch and adoption message starts with `Active task: <absolute task directory>`. Existing implementation workers read PRD, design, implement plan, implement.jsonl and its references at a safe boundary; reviewers use check.jsonl. Use explicit file loading when hooks are unavailable. The director runs task.py start in its own session; there is no global shared current-task pointer.

The core and gateway owners agree final admission identity, persistence and settlement contracts before integration. UI consumes backend-owned DTOs. Preserve exact 48h timing and the source slot_at/due_at algorithm, independent eight-decimal accounting, immutable snapshots, and atomic request/ledger settlement.

## Authorized Local Production-Data Copy

Latest user instruction authorizes a read-only remote database export and an isolated local test copy, located through the knowledge base. The source coordination task owns this operation. This supersedes earlier blanket prohibition of remote reads or real-data testing, but does not authorize production writes, real refunds/charges, upstream account use, outbound notifications, push, or deployment.

Local copied data must live outside version control, be access-restricted, and never appear in reports, screenshots, agent prompts, or logs. Before application startup, enforce network isolation and neutralize production authentication/provider credentials and externally active configuration in the local copy. Use synthetic test identities for screenshots and mutations. Never overwrite an existing local database or attach existing runtime volumes. No production-data migration or operation runs remotely.
