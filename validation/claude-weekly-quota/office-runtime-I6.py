"""Run one explicit isolated final-image acceptance step and save its receipt."""
import argparse
import hashlib
import json
import os
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
VALIDATION = ROOT / 'validation/claude-weekly-quota'
PRIVATE = Path.home() / '.codex/private/claude-weekly-quota/office-runtime-20261020-v2'
STEPS = {
    'prepare': ['TestSubscriptionQuotaI4PrepareEnvironment'],
    'second': ['TestSubscriptionQuotaI4V2PrepareSecondInstance'],
    'mixed': ['TestSubscriptionQuotaI6RuntimeRejectsMixedReplica'],
    'wallets': ['TestSubscriptionQuotaI4HTTPWalletMatrix'],
    'metadata': ['TestSubscriptionQuotaI4MetadataAndUnsupportedRoutes'],
    'pools': ['TestSubscriptionQuotaI4V2ConcurrentIndependentPools'],
    'conversion': ['TestSubscriptionQuotaI6RuntimeControlledConversion'],
    'boundary': ['TestSubscriptionQuotaI4V2CrossCardLeaseBoundary'],
    'redis-fault': ['TestSubscriptionQuotaI4V2RedisFaultCancelsActualUpstream'],
    'lost-lease': ['TestSubscriptionQuotaI6RuntimeLostLeaseCancelsUpstream'],
    'long-sse': ['TestSubscriptionQuotaI4V2LongSSELease'],
    'promotion': ['TestSubscriptionQuotaI6RuntimeClaudeQueuePromotion'],
    'metrics': ['TestSubscriptionQuotaI6RuntimeScopeMetricEvidence'],
    'rollback': ['TestSubscriptionQuotaI6ImageUpgradeAndRollback'],
}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('step', choices=[*STEPS, 'ws'])
    args = parser.parse_args()
    build = json.loads((VALIDATION / 'office-I6-build-receipt.json').read_text())
    assert build['status'] == 'built_source_stable_pending_runtime_acceptance'
    PRIVATE.mkdir(mode=0o700, exist_ok=True)
    env = os.environ | {
        'CI': 'true', 'GOMAXPROCS': '3', 'CWQ_RUNTIME_STAGE': 'I6',
        'CWQ_I4_PRIVATE_DIR': str(PRIVATE),
        'CWQ_LOOPBACK_BRIDGE': str(PRIVATE.parent / 'office/loopback-bridge'),
        'CWQ_I4_IMAGE': build['r2_image'], 'CWQ_I4_SOURCE': build['source_sha256'],
        'CWQ_I4_PROVIDER_IMAGE': build['provider_tag'],
        'CWQ_I4_NETWORK': 'cwq-env-i6-office-20261020',
        'CWQ_I4_PUBLIC_MANIFEST': str(VALIDATION / 'office-I6-environment.json'),
        'CWQ_I4_LONG_STREAM_MINUTES': '16', 'CWQ_I4_BOUNDARY_SECONDS': '25',
        'CWQ_I6_METRICS_RECEIPT': str(VALIDATION / 'office-I6-metrics.json'),
        'CWQ_I6_R1_IMAGE': build['r1_image'], 'CWQ_I6_R2_IMAGE': build['r2_image'],
        'CWQ_I6_SOURCE_SHA256': build['source_sha256'], 'CWQ_I6_PROVIDER_IMAGE': build['provider_tag'],
    }
    binary = PRIVATE.parent / 'office/repository-final.test'
    if args.step == 'promotion':
        binary = PRIVATE.parent / 'office/repository-promotion-final3.test'
    command = [str(binary), '-test.v', '-test.parallel=1', '-test.timeout=23m', '-test.run=^('+'|'.join(STEPS.get(args.step, []))+')$']
    if args.step == 'ws':
        command = [str(PRIVATE.parent / 'office/ws-proof'), str(PRIVATE / 'synthetic-fixture.json'), str(VALIDATION / 'office-I6-ws-proof.json')]
    driver = Path(command[0])
    driver_digest = hashlib.sha256(driver.read_bytes()).hexdigest()
    started = datetime.now(timezone.utc).isoformat()
    log = PRIVATE / (args.step + '.log')
    with log.open('w') as handle:
        result = subprocess.run(command, cwd=ROOT / 'backend/internal/repository', env=env, stdout=handle, stderr=subprocess.STDOUT, timeout=1400)
    raw = log.read_bytes(); contents = raw.decode(errors='replace')
    receipt = {
        'step': args.step, 'started_utc': started,
        'finished_utc': datetime.now(timezone.utc).isoformat(), 'exit_code': result.returncode,
        'tests': STEPS.get(args.step, ['standalone actual WS/Live route proof']),
        'top_level_pass': len(re.findall(r'^--- PASS:', contents, re.M)),
        'all_pass': len(re.findall(r'^\s*--- PASS:', contents, re.M)),
        'fail': len(re.findall(r'^\s*--- FAIL:', contents, re.M)),
        'skip': len(re.findall(r'^\s*--- SKIP:', contents, re.M)),
        'image_id': build['r2_image_id'], 'source_sha256': build['source_sha256'],
        'driver_sha256': driver_digest,
        'driver_stable': hashlib.sha256(driver.read_bytes()).hexdigest() == driver_digest,
        'runner_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'raw_log_sha256': hashlib.sha256(raw).hexdigest(), 'private_log': str(log),
        'synthetic_only': True, 'production_modified': False,
    }
    receipt['pass'] = result.returncode == 0 and receipt['driver_stable'] and not receipt['skip'] and (args.step == 'ws' or receipt['top_level_pass'] == len(receipt['tests']))
    (VALIDATION / ('office-I6-' + args.step + '-receipt.json')).write_text(json.dumps(receipt, indent=2)+'\n')
    print(json.dumps(receipt), flush=True)
    raise SystemExit(0 if receipt['pass'] else 1)

if __name__ == '__main__':
    main()
