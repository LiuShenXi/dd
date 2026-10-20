# Office continuation — 2026-10-20

The user resumed the remaining Claude carpool work after the I4 handoff. The
current checkout was clean, fetched from origin, switched to the tracking branch
`codex/claude-weekly-quota`, and fast-forward checked at
`203047eda` (already current). The historical stop-after-I4 instruction applies
to the previous session; the current request resumes I5 and the I6 preparation
and verification work. Production recipients, quotas and a paid probe identity
have not been supplied and must not be invented.

I5 changes belong to the existing plan configuration and payment creation /
fulfillment boundaries: immutable exact-decimal entitlements, shared term
creation, leased transaction and durable replay. The frontend adds plan defaults,
purchase and frozen-order display, and manual defaults with explicit override.
Tests use fresh synthetic PostgreSQL / Redis and a controllable payment provider.
Existing GPT accounting and payment behavior remain required regression gates.

The root coordinator owns local environment preparation, integration and later
I6 work. The backend, frontend and integration workers have distinct file scopes.
No commit, push or production modification has occurred in this continuation.

## Environment and live baseline

- macOS arm64; `backend` automatically selects Go 1.27.0. The system default Go
  is 1.24.3, so execute Go commands in `backend`.
- Docker Desktop 4.41.2 / Engine 28.1.1 started successfully. Node 24 and pnpm
  10.32.1 are available; the repository lockfile is retained.
- Trellis CLI and `.trellis/scripts/get_context.py` are absent. Existing task
  contracts and specs are read directly; no Trellis reinstall is needed.
- Read-only SSH check confirms production `sub2api:2.8.13-monas-bps.13`, revision
  `8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`, image
  `sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677`.
- Program SHA-256 remains
  `85d991720e15993813fdfdddc408852fefe90c54314e760d68765cf5ba0767cd`;
  started at `2026-10-18T15:19:07.917058186Z`, restart count 5,
  `RELEASE_DRAIN_START_HELD=false`, `CARPOOL_BACKGROUND_ENABLED=true`.
- Production PostgreSQL is 18.6, image digest
  `sha256:6c538e7206ea40ff740ef27883529390a690b6ead6ba96b44c67a9f7c638e8fd`.

This note records verified setup and scope, not I5/I6 acceptance.

## Implementation and functional acceptance closeout

I5/I6 implementation and actual browser acceptance are complete. The final F4
source/build is recorded in `office-I6-build-F4.json`; the source audit
separates each later fix from unchanged earlier runtime paths. The 74-case
functional matrix retains original I1–I6 identities. The final current/resume
files supersede the previous Windows I4 stop note, which remains archived.

Actual isolated evidence covers immutable purchased rights, shared manual
issuance, retry/lease/fault boundaries, controlled conversion/drain and cache
invalidation, default3 promotion,16m14s renewal, Redis/ownership faults, GPT
HTTP/WS preservation, and R0→R1→R2→R1 recovery. Actual browser discovery also
fixed canonical days validation, signed payment-result entitlement projection,
and the real admin-orders route's frozen detail/refund guards. CN/EN390px and
keyboard checks passed at the recorded image scopes.

The synthetic main/browser databases were backed up privately and verified
with pg_restore --list before exact task containers/volume/network removal.
Old F1/F2/F3 build images and rebuildable copies/cache were reclaimed with
per-artifact provenance; final F4/R1 and verified R0 remain. The test URL is no
longer serving. No source files, unrelated July Docker volumes, unknown stopped
containers, module sources or toolchain installations were removed.

Commit review is prepared in `office-I5-I6-commit-plan.md`; current task remains
uncommitted pending Phase3.4 confirmation. Production is unchanged. The final
PG18.6 synthetic capacity outcome belongs in its separate receipt; even a pass
does not replace representative production data/hardware, allocations or the
operator's maintenance/performance budget.

Final PG18.6 retry completed at2026-10-20T03:00:03Z:12top/0fail/0skip,
all four history scales through300k,300289 actual rows, with matching actual
program/version. Exact ledger correctness and no errors/deadlocks passed;
GPT mixed throughput was19.3% below its same-run control. The original disk-stop
receipt and older18.1 performance remain distinct. Final testcontainers were
removed by their exact session; free space after cleanup was about5.96GiB.
