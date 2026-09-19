"""Reuse the 026 synthetic harness with fresh, tightly scoped 027 identities.

Only transformed helper copies, credentials, executables and raw logs are private.
No existing runtime, production connection or historical evidence is reused.
"""
import argparse
import datetime
import hashlib
import importlib
import json
import os
import pathlib
import secrets
import subprocess
import sys
import urllib.error
import urllib.request

REPO = pathlib.Path(__file__).resolve().parents[2]
SOURCE = REPO / 'validation/upgrade-026/runtime'
PRIVATE = pathlib.Path(os.environ['LOCALAPPDATA']) / 'Codex/PrivateTests/sub2api-upgrade-027-20260919/runtime'
HELPERS = PRIVATE / 'harness'
EVIDENCE = REPO / 'validation/upgrade-027/runtime-evidence'
PREFIX = 'sub2api-upgrade027-app'
LABEL = 'sub2api-upgrade027-20260919'


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise RuntimeError('historical harness shape changed; inspect before adapting')
    return text.replace(before, after, 1)


def prepare():
    PRIVATE.mkdir(parents=True, exist_ok=True)
    if os.name == 'nt':
        result = subprocess.run(['icacls', str(PRIVATE), '/inheritance:r', '/grant:r', os.environ['USERNAME'] + ':(OI)(CI)F'], capture_output=True)
        if result.returncode:
            raise RuntimeError('private directory ACL failed')
    HELPERS.mkdir(exist_ok=True)
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    entries = {}
    for filename in ('runtime.py', 'lifecycle.py', 'verify_http.py', 'Dockerfile', 'nginx.conf', 'mock/probe.go'):
        source = SOURCE / filename
        text = source.read_text(encoding='utf-8')
        entries[filename] = hashlib.sha256(source.read_bytes()).hexdigest()
        text = text.replace('upgrade-026-20260918', 'upgrade-027-20260919').replace('upgrade026-20260918', 'upgrade027-20260919')
        text = text.replace('upgrade026', 'upgrade027').replace('38626', '38627')
        if filename == 'runtime.py':
            text = replace_once(text, 'REPO = SCRIPT.parents[2]', 'REPO = pathlib.Path(' + repr(str(REPO)) + ')')
            text = replace_once(text, "EVIDENCE = SCRIPT / 'evidence'", 'EVIDENCE = pathlib.Path(' + repr(str(EVIDENCE)) + ')')
            text = replace_once(text, "'GOTOOLCHAIN': 'go1.27.0'", "'GOTOOLCHAIN': 'go1.27.0', 'GOMAXPROCS': '4'")
            text = replace_once(text, "['go', 'build', '-tags=embed'", "['go', 'build', '-p', '4', '-tags=embed'")
            text = replace_once(text, "'OPS_ENABLED': 'false',", "'OPS_ENABLED': 'false', 'GOMAXPROCS': '4',\n        'CODEX_SENTINEL_TOKEN_FILE': '/app/data/sentinel.token',\n        'CODEX_SENTINEL_ACCOUNT_IDS': '99999999', 'CODEX_SENTINEL_MODELS': 'gpt-6-astra,gpt-5.6-sol',")
        target = HELPERS / filename
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding='utf-8', newline='\n')
    (EVIDENCE / 'harness-provenance.json').write_text(json.dumps({'source': 'existing 026 synthetic harness; limited naming/path/concurrency/sentinel config adaptation', 'source_files_sha256': entries, 'prefix': PREFIX, 'label': LABEL, 'host_port': 38627}, indent=2) + '\n', encoding='utf-8')
    sys.path.insert(0, str(HELPERS))
    return importlib.import_module('runtime')


def compile_helpers(runtime):
    env = os.environ.copy()
    env.update({'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0', 'GOTOOLCHAIN': 'go1.27.0', 'GOMAXPROCS': '4'})
    targets = [('carpool-mock', REPO / 'validation/source-runtime/mock/main.go'), ('carpool-probe', HELPERS / 'mock/probe.go'), ('carpool-release', './cmd/carpool-release')]
    for name, source in targets:
        runtime.run(['go', 'build', '-p', '4', '-trimpath', '-o', str(PRIVATE / name), str(source)], 'build-' + name, cwd=REPO / 'backend', env=env)
    runtime.save_json(EVIDENCE / 'helper-binaries.json', {name: runtime.digest(PRIVATE / name) for name, _ in targets})
    print('HELPER_BUILD_PASS mock probe carpool-release', flush=True)


def start(runtime):
    original = runtime.run
    token = secrets.token_hex(32)
    (PRIVATE / 'sentinel-token.private').write_text(token, encoding='ascii')
    def owned_run(args, step, **kwargs):
        output = original(args, step, **kwargs)
        if step == 'create-data':
            result = subprocess.run(['docker', 'run', '--rm', '-i', '--network', 'none', '--label', 'com.codex.local-task=' + LABEL,
                '--tmpfs', '/var/lib/postgresql:rw,nosuid,noexec,size=1m', '-v', PREFIX + '-data:/private', '--entrypoint', 'sh', 'postgres:18-alpine',
                '-c', 'umask 077; cat > /private/sentinel.token; chown 1000:1000 /private /private/sentinel.token; chmod 755 /private; chmod 400 /private/sentinel.token'], input=token.encode(), capture_output=True, timeout=60)
            (PRIVATE / 'sentinel-token-init.log').write_bytes(result.stdout + result.stderr)
            if result.returncode:
                raise RuntimeError('private sentinel token initialization failed')
        return output
    runtime.run = owned_run
    try:
        runtime.start()
    finally:
        runtime.run = original
        # Record partial starts too, allowing guarded cleanup after a failed probe.
        if runtime.own('network', PREFIX + '-internal') is not None:
            importlib.import_module('lifecycle').register()


