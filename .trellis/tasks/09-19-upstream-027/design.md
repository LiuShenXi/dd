# Design

Use an isolated worktree of the completed 0.2.6/sentinel repository, branch codex/compatible-0.2.7. Three-way merge v0.2.7 from baseline 2fefcf51f; common upstream ancestor efe9aab1e4ec89a42ba45e8dac20e882c5409a6a. The 59-file upstream delta merges without textual conflicts.

Preserve existing v25 auth snapshots, gpt-5.5 image orchestration, durable partial-usage settlement, image cache billing, 234z dual-column migration, release guards/CLI, carpool UI and sentinel control routes/renewal semantics. Audit automatically merged DI and route registration. The new Seedance asynchronous task API must reject carpool keys before upstream work until an explicit durable asynchronous carpool settlement contract exists; ordinary-key behavior remains upstream-native. Bulk API-key editing must retain existing carpool group assignment restrictions.

No historical migration edits, unrelated refactors, connection changes, production mutations, or upstream calls with real credentials. All database/runtime tests use isolated synthetic resources. Preserve the old dd/dd-update-026 and exploratory dd-update-027 directories; their stale-base attempt is superseded, not the source of this final result. Trellis scripts are absent; maintain artifacts directly.
