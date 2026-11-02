"""Record performed diagnostic work without marking the I4 gate accepted."""
import json
from datetime import datetime, timezone
from pathlib import Path

root = Path(__file__).resolve().parent
target = root / 'coordinator-acceptance.json'
data = json.loads(target.read_text(encoding='utf-8-sig'))
common = ['coordinator-I4-review.md', 'coordinator-v2-concurrency-review.md']
ui = 'I4-ui-20261019T174120Z-receipt.json'
redis = 'I4-v2-redis-policy-debug-20261019T173048Z-receipt.json'
entries = {
    'T45': (['I4-pg-debug-20261019T172029Z-receipt.json'], 'Actual candidate alias/wallet matrix passed diagnostically; final frozen candidate is pending.'),
    'T47': ([ui], 'Component identity/late-response checks passed; actual browser verification remains pending.'),
    'T48': ([ui], 'Component countdown/guarded refresh checks passed; actual final-image time boundary and browser checks remain pending.'),
    'T49': ([ui, 'I4-pg-debug-20261019T172029Z-receipt.json'], 'Frontend checks and synthetic multi-Key wallet diagnostics ran. No actual browser layout/language/keyboard acceptance yet.'),
    'T63': ([redis], 'Real Redis dual-client 5+2 diagnostic passed; full actual application matrix and final freeze remain pending.'),
    'T66': (['I4-v2-T68-debug-20261019T172448Z-observed.json'], 'Actual two-Key shared leases observed; final multi-group/term-boundary matrix remains pending.'),
    'T67': ([redis], 'Real Redis waiting/cancellation/timeout/promotion diagnostic passed; final frozen integration is pending.'),
    'T68': (['I4-v2-T68-debug-20261019T172448Z-observed.json'], 'Actual 972.5537202-second stream passed on its recorded candidate; critical files later changed, so final image duration test will be repeated.'),
    'T69': (['I4-v2-gateway-T69-final-receipt.json'], 'Actual local Router/handlers prove panic/cancel/pre-send account handoff using synthetic Redis boundaries; real Redis and final candidate coverage remain distinct.'),
    'T70': ([redis, 'I4-v2-PrepareSecondInstance-debug-20261019T172849Z-receipt.json'], 'Real Redis dual-client atomic tests and second running app preparation are evidence of partial work, not the completed two-app capacity test.'),
    'T71': ([redis, 'I4-v2-tcp-blackhole-fixed.log'], 'Real closed-client lease failure and bounded TCP-blackhole tests ran; final actual-router fault/usage evidence is pending.'),
    'T73': ([ui, 'I4-v2-legacy-bulk-pg-debug-receipt.json'], 'Policy, actual legacy bulk SQL, cache and UI component checks ran; final actual browser and frozen-source evidence are pending.'),
    'T74': (['I4-pg-debug-20261019T172029Z-receipt.json'], 'Recorded candidate covered aliases and interleaved synthetic GPT consumption. Full HTTP/WS/live concurrency acceptance is still pending.'),
}
for item in data['tests']:
    if item['id'] not in entries:
        continue
    evidence, note = entries[item['id']]
    for name in evidence:
        if not (root / name).is_file():
            raise RuntimeError(f'Missing evidence: {name}')
    if item['status'] != 'passed':
        item['status'] = 'partial'
    item['evidence'] = list(dict.fromkeys(item.get('evidence', []) + common + evidence))
    item['acceptance_note'] = note
data['current_iteration'] = 'I4 in progress; final browser/environment gate not accepted'
data['historical_acceptance_scope'] = 'I0-I3 v1 accepted at recorded commits; v2 supplements and I4-I6 are not implicitly accepted by earlier passes'
data['diagnostic_progress_updated_utc'] = datetime.now(timezone.utc).isoformat()
data['production_deployed'] = False
data['production_enabled'] = False
data['remote_branch_pushed'] = False
counts = {}
for item in data['tests']:
    counts[item['status']] = counts.get(item['status'], 0) + 1
data['status_counts'] = counts
target.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'recorded_tests': len(entries), 'status_counts': counts, 'I4_accepted': False, 'production_deployed': False}))
