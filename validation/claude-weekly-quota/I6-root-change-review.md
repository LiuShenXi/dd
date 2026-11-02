# I6 root-owned change review — 2026-10-20

> Historical source-review checkpoint. The nonblocking observer fix and final metric/lease/runtime acceptance are complete; see I6-final-metrics-unit-receipt.json, office-I6-metrics-receipt.json and coordinator-I5-I6-review.md.

Reviewer: integration worker. This is a source review, not a runtime acceptance
receipt. No Go command, Docker command, production operation or business-source
edit was performed by this reviewer. The coordinator owns the fixes and final
validation. This review document is the only file written in this review turn.

## Finding and correction

**P1 — synchronous metric logging could delay lease safety cancellation.**
The initial `observeClaudeScopeOperation` implementation called `slog.Warn` for
renewal errors, lost ownership and safety deadlines. The keeper invokes this
observer on its own goroutine, including before `k.cancel` in terminal branches.
The configured slog adapter synchronously calls zap, whose stdout/file sinks
can block. A blocked log write after a failed renewal could prevent that same
goroutine from handling its safety timer. Merely moving cancellation ahead of
the deadline log would leave the renewal-error path vulnerable.

The coordinator removed all log I/O from the observer. It now updates only the
bounded, process-local operation counters under a mutex. Periodic copied
snapshots are still logged by the recovery observer on its separate worker.
The reviewer reread the resulting implementation and confirmed that this
specific blocking path is removed.

`TestClaudeConcurrencyMetricsCannotBlockOnLogSink` was added by the coordinator.
It installs a logger with a deliberately blocked writer and checks that renewal
error, lost-lease and safety-deadline observation return without waiting for
the writer. The test source was reviewed; this reviewer did not execute it and
does not record it as passed.

## Remaining reviewed scope

No additional actionable regression was found in the following changed paths:

- `service/user_concurrency_scope.go`, `user_concurrency_scope_metrics.go` and
  `subscription_quota_observability.go`: fixed metric vocabulary, copied
  snapshots, no identity-derived labels, and unchanged lease/queue decisions.
- `repository/subscription_quota_observability.go`: the added WHERE predicate
  retains every row counted by the original FILTER expressions, including
  settled requests whose statistics acknowledgement is pending.
- Migration 254 and `migrations_runner.go`, `subscription_quota_migration.go`,
  `subscription_quota_readiness.go`: partial predicates match admission/backlog
  queries; the ledger covering index preserves the exact SUM invariant. The
  migration runs on the advisory-lock connection, removes only named invalid
  indexes before retry, and restores session timeouts through an independent
  bounded context. Readiness adds exact migration checksums and valid/ready
  checks for all three new indexes.
- `office-build-I6.py`, `office-source-snapshot.py`,
  `environment-I6-runtime.Dockerfile` and the portable R0 verification changes:
  the application targets linux/amd64 with CGO disabled; R1 and R2 use the same
  built executable and retained verified R0 layers. New issue/admission flags
  default off, while recovery remains enabled. Before/after source checks and
  separate executable, migration and runtime-recipe hashes are recorded.

The aggregate application source hash covers the explicitly listed backend,
frontend, legal and root build inputs, plus embedded frontend assets. It does
not include the validation provider or validation build recipes. Those have
separate receipt hashes and depend on the coordinator's source freeze; do not
describe the aggregate hash as covering every build-helper file.

## Validation limits

Targeted `git diff --check` for the tracked production files reviewed here
returned successfully. Source inspection covered the logger adapter, migration
runner, readiness caller and affected SQL callers. Unit/integration execution,
final-image provenance, long-stream behavior, upgrade/rollback and browser
acceptance remain the coordinator's separate gates. No skipped or unexecuted
test is counted as a pass in this document.
