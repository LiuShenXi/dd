# Implementation and verification

- [x] Inspect original tree and preserve dirty work.
- [x] Create independent fork and integration checkouts.
- [x] Recover BWH runtime and verify build-source/migration provenance.
- [x] Commit baseline image fix and task artifacts.
- [x] Merge and validate upstream 0.2.3 compatibility.
- [x] Merge and validate upstream 0.2.4 compatibility.
- [x] Merge and validate upstream 0.2.5 compatibility.
- [x] Merge fixed 0.2.6 and review every upstream feature/custom overlap.
- [x] Preserve safe current deployment scripts and branding assets.
- [x] Verify immutable migration inventory and upgrade/replay/rollback schema on isolated PostgreSQL.
- [x] Run backend unit suites, compile with build tags, build and vet.
- [x] Run frontend typecheck, lint check, tests and production build.
- [x] Validate new build HTTP/WebSocket and desktop/mobile UI with synthetic fixtures.
- [x] Independent semantic review, final evidence and staged rollout guide.

Commands are executed from the integration checkout only. Standard checks: backend `go test -tags=unit ./...`, `go test -tags=unit -run '^$' ./...`, `go vet ./...`; frontend `pnpm typecheck`, `pnpm lint:check`, `pnpm test:run`, `pnpm build`. Select focused tests after each stage and broaden at final integration, without classifying compile-only results as transactional proof.

Production read operations belong to the baseline agent/root. No other agent runs shared database fixtures. If local Docker is unavailable, use a local native disposable PostgreSQL runtime or document the actual limitation rather than running on BWH.

Completed automated evidence: frontend 310 suites / 2361 tests; backend 59 tested packages, 20147 passing test results including subtests, 19 conditional skips; vet passed; PostgreSQL 100 top-level / 180 total tests with no failures/skips. Initial fixture failures were investigated and retained in logs: missing shell/timezone data in the host/runner, upstream fallback assertions requiring the customized driver value, and a same-tick timestamp fixture that did not actually create a new generation. No behavior was weakened to make those cases pass.

The full runtime binary records its pre-commit source input hash (including the later committed compatibility fix). Later changes were tests/docs and standard Dockerfile packaging of the existing release CLI; application input bytes remain equal. The source manifest establishes the exact tested server code. Production remains unchanged.

Runtime acceptance passed both HTTP conversion and native Responses, each with two WebSocket turns and three exact ledger/receipt/usage records, without ordinary balance debit. The downloaded old production binary passed a same-database rollback and return to candidate, including dual-column group writes and auth-cache rebuild. Desktop/390px browser interactions and visible DOM widths passed. Screenshot capture consistently timed out, so no pixel-based visual approval is claimed. True external OAuth ticket harvesting, production-scale performance, and a full standard multi-stage Docker image build are explicitly outside the completed local acceptance; CLI Linux build/help and the dedicated embed runtime image were verified.
