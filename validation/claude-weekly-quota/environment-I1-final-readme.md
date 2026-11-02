# I1 final local verification: frozen source F3

Frozen source SHA-256:
`e2d31f2525ccf72a74eb03c308611785246ce45d0e75198ef4d06fd6d7651693`.
The manifest covers 4,383 source/build/test files, including every ignored
embedded frontend output file. The exact algorithm and file hashes are in
[`environment-I1-F3-20261019T133930Z-source.json`](environment-I1-F3-20261019T133930Z-source.json).
Frontend dist SHA-256:
`b657870fcdf990281b559656e5bd671843e0f626608a9bfd3e7dcc0d26d96542`.

The source was explicitly frozen by the development owner after the two old
unit fixtures and the PostgreSQL microsecond integration fixture were corrected.
Financial code did not change between F2 and F3. Validation documents/logs are
outside the program-source snapshot scope.

| Layer | Actual result | Evidence |
| --- | --- | --- |
| Fresh synthetic PostgreSQL 18.1 | 18 top-level tests passed, zero skips/failures; source stable | [PG receipt](environment-I1-F3-20261019T133930Z-pg-receipt.json), [JSONL](environment-I1-F3-20261019T133930Z-pg.jsonl) |
| Linux critical unit race | 15 top-level tests passed across service/handler/repository/routes, zero skips/failures; source stable | [Race receipt](environment-I1-F3-20261019T133930Z-race-receipt.json), [JSONL](environment-I1-F3-20261019T133930Z-race.jsonl) |
| Frontend | Offline frozen-lockfile install and complete build passed; dist reused because source/output were unchanged | [Environment evidence](environment-evidence.json), [build log](environment-frontend-build.log) |
| Local preliminary R1 image | PASS; Linux embedded server/helper build, immutable image packaging, nonroot network-disabled version/client smoke, and binary hash agreement; source stable | [Build receipt](environment-I1-F3-20261019T133930Z-build-receipt.json) |

Built local tag: `sub2api:claude-weekly-i1-r1-e2d31f2525cc`.
Image ID: `sha256:459dd4757b05983d0fcf79cec26d91fcca45edacb640f32056623f9b242d36ae`.
Platform: Linux/amd64; uncompressed size: 339,881,387 bytes.
Server binary SHA-256:
`29e0609d4b73bc805bc9f08665794cc5ff836b23d81eca0457d52bb061d7ab00`.
`carpool-release` SHA-256:
`b0c0a4f67f98dab2123f541517f8ca7eaffc516309e1295400206ed46f789136`.

The PG set contains eight migration tests, five protected-group tests, both
coexistence tests, and the three original GPT binding/open/renew/conflict tests.
It includes the real 247 lock-timeout/session-restore and invalid concurrent
index recovery cases. The unchanged original TestMain starts only fresh
synthetic PostgreSQL/Redis containers, with `CI=true` preventing silent skips.

The Linux set executes `^TestSubscriptionQuota` with `-tags=unit -race` in four
packages. It includes no-fallback checks in standard/simple run modes, all
legacy-operation guards, refund snapshots, auth-cache versioning, protected
admission/recovery holds, async HTTP settlement, the narrow index-runner rules,
and every actual generation router alias.

## Reproduce

Complete command argument arrays are recorded in each receipt. From this
worktree, using the retained environment and active frozen-source manifest:

```powershell
pwsh -NoProfile -File validation/claude-weekly-quota/environment-I1-final-tests.ps1 -Phase pg
pwsh -NoProfile -File validation/claude-weekly-quota/environment-I1-final-tests.ps1 -Phase race
pwsh -NoProfile -File validation/claude-weekly-quota/environment-I1-build-R1.ps1
```

The build script requires both final checks to pass on the exact same source.
It refuses to overwrite an existing per-run build context. Every build/copy/image
operation has its exact executable, argument array, timestamps, exit code and
separate log recorded. The final snapshot must equal the frozen source.

Windows PG execution uses the existing Go 1.27.0 toolchain. Linux race/build uses
the official minor-tag image's actual Go **1.27.2** plus GCC 15.2.0 for race. The
server and `carpool-release` binaries are built with CGO disabled; the server
uses `-tags=embed`. The standalone local packaging recipe mirrors the repository
runtime layout but uses pinned Alpine 3.23.3 and PostgreSQL 18.1 client stages.
It is an additional local validation Dockerfile, not a change to the production
build recipe. This patch-level/runtime-recipe difference is explicit and is not
evidence of a production-matching I6 image.

The image revision includes the frozen checkout HEAD, the literal `dirty-frozen-I1`
marker, and the complete frozen source hash. The image also carries a separate
source hash label; the baseline commit alone must never identify this uncommitted
binary. The actual revision is
`311d11718aae939eccdd794d85044c99e8e92b26-dirty-frozen-I1-e2d31f2525ccf72a74eb03c308611785246ce45d0e75198ef4d06fd6d7651693`.
The build receipt records image ID, binary hashes, frontend hash, labels,
and the network-disabled nonroot `--version`/client-tool smoke commands.

## Preserved failures and limits

- The first 247 run failed before business tests because its new comment had a
  semicolon; the migration owner removed the separator. The original strict SQL
  splitter was retained. See the first `I1-migration-20261019T132116Z` receipt.
- F2 PG had one failing fixture comparison: the Ent creation result retained
  nanoseconds while PostgreSQL stored microseconds. The owner truncated the
  synthetic input to microseconds and retained strict equality assertions.
  The failed F2 receipt remains, and all 18 cases passed on F3.
- F2 race compilation filled the task runner's 1 GiB `/tmp` tmpfs. F3 uses
  process-scoped `TMPDIR=/output/tmp` and `GOTMPDIR=/output/tmp` on its own
  disk-backed output volume, with `-p=2` and `GOMAXPROCS=2`. It passed without
  changing business code or weakening tests. The failed log remains separate.
- Docker Desktop startup automatically restarted existing containers according
  to their prior restart policies. No existing container was explicitly changed
  or used. All created test resources are identified in the environment manifest.

This is an **I1 preliminary compatibility R1**, with issuance/admission disabled.
It is not activation approval, a production deployment, a live upstream call,
browser acceptance, an application migration rollout, performance acceptance,
or a production-matching final image. The I3 recovery-capable final R1 still has
to be rebuilt, verified and frozen. No real user data or credentials were used.
