# Rolling reset local validation

This directory contains macOS/Linux validation tooling for the rolling seven-day
carpool refill. It uses only synthetic data and task-owned Docker resources.
It does not connect to production, publish PostgreSQL or Redis ports, or reuse
the existing `carpool-real-*` runtime.

## Real PostgreSQL repository tests

The host Go version is intentionally irrelevant. The script compiles the current
repository with the local `golang:1.27.0-alpine` image, replaces only the
testcontainers `TestMain` through the existing AST overlay generator, and runs
the selected tests against a new PostgreSQL 18 database and Redis 8 instance.

```sh
validation/rolling-reset/run-pg-tests.sh \
  '^(TestSourceRuntimeHarnessIsolation|TestExactRollingCase)$'
```

The regular expression must be anchored. Each invocation creates a private
network with no published ports and removes only resources carrying both its
random run label and captured Docker IDs. Private logs and credentials remain
under the ignored `.runtime/` directory.

## Local application

After backend and frontend checks pass, build and start the current checkout:

```sh
validation/rolling-reset/start-app.sh
```

The application is exposed only at `http://127.0.0.1:38100`. PostgreSQL and
Redis stay on the task network. Synthetic credentials are generated into
`.runtime/app-credentials.private.json` with mode `0600` and must never be
printed, committed, or included in screenshots.

Stop only this validation runtime with:

```sh
validation/rolling-reset/stop-app.sh
```

The default stop preserves the synthetic database for a later source rebuild.
After a reviewed stop, use `ROLLING_RESET_REUSE=1
validation/rolling-reset/start-app.sh` to rebuild the current embedded source and
reuse only those labeled demo volumes. Set `ROLLING_RESET_PURGE=1` when stopping
only when a fresh database is required. Purge still checks task labels and
targets only the two named rolling-reset volumes.

Create the realistic active, near-boundary, future and expired browser fixtures
through the live admin API, then add the explicitly labeled zero-increment display
fixture:

```sh
node validation/rolling-reset/seed-fixtures.mjs
validation/rolling-reset/inject-zero-reset-fixture.sh
```

The SQL fixture proves only UI/API projection of a successful zero increment and
the shifted natural deadline. It does not prove qualification, scheduling,
cooldown, execution, transactionality or replay; those require the focused real
PostgreSQL tests.
