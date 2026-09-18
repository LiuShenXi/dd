# Synthetic runtime acceptance — 2026-09-19 Asia/Shanghai

The candidate passed local HTTP acceptance, deterministic HTTP/WS accounting
acceptance and an actual old-binary rollback/return sequence. It is running at
`http://127.0.0.1:38626` for browser review. No production deployment, database
copy or production mutation occurred in this runtime test.

## Executable provenance and isolation

The Linux amd64 executable embeds the final 185-file frontend. Its SHA256 is
`d9a766fc1bd1e93b60411fdc7180c49c017eef47a82db3849d4e66256034aa01`.
Frontend `index.html` SHA256 is
`10c2582dcef9b46c32a994fed2b353035ee827a81783a694b5854f0b876cd726`.

Build HEAD was `7ac4f2bc5` with the compatibility production changes already in
the working tree. Exact production build inputs match committed `8c3275d59`:
`b27fb220de74f244dcf97f7e89e5f34d0499f3aa2b5100ed28de9054a9f387d3`.
The original HEAD label is not a claim that this binary represents a clean
`7ac4f2bc5` checkout. The per-file manifest and source equivalence evidence
record the actual input bytes.
This is a dedicated runtime validation image containing the server executable.
It is not a build of the repository's full multi-stage production Dockerfile.
The separately recovered `carpool-release` helper and production packaging check
are covered by the parent integration report, not this container test.

All PostgreSQL 18 / Redis 8 / app / mock data is synthetic. The database has 297
applied migrations. The app and mock have only the internal Docker network;
only Nginx publishes `127.0.0.1:38626`. All resources use the
`sub2api-upgrade026-app-*` prefix and task ownership label, except two unused
anonymous volumes inherited from the initial PostgreSQL base image. Those are
recorded separately and retained by scoped cleanup; newer launches override
the inherited mount with tmpfs. Secrets and raw process logs are private outside
Git. The initial health check was 200; unauthenticated `/v1/models` was exactly
401, as were the checks after each app switch.

## Passed checks

| Area | Observed result | Evidence |
| --- | --- | --- |
| Ticket settings | Initially off; enable and disable persisted and were visible without restart; authenticated proxy URL returned a masked password; masked round trip preserved configuration; invalid FTP scheme returned 400 without leaking the password or replacing the saved config | `ticket-settings-http.json` |
| Rolling carpool | Active, future and expired synthetic users; latest rolling plan; 28-day terms; preview/open reset boundary equal; active key `/v1/usage` quota matches user details and omits ordinary balance | `http-fixtures.json` |
| Ordinary balance isolation | Opening/binding carpool terms did not change ordinary balances; ordinary account starts and remains at 77 during fixture/pagination checks | `http-fixtures.json`, `redeem-history-pagination.json` |
| Redemption pagination | Three synthetic concurrency redemptions; legacy array preserved; page size 2 returns 2 + 1 items, equal ordering and no overlap; invalid page 0 returns 400 | `redeem-history-pagination.json` |
| Real HTTP and WS billing | One HTTP Responses request plus two turns on the same client WS connection; three receipts, three durable ledger debits and three usage records; mock per-model inference delta 3 and WS-turn delta 2; ordinary balance remains 47.25. Final native-upstream run also requires Responses delta 3 and Chat Completions delta 0 | `../mock/evidence/README.md`, `../mock/evidence/http-ws-native-responses.json` |
| Actual old executable | Recovered production executable starts against the same upgraded synthetic DB/data/config; health/auth/admin login and rolling carpool snapshot/key quota all pass | `old-binary-rollback.json` |
| Config rollback compatibility | Old `models_list_config` API write updates both DB columns; returned candidate reads it as `model_allowlist`; new API write synchronizes both columns back; original fixture config restored | `old-binary-rollback.json` |
| Return to candidate | Old executable stopped before candidate starts; health/auth/admin and carpool key checks pass after the old auth cache has been exercised | `old-binary-rollback.json` |

The recovered old binary SHA256 is
`7c8ac352ca44541b770c13bdcce913586cf138a75013b6477866e30af92b0bc1`,
the exact checksum independently verified from the BWH download. Both app
versions never ran concurrently. Nginx was restarted per switch so its static
DNS upstream resolved the active container. The old and probe containers remain
stopped, while candidate/PG/Redis/mock/ingress are running.

## Harness corrections and boundaries

The first HTTP mock request was rejected before reaching the mock because the
existing URL allowlist's strict mode always requires HTTPS. The isolated runtime
was changed to format-only URL validation with insecure HTTP allowed, following
the pre-existing local mock convention. Application network isolation remained
internal, the image/data were unchanged, and no product or production config was
modified. This local-only policy is recorded in `local-http-mock-policy.json`.

The next run completed HTTP, both WS turns and financial settlement, but its
harness incorrectly treated global mock connections/prewarm requests as user
turns. The probe now compares before/after counters scoped to a unique synthetic
model and records global statistics as observations. The final run exited 0
with all accounting assertions passing. Failed-run evidence stays private.
The first successful run used the client's Responses route with automatic
upstream Chat Completions conversion. A further independent fixture explicitly
selected native upstream Responses and passed all financial assertions with
three Responses calls and zero Chat Completions calls. Both successful paths
are retained as separate probe evidence; the native run did not restart the app
or invalidate the earlier rollback result.

There are zero OAuth accounts; the ticket checks cover settings, validation and
masking, not real collection/injection. No real upstream or image-generation
request was sent. The actual old executable check covers startup, selected
carpool reads and bidirectional model config writes on synthetic data; it is not
proof for every old-version write path or production load. Browser findings are
reported separately by the UI reviewer. Production rollout is still outside the
scope of this acceptance run.

`runtime.py check` performed the complete read-only ownership preflight. Stop and
cleanup commands are implemented with exact identity/ownership checks but were
not executed, to retain the candidate for review. See the runtime README for
commands and private credential file locations.
