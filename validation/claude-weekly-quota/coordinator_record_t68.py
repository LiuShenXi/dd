"""Record only independently reviewed final T68 evidence; do not accept I4."""
import json
import re
from collections import Counter
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path

HERE = Path(__file__).resolve().parent
PREFIX = 'I4-v2-T68-F3-20261019T181135Z'


def read(name):
    return json.loads((HERE / name).read_text(encoding='utf-8-sig'))


receipt = read(PREFIX + '-receipt.json')
observed = read(PREFIX + '-observed.json')
own_pg = read('coordinator-I4-T68-postgres-observed.json')
bridge = read('coordinator-I4-F2-F3-test-only-bridge.json')
assert receipt['acceptance'] and receipt['driver_source_stable']
assert receipt['exit_code'] == receipt['failure_count'] == receipt['skip_count'] == 0
assert receipt['candidate_image_id'] == bridge['candidate_image_id'] == own_pg['image_id']
assert receipt['candidate_source_sha256'] == bridge['image_source_sha256']
assert receipt['driver_source_sha256'] == bridge['driver_source_sha256']
assert bridge['approved'] and own_pg['accepted']
events = [json.loads(line) for line in (HERE / (PREFIX + '.jsonl')).read_text(encoding='utf-8-sig').splitlines()]
assert not any(e['Action'] in ('fail', 'skip') for e in events)
passed = [e for e in events if e['Action'] == 'pass' and e.get('Test')]
assert len(passed) == 1 and passed[0]['Elapsed'] > 960
samples = [e['Output'] for e in events if 'T68 actual HTTP SSE elapsed=' in e.get('Output', '')]
assert len(samples) == 62
assert all('active=2 waiting=0 original_UUIDs=true' in s and 'no_debit_before_known=true' in s for s in samples)
completion = [e['Output'] for e in events if 'T68 actual long-stream PASS' in e.get('Output', '')]
assert len(completion) == 1
match = re.search(r'elapsed=(\d+)m([\d.]+)s checks=(\d+)', completion[0])
seconds = Decimal(match[1]) * 60 + Decimal(match[2])
assert seconds > 960 and int(match[3]) == len(samples)
assert Decimal(str(observed['actual_stream_elapsed_seconds'])) == seconds
rows = own_pg['requests']
assert len(rows) == 2 and len({r['api_key_id'] for r in rows}) == 2
assert {r['term_id'] for r in rows} == {1}
assert all(r['status'] == 'settled' and r['ledger_rows'] == r['dedup_rows'] == 1 and r['log_acked'] for r in rows)
assert sum(Decimal(r['actual_cost_usd']) for r in rows) == Decimal('0.00120000')
now = datetime.now(timezone.utc).isoformat()
review = {
    'accepted_T68_only': True, 'reviewed_utc': now,
    'actual_stream_seconds': str(seconds), 'lease_samples': len(samples),
    'candidate_image_id': receipt['candidate_image_id'],
    'source_compatibility': 'approved F2 image with F3 test-only bridge',
    'independent_postgres_observation': 'coordinator-I4-T68-postgres-observed.json',
    'exact_original_card_debit': '0.00120000',
    'other_wallet_invariants': 'Executed original test full-row guards; separate SELECT proves final original-card requests only',
    'I4_gate_accepted': False, 'browser_executed': False,
    'production_deployed': False, 'remote_branch_pushed': False,
}
(HERE / 'coordinator-I4-T68-review.json').write_text(json.dumps(review, indent=2) + '\n', encoding='utf-8')
target = HERE / 'coordinator-acceptance.json'
data = read(target.name)
for test in data['tests']:
    if test['id'] == 'T68':
        test['status'] = 'passed'
        test['evidence'] = list(dict.fromkeys(test.get('evidence', []) + [PREFIX + '-receipt.json', PREFIX + '-observed.json', 'coordinator-I4-T68-postgres-observed.json', 'coordinator-I4-T68-review.json', 'coordinator-I4-F2-F3-test-only-bridge.json']))
        test['acceptance_note'] = 'Final F2 application with approved F3 test-only bridge: actual SSE973.0817914s, 62 original-UUID renewals, no third upstream/admission after15min; two original-term settlements each0.00060000 with one ledger/dedup and log acknowledgment, independently re-read by coordinator. Full other-wallet guards passed. Historical older-image T68 evidence remains diagnostic.'
    if test['id'] == 'T74':
        test['evidence'] = list(dict.fromkeys(test['evidence'] + ['I4-WS-runtime-F3-invocation.json', 'I4-WS-runtime-F3-postgres-observed.json']))
data['current_iteration'] = 'I4 automated runtime gates complete; real-browser gate pending. I5/I6 pending.'
data['concurrency_v2_status'] = 'I4 default3/per-user policy and isolated pools implemented and automatically tested; browser and I6 activation/metrics gates pending'
data['status_counts'] = dict(Counter(t['status'] for t in data['tests']))
data['frozen_evidence_updated_utc'] = now
assert data['I4_gate_accepted'] is False and data['production_deployed'] is False and data['remote_branch_pushed'] is False
target.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'T68_accepted': True, 'stream_seconds': str(seconds), 'counts_with_historical_scope': data['status_counts'], 'I4_accepted': False}))
