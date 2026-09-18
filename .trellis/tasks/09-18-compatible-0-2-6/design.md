# Compatibility design

Start from the custom production lineage, preserve the deployed image-model fix, and merge upstream in ascending release stages: 0.2.3, 0.2.4, 0.2.5, then fixed 0.2.6. Each stage is a local commit so conflicts and regressions can be assigned to a bounded delta. Preserve the upstream ancestry rather than copying a subset of advertised features.

Behavior lives across gateway admission/usage, carpool service/repository, lifecycle providers/routes, migrations, Vue/Pinia and deployment scripts. Resolve conflicts at those owning layers, keeping new upstream protocols alongside the existing custom financial transaction boundaries. Generated Wire output must reflect both provider sets. Source-level auto-merges require semantic review, especially WebSocket turns and image billing.

New upstream SQL files coexist with custom files even if numeric prefixes overlap: the runner keys complete filenames. Existing deployed filenames/checksums remain immutable; verify chronological dependencies and add forward convergence migrations only when a concrete schema issue requires it. Recovery never rewrites existing balances or term snapshots.

The new ticket feature remains opt-in. Its fail-closed behavior and proxy requirement must be explicit in acceptance and rollout documentation. No real ticket harvesting is required to validate collection/injection mocks.

Runtime recovery is separate from Git history: download BWH binaries/resources and hash them; compare against the saved 2026-09-15 build source/binary. Keep private runtime/config/data outside Git. Candidate runtime uses synthetic credentials and fresh isolated services, with no production network attachment.

Rollback uses staged Git checkpoints during development. Future production rollback preserves the prior image and deployed configuration; do not restore a stale DB snapshot after new traffic has produced accounting writes. Validate old application compatibility with additive new schema before any rollout.
