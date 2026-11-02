# 最新：2026-10-20 UI 精简已发布（覆盖下方历史状态）

用户明确取消并发整条和卡内请求用量表，已删除并复用现有使用记录。当前 green / sub2api2-green / 29081，.2 / bbb4903c4 / cb20cb0f9c56，蓝槽已自然排空停止。Group7 仍启用，管理员真实开卡、调用与结算已验证，临时 Key 已停用。生产界面、250次健康采样及受影响测试通过。最终回执：`ui-activation-20261020/cleanup-release.md`，教程：`docs/CLAUDE_WEEKLY_CARD_GUIDE.md`。不恢复已取消的界面，不启动子代理，不 push/main merge。

---

# Current state: production UI flow verified 2026-10-20

User explicitly accepts existing UI and requested actual UI opening on the administrator account. No further subagents (user instruction). Actual production browser created group7, entered maintenance, previewed an explicit empty conversion, confirmed, released maintenance, then opened a 1 USD card for user1. Browser created key101; live Haiku call returned200/end_turn and settled0.000822 against term1, remaining0.999178. Browser showed the actual usage row. Temporary key disabled; card retained. Do not issue other customer quotas without their amounts.

Active newblue sub2api2:29080/container778ebf12a698; same app image1ef6ac26/cace0104c. Green stopped/restart=no; R0 archived under sub2api2-r0-retired-20261020 and never restart. Only group7 enabled/protected; issue/admission/recovery true. New group uses the existing20-model whitelist, withoutFable. PG/Redis untouched. Reconciletimer active/already_active. Both future slot configurations retain group7 flags. Controller replacement-slot fix9118cb379 deployed;34controller/24reconciletests passed. Actual second release and544 two-domain health samples all200. Commit docs/evidence includes guide at docs/CLAUDE_WEEKLY_CARD_GUIDE.md and validation/claude-weekly-quota/ui-activation-20261020/.

No frontend/backend application changes this correction, no image rebuild, no push/main merge. Task metadata manually completed; Trellis scripts/developer identity absent. Prior compatible-mode/no-card records below are historical and superseded.

## Historical prior checkpoint

# Current delivery status — 2026-10-20 production released

The user subsequently authorized commit, production deployment and uninterrupted
blue/green setup. All are complete at 03:58:50 UTC. Commits: I5/I6 `63ecd0831`,
acceptance `7a75ac924`, blue/green `cace0104c`, restart recovery `a7e6b6de6`.
No push/main merge. main-server now serves both domains from sub2api2-green:29081,
version 2.8.13-monas-claude-weekly.1, image
sha256:1ef6ac26fd2405baa50ca36d6b6629bd2d19c736e8e85702902220c6aab152c0.
Old R0 naturally drained/stopped, restart=no; PG/Redis never restarted. All307
migration checksums match, all450 public health samples passed, actual admin UI
and two-domain asset/instance checks passed. New background ownership is active;
the verified reconcile timer is enabled with real normal token refresh.

Final-image protocol/rollback/ledger and separate restart/reconcile/refresh
acceptance passed. See production-deployment-20261020.md and bound receipts.
Issue/admission remain off, recovery on; no real group conversion/card issuance.
Business allocations remain a separate decision. Existing password worked; no
reset or secrets stored in Git/memory. Task bookkeeping is manual because
Trellis scripts/developer identity are missing. Historical approval blockers
below are superseded and must not prevent already-authorized release work.

## Historical local acceptance before deployment authorization

The latest instruction resumes the Claude carpool task on
`codex/claude-weekly-quota`. The initial fetch found HEAD/current remote at
`203047edaf09f17fc9e73bf4a3541b5fc190b282`; the clean checkout was switched from
main to that dedicated branch. I5 paid immutable card rights and I6 controlled
conversion, runtime observability, compatibility rollback and actual browser
behavior are implemented. Production is unchanged; this I5/I6 batch is not yet
committed or pushed.

Final local F4 source: `8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`.
R2 image: `sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`.
Compatible R1: `sha256:c8083c2153e7d43598fd36b5c50f54667de05e34d62e72f609ae4110e05f9cee`.
The build receipt/source audit and coordinator-I5-I6-review.md distinguish
F1 runtime, F2 plan-duration correction, F3 signed-resume result/rollback, and
F4 actual admin-route UI. All backend product files and migrations are identical
between F3 and F4. Do not label older executions as F4.

`coordinator-acceptance.json` now consolidates74 functional cases as passed at
their recorded I1–I6 source/image/layer scopes. This is not74 final-image runs or
a production/performance approval. F1 real16m14s stream, actual queue promotion,
Redis/ownership faults, controlled old-pool conversion, mixed replicas and scope
metrics passed. F3 actual R0→R1→R2→R1 plus restart passed. Actual local browser
proved purchase-time quota125.12345678/168h surviving plan999/unlisting,
manual default/override, conversion409 draft retention and separate release,
signed paid result, and F4 routed admin refund guards/detail in CN/EN390px.

Backend broad selected unit895top/1891all and PG193top/402all passed; PG selection
has2top/3total preexisting skips disclosed in its receipt. Later affected paths
were validated by focused runs: metrics20, final I5 PG16top/32all, signed-resume
handler8top/12all, and F4 actual admin view/dialog22. Frontend full327files/2485
passed before late deltas; those received focused tests, lint/typecheck/build.
Counts overlap and must not be added into a unique-test total.

Exact production PostgreSQL18.6 image/layers/binary/runtime version are verified.
The final full retry passed12 top-level tests, zero failures/skips in77.26s,
including all0/10k/100k/300k scales,300289 actual request/ledger rows and910.6MB
of synthetic data. No deadlocks/errors or financial disagreements occurred.
See office-PG18.6-rehearsal-20261020T025846Z.json. The first disk-stop attempt
remains preserved. Final18.6 GPT throughput was342.7/s alone versus276.6/s under
mixed load (19.3% lower), p956.93→11.43ms; Claude Apply p95116.20ms. The older
18.1 run's30% reduction remains separately attributed. These short synthetic
amd64-on-arm64 sweeps are not a production SLO or capacity commitment.

All planned local implementation/validation work is complete. Remaining step:
one batched commit confirmation required by workflow Phase3.4.
No push is included in that confirmation. After approved commits, finish-work
bookkeeping can run; absent Trellis scripts/developer identity must not be
replaced with an unrelated person's journal.

Production rollout remains separate: exact recipient/amount list or explicit
empty allocation, target groups, plan sale policy, maintenance window and
operational budget must be supplied; latest deployment/backup/R1 inventory must
be checked. No real payment, customer key call, card issuance or deployment was
performed. See I6-production-handoff.md and I6-pending-obligations.md.
