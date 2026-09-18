# Publication CI corrections — 2026-09-19

The first publication of `b5ca578ab` exposed repository checks that were not part
of the earlier selected compatibility test run. Production was still on the old
green image while these checks were corrected.

## Corrections

- Make deliberately ignored cleanup errors explicit in the custom release CLI,
  repositories and tests; assert usage-recording results in tests. Remove unused
  reset helpers and unused assignment values. Preserve the gateway wrapper's
  request-context fallback and nil Gin-context behavior.
- Document the operator-controlled CLI HTTP endpoint at the two exact gosec
  findings; redirects remain disabled. No runtime endpoint validation is removed.
- Reconstruct historical schemas for migration 236 in isolated fixture schemas,
  instead of breaking the compatibility trigger on the fully migrated public
  table. Existing data-preservation, repair and replay assertions remain.
- Establish a checked, committed identity cleanup boundary for four repository
  suites whose count assertions require fresh fixtures. Handle dependent carpool
  foreign keys, retain sequences/global reset scope, and explicitly recreate the
  default group inside GroupRepoSuite's transaction. No assertion is relaxed.

No production migration SQL or database business rule was changed by these CI
corrections. The production 291-to-297 checksum contract remains unchanged.

## Validation

- Go 1.27.0, golangci-lint 2.13.0, full repository without issue-count limits:
  **0 issues** for both Windows and `GOOS=linux GOARCH=amd64`.
- Targeted unit tests in the release CLI, repository, server, handler and service
  packages passed, including carpool, release gates, CyberPolicy and WS turn
  pricing; the nil-context and final repository cleanup changes were rechecked.
- Full repository integration tests on a fresh local PostgreSQL 18 / Redis 8:
  **1501 run events, 1500 pass, 0 fail, 1 existing skip**, exit 0. The skipped
  test is `TestConcurrencyCacheSuite/TestGetAccountsLoadBatch`.
- The isolated migration-236/compatibility subset also passed independently.
- Linux release-guard tests: **13/13 passed**, including business readiness,
  held-container rejection and failure propagation before router changes.
- `git diff --check` passed. Private logs remain outside the repository at
  `%LOCALAPPDATA%/Codex/PrivateTests/sub2api-upgrade-026-20260918/`.

The standalone test runner's full-suite source-file reads were satisfied with a
read-only backend source mount and the correct package working directory. Its
first broad run's missing-file errors were not application failures. Test-owned
containers and volumes were removed after each integration run; neither local
application acceptance stack nor production was used as the test database.
