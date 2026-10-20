#!/usr/bin/env python3
"""Append a real restart/auth-rotation proof to the retained synthetic v4 only."""
import argparse
from datetime import datetime, timedelta, timezone
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sys


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


runtime = load('runtime_proof', Path(__file__).with_name('blue-green-runtime-proof.py'))


def run(private, evidence):
    assert private.resolve().is_relative_to(runtime.PRIVATE_ROOT.resolve())
    assert evidence.resolve().is_relative_to(runtime.PRIVATE_ROOT.resolve())
    assert not evidence.exists(), 'evidence directory must be fresh'
    evidence.mkdir(mode=0o700)
    original_receipt = runtime.OUT / 'office-bluegreen-runtime-20261020T035512Z.json'
    original = json.loads(original_receipt.read_text())
    assert original['pass'] and original['private_evidence'] == str(private)
    original_receipt_hash = runtime.sha(original_receipt)
    for key, filename in [('timeline_sha256', 'timeline.json'), ('controller_state_sha256', 'release-state.json'),
                          ('nginx_access_sha256', 'nginx-access.log'), ('provider_audit_sha256', 'final-provider-audit.json')]:
        assert runtime.sha(private / filename) == original[key], 'original v4 evidence changed'
    snapshot = evidence / 'original-v4-snapshot'
    shutil.copytree(private, snapshot)
    snapshot.chmod(0o700)
    files = []
    for path in snapshot.rglob('*'):
        if path.is_file():
            path.chmod(0o600)
            files.append({'path': str(path.relative_to(snapshot)), 'bytes': path.stat().st_size, 'sha256': runtime.sha(path)})
    runtime.write_json(evidence / 'original-v4-snapshot-manifest.json', files)
    r = runtime.Runtime(private)
    helper = runtime.ROOT / 'deploy/blue-green/scripts/background-reconcile.py'
    hashes = {str(p.relative_to(runtime.ROOT)): runtime.sha(p) for p in [helper, r.controller, Path(__file__)]}
    old = runtime.inspect(r.state['blue'])
    green = runtime.inspect(r.state['green'])
    assert old['Id'] == original['processes']['blue']['id'] and not old['State']['Running']
    assert old['HostConfig']['RestartPolicy']['Name'] == 'no'
    assert green['Id'] == original['processes']['green']['id'] and green['Image'] == original['candidate_image']
    assert green['Config']['Labels'].get('com.codex.local-task') == 'claude-weekly-quota'
    assert all(not x['active'] and not x['cancelled'] for x in r.audit())
    financial_before = r.baseline()
    audit_before = r.audit()
    ports = {}
    for label, port in [('blue', 39080), ('green', 39081), ('front', 8080)]:
        bridge = runtime.ByteBridge(r.state['nginx'], port, r.state['bridge_ports'][str(port)])
        r.bridges.append(bridge)
        ports[label] = bridge.server_address[1]
    r.ports = ports
    events = []

    def event(kind, **fields):
        events.append({'time': runtime.now(), 'event': kind, **fields})
        runtime.write_json(evidence / 'timeline.json', events)
        print(json.dumps({'event': kind, 'time': runtime.now()}), flush=True)

    login_time = datetime.now(timezone.utc)
    code, value = runtime.json_http(ports['green'], 'POST', '/api/v1/auth/login',
                                    {'email': r.e['AdminEmail'], 'password': r.e['Password']})
    assert code == 200 and value['token_type'] == 'Bearer'
    assert isinstance(value['expires_in'], int) and value['expires_in'] > 0
    pair_path = evidence / 'token-pair.json'
    pair = {'version': 1, 'access_token': value['access_token'], 'refresh_token': value['refresh_token'],
            'expires_at': (login_time + timedelta(seconds=value['expires_in'])).isoformat()}
    runtime.write_json(pair_path, pair)
    r.token = pair['access_token']
    before = r.status('green')
    assert before['state'] == 'open' and before['background_jobs']['state'] == 'active'
    assert before['active_http'] == before['pending_usage'] == before['queued_http'] == 0
    assert before['background_jobs']['active_tasks'] == 0
    event('normal_login_pair_saved', mode=oct(pair_path.stat().st_mode & 0o777), token_contents_disclosed=False)

    runtime.docker('restart', '--time', '30', green['Id'])
    r.ready(ports['green'])
    restarted = runtime.inspect(green['Id'])
    held = r.status('green')
    assert restarted['Id'] == green['Id'] and restarted['State']['StartedAt'] != green['State']['StartedAt']
    assert held['state'] == 'open' and held['background_jobs']['state'] == 'held'
    assert held['background_jobs']['instance_id'] != before['background_jobs']['instance_id']
    assert not held['background_jobs']['leader'] and held['background_jobs']['active_tasks'] == 0
    event('restarted_http_open_jobs_held', release_status=held, started_at=restarted['State']['StartedAt'])

    def invoke(mode, expected):
        result = runtime.command([sys.executable, str(helper), '--config', str(private / 'release-config.json'),
                                  '--token-pair', str(pair_path), mode], check=False, timeout=30)
        log = evidence / ('helper-' + str(len(events)) + '.log')
        log.write_bytes(result.stdout + result.stderr)
        log.chmod(0o600)
        assert result.returncode == 0, 'background helper failed; inspect private redacted log'
        response = json.loads(result.stdout)
        assert response == {'ok': True, 'action': expected}
        event('helper_result', mode=mode, action=expected, log_sha256=runtime.sha(log))

    invoke('reconcile', 'background_activated')
    r.token = json.loads(pair_path.read_text())['access_token']
    active = r.status('green')
    assert active['background_jobs']['state'] == 'active' and active['background_jobs']['leader']
    assert active['background_jobs']['instance_id'] == held['background_jobs']['instance_id']
    for host in r.hosts:
        code, routed = runtime.json_http(ports['front'], 'GET', '/api/v1/admin/release/status', token=r.token, host=host)
        assert code == 200 and routed['background_jobs']['instance_id'] == held['background_jobs']['instance_id']
    event('both_routes_and_new_instance_active')

    before_pair = json.loads(pair_path.read_text())
    near_expiry = {**before_pair, 'expires_at': (datetime.now(timezone.utc) + timedelta(seconds=30)).isoformat()}
    assert near_expiry['access_token'] == before_pair['access_token'] and near_expiry['refresh_token'] == before_pair['refresh_token']
    runtime.write_json(pair_path, near_expiry)
    invoke('refresh', 'token_refreshed')
    after_pair = json.loads(pair_path.read_text())
    assert after_pair['access_token'] != before_pair['access_token']
    assert after_pair['refresh_token'] != before_pair['refresh_token']
    assert after_pair.get('refresh_pending', False) is False
    assert datetime.fromisoformat(after_pair['expires_at']) > datetime.now(timezone.utc) + timedelta(seconds=120)
    assert (private / 'admin-token').read_text().strip() == after_pair['access_token']
    assert pair_path.stat().st_mode & 0o777 == (private / 'admin-token').stat().st_mode & 0o777 == 0o600
    r.token = after_pair['access_token']
    for host in r.hosts:
        code, routed = runtime.json_http(ports['front'], 'GET', '/api/v1/admin/release/status', token=r.token, host=host)
        assert code == 200 and routed['background_jobs']['instance_id'] == held['background_jobs']['instance_id']
    event('actual_refresh_rotated_pair_and_new_token_accepted', access_rotated=True, refresh_rotated=True,
          only_local_expiry_changed_for_trigger=True, token_contents_disclosed=False)

    unchanged = {name: runtime.sha(private / name) for name in ['release-state.json', 'nginx/conf.d/release-upstream.conf']}
    pair_hash = runtime.sha(pair_path)
    invoke('reconcile', 'already_active')
    assert all(runtime.sha(private / name) == digest for name, digest in unchanged.items())
    assert runtime.sha(pair_path) == pair_hash
    assert r.status('green') == active
    assert r.baseline() == financial_before and r.audit() == audit_before
    assert runtime.inspect(old['Id'])['State'] == old['State']
    assert runtime.inspect(old['Id'])['HostConfig']['RestartPolicy']['Name'] == 'no'
    assert runtime.sha(original_receipt) == original_receipt_hash
    for path, digest in hashes.items():
        assert runtime.sha(runtime.ROOT / path) == digest, 'proof source changed during execution'
    for slot in ['green', 'nginx']:
        result = runtime.command(['docker', 'logs', r.state[slot]])
        path = evidence / (slot + '.log')
        path.write_bytes(result.stdout + result.stderr)
        path.chmod(0o600)
    event('background_reconcile_actual_proof_passed')
    receipt = {'pass': True, 'finished': runtime.now(), 'synthetic_only': True, 'production_modified': False,
               'original_runtime_receipt_sha256': original_receipt_hash, 'original_runtime_receipt_unchanged': True,
               'original_snapshot_manifest_sha256': runtime.sha(evidence / 'original-v4-snapshot-manifest.json'),
               'candidate_image': original['candidate_image'], 'container_id': green['Id'],
               'old_process_remained_stopped_restart_no': True, 'old_instance_id': before['background_jobs']['instance_id'],
               'new_instance_id': held['background_jobs']['instance_id'], 'new_process_started_at': restarted['State']['StartedAt'],
               'steps': ['normal_login_pair_0600', 'restart_same_synthetic_green', 'http_open_jobs_held',
                         'helper_background_activated', 'both_routes_exact_new_instance', 'actual_refresh_rotated_pair',
                         'new_access_token_both_routes_200', 'already_active_state_pair_route_and_accounting_unchanged'],
               'source_sha256': hashes, 'timeline_sha256': runtime.sha(evidence / 'timeline.json'),
               'private_evidence': str(evidence),
               'limitation': 'Early expiry was simulated only in local pair scheduling metadata; JWT bytes unchanged before actual refresh.'}
    output = runtime.OUT / ('office-bluegreen-background-reconcile-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '.json')
    runtime.write_json(output, receipt)
    print(json.dumps({'pass': True, 'receipt': str(output)}), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private', type=Path, required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    args = parser.parse_args()
    run(args.private.resolve(), args.evidence.resolve())
