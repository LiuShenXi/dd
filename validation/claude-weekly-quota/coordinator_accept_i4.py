"""Record the actual I4 acceptance; do not accept I5/I6 or authorize activation."""
import json
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

root = Path(__file__).resolve().parent
read = lambda name: json.loads((root / name).read_text(encoding='utf-8-sig'))
browser = read('I4-cua-browser-20261019T2303Z/receipt.json')
steps = browser['steps']
for text in ('F4 actual public Key usage', 'F4 real admin C', 'F4 C manual renew75', 'F4 actual UI409', 'F4 T48 actual expiry'):
    assert any(text in s.get('name', s.get('case', '')) and s.get('pass') is True for s in steps), text
data = read('coordinator-acceptance.json')
shared = ['I4-cua-browser-20261019T2303Z/receipt.json', 'I4-browser-independent-evidence-review.md', 'coordinator-I4-review.md']
notes = {
    'T47': 'Actual user Chrome/CUA and two independent real-backend preview gates: late A success and late A error cannot replace B draft, preview or pending state. F2 image used unchanged final F4 frontend assets. Earlier 30-second timeout diagnostics retained; do not use mismatched screenshot names as final-state proof.',
    'T48': 'Actual F4 Chrome at the real database time boundary: while server refresh was held and subsequently timed out, original 13/0/0/13 and queued17 stayed visible. Only a fresh server response after explicit Retry selected17/0/0/17. Both synthetic historical fixture terms are complete168h; no existing card window was shortened or reset.',
    'T49': 'Actual Chrome A/B profiles, A two Claude Keys and GPT Key, CN/EN390px settled layout and modal keyboard focus/Escape checked. Public Key field omission was found, fixed via native selected field plus auth snapshotv28, and rerun on F4; A/A2/GPT-ownerA now override2, B default3. GPT stays5. Earlier projection failure and transition screenshot retained.',
    'T73': 'Actual F4 admin C changed independent Claude default3 to override4 while GPT remained5. Actual A/B and public Key projections plus native PG/Redis cold/L1/L2/v27-to-v28 tests passed. Existing omitted-input/no-change, invalid overrides, original bulk concurrency and policy tests retained. This does not accept I6 pool activation/rollback.',
}
for test in data['tests']:
    if test['id'] in notes:
        test['status'] = 'passed'
        test['evidence'] = list(dict.fromkeys(test.get('evidence', []) + shared))
        test['acceptance_note'] = notes[test['id']]
data['scope'] = 'Latest user direction: finish current I4, commit and push feature branch, then hand off for continued work at office. I5/I6 and production deployment are deferred this round.'
data['current_iteration'] = 'I4 accepted for commit/push. I5 and I6 remain pending; product is not production-ready.'
data['I4_gate_accepted'] = True
data['concurrency_v2_status'] = 'I4 default3/per-user override and independent pools accepted with actual browser and isolated tests. I6 activation/rollback/operational metrics remain pending.'
data['current_round_production_deployment_deferred'] = True
data['historical_acceptance_scope'] = 'Each pass retains its actual iteration, source, image and test layer. F2/F3 complex runtime evidence is explicitly reused only for unchanged F4 paths; no claim of 74 final-release passes.'
data['status_counts'] = dict(Counter(t['status'] for t in data['tests']))
data['frozen_evidence_updated_utc'] = datetime.now(timezone.utc).isoformat()
data['iterations'] = [x for x in data['iterations'] if x['id'] != 'I4'] + [{
    'id': 'I4', 'status': 'accepted', 'receipt': 'coordinator-I4-review.md',
    'source_sha256': '9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c',
    'candidate_image_id': 'sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9',
}]
assert data['production_deployed'] is False and data['production_enabled'] is False
(root / 'coordinator-acceptance.json').write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
(root / 'coordinator-current-status.md').write_text('''# Current delivery status — 2026-10-20 I4 handoff

I0–I4 are accepted at their recorded iteration scopes. I4 is ready for commit and
remote push on `codex/claude-weekly-quota`; the latest user instruction ends this
round here for continuation at the office. I5/I6 and production release remain
pending. This is not a production-readiness approval.

Read coordinator-I4-review.md, coordinator-acceptance.json and
I4-next-machine-handoff.md first. Final F4 source:
`9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`.
Final image:
`sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9`.
The native Key auth projection issue found in the user's Chrome was fixed and
actually rerun; auth snapshots now use version28. Browser acceptance is complete:
real admin card writes, response loss/replay,409 draft preservation, multiple Keys,
independent concurrency, CN/EN narrow display, keyboard focus and actual expiry.
No browser authorization remains pending; standalone Playwright was not launched.

Actual F4 Windows532top/1005all, Linux unit race512top/968all, native PG/Redis1top/5all
and actual HTTP metadata1top/15all passed. Unchanged frontend checks and2440component
tests passed. Complex F2/F3 runtime evidence is explicitly retained for unchanged
paths; see the review for provenance and limits. The74-case matrix now has
62passed/5partial/7not_run with historical iteration scope retained. This does not
mean62final-release requirements passed on one image.

Production is unchanged: no migration, deployment, activation, real-user card,
real payment or model-consumption action was performed. Prior deployment
authorization does not override the latest request to hand off after I4.
Remote push is to the feature branch only; verify its actual SHA in the final
delivery response. The acceptance JSON's push flag is a pre-push snapshot.
''', encoding='utf-8')
(root / 'coordinator-resume-state.md').write_text('''# Resume checkpoint — accepted I4, office continuation

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
''', encoding='utf-8')
print(json.dumps({'I4_accepted': True, 'historically_scoped_counts': data['status_counts'], 'production_deployed': False}))