def sentinel(runtime):
    token = (PRIVATE / 'sentinel-token.private').read_text(encoding='ascii')
    def request(path, supplied=None, body=None):
        headers = {'Accept': 'application/json'}
        if supplied is not None:
            headers['Authorization'] = 'Bearer ' + supplied
        if body is not None:
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(runtime.BASE + path, headers=headers, data=None if body is None else json.dumps(body).encode())
        try:
            with urllib.request.urlopen(req, timeout=10) as response:
                return response.status, response.read(), response.headers.get('Content-Type', '')
        except urllib.error.HTTPError as error:
            return error.code, error.read(), error.headers.get('Content-Type', '')
    checks = {}
    for mode, supplied in [('unauthenticated', None), ('wrong_token', 'invalid-synthetic-token')]:
        status, raw, _ = request('/internal/codex-sentinel/status', supplied)
        assert status == 401 and b'<html' not in raw.lower()
        checks[mode] = status
    status, raw, content_type = request('/internal/codex-sentinel/status', token)
    assert status == 200 and 'application/json' in content_type
    data = json.loads(raw)
    assert data['protocol_version'] == 1 and data['enabled'] is False and data['events'] == []
    assert len(data['targets']) == 2 and all(not t['eligible'] and not t['ready'] for t in data['targets'])
    status, raw, _ = request('/internal/codex-sentinel/refresh', token, {'account_id': 99999999, 'model': 'gpt-6-astra', 'reason': 'missing', 'expected_ticket_version': '', 'event_id': ''})
    assert status == 200 and json.loads(raw)['status'] == 'disabled'
    http = importlib.import_module('verify_http')
    assert http.sql("SELECT count(*) FROM accounts WHERE type='oauth';") == '0'
    checks.update({'authorized_metadata': 200, 'master_default_disabled': True, 'master_off_refresh': 'disabled', 'embedded_routes_reachable': True, 'oauth_accounts': 0, 'real_upstream_requests': False})
    runtime.save_json(EVIDENCE / 'sentinel-runtime.json', checks)
    print('SENTINEL_RUNTIME_PASS auth401 metadata200 master-off refresh-disabled', flush=True)


def probe(runtime):
    for suffix in ('mock', 'probe'):
        if runtime.inspect('container', PREFIX + '-' + suffix) is not None:
            raise RuntimeError('probe resources already exist; refusing replacement')
    common = ['--label', 'com.codex.local-task=' + LABEL, '--network', PREFIX + '-internal', '--read-only', '--tmpfs', '/var/lib/postgresql:rw,nosuid,noexec,size=1m']
    runtime.run(['docker', 'run', '-d', '--name', PREFIX + '-mock', '--network-alias', 'carpool-mock', *common,
        '-v', str(PRIVATE / 'carpool-mock') + ':/probe:ro', '--entrypoint', '/probe', 'postgres:18-alpine'], 'start-mock')
    runtime.run(['docker', 'create', '--name', PREFIX + '-probe', *common,
        '-v', str(PRIVATE) + ':/private', '-v', str(PRIVATE / 'carpool-probe') + ':/probe:ro', '--entrypoint', '/probe', 'postgres:18-alpine', '-private-dir', '/private'], 'create-probe')
    importlib.import_module('lifecycle').register()
    runtime.run(['docker', 'start', '-a', PREFIX + '-probe'], 'run-probe')
    result = runtime.own('container', PREFIX + '-probe')
    assert result['State']['ExitCode'] == 0 and not result['State']['OOMKilled']
    report = json.loads((PRIVATE / 'http-ws-probe-report.json').read_text())
    assert report['passed'] is True and report['source'] == 'fresh-synthetic-local-only'
    runtime.save_json(EVIDENCE / 'http-ws-probe-report.json', report)
    print('HTTP_WS_RUNTIME_PASS receipts3 ledger-debits3 ordinary-balance47.25', flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('prepare', 'build', 'start', 'settings', 'sentinel', 'probe', 'check', 'cleanup'))
    args = parser.parse_args()
    runtime = prepare()
    if args.action == 'prepare':
        print('HARNESS_READY ' + str(HELPERS), flush=True)
    elif args.action == 'build':
        runtime.build()
        compile_helpers(runtime)
    elif args.action == 'start':
        start(runtime)
    elif args.action == 'settings':
        importlib.import_module('verify_http').verify_settings()
    elif args.action == 'sentinel':
        sentinel(runtime)
    elif args.action == 'probe':
        probe(runtime)
    elif args.action == 'check':
        importlib.import_module('lifecycle').check()
        print('OWNERSHIP_PREFLIGHT_PASS', flush=True)
    elif args.action == 'cleanup':
        importlib.import_module('lifecycle').cleanup()
        runtime.save_json(EVIDENCE / 'cleanup.json', {'passed': True, 'label': LABEL, 'prefix': PREFIX, 'removed': 'recorded containers, networks, synthetic volumes', 'retained': 'private files and local candidate build image', 'completed_at': datetime.datetime.now(datetime.timezone.utc).isoformat()})


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Raw subprocess output and response bodies remain in private logs.
        print(type(error).__name__ + ': ' + str(error), flush=True)
        raise SystemExit(1)
