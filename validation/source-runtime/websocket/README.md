# WebSocket runtime acceptance driver

This directory contains the independent source-runtime driver for the four required Carpool WebSocket contracts:

- two turns create two durable receipts and two one-time debits;
- a later turn after term expiry is rejected before the mock;
- same-turn account failover uses distinct run-owned accounts, retains the originally admitted cycle and durable snapshot, and settles once;
- a successful terminal event without usage is exposed as `usage_receipt_missing` without an ordinary-balance charge.

Fresh terms use the current 28-day contract. Boundary scenarios derive expiry
from that duration; they do not alter the host or database clock.

The driver accepts only `http://carpool-app:8080`, `ws://carpool-app:8080/v1/responses`, and `http://carpool-mock:8090`. It reuses only the validated bootstrap admin, creates fresh `carpool-ws-<runID>-*` identities through real APIs, and writes per-run credentials only to the private `-run-fixtures` output.

Database access is evidence-only. The runner must inject `CARPOOL_WS_DB_HOST=carpool-db`, `CARPOOL_WS_DB_PORT=5432`, `CARPOOL_WS_DB_NAME=carpool_test`, `CARPOOL_WS_DB_USER=carpool_test`, and the private password through `CARPOOL_WS_DB_PASSWORD`. The executable enforces `default_transaction_read_only=on`, joins every billing read to the exact run-owned synthetic email, and rejects any other database identity. It performs no SQL fixture mutation or clock manipulation.

Build and unit-test from the existing backend module so dependency versions remain pinned:

```powershell
go test -count=1 ../validation/source-runtime/websocket/main.go ../validation/source-runtime/websocket/main_test.go
go build -trimpath -o <private-output> ../validation/source-runtime/websocket/main.go
```

The generic runtime wrapper should stream the binary, bootstrap fixture, and environment file through stdin into bounded tmpfs paths, then copy the report and private run fixture through `docker exec ... /bin/cat`. It must not use host bind mounts. A report is successful only when all four fixed scenario assertions are present and passed.

The runtime case uses fixed per-request pricing and therefore proves one durable `$1` cost and one debit across the boundary, not time-varying price selection. Pricing-time freezing is covered separately by the gateway's focused WebSocket unit tests.
