# Carpool v1.4 source-runtime HTTP acceptance

This directory contains an external Go acceptance driver. It talks only to the
isolated application and mock upstream on their private Docker network. It does
not import backend internals, connect to PostgreSQL, inspect copied records, or
write production data.

The driver requires a runtime-created fixture file with this exact contract:

```json
{
  "version": 1,
  "source": "synthetic-local-only",
  "base_url": "http://carpool-app:8080",
  "mock_url": "http://carpool-mock:8090",
  "users": [
    {"name": "admin", "id": 1, "email": "generated", "password": "generated"}
  ]
}
```

The two URLs are strict allowlist values; aliases, alternate ports, paths,
query strings, credentials in URLs, HTTPS endpoints, and trailing slashes are
rejected.

`users` must include `admin`, `four`, `three`, `two`, `fifth_four`,
`fifth_three`, `fifth_two`, `ordinary`, `expired`, `renewal`, `termination`,
and `takeover`, each with its exact `carpool-test-<name>@example.invalid`
address, positive unique ID, and a 24-byte hexadecimal password. The driver
validates all twelve bootstrap records but authenticates only `admin` from this
file.

The three `fifth_*` bootstrap names are retained only because the sealed private
fixture contract is immutable. Fresh users created under those names exercise
the fourth week of a new 28-day term; they do not assert or create a fifth
cycle.

After authenticating and verifying the bootstrap administrator identity, the
driver creates eleven fresh users through the admin API. Their email addresses
and passwords are newly generated for the run. The in-memory test identities
are then replaced with this fresh batch, so no bootstrap non-admin user is
authenticated or reused. `ordinary` receives a positive synthetic ordinary
balance for the no-fallback assertion; the other carpool-only users start at
zero, except for the explicit takeover fixture.

`-batch-fixtures` is required. It receives a private envelope containing the
fresh user IDs, emails, and passwords for the current run. The file is created
exclusively with owner-only permissions on platforms that support them, is
synced before carpool mutations begin, and is removed if writing fails. Its
path must differ from both the bootstrap fixture and report paths. A rerun must
use a new, nonexistent batch-fixture path; the driver never overwrites a prior
batch. Do not put either fixture file in Git or print either one in command
output.

Build and run from `backend/` so the repository's pinned Go toolchain and
module settings are used without a second dependency tree:

```powershell
$go = 'C:\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.27.0.windows-amd64\bin\go.exe'
$acceptance = Join-Path $env:TEMP 'carpool-acceptance.exe'
& $go test ../validation/source-runtime/acceptance/main.go ../validation/source-runtime/acceptance/main_test.go
& $go build -o $acceptance ../validation/source-runtime/acceptance/main.go
& $acceptance `
  -fixtures <runtime-fixtures.json> `
  -batch-fixtures <runtime-output/carpool-http-batch.json> `
  -report <runtime-output/carpool-http-acceptance.json>
```

The executable returns nonzero when an assertion fails. Its report contains
only assertion names/statuses, numeric results, and IDs belonging to synthetic
records created or supplied for this run. Access tokens, API keys, passwords,
request authorization, response bodies, prompts, and copied-user data are
never written to the report.

The missing-idempotency-key no-mutation assertion accepts both `items: []` and
`items: null` when the same paginated response has numeric `total: 0`. This is
limited to that zero-result check: an absent `items` field, a non-array non-null
value, a nonzero total, or a nonempty array still fails. Other array contracts
remain strict.

Before gateway checks, the driver registers one named mock case per synthetic
model through `POST /__control`. This makes `GET /__stats` `by_case` deltas an
exact forward-count signal for HTTP, SSE, and admission-rejection assertions.

The current suite covers:

- admin login and creation of an isolated carpool group, mock-backed OpenAI
  account, and administrator-bound user keys;
- exact latest-plan projections and all three plan previews, including fixed
  pricing, four weekly windows, 28-day expiry, base totals, three boosts, and
  validation failures;
- open replay/conflict behavior, full-value fourth-cycle boosts, four-way
  concurrent claiming capped at three successes, exact remaining counts,
  exhaustion state, and exact boost GET/POST field allowlists;
- server-authoritative quota before and after all three boosts, exact
  user-details quota/reset-event projections, and admin/user authorization
  boundaries, including ignored user/term/cycle query selectors;
- real `/v1/responses` HTTP and SSE forwarding with an independently observed
  one-dollar debit after each request;
- rejection before forwarding for missing, expired, and terminated terms, with
  no ordinary-balance fallback;
- future renewal with exact contiguous timing and no early grant, payment/refund
  separation and net CNY, reversible audited manual adjustment, and idempotent
  explicit takeover transfer;
- immutable term snapshots across renewal and creation of a harmless same-value
  latest plan version, including rejection of the superseded plan in preview.

WebSocket traffic, reset observation/qualification/scheduling/execution,
reset announcements, delayed billing-recovery behavior, and UI screenshots are
intentionally excluded and owned by separate acceptance packages. A passing
report from this HTTP driver is not full-feature acceptance evidence and must
not be presented as evidence for any of those areas.
