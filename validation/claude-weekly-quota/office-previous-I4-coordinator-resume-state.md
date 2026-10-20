# Resume checkpoint — accepted I4, office continuation

Latest user direction: finish current I4, commit/push remote, then stop for
continuation at the office. Do not start I5/I6 or deploy as part of this round.
The final delivery response identifies the actual remote HEAD.

Repository: https://github.com/LiuShenXi/dd.git, branch codex/claude-weekly-quota.
Local isolated worktree: C:/Users/Administrator/.codex/worktrees/claude-weekly-quota/dd.
Production baseline: 8d869d41f40d6eedbe2ee05aaf8973145cccbe4d.
Accepted I3 commit: 9005acffad8f9b30975ea3dd27e82095eb9e5520.
Never use the old dirty C:/WORK-SPACE/dd as the baseline or overwrite reference worktrees.

Read architecture README.md and sol-handoff.md, coordinator-current-status.md,
coordinator-I4-review.md, coordinator-acceptance.json, I4-next-machine-handoff.md,
then implement.md I5/I6, test-plan.md and traceability.md. The v2 concurrency
supplement remains mandatory: Claude default3, independent per-user override/pool,
GPT settings unchanged. Use gpt-6.1-sol xhigh development workers as requested;
coordinator handles scheduling/acceptance. Backend, frontend/review and isolated
environment were separate worker responsibilities; main developer owns commits.

F4 source9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c;
image4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9.
Two auth-projection business files changed after F2/F3 runtime tests: native auth
select includes the Claude override; old snapshots are invalidated usingv28.
F4 regression/race/native PG/Redis/HTTP/browser gates passed. Source manifests,
failed runs and diagnostic receipts remain recoverable via the evidence archive
index. Never relabel older complex runtime tests as F4 executions.

Browser used user Chrome/CUA. Standalone script was never executed. Synthetic C
retains current100/queued75,2terms/2operations,Claude4/GPT5 and zero consumption.
Real response drop caused automatic transport replay; separate commit+proxy503
enabled manual same-intent retry. Synthetic409 was before upstream. D retains
historical13 then17, both168h: held refresh/timeout preserved old amounts and a
fresh actual Retry selected17. No fixture finances were reset to fabricate passes.
Credentials are outside git in LocalAppData/Codex/PrivateTests. Do not publish or
migrate them; rebuild fresh isolated fixtures at the office.

Next implementation: I5 paid plans/order immutable snapshots, frozen604800s and
exact8 quota, fulfillment/replay/refund/lease-version safeguards and recovery.
I6 remains controlled conversion, observability, pool activation/rollback,
PG18.6 production-scale migration rehearsal, performance and final latest-DB
R0→R1→R2→R1 rehearsal. Preparation files are not completion evidence.

Production is unchanged. Before any later release recheck main-server image,
revision/program digest/migrations. Last verified deployment is single Compose
/opt/sub2api2/docker-compose.migrated.yml with Nginx to29080; do not blindly use
historical blue-green commands. Read coordinator-release-preparation.md and retain
latest DB on rollback. No production card recipients, conversion quota list or
paid probe identity were supplied; do not invent them.
