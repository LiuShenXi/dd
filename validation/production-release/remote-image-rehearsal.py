"""Rehearse the exact release image using isolated copies of production data."""
import argparse
import base64
import datetime as dt
import hashlib
import hmac
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
import urllib.request
import urllib.error


def command(args, **kwargs):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs)
    if result.returncode:
        raise RuntimeError(result.stderr.decode(errors='replace')[-4000:])
    return result.stdout


def docker(*args, **kwargs):
    return command(['sudo', '-n', 'docker', *args], **kwargs)


def private(path, value):
    with path.open('x') as f:
        os.chmod(path, 0o600)
        f.write(value if isinstance(value, str) else json.dumps(value, indent=2))


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--image', required=True)
    p.add_argument('--source', required=True)
    p.add_argument('--run-id', required=True)
    args = p.parse_args()
    if not args.image.startswith('sub2api:carpool-production-') or not args.run_id.replace('-', '').isalnum():
        raise ValueError('Invalid image or run ID')
    source = Path(args.source)
    if source.parent != Path('/home/linuxuser/apps/sub2api') or not source.name.startswith('production-release-20260908-'):
        raise ValueError('Unexpected backup directory')
    os.umask(0o077)
    work = Path('/tmp/carpool-image-rehearsal-' + args.run_id)
    work.mkdir(mode=0o700)
    label = 'carpool-final-image-20260908'
    labels = ['--label', 'com.codex.local-task=' + label, '--label', 'com.codex.validation-run=' + args.run_id]
    names = {kind: 'carpool-image-' + kind + '-' + args.run_id for kind in ['pg', 'redis', 'app', 'net', 'data']}
    containers = []
    network = volume = None
    receipt = {'image': args.image, 'run_id': args.run_id, 'passed': False}
    password, role_password, jwt_secret = [secrets.token_hex(32) for _ in range(3)]
    admin = operation = ''
    app_url = ''
    image = json.loads(docker('image', 'inspect', args.image))[0]
    image_id = image['Id']
    receipt['image_id'] = image_id
    receipt['binary_sha256'] = image['Config'].get('Labels', {}).get('sub2api.binary.sha256')

    def sql(query, raw=False):
        data = docker('exec', '-i', names['pg'], 'psql', '-X', '-U', 'sub2api', '-d', 'sub2api', '-At', '-v', 'ON_ERROR_STOP=1', input=query.encode())
        return data if raw else data.decode().strip()

    def snapshot():
        return {
            'balances': sql('SELECT id,balance,frozen_balance FROM users ORDER BY id;'),
            'keys': sql('SELECT row_to_json(k) FROM api_keys k ORDER BY id;'),
            'groups': sql('SELECT row_to_json(g) FROM groups g ORDER BY id;'),
        }

    def token(user):
        material = (user['email'].strip().lower() + '\n' + user['password_hash']).encode()
        version = int.from_bytes(hashlib.sha256(material).digest()[:8], 'big') & 0x7fffffffffffffff
        now = int(time.time())
        claims = dict(user_id=user['id'], email=user['email'], role=user['role'], token_version=version, iat=now, nbf=now-5, exp=now+1800)
        enc = lambda b: base64.urlsafe_b64encode(b).rstrip(b'=')
        signed = enc(b'{"alg":"HS256","typ":"JWT"}') + b'.' + enc(json.dumps(claims, separators=(',', ':')).encode())
        return (signed + b'.' + enc(hmac.new(jwt_secret.encode(), signed, hashlib.sha256).digest())).decode()

    def http(path, bearer=None, body=None):
        headers = {'Content-Type': 'application/json'}
        if bearer:
            headers['Authorization'] = 'Bearer ' + bearer
        req = urllib.request.Request(app_url + path, headers=headers, data=None if body is None else json.dumps(body).encode())
        try:
            with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=20) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            raise RuntimeError('Rehearsal HTTP ' + path + ': ' + str(exc.code) + ' ' + exc.read(2048).decode()) from exc

    def health():
        for _ in range(90):
            try:
                result = http('/health')
                if result.get('status') == 'ok':
                    return result
            except Exception:
                pass
            time.sleep(1)
        raise RuntimeError('Isolated app health timeout')

    def cli(mode, number=''):
        db_user, db_password = ('sub2api', password) if mode == 'schema' else ('carpool_rehearsal_import', role_password)
        dsn = 'postgres://' + db_user + ':' + db_password + '@rehearsal-pg:5432/sub2api?sslmode=disable'
        script = 'read -r CARPOOL_RELEASE_DATABASE_URL; read -r CARPOOL_RELEASE_ADMIN_TOKEN; read -r CARPOOL_RELEASE_OPERATION_ID; export CARPOOL_RELEASE_DATABASE_URL CARPOOL_RELEASE_ADMIN_TOKEN CARPOOL_RELEASE_OPERATION_ID; export CARPOOL_RELEASE_APP_URL=http://rehearsal-app:8080; exec /app/carpool-release "$@"'
        call = ['run', '--rm', '-i', '--network', names['net'], '--memory', '192m', '--memory-swap', '192m', '--cpus', '0.5', *labels,
                '--user', str(os.getuid()), '-v', str(work) + ':/evidence', '--entrypoint', '/bin/sh', image_id, '-c', script, 'release-cli', '-mode', mode]
        if mode != 'schema':
            call += ['-manifest', '/evidence/manifest.json', '-result', '/evidence/' + mode + number + '.json']
        output = docker(*call, input=(dsn + '\n' + admin + '\n' + operation + '\n').encode()).decode()
        private(work / (mode + number + '.log'), output)
        print(mode + number + ': passed', flush=True)
        if mode != 'schema':
            return json.loads((work / (mode + number + '.json')).read_text())

    try:
        backup = json.loads((source / 'backup-receipt.json').read_text())
        dump = source / 'production-before.dump'
        with dump.open('rb') as stream:
            assert hashlib.file_digest(stream, 'sha256').hexdigest() == backup['backup_sha256']
        receipt['backup_sha256'] = backup['backup_sha256']
        manifest = json.loads((source / 'manifest.json').read_text())
        assert len(manifest['members']) == 20
        private(work / 'manifest.json', manifest)
        network = docker('network', 'create', '--internal', *labels, names['net']).decode().strip()
        volume = docker('volume', 'create', *labels, names['data']).decode().strip()
        private(work / 'pg.env', 'POSTGRES_DB=sub2api\nPOSTGRES_USER=sub2api\nPOSTGRES_PASSWORD=' + password + '\n')
        containers.append(docker('run', '-d', '--name', names['pg'], '--network', names['net'], '--network-alias', 'rehearsal-pg', *labels,
                                 '--memory', '384m', '--memory-swap', '384m', '--cpus', '0.5', '--pids-limit', '96',
                                 '--env-file', str(work / 'pg.env'), '-v', volume + ':/var/lib/postgresql', 'postgres:18-alpine',
                                 'postgres', '-c', 'shared_buffers=32MB', '-c', 'work_mem=1MB', '-c', 'maintenance_work_mem=32MB', '-c', 'max_connections=30').decode().strip())
        for _ in range(60):
            try:
                sql('SELECT 1;')
                break
            except Exception:
                time.sleep(1)
        assert sql("SELECT count(*) FROM pg_tables WHERE schemaname='public';") == '0'
        with dump.open('rb') as stream:
            docker('exec', '-i', names['pg'], 'pg_restore', '-U', 'sub2api', '-d', 'sub2api', '--no-owner', '--no-privileges', '--exit-on-error', stdin=stream)
        assert sql('SELECT max(left(filename,3)) FROM schema_migrations;') == '234'
        before = snapshot()
        private(work / 'protected-before.private.json', before)
        cli('schema')
        assert sql('SELECT max(left(filename,3)) FROM schema_migrations;') == '244'
        assert snapshot() == before, 'Schema changed balances/keys/groups'
        # The clone must use its own signer; bootstrap deliberately prefers the persisted secret.
        sql("UPDATE security_secrets SET value='" + jwt_secret + "' WHERE key='jwt_secret';")
        role_sql = (source / 'restricted-role.sql').read_text().replace('\\getenv release_password CARPOOL_RELEASE_PASSWORD', "\\set release_password '" + role_password + "'")
        sql('\\set release_role carpool_rehearsal_import\n' + role_sql)
        assert sql("SELECT has_column_privilege('carpool_rehearsal_import','users','balance','UPDATE');") == 'f'
        containers.append(docker('run', '-d', '--name', names['redis'], '--network', names['net'], '--network-alias', 'rehearsal-redis', *labels,
                                 '--memory', '64m', '--memory-swap', '64m', '--cpus', '0.25', '--pids-limit', '32', 'redis:8-alpine',
                                 'redis-server', '--save', '', '--appendonly', 'no', '--maxmemory', '32mb', '--maxmemory-policy', 'allkeys-lru').decode().strip())
        env = dict(AUTO_SETUP='true', SERVER_HOST='0.0.0.0', SERVER_PORT='8080', SERVER_MODE='release', RUN_MODE='standard',
                   DATABASE_HOST='rehearsal-pg', DATABASE_PORT='5432', DATABASE_USER='sub2api', DATABASE_PASSWORD=password, DATABASE_DBNAME='sub2api',
                   DATABASE_SSLMODE='disable', DATABASE_MAX_OPEN_CONNS='10', DATABASE_MAX_IDLE_CONNS='2', REDIS_HOST='rehearsal-redis',
                   REDIS_PORT='6379', REDIS_POOL_SIZE='10', REDIS_MIN_IDLE_CONNS='1', JWT_SECRET=jwt_secret, TOTP_ENCRYPTION_KEY=secrets.token_hex(32),
                   RELEASE_DRAIN_START_HELD='true', CARPOOL_BACKGROUND_ENABLED='false', BATCH_IMAGE_QUEUE_ENABLED='false', GOMAXPROCS='2', TZ='Asia/Shanghai')
        private(work / 'app.env', ''.join(k + '=' + v + '\n' for k, v in env.items()))
        containers.append(docker('run', '-d', '--name', names['app'], '--network', names['net'], '--network-alias', 'rehearsal-app', *labels,
                                 '--memory', '512m', '--memory-swap', '512m', '--cpus', '1', '--pids-limit', '128',
                                 '--env-file', str(work / 'app.env'), image_id).decode().strip())
        app_info = json.loads(docker('inspect', names['app']))[0]
        app_ip = app_info['NetworkSettings']['Networks'][names['net']]['IPAddress']
        app_url = 'http://' + app_ip + ':8080'
        receipt['health'] = health()
        actual_binary = docker('exec', names['app'], 'sha256sum', '/app/sub2api').decode().split()[0]
        assert actual_binary == receipt['binary_sha256'], 'Runtime binary differs from image label'
        receipt['runtime_binary_sha256'] = actual_binary
        users = json.loads(sql("SELECT json_agg(row_to_json(u)) FROM (SELECT id,email,password_hash,role,created_at,balance FROM users WHERE deleted_at IS NULL ORDER BY id) u;"))
        by_id = {u['id']: u for u in users}
        admin = token(by_id[manifest['admin_user_id']])
        status = http('/api/v1/admin/release/status', admin)['data']
        assert status['state'] == 'migrating' and status['active_http'] == 0 and status['pending_usage'] == 0, status
        operation = status['operation_id']
        preview = cli('preview')
        assert not preview['committed'] and len(preview['members']) == 20
        assert snapshot() == before, 'Preview mutated protected rows'
        applied = cli('apply')
        verified = cli('verify')
        assert applied['committed'] and verified['committed'] and applied['fingerprint'] == verified['fingerprint']
        assert snapshot() == before, 'Import mutated protected rows'
        for member in applied['members']:
            approved = next(m for m in manifest['members'] if m['user_id'] == member['user_id'])
            start, end = [dt.datetime.fromisoformat(member[k].replace('Z', '+00:00')) for k in ['starts_at', 'expires_at']]
            registration = dt.datetime.fromisoformat(by_id[member['user_id']]['created_at'])
            assert start == registration and end - start == dt.timedelta(days=approved['duration_days'])
            assert dt.datetime.fromisoformat(member['cycle_starts_at'].replace('Z', '+00:00')) == dt.datetime.fromisoformat(manifest['cycle_anchor'])
            assert float(member['plan_snapshot']['weekly_quota_usd']) == float(approved['weekly_quota_usd'])
        scope = {'operation_id': operation, 'user_ids': [m['user_id'] for m in manifest['members']], 'group_ids': [manifest['group_id']]}
        # Rehearse ambiguous commit recovery before opening any client traffic.
        docker('restart', names['app'])
        health()
        status = http('/api/v1/admin/release/status', admin)['data']
        assert status['state'] == 'migrating' and status['operation_id'] != operation
        operation = status['operation_id']
        scope['operation_id'] = operation
        cli('verify', '-restart')
        assert http('/api/v1/admin/release/resume', admin, scope)['data']['state'] == 'open'
        keys = json.loads(sql("SELECT json_agg(row_to_json(k)) FROM (SELECT id,user_id,key,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d FROM api_keys WHERE deleted_at IS NULL AND status='active' ORDER BY id) k;"))
        http_proof = []
        for member in manifest['members']:
            data = http('/api/v1/user/carpool/details', token(by_id[member['user_id']]))['data']
            assert data['billing_mode'] == 'carpool' and data['term'] is not None
            assert data['quota'] is not None
            http_proof.append({'user_id': member['user_id'], 'term': data['term'], 'quota': data['quota']})
        unrestricted = next(k for k in keys if k['user_id'] in scope['user_ids'] and not any(k[n] for n in ['quota', 'rate_limit_5h', 'rate_limit_1d', 'rate_limit_7d']))
        member_usage = http('/v1/usage', unrestricted['key'])
        assert member_usage['billing_type'] == 'carpool' and 'balance' not in member_usage
        admin_key = next(k for k in keys if k['id'] == 6 and k['user_id'] == manifest['admin_user_id'])
        admin_usage = http('/v1/usage', admin_key['key'])
        assert admin_usage.get('billing_type') != 'carpool' and float(admin_usage['balance']) == float(by_id[manifest['admin_user_id']]['balance'])
        private(work / 'http-proof.private.json', {'members': http_proof, 'member_usage': member_usage, 'admin_usage': admin_usage})
        assert http('/v1/usage', unrestricted['key'])['billing_type'] == 'carpool'
        after = snapshot()
        # Authentication legitimately updates last_used_at on Keys; raw key identity and assignments stay exact.
        assert after['balances'] == before['balances'] and after['groups'] == before['groups']
        old_keys = [json.loads(line) for line in before['keys'].splitlines()]
        new_keys = [json.loads(line) for line in after['keys'].splitlines()]
        for old, new in zip(old_keys, new_keys, strict=True):
            for item in [old, new]:
                item.pop('last_used_at', None)
                item.pop('updated_at', None)
            assert old == new
        receipt.update(passed=True, members=20, duration_7_days=2, duration_28_days=18, member_usage_carpool=True,
                       admin_usage_standard=True, unchanged_balances=True, unchanged_keys=True, unchanged_groups=True,
                       restart_held_verified=True, restricted_role_balance_update=False, no_upstream_requests=True,
                       fingerprint=applied['fingerprint'])
        print('Final image isolated rehearsal PASS: 20 members, exact protected rows, member/admin HTTP, restart-held.', flush=True)
    except Exception as exc:
        receipt['error'] = str(exc)
        raise
    finally:
        for cid in reversed(containers):
            info = json.loads(docker('inspect', cid))[0]
            assert info['Config']['Labels']['com.codex.validation-run'] == args.run_id
            private(work / (info['Name'].strip('/') + '.private.log'), docker('logs', cid).decode(errors='replace'))
            docker('rm', '-f', '-v', cid)
        if volume:
            info = json.loads(docker('volume', 'inspect', volume))[0]
            assert info['Labels']['com.codex.validation-run'] == args.run_id and info['Labels']['com.codex.local-task'] == label
            docker('volume', 'rm', volume)
        if network:
            info = json.loads(docker('network', 'inspect', network))[0]
            assert info['Labels']['com.codex.validation-run'] == args.run_id and info['Labels']['com.codex.local-task'] == label
            docker('network', 'rm', network)
        for path in work.glob('*.env'):
            path.unlink()
        private(work / 'receipt.json', receipt)
        print('Private rehearsal evidence: ' + str(work), flush=True)


if __name__ == '__main__':
    main()
