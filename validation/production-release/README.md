# Isolated Release Database Tests

`run-remote-pg-tests.sh` cross-compiles the repository integration package with the
existing AST TestMain overlay, then executes it on the configured Sub2API host
`newapi-vultr`. The user explicitly authorized uploading the test binary and
running isolated acceptance on this production host on 2026-09-08.

The runner creates only new, uniquely named resources labeled with the task and
run ID. PostgreSQL 18 and Redis 8 use cached image IDs, empty temporary storage,
and a new internal Docker network. No host ports, production networks, existing
volumes, application containers, or Docker socket mounts are used. Limits are
384 MiB for PostgreSQL, 64 MiB for Redis, and 384 MiB for the test runner. CPU
limits are 0.5, 0.25, and 0.5 respectively.

The static test binary is streamed into an executable runner tmpfs. Tests run as
UID 65534 with all capabilities dropped and a read-only root filesystem. The
replacement TestMain rejects unexpected database/DNS identities and nonempty
PostgreSQL or Redis state before applying the real migrations.

```sh
sh validation/production-release/run-remote-pg-tests.sh \
  '^(TestSourceRuntimeHarnessIsolation|TestCarpoolReleaseImportRestrictedRoleAndReplay|TestCarpoolReleaseImportRollbackAndRosterGuards)$'
```

Append `--compile-only` to build locally without SSH or upload. The script uses
the existing Go cache; override `CARPOOL_RELEASE_GO_BIN` when the pinned local
toolchain is installed elsewhere. Private run paths are printed at startup.

To reuse a previously compiled artifact, set `CARPOOL_RELEASE_REUSE_DIR` to its
private local run directory. Reuse is rejected unless the Go/SQL/module source
fingerprint and the stored binary checksum still match. Source changes during
compilation also reject the artifact before upload.

The remote execution has a 25-minute outer deadline and a 20-minute test timeout.
Cleanup verifies task/run labels before removing only the captured resource IDs
(or their exact unique names if creation was interrupted). Logs and fingerprints
remain in mode-700 local and remote temporary directories. Secret env files are
deleted during cleanup. A failed cleanup, failed test, empty test selection, or
skipped test returns nonzero; compile-only success is not database acceptance.

## Backup Restore Proof

`verify-remote-backup.sh /remote/production-before.dump expected-sha256` reads the
specified release backup on the same remote host and restores it into a new empty
PostgreSQL 18 container and newly created labeled volume. The database has no
published ports and belongs to a separate internal network. This verifier expects
the recorded baseline of 24 undeleted users, 29 undeleted API keys, and maximum
migration prefix 234. It checks the dump hash before and after restoration, removes
only its own container/network/volume, and retains private restore logs. It never
downloads the production dump or restores over any existing database.
