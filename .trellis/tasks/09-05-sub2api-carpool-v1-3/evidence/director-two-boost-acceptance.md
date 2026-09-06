# Two-Boost Default Acceptance, 2026-09-06

Completed directly as requested, without new workers, commit, archive, push or production changes.

## Behavior

Migration240 appends a two-boost version of each latest three-boost plan and changes the column default to2. Names, prices, weekly quota, dates/cycle parameters and enabled states are retained. No existing plan, term snapshot, claim or ledger row is rewritten. The existing administrator count input is editable with integer bounds2..3; the backend enforces exactly2 or3. New openings and renewals use the chosen latest plan snapshot. Existing three-boost terms retain three. Duration28days, four cycles,10% amounts and timeline UI are unchanged.

Management URL: http://127.0.0.1:38088/admin/carpool. Existing synthetic administrator login succeeded; credentials remain only in private fixtures and the user's requested chat handoff, not this report.

Live authenticated GET /api/v1/admin/carpool/plans returned three latest plans with boost_count2, duration28, cycle_days7, ratio0.1 and amounts55/70/110. Administrator route and synthetic login were verified via API without taking over the user's testing browser. The field is under the existing plans tab/new-version dialog.

## Verification

- Admin component tests:10/10; scoped ESLint passed.
- Frontend build including vue-tsc passed; backend `go build ./...` passed. No fresh full frontend/backend-suite claim for this narrow change.
- Fresh isolated PostgreSQL run20260906T135357Z-56aa38eb:5 selected tests passed. Coverage includes actual migration replay/custom-state preservation, four concurrent requests producing exactly two credits, exhaustion and replay, old-three entitlement after plan downgrade, preview and renewal.
- Initial PG run20260906T135142Z-31646a6b remains as failed evidence: one test selected an unrelated30-day fixture as a28-day upgrade; another compared Decimal internal representation instead of serialized snapshot values. Test fixtures/assertions corrected; no production behavior was weakened.
- Acceptance driver updated for new default2 and its unit suite passed. The mutating HTTP/WS acceptance suite was not rerun against shared user-testing data.
- Source SHA-256: `2DCD7150000777A6801DC48F7B8C32B45577C1CC3310E46C178B90EF393D777F`.
- Embedded frontend SHA-256: `F205F8C3D1E53F00CFDBD3EBAC210D199656910AB177B84BF37C3CEEFBD20AEE`.
- Code gate: `director-code-ready-two-boost.json`, SHA-256 `928458AFEC4E155F30A62316FA2569C4E6A5FD5F6D9FAA6E135C4477BC295C25`.
- Runtime image: `sha256:3a662cf5a631128d93c24d2474e2a264109e5e78c9560e93b3ed200fc4cfa86c`; controlled rebuild completed, internal isolation/denied-egress gates passed, host health200 and synthetic administrator login passed.

## Conservation Boundary

Copied original user/key/usage/group and announcement checks passed before/after rebuild. Existing database, Redis, volumes, network, configuration and credentials were retained. No manual claim, reset, term import, copied-user write or clock adjustment was issued.

The old21-envelope synthetic baseline check did NOT pass and was not overwritten. At2026-09-06T14:00:04.707225Z (Shanghai22:00:04), the previously scheduled reset executed through the unchanged runtime, adding a reset ledger row for synthetic reporter cycle386 and bringing base balance698 to700; boost balance remains140, term78 boost_used/count remain2/2. Thus the old baseline's hardcoded698 no longer describes the live test state. Private audit logs record this separately (`two-boost-synthetic-cycle-audit`, `two-boost-existing-synthetic-audit-v2`). Do not mistake normal live scheduling for a complete unchanged-synthetic-baseline claim or roll back the user's test state.

PRD, original architecture, OpenSpec and backend spec reflect default2 with versioned configuration; historical three-boost acceptance remains historical.
