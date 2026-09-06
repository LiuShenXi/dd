# Reset HTTP runtime acceptance driver

This standalone black-box driver covers the Carpool v1.4 reset administration and user-announcement HTTP contracts without database writes, clock changes, provider traffic, reset-card access, observation scans, or external notifications. Its narrowly scoped database reads verify source identity that the public announcement DTO intentionally omits. Fresh terms use the current 28-day, four-cycle, three-boost plan contract.

It accepts only the exact `synthetic-local-only` bootstrap fixture and `http://carpool-app:8080`. The fixture must contain the twelve bootstrap identities in their canonical order, with the canonical synthetic emails and 24-byte hex passwords; the administrator must be ID 14. Only that administrator is reused. Each attempt creates fresh `member`, `future_member`, and `outsider` users through `/api/v1/admin/users`, then writes their credentials to the private `-run-fixtures` output before creating a synthetic carpool group or terms.

The driver selects and validates the current enabled `four_seat` plan, opens one active and one future term through the real admin APIs, and then verifies:

- reset admin authentication and administrator authorization;
- missing registration idempotency-key rejection without a batch mutation;
- manual confirmed qualification, exact replay, and conflicting-payload rejection;
- precise next Shanghai 22:00 scheduling from the returned database timestamp;
- schedule success, exact replay, and conflicting-payload rejection;
- due-only execution refusal before the slot, followed by a batch read proving zero targets and zero grant;
- publication within 20 seconds and version/list/read-state audience isolation for the active member, future member, and outsider.

Announcement list responses are decoded into an identity/read-state projection that has no title or content fields. Each snapshot is accepted only when version reads immediately before and after the list agree on both version and unread count, and the unread count matches the list projection; publication races are retried within a bounded child context. The newly published announcement is identified by the exact pre-action ID-set delta shared by both run-owned carpool members and absent from the run-owned outsider. Copied announcement bodies and copied users are never queried, validated, or reported.

The report identifies one of two bounded scenarios. `first_publication` requires an empty reset-batch baseline, proves one new batch and one exact source announcement, and polls for its publication. `pending_merge` accepts exactly one already scheduled, unexecuted, zero-grant batch, proves the new run-owned qualification joins that batch, and proves the batch ID, schedule, source announcement ID, user versions, and user lists do not produce a duplicate publication. Source identity is checked through a read-only PostgreSQL connection using only reset qualification hashes and announcement `id/source_*/status` columns; title and content are never selected.

Actual execution inside the one-minute 22:00 slot is intentionally not attempted. The report keeps it out of `assertions` and records `excluded_checks: [{"name":"reset.actual_due_execution","coverage":"postgresql_covered"}]`; real grant execution remains PostgreSQL repository coverage and cannot be claimed as passed by this driver. The driver also refuses to register a qualification when the server-derived next slot is less than three minutes away.

Run it only through the isolated runtime wrapper, using private, distinct, nonexistent output paths:

```text
reset-http -fixtures <bootstrap-fixtures> -run-fixtures <fresh-run-fixtures> -report <sanitized-report>
```

Both outputs are mode `0600` on Unix, are published through a same-directory hard link, and refuse overwrite. The report contains only fixed assertion/status/category fields and numeric synthetic IDs. It never contains credentials, tokens, request payloads, response bodies, announcement copy, source event text, actor notes, or copied data.

The wrapper must inject `CARPOOL_RESET_DB_HOST=carpool-db`, `CARPOOL_RESET_DB_PORT=5432`, `CARPOOL_RESET_DB_NAME=carpool_test`, `CARPOOL_RESET_DB_USER=carpool_test`, and the private password through `CARPOOL_RESET_DB_PASSWORD`. The driver enforces `default_transaction_read_only=on` and rejects every other database identity. It performs no SQL writes.

Build and test from `backend/` so dependency versions remain pinned:

```powershell
go test -count=1 ../validation/source-runtime/reset-http/main.go ../validation/source-runtime/reset-http/main_test.go
go vet ../validation/source-runtime/reset-http/main.go ../validation/source-runtime/reset-http/main_test.go
$env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'; go build -trimpath -o <private-output> ../validation/source-runtime/reset-http/main.go
```
