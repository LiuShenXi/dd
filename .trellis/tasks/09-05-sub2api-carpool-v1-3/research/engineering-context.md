# Verified Engineering and Adoption Context

Date: 2026-09-05. Sources: local package manifests, existing OpenSpec artifacts, and director status confirmation. No production-data contents are recorded here.

- Repository architecture: Go/Gin/Ent/PostgreSQL/Redis and Vue/Vite/TypeScript/pnpm. Do not apply monasapi's unrelated GORM/React rules.
- Backend uses appended SQL migrations and coordinated Ent generation. Update affected interface stubs. Current core migration ownership is `235_carpool_core.sql`.
- Host Go 1.21.1 is too old; the director verified cached `C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe` as Go 1.27.0. Verify the executable before running checks.
- Frontend package scripts expose typecheck, lint:check, test:run and build. The lint script includes autofix, so review uses lint:check.
- Required full reads: `openspec/changes/add-carpool-v1-3/proposal.md`, `design.md`, `api-contract.md`, `tasks.md`, and source architecture `C:/WORK-SPACE/monasapi/sub2api-carpool-architecture.md`.
- Existing director workers `/root/core` and `/root/gateway` were confirmed running with model gpt-5.6-sol/high/fork_turns=none. Neither is replaced for Trellis adoption. Their exact ownership is in task design.md.
- Do not inject credential-bearing development-environment instructions or copy secrets to task docs. Generated guideline placeholders and bootstrap task are not evidence of completed specification work.
- Automatic Codex hooks are not assumed. Explicit task path, manifests and child-side file reads provide context without changing global configuration.
- The latest user authorized read-only export of remote Sub2API database and isolated local-copy testing. Source coordinator owns that operation. Production writes, real provider use, external notifications, push and deployment remain forbidden. Use local network isolation, restricted storage, neutralized live credentials, and synthetic UI/test identities.

## Handoff Evidence

Director task: `01a0722c-e17a-7092-8abd-d7776b150f1b`. Source coordinator: `01a07224-803c-7ef0-938d-10119388b6ad`. The director acknowledged avoiding duplicate .trellis initialization and preserving workers. Director-session task activation and worker context acknowledgement must be recorded after they actually occur. Implementation acceptance is still pending.
