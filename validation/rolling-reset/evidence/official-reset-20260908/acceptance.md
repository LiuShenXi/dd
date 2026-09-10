# Official Global Reset Acceptance

Accepted locally on 2026-09-08. Architecture and API contract were updated before
implementation. Sol backend/frontend workers implemented the change; a separate
reviewer and the director inspected it. No Trellis workflow was run.

## Accepted Behavior

- Admin > Carpool > Reset Operations now offers Official Global Reset with an
  explicit confirmation dialog. Cancel performs no mutation.
- Confirmation immediately resets all effective scope-1 memberships, skipping
  the card window and cooldown. Pending card schedules and their cooldown state
  remain unchanged. No official-event identification/deduplication is attempted.
- One request key survives uncertain failure and close/reopen. In-flight submit
  and close are blocked; replay returns the original result. After success, a new
  deliberate confirmation gets a new key and may execute again.
- Each affected rolling member gets a numbered successor, including full-balance
  members. The old period remains history. New deadlines are seven days after
  effective time, capped at fixed membership expiry. Existing frozen legacy
  snapshots retain their compatibility behavior.
- Official/card history displays source and effective time. Completion copy
  explicitly describes membership eligibility at reset time.

## Verification

Real PostgreSQL runs, inspected by the director:

- `20260908T034920Z-5983`: 7 official/shared cases passed, including concurrent
  replay, two-member partial rollback, scope/card preservation, zero targets,
  effective/future/expired/terminated selection, expiry cap, legacy behavior,
  official announcement publication and negative-base grant accounting.
- `20260908T035010Z-6146`: 12 existing card/carry regression cases passed.
- Backend worker verified service confirmation/operation identity and package
  compilation for repository, service, admin handler and server routes.
- Frontend final focused tests: 20/20 (`a109cc`); typecheck (`75b3ff`), ESLint
  (`d27541`) and final build (`3fb2dc`) passed. Earlier full suite passed 1954/1954
  (`87930d`), before the final focused retry/time-label corrections. These are
  worker transcript identifiers; separate frontend log files were not saved.

Director ran `verify-official-reset.mjs` against the actual embedded application:

- Batch 2 executed at `2026-09-08T11:51:43.616811+08:00`, targeting 5 memberships.
- Terms 1/2 advanced 2 -> 3, terms 6/8 advanced 1 -> 2, term 4 advanced 4 -> 5.
- Future terms 3/7 and expired term 5 did not change. All expiries and ordinary
  user balances remained unchanged. Current base quotas were full.
- Four HTTP rejection checks passed: unauthenticated, non-admin, unconfirmed and
  missing idempotency key. Cancel left batch history unchanged. In-flight submit
  was disabled. HTTP replay created neither a new batch nor another successor.
- Scope cooldown remained `2026-09-07T09:49:52.254092Z`; no pending card appeared.
- Official completion announcement published successfully. Health was `ok`,
  restart count zero, and no panic/fatal-error log markers were found.
- Inspected 1365px and 390px dialogs, completed admin list and mobile user history.
  A screenshot-only rerun waited for modal transitions and cancelled without
  submitting another reset. `http-browser-results.json` retains the mutation run.

The first browser attempt stopped at the existing first-use guide before any
reset was submitted. The acceptance browser now marks that synthetic account's
guide as seen. The initial PG rollback assertion compared with global zero rather
than its pre-test baseline; the corrected clean runs above passed.

## Runtime Identity

- URL: `http://127.0.0.1:38100/admin/carpool`
- Image: `sha256:d4af37070610d6f3ef5d302c18cbaa70da109ebe3ae480019a30cc878165e8f2`
- Binary SHA256: `f83ed53cbfd0a6974d6287b416c45792099f4a409581a4987f58d73a7b8c4498`
- Embedded index SHA256: `654665477df27022da53b7f4c824b8d50dbcb171b5bda54797132d7f6316ceba`
- No changed backend production Go source was newer than the built binary.
- Only task-labeled local containers and synthetic data were used. Pre-action
  database backup remains private under ignored `.runtime/` with mode 0600.

No commit, push or remote deployment was performed for this follow-up. Outbound
provider access remains blocked: this proves local administration and accounting,
not detection of a real upstream reset event or live model API usage.
