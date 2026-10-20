# Resume checkpoint — production released

Current request is complete: commits and main-server deployment authorized by
the user, completed 2026-10-20 03:58:50 UTC. Read coordinator-current-status.md
and production-deployment-20261020.md. No commit approval remains pending.
No push/main merge was performed. App commit cace0104c, version
2.8.13-monas-claude-weekly.1, active sub2api2-green:29081; old sub2api2 is stopped
with restart=no. Never restart R0. Private app-only Compose/controller state is
under /opt/sub2api2/blue-green. Original Compose now contains only PG/Redis;
their containers were never restarted. Background reconcile timer enabled.
Normal private token-pair refresh verified; password unchanged. Unknown refresh
outcomes require normal login, not replay or token fabrication.

Final-image runtime/restart proof passed; fixtures backed up and removed,
images/native drivers/evidence retained. Business term conversion is separate:
issue/admission=false, recovery=true, term groups/cards/rollouts=0. Obtain explicit
targets/quotas/sale policy before that action. Do not infer quotas from balances.
Trellis scripts/developer identity are missing; no unrelated journal is used.

## Historical checkpoint before deployment authorization (superseded)

Current checkout: `/Users/shenxi/Desktop/WORK-SPACE/dd`.
Branch: `codex/claude-weekly-quota`, baseline HEAD/origin
`203047edaf09f17fc9e73bf4a3541b5fc190b282`.
Latest user scope: pull latest dedicated Claude carpool branch and finish the
remaining work. Earlier I4 stop/commit/push notes are historical, archived as
`office-previous-I4-coordinator-resume-state.md` and must not override this request.

Read coordinator-current-status.md, coordinator-I5-I6-review.md,
coordinator-acceptance.json, office-I6-final-source-audit.json,
office-I6-browser-receipt.json, I6-PG18.6-rehearsal-receipt.md,
I6-production-handoff.md and I6-pending-obligations.md.

Implementation and actual browser verification complete on F4:
source `8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`;
R2 `sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`;
R1 `sha256:c8083c2153e7d43598fd36b5c50f54667de05e34d62e72f609ae4110e05f9cee`.
Backend identical to F3; F4 fixes actual AdminOrdersView route, guards legacy
refund actions and displays frozen rights/sale price. Final native test driver
for PG is private `office/repository-promotion-final3.test`, SHA
`c228980169044eff0a381975577a2e717bd541cf197fb1baa84241e6cf3b8969`.
F1 16m SSE/promotion/metrics completed; do not restart or relabel those runs.
F2/F3 actual rollback and F3/F4 CUA are complete with per-image evidence.

Final PG18.6 retry passed12top/0fail/0skip, all0/10k/100k/300k scales and
300289 actual rows, on the exact verified production-version image. Receipt:
office-PG18.6-rehearsal-20261020T025846Z.json; raw first failure is preserved.
No WAL or scale workaround was used. The final18.6 mixed-burst GPT throughput
fell19.3%; the older18.1 run's30% is separate, not a before/after improvement
claim. Representative production data/hardware/window and operational budget
remain deployment inputs.

Outstanding local step: reviewed batched commit confirmation. See
`office-I5-I6-commit-plan.md` for every path and two proposed messages;
`office-I5-I6-final-verification.json` records final source/diff/secret checks.

Synthetic runtime/browser fixtures were saved privately and removed after
acceptance. Inspect their cleanup receipts rather than assuming URLs, containers
or old F1/F2/F3 images still exist. Secrets, synthetic DB dumps and raw logs are
under `/Users/shenxi/.codex/private/claude-weekly-quota/` and must never be added
to Git. Build/source hashes and failure attempts remain public evidence.

Do not stage/commit/push before the workflow's one-shot reviewed commit plan.
No journal developer exists and Trellis CLI/scripts are absent; do not write
under another person's identity. Preserve all real user/production data.

Recorded production is main-server single Compose projectsub2api2/service
sub2api/container sub2api2, config/opt/sub2api2/docker-compose.migrated.yml,
Nginx→127.0.0.1:29080. R0 `.13` provenance is verified. No production changes
were made. Release needs explicit business allocations, maintenance budget,
current inventory/backup and authorized probe identity; historical BWH blue/green
commands and old authorization do not fill missing decisions.
