"""Build/start an isolated synthetic candidate; never reads a live deployment."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import secrets
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.request

SCRIPT = pathlib.Path(__file__).resolve().parent
REPO = SCRIPT.parents[2]
PRIVATE = pathlib.Path(os.environ['LOCALAPPDATA']) / 'Codex/PrivateTests/sub2api-upgrade-026-20260918/runtime'
EVIDENCE = SCRIPT / 'evidence'
LABEL = 'sub2api-upgrade026-20260918'
PREFIX = 'sub2api-upgrade026-app'
IMAGE = PREFIX + ':candidate'
PORT = 38626
BASE = f'http://127.0.0.1:{PORT}'

def save_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + '\n', encoding='utf-8')

def run(args, step, *, cwd=None, env=None):
    PRIVATE.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True)
    (PRIVATE / (step + '.log')).write_bytes(result.stdout + result.stderr)
    if result.returncode:
        raise RuntimeError(f'{step} failed (exit {result.returncode}); inspect its private log')
    return result.stdout.decode(errors='replace').strip()

def inspect(kind, name):
    result = subprocess.run(['docker', kind, 'inspect', name], capture_output=True)
    if result.returncode:
        return None
    return json.loads(result.stdout)[0]

def own(kind, name):
    data = inspect(kind, name)
    if data is None:
        return None
    labels = data.get('Config', {}).get('Labels', {}) if kind == 'container' else data.get('Labels', {})
    if labels.get('com.codex.local-task') != LABEL:
        raise RuntimeError(f'refusing foreign {kind} {name}')
    return data

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def inputs():
    files = {}
    for path in (REPO / 'backend').rglob('*'):
        if not path.is_file():
            continue
        rel = path.relative_to(REPO).as_posix()
        include = (path.suffix == '.go' and not path.name.endswith('_test.go')) or path.suffix == '.sql' or path.name in ('go.mod', 'go.sum', 'VERSION')
        include = include or '/internal/web/dist/' in rel or '/resources/' in rel
        if include:
            files[rel] = digest(path)
    encoded = json.dumps(files, sort_keys=True, separators=(',', ':')).encode()
    return {'sha256': hashlib.sha256(encoded).hexdigest(), 'files': files}

def build():
    dist = REPO / 'backend/internal/web/dist'
    if not (dist / 'index.html').is_file():
        raise RuntimeError('new frontend dist must exist before embedding')
    PRIVATE.mkdir(parents=True, exist_ok=True)
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    if os.name == 'nt':
        run(['icacls', str(PRIVATE), '/inheritance:r', '/grant:r', os.environ['USERNAME'] + ':(OI)(CI)F'], 'private-acl')
    before = inputs()
    context = PRIVATE / 'image-context'
    context.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update({'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0', 'GOTOOLCHAIN': 'go1.27.0'})
    commit = run(['git', 'rev-parse', 'HEAD'], 'source-commit', cwd=REPO)
    print('BUILD_START Linux amd64 embed, current source and frontend dist', flush=True)
    run(['go', 'build', '-tags=embed', '-trimpath', '-ldflags', '-s -w -X main.Commit=' + commit + '-upgrade026-local -X main.BuildType=source', '-o', str(context / 'sub2api'), './cmd/server'], 'go-build', cwd=REPO / 'backend', env=env)
    after = inputs()
    if before['sha256'] != after['sha256']:
        raise RuntimeError('build inputs changed during compilation; rebuild once source is stable')
    shutil.copytree(REPO / 'backend/resources', context / 'resources', dirs_exist_ok=True)
    shutil.copyfile(SCRIPT / 'Dockerfile', context / 'Dockerfile')
    run(['docker', 'build', '--network=none', '--label', 'com.codex.local-task=' + LABEL, '-t', IMAGE, str(context)], 'docker-build')
    image = json.loads(run(['docker', 'image', 'inspect', IMAGE], 'image-inspect'))[0]
    manifest = {'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'git_head': commit,
                'source_input_sha256': before['sha256'], 'source_files': before['files'],
                'frontend_index_sha256': digest(dist / 'index.html'), 'frontend_files': sum(p.is_file() for p in dist.rglob('*')),
                'binary_sha256': digest(context / 'sub2api'), 'image_id': image['Id'], 'image_tag': IMAGE,
                'target': 'linux/amd64', 'source': 'current isolated integration checkout, synthetic local acceptance only'}
    save_json(EVIDENCE / 'build-manifest.json', manifest)
    print('BUILD_READY ' + manifest['binary_sha256'], flush=True)

def wait_ready(check, seconds, description):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(1)
    raise RuntimeError(description + ' did not become ready')

def http_status(path):
    try:
        with urllib.request.urlopen(BASE + path, timeout=2) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except (urllib.error.URLError, TimeoutError):
        return None

def start():
    build_manifest = json.loads((EVIDENCE / 'build-manifest.json').read_text())
    if build_manifest['source_input_sha256'] != inputs()['sha256']:
        raise RuntimeError('source changed since candidate build')
    for suffix in ('postgres', 'redis', 'server', 'ingress'):
        if inspect('container', PREFIX + '-' + suffix) is not None:
            raise RuntimeError('candidate containers already exist; inspect them without replacing them')
    for suffix in ('internal', 'ingress-net'):
        if inspect('network', PREFIX + '-' + suffix) is not None:
            raise RuntimeError('candidate network already exists; inspect without replacing it')
    for suffix in ('pgdata', 'data'):
        if inspect('volume', PREFIX + '-' + suffix) is not None:
            raise RuntimeError('candidate volume already exists; fresh run requires unused names')
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', PORT))
    # A previous cleaned run may retain private artifacts for audit; never reuse
    # its database IDs or user credentials in a new synthetic database.
    fixture = PRIVATE / 'browser-fixtures.private.json'
    if fixture.exists():
        fixture.rename(PRIVATE / ('browser-fixtures.previous-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '.private.json'))
    db_password, admin_password, jwt_secret = (secrets.token_hex(32) for _ in range(3))
    pg_env = {'POSTGRES_DB': 'upgrade026', 'POSTGRES_USER': 'upgrade026', 'POSTGRES_PASSWORD': db_password}
    app_env = {
        'AUTO_SETUP': 'true', 'DATABASE_HOST': PREFIX + '-postgres', 'DATABASE_PORT': '5432',
        'DATABASE_USER': 'upgrade026', 'DATABASE_DBNAME': 'upgrade026', 'DATABASE_PASSWORD': db_password,
        'DATABASE_SSLMODE': 'disable', 'REDIS_HOST': PREFIX + '-redis', 'REDIS_PORT': '6379',
        'ADMIN_EMAIL': 'upgrade026-admin@example.invalid', 'ADMIN_PASSWORD': admin_password,
        'JWT_SECRET': jwt_secret, 'TOTP_ENCRYPTION_KEY': secrets.token_hex(32),
        'SERVER_HOST': '0.0.0.0', 'SERVER_PORT': '8080', 'SERVER_MODE': 'release', 'TZ': 'Asia/Shanghai',
        'RELEASE_DRAIN_START_HELD': 'false', 'TOKEN_REFRESH_ENABLED': 'false', 'OPS_ENABLED': 'false',
        'BATCH_IMAGE_ENABLED': 'false', 'IMAGE_STORAGE_ENABLED': 'false',
        # HTTP mocks require format-only validation. This process has only an
        # internal Docker network and synthetic credentials, with no Internet route.
        'SECURITY_URL_ALLOWLIST_ENABLED': 'false', 'SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS': 'carpool-mock,synthetic.invalid',
        'SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP': 'true', 'SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS': 'true',
        'SECURITY_URL_ALLOWLIST_PRICING_HOSTS': 'synthetic.invalid', 'SECURITY_URL_ALLOWLIST_CRS_HOSTS': 'synthetic.invalid',
    }
    for filename, entries in [('postgres.private.env', pg_env), ('app.private.env', app_env)]:
        (PRIVATE / filename).write_text(''.join(k + '=' + v + '\n' for k, v in entries.items()), encoding='utf-8')
    save_json(PRIVATE / 'app-credentials.private.json', {'source': 'synthetic-local-only', 'base_url': BASE,
              'admin': {'email': app_env['ADMIN_EMAIL'], 'password': admin_password}})
    common = ['--label', 'com.codex.local-task=' + LABEL]
    internal, ingress = PREFIX + '-internal', PREFIX + '-ingress-net'
    run(['docker', 'network', 'create', '--internal', *common, internal], 'create-internal-network')
    run(['docker', 'network', 'create', *common, ingress], 'create-ingress-network')
    for suffix in ('pgdata', 'data'):
        run(['docker', 'volume', 'create', *common, PREFIX + '-' + suffix], 'create-' + suffix)
    run(['docker', 'run', '-d', '--name', PREFIX + '-postgres', '--network', internal, *common,
         '--env-file', str(PRIVATE / 'postgres.private.env'), '-v', PREFIX + '-pgdata:/var/lib/postgresql', 'postgres:18-alpine'], 'start-postgres')
    run(['docker', 'run', '-d', '--name', PREFIX + '-redis', '--network', internal, *common, '--read-only',
         '--tmpfs', '/data:rw,nosuid,noexec,size=64m', 'redis:8-alpine', 'redis-server', '--save', '', '--appendonly', 'no'], 'start-redis')
    wait_ready(lambda: subprocess.run(['docker', 'exec', PREFIX + '-postgres', 'pg_isready', '-U', 'upgrade026', '-d', 'upgrade026'], capture_output=True).returncode == 0, 60, 'isolated PostgreSQL')
    run(['docker', 'run', '-d', '--name', PREFIX + '-server', '--network', internal, '--network-alias', 'upgrade026-app', *common,
         '--env-file', str(PRIVATE / 'app.private.env'), '-v', PREFIX + '-data:/app/data', '--read-only',
         '--tmpfs', '/tmp:rw,nosuid,noexec,size=64m', '--tmpfs', '/var/lib/postgresql:rw,nosuid,noexec,size=1m', IMAGE], 'start-app')
    run(['docker', 'create', '--name', PREFIX + '-ingress', '--network', ingress, *common,
         '-p', f'127.0.0.1:{PORT}:8080', '-v', str(SCRIPT / 'nginx.conf') + ':/etc/nginx/conf.d/default.conf:ro', 'nginx:1.27-alpine'], 'create-ingress')
    run(['docker', 'network', 'connect', internal, PREFIX + '-ingress'], 'connect-ingress')
    run(['docker', 'start', PREFIX + '-ingress'], 'start-ingress')
    wait_ready(lambda: http_status('/health') == 200, 120, 'candidate HTTP health')
    if http_status('/v1/models') != 401:
        raise RuntimeError('business readiness must return exact unauthenticated HTTP 401')
    network = own('network', internal)
    server = own('container', PREFIX + '-server')
    if network.get('Internal') is not True or list(server['NetworkSettings']['Networks']) != [internal]:
        raise RuntimeError('candidate application must belong only to its internal network')
    save_json(EVIDENCE / 'runtime-ready.json', {'base_url': BASE, 'private_credentials_file': str(PRIVATE / 'app-credentials.private.json'),
              'source': 'fresh synthetic local database', 'health_status': 200, 'unauthenticated_models_status': 401,
              'app_networks': list(server['NetworkSettings']['Networks']), 'app_network_internal': network['Internal'],
              'container_started_at': server['State']['StartedAt'], 'image_id': server['Image'], 'label': LABEL})
    print('RUNTIME_READY ' + BASE, flush=True)
    print('Private synthetic credentials: ' + str(PRIVATE / 'app-credentials.private.json'), flush=True)
    from lifecycle import register
    register()

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('build', 'start', 'register', 'check', 'stop', 'cleanup'))
    args = parser.parse_args()
    try:
        if args.action in ('build', 'start'):
            {'build': build, 'start': start}[args.action]()
        else:
            import lifecycle
            {'register': lifecycle.register, 'check': lambda: (lifecycle.check(), print('OWNERSHIP_PREFLIGHT_PASS')),
             'stop': lifecycle.stop, 'cleanup': lifecycle.cleanup}[args.action]()
    except Exception as error:
        print(type(error).__name__ + ': ' + str(error), flush=True)
        raise SystemExit(1)
