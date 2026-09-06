# Isolated Runtime Acceptance

This package owns only local test tooling. Application changes remain with the director's core/gateway/frontend owners. No script accesses production, attaches old local volumes, or sends real upstream traffic.

## Preconditions

- The source coordinator's sanitization gate must be complete. Its evidence is `research/local-data-copy.md`.
- The director must explicitly provide backend and frontend code-ready evidence before the startup script runs. Preparation alone never starts an application.
- Docker must resolve to the verified local named pipe. PostgreSQL/Redis must have only the internal `carpool-v13-test-net` network and no published ports.
- Web port 38088 must be unused. Port 18080 belongs to another service and is not part of this package.

## Commands

Run from the isolated worktree, with PowerShell 7:

```powershell
pwsh -NoProfile -File validation/source-runtime/prepare-runtime.ps1
pwsh -NoProfile -File validation/source-runtime/check-copied-records.ps1 -CaptureBaseline
pwsh -NoProfile -File validation/source-runtime/check-copied-announcements.ps1 -CaptureBaseline
pwsh -NoProfile -File validation/source-runtime/start-runtime.ps1 -CodeReadyEvidence <director-approved-evidence-file>
pwsh -NoProfile -File validation/source-runtime/run-acceptance.ps1 -CodeReadyEvidence <same-director-approved-evidence-file>
pwsh -NoProfile -File validation/source-runtime/run-acceptance.ps1 -Suite websocket -CodeReadyEvidence <same-director-approved-evidence-file>
pwsh -NoProfile -File validation/source-runtime/run-acceptance.ps1 -Suite reset -CodeReadyEvidence <same-director-approved-evidence-file>
```

Preparation and baseline capture are one-time operations and refuse to replace existing evidence. The current private runtime was prepared before environment hashing was added; its one-time `seal-prepared-runtime.ps1` operation validates the canonical local-only environment and adds only that hash to the existing resource binding. It does not rebind the database, rotate secrets, or start services.

Code-ready JSON belongs in this task's `evidence/` directory. It requires the task ID, current Git HEAD, `backend_build:passed`, `frontend_build:passed`, `approved_for_local_start:true`, `source_sha256` and `embedded_frontend_sha256`. The director obtains both hashes through `Get-LocalRuntimeCodeIdentity` after dot-sourcing `runtime-common.ps1`. Startup verifies these before and after compilation; Git HEAD alone does not identify uncommitted implementation changes.

After that immutable image is captured, implementation may continue while acceptance runs against the captured image and exact original code-ready file. Such reports prove only that image, not later checkout edits. Final delivery still requires the latest checkout to pass the full source-identity check and be rebuilt/retested when it differs; passing an older image never accepts newer untested code.

Preparation creates a fresh private config and compiles the standalone synthetic bootstrap using the existing backend Go dependencies. Startup builds the current embedded Linux application and deterministic mock, packages them using the existing local PostgreSQL Alpine image, creates a fresh app volume, installs only the local config, bootstraps synthetic identities, and starts the app/mock on the one internal network.

The application configuration has empty pricing remote/hash URLs, fresh JWT/TOTP keys, disabled external/background features, empty proxy variables, and a mock-only upstream allowlist. It is written as structured JSON, valid YAML, outside git. Config is not baked into the runtime image. The application is nonroot with a read-only root filesystem and loopback-only web exposure.

## Synthetic Bootstrap

The original database users, including administrators, remain disabled for login. The bootstrap refuses an unsanitized copy, inserts twelve new identities in one transaction, and gives each a newly generated bcrypt password. It never updates an old user, balance, group or timestamp. A session advisory lock and durable pending credential file protect commit/recovery: committed rows must match their saved IDs and password hashes; partial or unverifiable states stop without overwriting anything.

Fixture names are `admin`, `four`, `three`, `two`, `fifth_four`, `fifth_three`, `fifth_two`, `ordinary`, `expired`, `renewal`, `termination`, and `takeover`. Ordinary begins with USD 100 and takeover with USD 25; all other balances are zero. Tier names are intended test scenarios, not guessed mappings of copied users. For each HTTP attempt, the driver authenticates only the trusted synthetic administrator and creates eleven fresh run-specific users through the real admin API. The complete private batch is saved before carpool mutations; failed attempts never reuse previous term holders or reset the database. Plans, terms, groups and keys are exercised through the real application API.

