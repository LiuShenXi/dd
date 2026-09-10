# Reset Successor Acceptance, 2026-09-08

Accepted locally against synthetic data. No remote deployment or production
data changes were performed for this follow-up.

## Backend

- Unit compilation and `TestCarpoolBaseRemainingPercent` passed.
- PostgreSQL run `20260908T022827Z-90678`: 7 passing top-level reset tests.
  Coverage: distinct successors, zero-delta replay, fixed expiry, coincident
  natural/special boundaries, rollback/retry and cycle number greater than five.
- PostgreSQL run `20260908T023006Z-91064`: 8 passing top-level carry, receipt and
  closure tests. Old receipts spend the original balances once; only remaining
  extra quota carries forward. Consecutive special resets preserve eligible
  carry, while natural and membership expiry prevent its resurrection.
- PostgreSQL run `20260908T023052Z-91186`: 8 passing top-level lifecycle and
  projection regressions, including downtime, renewal, takeover, inactive quota
  and the last short natural period.
- Independent backend review completed with no remaining correctness blockers.

## Frontend and Runtime

The focused view suite passed 23 tests with no skips. Typecheck, lint and the
production build passed. The old annotation-layout tests were removed because
that widget no longer exists.

Chrome/Playwright loaded real authenticated HTTP pages at 1365x1000 and 390x844.
Five existing synthetic scenarios passed: full quota, naturally advanced cycle 2,
partial base quota with additional balances, pending membership and expiry.
The displayed bar ratio matches `quota.remaining_percent`. No filled-to markers,
horizontal overflow or page errors were present after page load and reload.

`results.json` records the HTTP values. `quota-progress.png` shows the simplified
widget; `takeover_membership-390.png` shows the mobile page. These screenshots do
not prove an actual special-reset transaction. The PostgreSQL tests above prove
that behavior.

## Historical Fixture Correction

The first UI acceptance missed that the old `active_reset_marker` fixture still
extended cycle 1 in place (September 2-14), despite the updated reset code. This
was an acceptance gap, subsequently reported by the user.

After backing up the local synthetic database, the fixture was normalized at
its original September 7 reset timestamp: cycle 1 is closed (September 2-7),
cycle 2 is active (September 7-14). Initial/reset/expiry ledger entries reconcile;
available quota stays 550 and membership still expires September 30. The script
refuses any unexpected history, billing receipts or balances. Replay is a no-op.

The revised `verify-http.mjs` now verifies predecessor/successor continuity,
numbering and a seven-day maximum; all six HTTP scenarios passed. Desktop and
390 px browser checks confirm cycle 2, both history rows and no overflow after
reload. See `cycle-history-1365.png`, `cycle-history-390.png` and
`http-fixture-verification.json`. This remains synthetic display evidence.

The first desktop-to-mobile resize assertion ran before responsive layout had
settled. Reloading at each target viewport passed; screenshots were inspected.
A separate resize-only check subsequently confirmed that the layout settles
without horizontal overflow at both widths before any reload.

## Source Binding

- Base Git commit: `35b42bb2b217f87059111bb5ef026c6beb93f192` plus the current
  uncommitted successor/percentage changes.
- Embedded frontend SHA-256:
  `503ad7466090684d0139209b7704851fb6bebe446ae82504deaf8a3355f73147`
- Application binary SHA-256:
  `25e28c57d22d01a08c0a726bf7553794fcfa81d88de6951a045035aedab94f21`
- Runtime image:
  `sha256:c57c3e4198c27a0657607445502fdf9be193b0ec659ae820c382a68560e52b57`
- Local preview: <http://127.0.0.1:38100/carpool>. Running with zero restarts.
- Migration 242 applied to the isolated synthetic database during startup.
- Pre-upgrade synthetic database dump and credentials remain in ignored
  `.runtime/`; none are included in these artifacts.
