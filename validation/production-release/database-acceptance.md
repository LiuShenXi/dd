# Release Database Acceptance

Date: 2026-09-08. These checks used isolated PostgreSQL 18 resources on the
existing deployment host after the user's explicit isolated-test authorization.
They did not modify the production database, application containers, groups or
API keys.

## Repository Suite

- Final run: `20260908T092833Z-66599-88f791ce`.
- Result: 80 top-level tests passed, 95 executions including subtests, zero skips.
- Selection: the harness isolation test plus every current repository integration
  test whose exact function name contains `Carpool`; the anchored selection is
  recorded in the private run's `test-pattern` file.
- Binary SHA-256: `48eca8bca94c697d805d5c67e39e46d78566f758b4f4f4f5b37994f781f114fa`.
- Go/SQL/module and overlay-helper source fingerprint:
  `9341fb1f36860cbbb95fc17ef9d2be71fc1a8eb9751d774043a898f92d5503f9`.
- Local private artifacts:
  `/var/folders/fb/mvdcfcws0jz4hffbdntzj1fc0000gn/T/carpool-release-pg.a2HOBA`.
- Remote private artifacts:
  `/tmp/carpool-release-pg-20260908T092833Z-66599-88f791ce`.

This includes restricted-role import, live balance capture, replay and rollback,
original-group and administrator-key preservation, expired/missing-term billing
mode, independently anchored takeover, special 450/1000 renewal, and existing
admission/reset/settlement/reconciliation regressions. Earlier runs exposed test
fixtures missing a required key name, carryover deletion before reset batches,
and the required standard/OpenAI source group for personal billing bindings.
The final run includes those fixture corrections.

## Backup Restore

- Run: `20260908T092440Z-66040-b621f8be`.
- Restored the recorded production dump into a new empty PostgreSQL 18 database
  on a new labeled volume/internal network; no existing data volume was mounted.
- Verified 24 undeleted users, 29 undeleted API keys, and maximum migration prefix
  234 after successful `pg_restore --exit-on-error`.
- Dump SHA-256 before and after restore:
  `5dd77cba4044d5b55573386d3737cc93b571d8aebcebdcfcee7b8f086e1540f8`.
- Remote private evidence:
  `/tmp/carpool-backup-restore-20260908T092440Z-66040-b621f8be`.

Both final runs reported `exit=0` and `cleanup_failed=0`; follow-up label queries
found no remaining final-suite containers/network or backup-restore data volume.
Final application-image HTTP/WebSocket/UI behavior and live upstream connectivity
are separate acceptance steps and are not claimed by this database evidence.