Bootstrap passwords are stored in private `runtime/fixtures.json`. Its envelope has `version:1`, `source:synthetic-local-only`, `base_url`, `mock_url`, and `users` with `name,id,email,password`. Do not print this file or include it in screenshots, prompts or git. Browser automation can consume only the chosen synthetic identity in memory. User-visible artifacts must be aggregate-only.

Per-attempt synthetic credentials are also stored privately in `runtime/acceptance-<id>/results/batch-fixtures.json`; the public report contains only fixed assertion names and outcomes. These are not copied users or production credentials.

## Recovery And Execution

After a failed startup, inspect private diagnostics and use `start-runtime.ps1 -Resume` with the same code-ready evidence. Before build completion, the script requires identical source; after verified immutable image capture, it binds to that image's saved evidence so subsequent checkout changes are not accepted implicitly. Config, environment, tooling, network, per-run labels and concrete container identities must remain identical. It verifies the installed config's hash and file modes every time. Do not delete bootstrap `.pending` files: bootstrap recovery distinguishes an uncommitted attempt from a committed transaction whose credential publication was interrupted.

The startup health gate probes inside the application container. Docker Desktop can suppress host port publication for an internal-only network, so Windows loopback access is a separate check. Never attach the application to an external network to make the browser reachable.

The read-only application cannot receive `docker cp` directly. A stopped, task-owned, network-none helper transfers the exact config into the app volume and is then removed by verified ID; the application remains read-only. Inherited PostgreSQL image volumes are suppressed with bounded tmpfs mounts.

Windows ACL-private bind mounts were found to expose empty directories to the Docker VM. Acceptance binaries and synthetic inputs therefore stream through private process stdin into a read-only runner's bounded tmpfs; no input bytes are logged. The binary and fixture hashes are verified inside the runner. Reports are collected privately before exact-ID runner removal. The runner publishes no ports and has no Docker socket or host mounts.

The separate `repository-tests/` runner uses a new empty PG18 database and Redis for real repository integration tests. Its TestMain-only Go overlay preserves actual migration, Ent, Redis, transaction and test logic. It never connects to the copied database. Only the source coordinator may start or clean up its fixed resource set; implementation workers submit test patterns to that coordinator.

The `websocket` suite uses its own freshly created synthetic users and real WebSocket connections. Its SQL connection is read-only and restricted to the exact copied-test database contract for receipt/ledger verification; setup mutations go through the application API. The local database password is passed only through a private temporary environment file, which is removed after container creation. HTTP, WebSocket and reset suites share a single runner name and must run serially.

The `reset` suite checks qualification, Shanghai scheduling, idempotency, early-execution refusal and announcement audience through the real application API. It receives the same exact local-only database contract under `CARPOOL_RESET_DB_*` for read-only source metadata verification. The wrapper first requires zero copied-user carpool terms; later reset grants can therefore affect synthetic terms only. No copied-user tier is guessed or assigned.

Actual due-time execution is deliberately not an HTTP pass: the public report preserves the fixed `reset.actual_due_execution` exclusion and points to the separate PostgreSQL evidence. First-publication and same-window merge checks are distinct because pending scope qualifications intentionally merge without publishing a second announcement. Test-created batches remain scheduled; there is no cancellation API and the suite never deletes or resets the copied database to obtain a fresh scenario.

Historical reset-marker browser evidence uses the separate create-only importer documented in `reset-marker-fixture/README.md`. It creates only a new synthetic user/group/term in an int32-safe non-1 scope, labels both reset events as imported historical fixtures, and cannot exercise scope-1 boost eligibility. `check-preserved-synthetic.ps1` freezes users 122/127, terms 78/94, key 64, batch 1, announcement 10, and all three existing qualifications before any authorized import. Neither baseline capture nor import is part of ordinary runtime startup or acceptance.

## Evidence Boundaries

Successful health and denied-egress checks prove runtime availability/isolation, not feature correctness. HTTP evidence includes original-row and original-announcement conservation plus post-run isolation outcomes; a failed postcondition cannot yield an overall pass. WebSocket, reset/observer/recovery and browser evidence remain separate required gates. Scripts refuse silent reuse/replacement of existing containers, app volumes or secrets. A failed startup requires a bounded diagnosis and explicit continuation against these exact task resources, never broad cleanup.
