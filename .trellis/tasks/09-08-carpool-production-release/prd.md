# Approved Production Release

Business authority: `openspec/changes/add-carpool-v1-3/production-release-2026-09-08.md` and the current task conversation.

- Import exactly the approved 20 existing users. Eighteen terms last 28 days from original registration; two explicitly approved terms last 7 days. Preserve timestamp precision.
- Current period starts at 2026-09-08T09:40:16+08:00, ends seven days later or at individual expiry.
- Special four-seat quota is 450, special two-seat quota is 1000. Standard quotas 550/700/1100. Preserve special quota on default renewal.
- Snapshot actual remaining balance at takeover, never update ordinary balances, refill, truncate remaining quota, or fabricate historical consumption. Boost usage is zero; do not auto-claim boosts.
- Preserve administrator ordinary billing and both original keys, including group association. Exclude three confirmed unactivated zero-balance users and deleted users.
- Keep the original standard group and every key association unchanged. Persist a per-user carpool billing binding for the 20 members, retain it after expiry, and prevent members reusing the legacy balance. The administrator has no binding and keeps ordinary billing.
- Preserve URLs, keys, sessions and running streams. Quiesce new admissions for the shortest bounded interval needed for an atomic takeover. Never kill a stream merely because a drain timeout elapsed.
- Pass isolated real-PG, HTTP/WS, frontend and migration/rollback acceptance before production traffic/accounting cutover.

No implementation of the separate prompt-policy proposal. No unrelated cleanup or credential printing. No production actions by workers.
