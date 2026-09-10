# Old Application Drain Evidence

These checks are read-only. The release coordinator owns router reload and any
later application stop. Do not stop green merely because a timer expires.

## Capture Before Router Reload

Run on the configured Sub2API host:

```sh
docker top sub2api-router -eo pid,ppid,lstart,args
docker inspect sub2api-green --format '{{json .NetworkSettings.Ports}}'
docker exec sub2api-router nginx -T 2>&1 |
  awk '/worker_shutdown_timeout|keepalive_timeout|proxy_read_timeout|proxy_send_timeout|upstream|server sub2api/ {print}'
```

Save the exact worker PIDs and their start times before reload. A successful
reload starts new workers; the previous workers remain while their old client
connections drain. `nginx: worker process is shutting down` is expected during
this period. Keep waiting while any previous worker remains. Persistent
WebSockets can keep a worker alive indefinitely, including new turns on an
already established connection.

The 2026-09-08 inspection found master PID 3477362 and workers 2196997/2196998;
these are snapshots, not reusable deployment constants. Active upstream was
`sub2api-green:8080`, `keepalive_timeout` was 65 seconds, and proxy read/send
timeouts were 1200 seconds. No `worker_shutdown_timeout` was configured. The
proxy timeouts measure inactivity, not total generation time. Do not add a forced
worker-shutdown deadline to make the drain appear complete.

## Check After Router Reload

```sh
docker top sub2api-router -eo pid,ppid,lstart,args
docker exec sub2api-green sh -c '
  awk '\''$2 ~ /:1F90$/ { states[$4]++ }
       END { for (state in states) print state, states[state] }'\'' \
       /proc/net/tcp /proc/net/tcp6
'
```

`1F90` is port 8080. State `01` is ESTABLISHED, `06` is TIME_WAIT, `08` is
CLOSE_WAIT, and `0A` is LISTEN. Count both IPv4 and IPv6 tables. The initial
inspection observed 26 established sockets, one TIME_WAIT, and one listener;
that does not mean 26 generating requests. The upstream configuration includes
`keepalive 64`; idle pooled proxy connections and health probes can also be
established sockets. Old worker exit closes that worker's upstream pool.
TIME_WAIT and LISTEN are not active user requests.

Require previous Nginx workers to disappear and old upstream connections to
clear. The inspected green port mapping is `127.0.0.1:28080 -> 8080`; keep local
administrative scripts and validation requests off that endpoint during drain.
Recheck there is no additional ingress path that can admit work after the router
switch. A transient localhost health probe
does not bill, but unexplained persistent connections must be investigated.
Do not kill individual sockets or disable production health checks to force a
zero counter.

## Finish Accounting Before Import

Zero sockets alone does not prove accounting completion. Independently require
no `processing` Redis `image_task:*` records for migrated users, no active batch
jobs or held user balances, and no pending payment fulfillment. The old async
image handler detaches a goroutine and has no shutdown drain hook.

After those conditions, the coordinator can gracefully stop the old application
without a forced kill deadline. The deployed source revision `36266f512776`
defers `app.Cleanup()`; cleanup calls `UsageRecordWorkerPool.Stop()` and waits for
all service cleanup goroutines before closing Redis and Ent. Its 10-second
cleanup context only produces a warning; it does not stop waiting. Verify log
messages for usage-pool cleanup success, infrastructure cleanup, and normal
container exit. Do not accept a SIGKILL or unexplained exit as proof of settlement.

Only after the old process is gone should the single serving new process enter
the release drain, reach zero active HTTP/pending usage, and lock migration.
Verify the new container's startup-held setting before the import transaction.
This ordering removes old application writers while users continue using the
new compatible application until its brief final migration hold.
