# Rolling reset local acceptance

Accepted locally on 2026-09-07 against synthetic data only. No production
service, real user, or existing `carpool-real-*` resource was used.

## Source and runtime binding

- Git HEAD: `91a9a48b10ef7345d960094f6c2d8b2209ad6d3e`
- Backend/frontend worktree SHA-256: `2c4039cdcf3d2f94a12414ade95d25c5239443d2b43e6a230333c0b8a2f2698d`
  - Computed from the sorted per-file SHA-256 output for Git-tracked and
    non-ignored untracked files under `backend`, `frontend`, `go.mod`, and
    `go.sum`.
- Embedded frontend `index.html` SHA-256:
  `6bafc11dc61a49832d840c5093e1883856e1a1cc69d8d73888e21bd16df4cea6`
- Compiled application binary SHA-256:
  `8e5db506c9ef71e53dea16e73a563223c555a62f6d6a6a275d2acdd856fc4d62`
- Running application image ID:
  `sha256:ad2a4214802dc2b88161ddd80182724b9045220a0f8fdb23ac0c0ceb19ba4f45`
- Local URL: <http://127.0.0.1:38100>

At acceptance time, the application, PostgreSQL 18, Redis 8, and nginx ingress
containers were running. Only nginx published a host port. PostgreSQL and Redis
had no host port.

## Database integration

The current repository was compiled in `golang:1.27.0-alpine`; host Go 1.24.3
was not used for backend compilation or testing.

- Run `20260907T093726Z-63154`: 11 top-level tests passed against fresh
  PostgreSQL 18 and Redis 8 resources.
- Run `20260907T094833Z-65125`: 8 legacy/regression top-level tests passed
  against fresh PostgreSQL 18 and Redis 8 resources.
- Total: 19 top-level integration cases passed.

Coverage includes consecutive special resets, zero-increment replay, the day-27
expiry cap, the natural/special reset race, cooldown correction, transactional
rollback, unresolved receipts, downtime catch-up, the final short period,
legacy fixed snapshots, renewal, and concurrent boost claims. Disposable test
containers, networks, and volumes were removed after each run.

## Frontend checks

The frozen-lockfile install, typecheck, lint, focused tests, and production build
passed. The focused frontend suites reported 77 passing tests. The built page was
then embedded at `backend/internal/web/dist/index.html` before the application
image was built.

## HTTP acceptance

Six scenarios were created through the real local admin APIs: active after a
special reset, near a natural reset, pending membership, near expiry without a
further refill, expired membership, and takeover membership.

Fresh admin user reads and authenticated user carpool-detail reads verified:

- preview/open agreement and fixed 28-day membership expiry;
- renewal beginning at the current fixed expiry;
- takeover deadline handling and HTTP 400 for an invalid deadline;
- a successful reset moving the natural refill deadline without moving expiry;
- ordinary balance remaining independent from carpool quota.

Machine-readable results are in `runtime-fixtures.json`.

## Browser acceptance

Authenticated desktop and 390 px mobile views show the active synthetic user
with a fixed expiry of 2026-09-30 17:49, a shifted next natural refill at
2026-09-14 17:49, one successful `已加满 $550` reset annotation, and a separate
`$550.00` carpool balance. Refreshing preserved the same state. At the 390 px
viewport the document width was 382 px, with no horizontal overflow; the rail,
labels, and primary content had no visible overlap or clipping.

The authenticated admin overview shows the active, pending, near-expiry,
expired, takeover, and renewed pending terms. The near-expiry and expired rows
show `到期前无` for the next natural refill. The preview modal shows the
server-computed 28-day term, first seven-day period, next natural refill,
open action, and the historical-takeover entry point.

The admin UI also opened a new synthetic Two-seat term without recording a
payment. The resulting term has one current period, $1,100 carpool quota, a
2026-09-14 natural refill, a fixed 2026-10-05 expiry, zero net payment, and the
user's ordinary balance remained 19. Its renewal preview showed the next fixed
term from 2026-10-05 through 2026-11-02, a first refill on 2026-10-12, and one
scheduled period row. That renewal was intentionally not submitted again; the
already submitted renewal is verified by the HTTP evidence above.

- `user-desktop.png`
- `user-mobile.png`
- `user-mobile-detail.png`
- `admin-overview.png`
- `admin-preview.png`
- `admin-open-success.png`
- `admin-renewal.png`

The active authenticated user detail page was left open for inspection.

## Evidence boundary

`zero-reset-fixture.txt` records an explicitly synthetic zero-increment reset
marker used to exercise API and UI projection. It does not prove qualification,
scheduling, concurrency, cooldown, transactionality, or replay. Those behaviors
are proven by the PostgreSQL integration runs above.

Private synthetic credentials remain only in the ignored `.runtime` directory
and are not included in this report or any screenshot.
