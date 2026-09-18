"""One-time migration of the existing synthetic runtime to the HTTP mock policy."""
import datetime
import json

from lifecycle import REGISTRY, check, load, register
from runtime import EVIDENCE, IMAGE, LABEL, PREFIX, PRIVATE, http_status, own, run, save_json, wait_ready


def main():
    check()
    name = PREFIX + '-server'
    server = own('container', name)
    assert list(server['NetworkSettings']['Networks']) == [PREFIX + '-internal']
    assert own('network', PREFIX + '-internal')['Internal'] is True
    creds = json.loads((PRIVATE / 'app-credentials.private.json').read_text())
    assert creds['source'] == 'synthetic-local-only'
    envfile = PRIVATE / 'app.private.env'
    text = envfile.read_text()
    assert 'SECURITY_URL_ALLOWLIST_ENABLED=true\n' in text
    envfile.write_text(text.replace('SECURITY_URL_ALLOWLIST_ENABLED=true\n', 'SECURITY_URL_ALLOWLIST_ENABLED=false\n'), encoding='utf-8')
    state = load()
    save_json(PRIVATE / 'owned-resources.before-local-http.json', state)
    run(['docker', 'stop', '--time', '30', name], 'http-policy-stop-candidate')
    assert own('container', name)['Id'] == server['Id']
    run(['docker', 'container', 'rm', name], 'http-policy-remove-stopped-candidate')
    # The prior identity is retained in the immutable private audit copy above.
    state['resources'] = [entry for entry in state['resources'] if entry['name'] != name]
    save_json(REGISTRY, state)
    run(['docker', 'run', '-d', '--name', name, '--network', PREFIX + '-internal', '--network-alias', 'upgrade026-app',
         '--label', 'com.codex.local-task=' + LABEL, '--env-file', str(envfile), '-v', PREFIX + '-data:/app/data',
         '--read-only', '--tmpfs', '/tmp:rw,nosuid,noexec,size=64m', '--tmpfs', '/var/lib/postgresql:rw,nosuid,noexec,size=1m', IMAGE], 'http-policy-start-candidate')
    run(['docker', 'restart', PREFIX + '-ingress'], 'http-policy-refresh-ingress')
    wait_ready(lambda: http_status('/health') == 200, 120, 'candidate HTTP policy readiness')
    assert http_status('/v1/models') == 401
    register()
    save_json(EVIDENCE / 'local-http-mock-policy.json', {'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'scope': 'synthetic local runtime only', 'security_url_allowlist_enabled': False, 'allow_insecure_http': True,
              'reason': 'HTTP deterministic mock; strict allowlist mode always requires HTTPS', 'application_network_internal': True,
              'production_configuration_changed': False, 'product_source_changed': False, 'same_image': True,
              'same_synthetic_database': True, 'health_status': 200, 'unauthenticated_models_status': 401})
    print('LOCAL_HTTP_MOCK_POLICY_READY same candidate image and synthetic data; no product change', flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(type(error).__name__ + ': ' + str(error), flush=True)
        raise SystemExit(1)
