# Implementation and verification

- [x] Inspect original tree and preserve dirty work.
- [x] Create independent fork and integration checkouts.
- [ ] Recover BWH runtime and verify build-source/migration provenance.
- [ ] Commit baseline image fix and task artifacts.
- [ ] Merge and validate upstream 0.2.3 compatibility.
- [ ] Merge and validate upstream 0.2.4 compatibility.
- [ ] Merge and validate upstream 0.2.5 compatibility.
- [ ] Merge fixed 0.2.6 and review every upstream feature/custom overlap.
- [ ] Preserve safe current deployment scripts and branding assets.
- [ ] Verify immutable migration inventory and upgrade/replay/rollback schema on isolated PostgreSQL.
- [ ] Run backend unit suites, compile with build tags, build and vet.
- [ ] Run frontend typecheck, lint check, tests and production build.
- [ ] Validate new build HTTP/WebSocket and desktop/mobile UI with synthetic fixtures.
- [ ] Independent semantic review, final evidence and staged rollout guide.

Commands are executed from the integration checkout only. Standard checks: backend `go test -tags=unit ./...`, `go test -tags=unit -run '^$' ./...`, `go vet ./...`; frontend `pnpm typecheck`, `pnpm lint:check`, `pnpm test:run`, `pnpm build`. Select focused tests after each stage and broaden at final integration, without classifying compile-only results as transactional proof.

Production read operations belong to the baseline agent/root. No other agent runs shared database fixtures. If local Docker is unavailable, use a local native disposable PostgreSQL runtime or document the actual limitation rather than running on BWH.
