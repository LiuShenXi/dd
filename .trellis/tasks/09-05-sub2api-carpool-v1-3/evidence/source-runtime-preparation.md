# Source Runtime Preparation

Status at 2026-09-06 00:55 Asia/Shanghai: preparation passed; application startup and feature acceptance are still pending director code-ready authorization.

## Ownership

Source coordinator owns repository-root `validation/source-runtime/` and these source-runtime evidence files. Mock and HTTP acceptance subpackages are delegated to separate `gpt-5.6-sol` / `high` workers. No application, generated Ent, route, Wire, frontend, or director-owned task documents were changed by this work package.

## Independent Checks

| Check | Actual result |
| --- | --- |
| Cached Go executable version | `go version go1.27.0 windows/amd64` |
| `prepare-runtime.ps1` | Exit 0; local config and Linux synthetic bootstrap generated outside git; no application started |
| Mock `go test -count=1 ../validation/source-runtime/mock/main.go ../validation/source-runtime/mock/main_test.go` from backend | Exit 0, `ok command-line-arguments 0.866s` |
| Mock `go vet` against the same files | Exit 0, no diagnostics |
| PowerShell parser over runtime scripts | Four scripts parsed without errors |
| Copied-record baseline capture and immediate comparison | Both exit 0; original users/keys/usage/dedup/groups unchanged |

The mock covers JSON and SSE usage, configured failure/next/named cases, size/delay/private-peer boundaries, aggregate statistics without prompt/authorization/arbitrary-path leakage, and multi-turn Responses WebSocket messages. Source review found and returned a control-response data race; the worker fixed it by capturing immutable response data while holding the scenario lock. Documentation now uses only `carpool-mock:8090` for the isolated deployment. Windows CGO was not enabled, so no race-detector pass is claimed.

The full copied-database sanitization and 109-metric conservation evidence is in `research/local-data-copy.md`. Runtime preparation creates no carpool memberships for copied users and does not infer a tier from balances. User registration timestamps remain unchanged.

## Pending Gates

- Director's explicit backend/frontend code-ready evidence.
- Independent review of runtime/bootstrap safety and failure paths.
- Complete HTTP acceptance driver and review.
- Linux embedded app build, fresh app volume, bootstrap, actual local health/egress checks.
- End-to-end API/accounting acceptance and post-test copied-record conservation.
- Director/frontend-owner desktop and narrow-screen browser acceptance.

Generated secrets, credentials and raw process diagnostics remain under the restricted private test directory outside git. This document contains no source-user rows, credentials or copied identifiers.
