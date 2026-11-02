"""Attach completed I4 receipts while preserving unfinished browser/I5/I6 gates."""
import json
from datetime import datetime, timezone
from pathlib import Path

HERE=Path(__file__).resolve().parent
target=HERE/'coordinator-acceptance.json'
data=json.loads(target.read_text(encoding='utf-8-sig'))
windows='I4-windows-F2-20261019T175936Z-receipt.json'
redis='I4-v2-redis-policy-F2-20261019T180011Z-receipt.json'
matrix='I4-pg-F2-20261019T180032Z-receipt.json'
pools='I4-v2-ConcurrentIndependentPools-F2-20261019T180404Z-receipt.json'
boundary='I4-v2-CrossCardLeaseBoundary-F2-20261019T180722Z-receipt.json'
metadata='I4-v2-MetadataAndUnsupportedRoutes-F2-20261019T180755Z-receipt.json'
fault='I4-v2-RedisFaultCancelsActualUpstream-F2-20261019T180809Z-receipt.json'
fault_observed='I4-v2-RedisFault-observed-20261019T180956Z.json'
race='I4-unit-race-F3-20261019T181120Z-receipt.json'
bridge='coordinator-I4-F2-F3-test-only-bridge.json'
for name in [windows,redis,matrix,pools,boundary,metadata,fault,race]:
    receipt=json.loads((HERE/name).read_text(encoding='utf-8-sig'))
    if name == windows:
        assert receipt['pass'] is True and receipt['source_stable'] is True, name
        assert all(command['exit_code'] == 0 for command in receipt['commands']), name
    else:
        assert receipt['acceptance'] is True, name
entries={
 'T45':('passed',[matrix], 'Current F2 image: six aliases x JSON/SSE x two users, two A Keys and interleaved synthetic GPT; exact once-only request/ledger/dedup/log and opposite-wallet assertions.'),
 'T46':('passed',[windows,metadata], 'Registered route negatives and current-image metadata/unsupported-entry tests passed. Normal auth last-use metadata is not a charge; the original full-row failure and explicit warmup diagnosis remain recorded.'),
 'T63':('passed',[redis,pools], 'Two real application instances and Redis prove same-user GPT5 plus Claude2 concurrently without a shared user cap; these are synthetic test policy values.'),
 'T64':('passed',[redis,pools], 'Actual GPT active5/waiting20 full while Claude admits independently; overflow and ownership checked.'),
 'T65':('passed',[redis,pools], 'Actual Claude active2/waiting20 full while a freed GPT slot admits its replacement; counts remain separate.'),
 'T66':('passed',[redis,pools,boundary], 'Two Keys and two Claude term groups share one user pool. Actual leases survive card expiry; late cost stays on original card and next card remains untouched until a new admission.'),
 'T67':('passed',[redis,pools], 'Real Redis promotion, cancellation, timeout, repeated/late cleanup and actual full queues preserve ticket ownership and avoid resurrection.'),
 'T69':('passed',[windows,'I4-v2-gateway-T69-final-receipt.json',redis,fault,race,bridge], 'Combined explicit layers: actual local Router panic/cancel/pre-send account handoff with synthetic ports, real Redis cleanup/renewal, and actual-image upstream cancellation. This is not a claim of one real-PG handler-panic test.'),
 'T70':('passed',[redis,pools], 'Two real application instances plus independent Redis clients enforce shared active/waiting capacities.'),
 'T71':('passed',[redis,fault,fault_observed,race,bridge], 'Actual Redis failure cancels provider before lease expiry; independently observed cancelled=true, SQL NULL actual cost, reconciliation state, zero debit/dedup/used and unchanged legacy keys.'),
 'T73':('partial',[windows,redis,metadata,race,bridge], 'Independent default3/override2, actual auth/me/profile/admin projections, original bulk updates and cache/policy tests passed. Real browser editing/display remains required.'),
 'T74':('passed',[windows,matrix,pools,'I4-WS-runtime-F3-proof.json',race,bridge], 'Current image aliases and interleaved GPT HTTP/WS HTTP-bridge passed; legacy WS/live lease and Kiro normalization regressions passed. Actual Linux live route refuses with zero provider calls; no external live-provider success is claimed.'),
}
for row in data['tests']:
    if row['id'] not in entries:
        continue
    status,names,note=entries[row['id']]
    for name in names:
        assert (HERE/name).is_file(), name
    row['status']=status
    row['evidence']=list(dict.fromkeys(row.get('evidence',[])+names+['I4-readonly-evidence-gap-review.md']))
    row['acceptance_note']=note
data['current_iteration']='I4 awaiting completed final long-stream and real browser gates; I5/I6 pending'
data['production_deployed']=False
data['production_enabled']=False
data['remote_branch_pushed']=False
data['I4_gate_accepted']=False
data['frozen_evidence_updated_utc']=datetime.now(timezone.utc).isoformat()
data['status_counts']={status:sum(row['status']==status for row in data['tests']) for status in sorted({row['status'] for row in data['tests']})}
target.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print(json.dumps({'updated':len(entries),'status_counts':data['status_counts'],'I4_gate_accepted':False,'production_deployed':False}))
