# I3 F3 isolated execution receipt

All required execution finished successfully on frozen source
`c9d25ce07053fba7642a8eb4473288b88877d4bec62472df8cf2180ef4b7eebe`.
The consolidated machine-readable evidence is
`I3-pg-F3-final-receipt.json`; coordinator acceptance remains a separate review.

| Actual validation | Top-level passes | Including subtests | Fail / individual skip | Receipt |
| --- | ---: | ---: | ---: | --- |
| Original unmodified TestMain, real PG18 integration/fault/concurrency | 87 | 234 | 0 / 0 | I3-pg-F3-20261019T160927Z-receipt.json |
| Linux critical unit concurrency race, tags=unit | 293 | 636 | 0 / 0 | I3-pg-race-F3-20261019T160927Z-receipt.json |
| Windows focused unit regression | 356 | 692 | 0 / 0 | I3-windows-F3-20261019T160855Z-receipt.json |
| Actual R1 server recovery/restart/HTTP proof | 1 | 1 | 0 / 0 | I3-pg-F3-R1-final-receipt.json |

The87 PG cases comprise37 billing cases,29 retained I1/I2/protected/coexistence/GPT
cases,8 additional original GPT lifecycle/recovery cases and13 original automatic
ExecuteResetBatch cases. T16 includes the actual GPT-terminated combination:
first issue the Claude card, fingerprint complete Claude tables/parents/Keys/users,
then actual CarpoolService.Terminate, prove those rows unchanged, reject GPT with
zero writes, and settle against the exact already-issued Claude term ID. T21
automatic execution and official manual reset are tested separately.

Windows also compiled all packages and ran the recorded vet scope successfully.
All84 original GPT baseline unit names are present; the sole package skip is an
empty helper package, with zero skipped individual tests. Linux race is a unit
concurrency check, not a PostgreSQL integration race. Real PG concurrency/fault
cases provide the separate durable-database layer.

Every receipt records exact commands, full JSONL and before/after manifests with
the same source hash. The source snapshot includes test sources and ignored
embedded frontend dist files. HEAD is
`0d61e956e2a9d86fbe7fb9a24adbdb47e687c486` with dirty frozen I3 changes.
The frontend dist was reused unchanged, SHA256
`b657870fcdf990281b559656e5bd671843e0f626608a9bfd3e7dcc0d26d96542`.
F1→F3 differs only in the new billing integration test file, documented in
`I3-pg-F3-vs-F1-source-diff.json`; no business implementation changed between
those freezes. F1/F2 evidence remains archived and is superseded.

## Actual current-receipt R1 image

Image: `sub2api:claude-weekly-i3-receipt-r1-c9d25ce07053`

ID: `sha256:c17ee70e59572a06cb4b44d7334739120f0425d71cb2978e705ae36943ea0897`

The version and image revision explicitly bind the I2 HEAD plus
`dirty-frozen-I3-` and the complete F3 hash. The build receipt
`I3-pg-F3-20261019T161328Z-build-receipt.json` records actual binary hashes,
image inspect, version output and PostgreSQL18.1 client smoke checks. This is a
local dedicated recipe, `environment-I3-R1.Dockerfile`, using pinned Alpine3.23.3
and PostgreSQL18.1 client layers. The Linux builder uses Go1.27.2 with CGO disabled;
Windows tests use Go1.27.0. It is additional local evidence, not a claim that the
normal deployment recipe or production compiler was used.

The actual image test used new synthetic PG/Redis fixtures from original TestMain,
an internal10.247.191.0/28 network, no published ports, no Docker socket and no
external upstream. It booted the real Wire/server with issue=false,
admission=false and recovery=true. Independently committed Claude known0.50000000
(original account0.10000000) and original GPT known0.25000000 recovered once.
Restarting the same real container left ledger/counters/request/dedup/log facts
unchanged. A real term HTTP request returned503/TERM_BILLING_UNAVAILABLE with no
new admission. Ordinary user/native-parent rows remained unchanged. The proof
resources were removed by test cleanup; the image is retained.

This R1 supports current receipt recovery plus existing GPT recovery. It does
not establish I5 paid-order snapshot/fulfillment recovery. I6 must revalidate
the final receipt and order schemas and freeze another R1 if needed. No image
was published, deployed, activated or connected to production.

## Exact ownership and failure evidence

`I3-pg-traceability.md` maps each T13–T44 to actual test names and evidence layers.
Claude settlement allows changes only to the admitted original term/request/
ledger, the admitted Key counters, actual account counters, owned usage log and
that Key/account's cache/scheduler publication. Complete foreign term/request/
ledger/log/Key/account/outbox fingerprints protect user B, next-period cards and
other requests. Separate full-table fingerprints protect users (balance/frozen),
native parent windows, redeem codes and all carpool/payment tables. Reverse GPT
settlement keeps all Claude rows unchanged. The original account's last-used
time is checked as monotonic admitted-at publication, and disabled/deleted
entities are never revived.

Maximum999999999999.99999999 and minimum0.00000001 are checked exactly through
JSONB receipt, original ledger, Key/account counters, log and hourly/daily/group
aggregates. The maximum fixture tests exact durable storage, not legacy float
pricing representability. Another test uses the actual Gateway pricing factory
and verified-usage calculator after price edits/card expiry. JSONB exponent and
decimal spelling changes retain identity/hash.

Historical diagnostic failures remain in `I3-pg-test-plan.md` and their original
JSONL. The reproduced PostgreSQL40P01 was a real lock-order defect: Apply waited
on api_keys FOR UPDATE while statistics INSERT waited on user_subscriptions FK.
The writer now takes dashboard writer→scope→parent→term→request before INSERT/FKs.
The identical two-worker5s/15s case passes; timeout relaxation was not the fix.
The earlier1s observation, migration250 trigger dependency, unacknowledged cleanup,
test-only cycle-column typo and default Docker IP-pool exhaustion remain distinct
historical records. Default-pool failure occurred before any server started.

## Reproduction and retained environment

Run from this worktree in PowerShell with source frozen:

```powershell
./validation/claude-weekly-quota/I3-pg-final-run.ps1 -Freeze F3
./validation/claude-weekly-quota/I3-pg-race.ps1 -Freeze F3 -Expected 293
```

Then pass those actual receipts to `I3-pg-build-R1.ps1 -Freeze F3 -PGReceipt ...
-RaceReceipt ...`, and pass the resulting build receipt to
`I3-pg-R1-proof.ps1 -BuildReceipt ...`. The final aggregation script
`I3-pg-final-receipt.ps1` verifies all five receipt hashes/counts/source identities.
Exact commands for this run are preserved in the machine-readable receipts.

The task-only PG18, Redis, internal10.247.190.0/24 network, Go runner and four
volumes remain enumerated in `environment-active.json`. They have no published
ports. The runner has8GiB,4CPU,512PIDs, GOMAXPROCS=2 and Go-p2; build temporaries
use its dedicated output volume. The approved4→8GiB update is recorded in
`environment-I3-runner-limits.json`. No test timeout or assertion was weakened.
Docker Desktop had automatically restarted pre-existing containers under their
own policies; none were explicitly modified or used by this task.

`environment-I4-design.md` reserves only the future isolated candidate/provider
design. It has not allocated an I4 browser port or created an I4 provider.
