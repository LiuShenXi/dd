# Reset focused verification

Working directory: `C:\WORK-SPACE\sub2api-carpool-v1.3\backend`

Command:

```powershell
gofmt -w internal/service/carpool_reset_service.go internal/service/carpool_reset_runtime_test.go
go test ./internal/service -run 'CarpoolReset|ResetObservation|ResetSchedule' -count=1
go test ./internal/repository ./internal/handler/admin -run '^$' -count=1
```

Exit code: `0`

Output:

```text
ok  github.com/Wei-Shaw/sub2api/internal/service 0.898s
ok  github.com/Wei-Shaw/sub2api/internal/repository 0.697s [no tests to run]
ok  github.com/Wei-Shaw/sub2api/internal/handler/admin 0.660s [no tests to run]
```

The manual-scan timeout regression initially failed because worker cancellation was converted into a successful incomplete result. The service now checks `ctx.Err()` after all workers stop; the rerun above proves the timeout is returned to the operation layer instead of being persisted as a successful replay.
