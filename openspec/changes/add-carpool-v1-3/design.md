# Implementation Design and Ownership (v1.4)

## Current Revision

The2026-09-06 user-approved revision changes new openings/renewals to28-day/four-week/two-boost default contracts. Active Trellis design.md owns rules28_backend/frontend/validation dispatch; the work-package table below is historical. The main director owns docs and independent integrated-image/browser acceptance.

Append a migration after238 admitting legacy/new constraints and appending latest per-code versions. Preserve custom name/price/weekly/enabled and all terms/snapshots/ledger. New writes use28/7/3/0.10; cycles/preview derive from snapshot duration. Old30-day5-cycle contracts still execute. Renewal resolves latest same-code plan including omitted plan_id, without reviving disabled tiers.

User API adds only details.quota and term.reset_events per api-contract.md. Available quota is an active-cycle server net projection, not users.balance. Succeeded targets provide actual executed_at and immutable base target, including zero grants, joined to completed batches and selected user's term/cycle. Frontend shares the Pinia projection across header/dashboard and guards identity/reordering/failures. No optimistic credits.

New time bars have four equal weeks; legacy bars follow actual timestamps. The latest timeline-only visual follow-up locates successful reset instants with subtle nodes, pairs localized base target and time ABOVE the rail, and uses fine leaders/collision-safe grouping rather than red numbered badges or a detached timestamp list. Surrounding page sections remain unchanged. Pending/failed events never display completed. Exact48h/22:00 scheduling and accounting transactions remain unchanged.

Runtime updates require source/embedded hashes and pre/post conservation. Preserve V5 reporter, prior acceptance records and reset batch1/announcement10. Fresh synthetic users cover new rules. For marker rendering, a documented create-only importer may add an internally consistent historical fixture in a wholly new synthetic scope/user/group/term/reset/ledger set within the sanitized test DB. It cannot UPDATE/DELETE old rows, alter scope1, add product scope overrides or modify clocks. Imported success is labeled fixture evidence; actual due execution is independently proven by fresh-PG tests. No remote state or unrelated services change.

## Baseline

Base `36266f512776d78d4f1645a75ae0e84816f8a0a7`; architecture v1.4 is business authority. Existing OpenSpec config is spec-driven; DEV_GUIDE requires pnpm, appended SQL migrations and regenerated Ent. AGENTS.md now records Trellis/safety continuity; it was absent at original baseline.

## Boundaries

PostgreSQL is quota and idempotency authority. All balances and ledger changes share SQL transactions. Carpool groups use the existing subscription_type field. The existing billing repository owns atomic carpool debit integration; ordinary/subscription fingerprint semantics remain unchanged. User projection types are separate from administrator records. Runtime wiring has one owner at a time.

## Work Packages

| Package | Owner | Exclusive files and responsibilities |
| --- | --- | --- |
| A Core | core agent | New carpool domain/service/repository/handler files, appended core SQL migration, Ent carpool schemas; openings/previews/renewals/termination/takeover/payments/adjustments/boost/details/cycles. Owns server routes, handler/provider aggregates, Wire and lifecycle integration initially. |
| B Gateway | gateway agent | Existing group enum/validation/key authorization and billing-cache/middleware, HTTP/WS gateway admission, usage billing command/repository, usage receipt recovery, relevant tests; group and usage-log Ent schemas. Must coordinate generated Ent and Wire with A. |
| C Frontend | frontend agent | Entire frontend carpool-related implementation, routes/sidebar, user dashboard boost, admin Users entry, views/API/types/group form/i18n/announcement polling and tests. No backend writes. |
| D Resets | next available agent | New carpool reset files/migration/schemas, upstream read-only query observation hook, existing announcement domain/service/repository audience and dedup; obtain public wiring ownership from A before edits. |
| E Verification | reused agents | Isolated fixtures/DB/runtime tests and independent cross-owner review. No author self-report counts as final acceptance. |

All subagents use `gpt-5.6-sol`, `high`, and `fork_turns=none` or short context. No further user-owned tasks. Shared working directory means edits are immediately visible: preserve other owners' work, do not regenerate shared code concurrently. Review feedback goes back to the same owner where possible.

## Integration Contracts

A and B agree a typed CarpoolBillingSnapshot (term/cycle/admitted_at) and eligibility/admit/persist/recovery methods before implementation. A publishes administrator/user DTO contracts for C early. All API paths remain under `/api/v1`; architecture section 9.4 defines required endpoints. Every administrator write requires stable idempotency key plus payload consistency. User identity only comes from auth, not user/term/cycle parameters. C never invents endpoint response shapes without confirming A's contract.

## Precise Reset Scheduling

For a NEW qualification, start at today's Shanghai 22:00 and advance while slot_at <= database_now. For each slot compute due_at=max(slot_at,last_successful_reset_at+48h); use that day only if due_at < slot_at+1 minute. Store slot_at and scheduled_at=due_at. Already scheduled execution never applies the new-qualification comparison; it requires now>=scheduled_at, now>=last_success+48h, and now in [slot,slot+1 minute). Preserve seconds, e.g. prior 22:00:20 success allows two days later 22:00:20. Missed minutes reschedule and correct announcements; only successful committed execution advances cooldown.

Lock ordering: scope -> batch -> ascending terms -> cycles for reset; term -> cycle for grants; billing locks original cycle and settles its persisted request within existing usage dedup transaction. No receipt row lock may surround nested Apply calls. Scope has at most one pending/running batch. Observation stores hashed identity/card fingerprints only, complete baselines including zero, no raw credentials/card IDs in DTOs/logs.

## Validation Environment

Use new, uniquely named disposable containers or fresh local temporary clusters with loopback-only ports. Never attach existing volumes or local live service data. Synthetic users, groups and upstream responses only. Tests own their fixtures and produce evidence under this task directory or ignored local runtime directory. No broad cleanup commands. A local runnable frontend/backend URL is required for final completion.

The source coordinator's authorized read-only export and neutralized copy already exist. New copied-user test terms, only with a verified tier mapping, start at users.created_at with exact28-day expiry and7/7/7/7 cycles. Preserve older expired/future states, missed cycles and already-created contracts; never auto-renew or infer tiers from balance. Unmapped users remain pending mapping. Fresh synthetic users cover new rules/screenshots. These fixture rules do not change production or the administrator opening API; source coordinator alone owns copy/runtime safety.
