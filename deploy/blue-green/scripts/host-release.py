#!/usr/bin/env python3
"""Durable host-Nginx cutover. Never starts containers or locks migrations."""
import argparse
import base64
from datetime import datetime, timezone
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

_spec = importlib.util.spec_from_file_location('release_guard', Path(__file__).with_name('release-guard.py'))
guard = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(guard)


class ReleaseError(RuntimeError):
    pass


class Deferred(ReleaseError):
    pass


def require(ok, message):
    if not ok:
        raise ReleaseError(message)


def read_file(path, private=False):
    p = Path(path)
    require(p.is_absolute() and not p.is_symlink(), 'File must be an absolute regular non-symlink path')
    info = p.stat()
    require(stat.S_ISREG(info.st_mode), 'Expected a regular file')
    if private:
        require(info.st_mode & 0o077 == 0, 'Private file permissions must exclude group and others')
    return p.read_bytes()


def atomic_write(path, data, mode=0o600):
    p = Path(path)
    require(p.is_absolute() and not p.is_symlink(), 'Unsafe atomic destination')
    p.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, tmp = tempfile.mkstemp(prefix='.' + p.name + '.', dir=p.parent)
    try:
        with os.fdopen(fd, 'wb') as out:
            os.fchmod(out.fileno(), mode)
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
        os.replace(tmp, p)
        parent = os.open(p.parent, os.O_RDONLY)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def encode(data):
    return base64.b64encode(data).decode('ascii')


def decode(data):
    return base64.b64decode(data, validate=True)


def config_read(path):
    c = json.loads(read_file(path, private=True))
    require(c.get('version') == 1, 'Unsupported configuration version')
    require(len(c.get('slots', {})) == 2, 'Exactly two named slots are required')
    require(c.get('initial_slot') in c['slots'], 'Invalid initial slot')
    require(re.fullmatch(r'[A-Za-z][A-Za-z0-9_]*', c.get('upstream_name', '')) is not None, 'Invalid upstream name')
    require(len(c.get('nginx_sites', [])) == 2 and len(set(c['nginx_sites'])) == 2, 'Exactly two distinct Nginx sites are required')
    require(len(c.get('routes', [])) == 2, 'Exactly two stable routes are required')
    for route in c['routes']:
        require(isinstance(route, dict) and re.match(r'^https?://[^\s]+$', route.get('url', '')), 'Invalid stable route URL')
        require(not any(x in route.get('host', '') for x in '\r\n'), 'Invalid Host header')
    ports = set()
    for name, slot in c['slots'].items():
        require(re.fullmatch(r'[a-z][a-z0-9_-]*', name) is not None, 'Invalid slot name')
        require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', slot.get('container', '')) is not None, 'Invalid container name')
        require(type(slot.get('port')) is int and 1024 <= slot['port'] <= 65535, 'Invalid slot port')
        require(slot['port'] not in ports, 'Duplicate slot port')
        ports.add(slot['port'])
        require(re.fullmatch(r'sha256:[a-f0-9]{64}', slot.get('image_id', '')) is not None, 'Immutable image ID required')
        require(type(slot.get('preserve_mode_required', True)) is bool, 'Invalid preserve-mode policy')
        require(slot.get('preserve_mode_required', True) or name == c['initial_slot'], 'Only original slot may use legacy startup policy')
        require(Path(slot.get('admin_token_file', '')).is_absolute(), 'Absolute private token file required')
        direct = slot.get('direct_url', f"http://127.0.0.1:{slot['port']}")
        require(re.fullmatch(r'http://127\.0\.0\.1:[0-9]+', direct) is not None, 'Direct readiness URL must be loopback HTTP')
    for key in ('state_file', 'upstream_file'):
        require(Path(c.get(key, '')).is_absolute(), 'Absolute state/upstream path required')
    require(c['state_file'] != c['upstream_file'] and c['upstream_file'] not in c['nginx_sites'], 'Conflicting managed paths')
    for path in c['nginx_sites']:
        require(Path(path).is_absolute(), 'Absolute Nginx site path required')
    for key, default in [('docker_command', ['docker']), ('nginx_command', ['nginx'])]:
        c.setdefault(key, default)
    c.setdefault('observer_command', [sys.executable, str(Path(__file__).resolve()), 'observe', '--nginx-pid-file', '/run/nginx.pid'])
    for key in ('docker_command', 'nginx_command', 'observer_command'):
        require(isinstance(c[key], list) and c[key] and all(isinstance(x, str) and x for x in c[key]), 'Invalid command argv')
    if 'inflight_evidence_command' in c:
        value = c['inflight_evidence_command']
        require(isinstance(value, list) and value and all(isinstance(x, str) and x for x in value), 'Invalid in-flight evidence command argv')
    c.setdefault('poll_seconds', 1)
    c.setdefault('reload_timeout_seconds', 15)
    require(0 < c['poll_seconds'] <= 10 and 1 <= c['reload_timeout_seconds'] <= 120, 'Invalid polling bounds')
    return c


class System:
    def run(self, argv, timeout=10):
        try:
            result = subprocess.run(argv, capture_output=True, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired):
            raise ReleaseError('External operation failed or timed out; output withheld') from None
        require(result.returncode == 0, 'External operation failed; output withheld')
        return result.stdout

    def inspect(self, c, slot):
        info = json.loads(self.run(c['docker_command'] + ['inspect', slot['container']]))[0]
        return info

    def observe(self, c):
        value = json.loads(self.run(c['observer_command'] + ['--ports', ','.join(str(s['port']) for s in c['slots'].values())]))
        require(isinstance(value, dict), 'Invalid process observation')
        for process in [value.get('master', {})] + value.get('workers', []):
            require(type(process.get('pid')) is int and process['pid'] > 0 and isinstance(process.get('start_time'), str) and process['start_time'], 'Invalid process identity')
        require(isinstance(value.get('workers'), list) and value['workers'], 'No live Nginx workers observed')
        require(isinstance(value.get('established_ports'), list) and all(type(x) is int for x in value['established_ports']), 'Invalid connection observation')
        return value

    def request(self, base, path, token=None, host=None, body=None):
        headers = {'Connection': 'close'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        if host:
            headers['Host'] = host
        data = None if body is None else json.dumps(body).encode()
        if data is not None:
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(base.rstrip('/') + path, data=data, headers=headers)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), guard.NoRedirect())
        try:
            with opener.open(req, timeout=2) as res:
                return res.status, res.read(1024 * 1024)
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
            return status, b''
        except (OSError, TimeoutError):
            raise ReleaseError('HTTP request unavailable; body and credentials withheld') from None

    def probe(self, route):
        if not route.get('host'):
            try:
                guard.probe(route['url'])
            except guard.GuardError:
                raise ReleaseError('Strict readiness probe failed') from None
        else:
            for path, expected in [('/health', 200), ('/v1/models', 401)]:
                code, _ = self.request(route['url'], path, host=route['host'])
                require(code == expected, 'Strict readiness probe failed')

    def sleep(self, seconds):
        time.sleep(seconds)

    def now(self):
        return time.monotonic()


def pid_key(value):
    return (value['pid'], value['start_time'])


class Controller:
    def __init__(self, config, system=None):
        self.c = config
        self.sys = system or System()
        self.path = Path(config['state_file'])
        topology = {k: config[k] for k in ('upstream_name', 'upstream_file', 'nginx_sites', 'initial_slot', 'routes')}
        topology['slots'] = {k: {'container': v['container'], 'port': v['port']} for k, v in config['slots'].items()}
        self.topology = hashlib.sha256(json.dumps(topology, sort_keys=True).encode()).hexdigest()
        if self.path.exists():
            self.state = json.loads(read_file(self.path, private=True))
            require(self.state.get('version') == 1 and self.state.get('topology') == self.topology, 'State does not match configured topology')
        else:
            self.state = {'version': 1, 'topology': self.topology, 'phase': 'uninstalled', 'active_slot': config['initial_slot'], 'installed': False, 'transition': None, 'retirements': {}, 'stopped_processes': []}

    def save(self, phase=None):
        if phase:
            self.state['phase'] = phase
        atomic_write(self.path, (json.dumps(self.state, sort_keys=True, indent=2) + '\n').encode())

    def slot(self, name):
        require(name in self.c['slots'], 'Unknown slot')
        return self.c['slots'][name]

    def direct(self, name):
        slot = self.slot(name)
        return slot.get('direct_url', f"http://127.0.0.1:{slot['port']}")

    def token(self, name):
        token = read_file(self.slot(name)['admin_token_file'], private=True).decode().strip()
        require(token and not any(x in token for x in '\r\n'), 'Invalid private admin token')
        return token

    def inspect(self, name, running=True):
        slot = self.slot(name)
        info = self.sys.inspect(self.c, slot)
        require(info['Image'] == slot['image_id'], 'Container runtime image differs from configured immutable ID')
        if running:
            require(info['State']['Running'], 'Container is not running; this controller never restarts it')
            env = guard.environment(info['Config'].get('Env', []))
            require(str(env.get('RELEASE_DRAIN_START_HELD', '')).lower() == 'false', 'RELEASE_DRAIN_START_HELD must be explicitly false')
            if slot.get('preserve_mode_required', True):
                require(env.get('GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE') == 'preserve', 'Candidate must preserve shared concurrency state')
                require(env.get('GATEWAY_BACKGROUND_JOB_MODE') == 'coordinated', 'Candidate background mode must be coordinated')
                require(str(env.get('GATEWAY_BACKGROUND_JOBS_START_HELD', '')).lower() == 'true', 'Candidate background jobs must start held')
        return {'container_id': info['Id'], 'started_at': info['State']['StartedAt'], 'running': info['State']['Running'], 'image_id': info['Image'], 'restart_policy': info.get('HostConfig', {}).get('RestartPolicy', {}).get('Name')}

    def identity(self, name, expected=None):
        actual = self.inspect(name)
        identity = {k: actual[k] for k in ('container_id', 'started_at', 'image_id')}
        require(identity not in self.state['stopped_processes'], 'A retired process cannot be reused')
        if not self.slot(name).get('preserve_mode_required', True) and self.state.get('initial_identity'):
            require(identity == self.state['initial_identity'], 'Legacy policy applies only to the original running process')
        if expected:
            require(identity == expected, 'Process identity changed; automatic operation refused')
        return identity

    def api(self, name, action='status', body=None, route=None):
        route = route or {'url': self.direct(name)}
        code, raw = self.sys.request(route['url'], '/api/v1/admin/release/' + action, token=self.token(name), host=route.get('host'), body=body)
        require(code == 200, 'Release control request failed; response withheld')
        try:
            value = json.loads(raw)['data']
            require(value.get('state') in ('open', 'draining', 'migrating', 'refreshing'), 'Invalid release state')
            require(isinstance(value.get('operation_id'), str), 'Invalid release operation identity')
            for key in ('active_http', 'pending_usage', 'queued_http'):
                require(type(value.get(key)) is int and value[key] >= 0, 'Invalid release counters')
            bg = value.get('background_jobs')
            if bg is not None:
                require(bg.get('mode') in ('legacy', 'coordinated') and bg.get('state') in ('held', 'active'), 'Invalid background state')
                require(type(bg.get('active_tasks')) is int and bg['active_tasks'] >= 0 and isinstance(bg.get('instance_id'), str) and bg['instance_id'], 'Invalid background identity/counter')
            return value
        except (ValueError, KeyError, TypeError, AttributeError):
            raise ReleaseError('Invalid release response; body withheld') from None

    def ready(self, name):
        identity = self.identity(name)
        self.sys.probe({'url': self.direct(name)})
        status = self.api(name)
        require(status['state'] == 'open' and status['operation_id'] == '', 'Target release gate must be open')
        if self.slot(name).get('preserve_mode_required', True):
            bg = status.get('background_jobs', {})
            require(bg.get('mode') == 'coordinated' and bg.get('instance_id'), 'Missing coordinated background identity')
        return identity, status

    def legacy_evidence(self, name, identity):
        if self.slot(name).get('preserve_mode_required', True):
            return
        path = self.slot(name).get('legacy_background_evidence_file')
        require(isinstance(path, str), 'Fresh legacy background evidence is required before retirement')
        value = json.loads(read_file(path, private=True))
        require(value.get('version') == 1, 'Invalid legacy background evidence version')
        require(all(value.get(k) == v for k, v in identity.items()), 'Legacy evidence belongs to another process')
        for key in ('scheduled_enabled', 'channel_enabled', 'batch_jobs'):
            require(type(value.get(key)) is int and value[key] == 0, 'Legacy background targets must be explicitly zero')
        require(value.get('backup_schedule_enabled') is False, 'Legacy backup schedule must be explicitly verified disabled')
        balance_disabled = value.get('background_balance_enabled') is False
        no_cn_targets = type(value.get('cn_active_accounts')) is int and value['cn_active_accounts'] == 0
        require(balance_disabled or no_cn_targets, 'Legacy CN balance work requires verified disabled configuration or zero active CN accounts')
        checked = datetime.fromisoformat(value['checked_at_utc'].replace('Z', '+00:00'))
        require(checked.tzinfo is not None, 'Legacy evidence timestamp must include timezone')
        max_age = self.slot(name).get('legacy_evidence_max_age_seconds', 300)
        require(type(max_age) is int and 1 <= max_age <= 900, 'Invalid legacy evidence freshness limit')
        age = (datetime.now(timezone.utc) - checked).total_seconds()
        require(-5 <= age <= max_age, 'Legacy background evidence is stale; refresh it before retiring')

    def inflight_evidence(self, identity):
        command = self.c.get('inflight_evidence_command')
        require(isinstance(command, list) and command, 'A live in-flight evidence collector is required before retiring any slot')
        requested = datetime.now(timezone.utc)
        raw = self.sys.run(command + ['--container-id', identity['container_id'], '--started-at', identity['started_at']])
        value = json.loads(raw)
        require(value.get('version') == 1, 'Invalid in-flight evidence version')
        require(value.get('container_id') == identity['container_id'] and value.get('started_at') == identity['started_at'], 'In-flight evidence belongs to another process')
        checked = datetime.fromisoformat(value['checked_at_utc'].replace('Z', '+00:00'))
        require(checked.tzinfo is not None, 'In-flight evidence timestamp needs timezone')
        age = (datetime.now(timezone.utc) - checked).total_seconds()
        require(-1 <= age <= 30 and checked.timestamp() >= requested.timestamp() - 1, 'In-flight collector must perform a fresh read for this request')
        for key in ('backup_running', 'restore_running', 'detached_image_tasks', 'batch_active'):
            require(type(value.get(key)) is int and value[key] >= 0, 'Missing or invalid in-flight count')
        if any(value[k] for k in ('backup_running', 'restore_running', 'detached_image_tasks', 'batch_active')):
            self.save('retirement_deferred')
            raise Deferred('External backup, restore, detached image or batch work remains; old process preserved')

    def upstream(self, name):
        return (f"# Managed by host-release.py; changes require its flock/state protocol.\n"
                f"upstream {self.c['upstream_name']} {{\n    server 127.0.0.1:{self.slot(name)['port']};\n}}\n").encode()

    def sites_match(self):
        expected = 'proxy_pass http://' + self.c['upstream_name'] + ';'
        for path in self.c['nginx_sites']:
            text = read_file(path).decode()
            directives = re.findall(r'^\s*proxy_pass\s+[^;]+;', text, re.M)
            require(len(directives) == 1 and directives[0].strip() == expected, 'Managed site route drift detected')

    def routes(self, name, direct_status=None):
        status = direct_status or self.api(name)
        instance = status.get('background_jobs', {}).get('instance_id')
        for route in self.c['routes']:
            self.sys.probe(route)
            actual = self.api(name, route=route)
            require(actual['state'] == 'open', 'Stable route release gate is not open')
            if instance:
                require(actual.get('background_jobs', {}).get('instance_id') == instance, 'Stable route reached a different process')
        return 'instance_id' if instance else 'legacy_config_generation'

    def reload(self, before):
        self.nginx_safety()
        self.sys.run(self.c['nginx_command'] + ['-t'])
        self.sys.run(self.c['nginx_command'] + ['-s', 'reload'])
        deadline = self.sys.now() + self.c['reload_timeout_seconds']
        old = {pid_key(x) for x in before['workers']}
        while True:
            now = self.sys.observe(self.c)
            require(pid_key(now['master']) == pid_key(before['master']), 'Nginx master identity changed during reload')
            if any(pid_key(x) not in old for x in now['workers']):
                return
            if self.sys.now() >= deadline:
                raise ReleaseError('New Nginx worker generation was not observed')
            self.sys.sleep(self.c['poll_seconds'])

    def nginx_safety(self):
        # A configured forced worker-shutdown deadline defeats natural ingress
        # drain even if this controller never stops the old application.
        text = self.sys.run(self.c['nginx_command'] + ['-T']).decode()
        values = re.findall(r'^\s*worker_shutdown_timeout\s+([^;\s]+)\s*;', text, re.M)
        require(all(re.fullmatch(r'0(?:ms|s|m|h|d|w)?', value) for value in values), 'Nginx forced worker shutdown timeout must be absent or zero')

    def install(self):
        self.nginx_safety()
        if self.state['installed']:
            require(self.state['transition'] is None, 'Unfinished transition; resume that command')
            self.sites_match()
            require(read_file(self.c['upstream_file']) == self.upstream(self.state['active_slot']), 'Upstream drift')
            self.routes(self.state['active_slot'])
            return
        name = self.state['active_slot']
        initial_identity, _ = self.ready(name)
        tx = self.state.get('transition')
        if tx is None:
            require(not Path(self.c['upstream_file']).exists(), 'Shared upstream destination already exists')
            originals = []
            direct = f'proxy_pass http://127.0.0.1:{self.slot(name)["port"]};'
            for path in self.c['nginx_sites']:
                raw = read_file(path)
                text = raw.decode()
                matches = re.findall(r'^\s*proxy_pass\s+[^;]+;', text, re.M)
                require(len(matches) == 1 and matches[0].strip() == direct, 'Each site must have exactly one original direct proxy')
                changed = text.replace(direct, f'proxy_pass http://{self.c["upstream_name"]};')
                originals.append({'path': path, 'before': encode(raw), 'after': encode(changed.encode()), 'mode': stat.S_IMODE(Path(path).stat().st_mode)})
            tx = {'kind': 'install', 'files': originals}
            self.state['initial_identity'] = initial_identity
            self.state['transition'] = tx
            self.save('install_prepared')
        require(tx['kind'] == 'install', 'Another transition is pending')
        try:
            atomic_write(self.c['upstream_file'], self.upstream(name), 0o644)
            for item in tx['files']:
                current = read_file(item['path'])
                require(current in (decode(item['before']), decode(item['after'])), 'Nginx site changed outside controller')
                atomic_write(item['path'], decode(item['after']), item['mode'])
            self.save('install_reload_requested')
            self.reload(self.sys.observe(self.c))
            self.sites_match()
            self.routes(name)
        except ReleaseError:
            # Running Nginx is unchanged until HUP. Restore all files before another
            # syntax check/reload; interruption leaves the original tx recoverable.
            for item in tx['files']:
                atomic_write(item['path'], decode(item['before']), item['mode'])
            Path(self.c['upstream_file']).unlink(missing_ok=True)
            self.save('install_restore_requested')
            self.reload(self.sys.observe(self.c))
            self.state['transition'] = None
            self.save('uninstalled')
            raise
        self.state['installed'] = True
        self.state['transition'] = None
        self.save('active')

    def switch(self, target, rollback=False):
        require(self.state['installed'], 'Install the shared route first')
        self.nginx_safety()
        self.sites_match()
        identity, status = self.ready(target)
        if target == self.state['active_slot'] and self.state['transition'] is None:
            require(read_file(self.c['upstream_file']) == self.upstream(target), 'Upstream drift')
            self.routes(target, status)
            return
        if self.slot(target).get('preserve_mode_required', True) and not rollback:
            require(status['background_jobs']['state'] == 'held', 'Inactive candidate background jobs must remain held')
        tx = self.state['transition']
        if tx is None:
            old = self.state['active_slot']
            old_identity, _ = self.ready(old)
            require(read_file(self.c['upstream_file']) == self.upstream(old), 'Upstream drift before cutover')
            previous = self.state['retirements'].get(target)
            if previous and not (rollback and previous.get('phase') == 'cancelled'
                                 and previous.get('operation_id') is None
                                 and previous.get('identity') == identity):
                predecessor = previous.get('identity', {})
                require(previous.get('phase') == 'stopped'
                        and predecessor in self.state['stopped_processes']
                        and predecessor.get('container_id') != identity['container_id']
                        and self.slot(target).get('preserve_mode_required', True),
                        'Target has an unresolved retirement; use rollback or reconcile the exact process')
            observation = self.sys.observe(self.c)
            tx = {'kind': 'switch', 'from': old, 'to': target, 'old_identity': old_identity, 'target_identity': identity, 'workers': observation['workers'], 'master': observation['master']}
            self.state['transition'] = tx
            self.save('switch_prepared')
        require(tx['kind'] == 'switch' and tx['to'] == target, 'Resume the existing cutover target')
        self.identity(target, tx['target_identity'])
        require(read_file(self.c['upstream_file']) in (self.upstream(tx['from']), self.upstream(target)), 'Upstream drift during cutover')
        try:
            before = self.sys.observe(self.c)
            require(pid_key(before['master']) == pid_key(tx['master']), 'Nginx master changed since cutover preparation')
            # A prior HUP may have completed before an interrupted command. Include
            # every still-live generation before issuing another graceful reload.
            known = {pid_key(x): x for x in tx['workers'] + before['workers']}
            tx['workers'] = list(known.values())
            self.save('switch_write_requested')
            atomic_write(self.c['upstream_file'], self.upstream(target), 0o644)
            self.save('switch_reload_requested')
            self.reload(before)
            self.routes(target, status)
        except ReleaseError:
            # Neither slot has been drained yet. Re-establish the previous route;
            # a failed compensation retains the tx and both running processes.
            self.identity(tx['from'], tx['old_identity'])
            self.ready(tx['from'])
            self.save('switch_restore_requested')
            atomic_write(self.c['upstream_file'], self.upstream(tx['from']), 0o644)
            self.reload(self.sys.observe(self.c))
            self.routes(tx['from'])
            self.state['transition'] = None
            self.save('active')
            raise
        self.state['active_slot'] = target
        self.state['cutover'] = tx
        self.state['transition'] = None
        # New process in a reused slot must not inherit an old drain record.
        self.state['retirements'].pop(target, None)
        self.save('active')

    def retire(self, name, timeout):
        require(self.state['installed'] and self.state['transition'] is None, 'Resolve route transition before retiring')
        require(name != self.state['active_slot'], 'Cannot retire active slot')
        cut = self.state.get('cutover', {})
        require(cut.get('from') == name and cut.get('to') == self.state['active_slot'], 'No verified cutover from this slot')
        self.sites_match()
        require(read_file(self.c['upstream_file']) == self.upstream(self.state['active_slot']), 'Active upstream drift')
        self.ready(self.state['active_slot'])
        self.routes(self.state['active_slot'])
        record = self.state['retirements'].get(name)
        inspection = self.inspect(name, running=False)
        identity = {k: inspection[k] for k in ('container_id', 'started_at', 'image_id')}
        require(identity == cut['old_identity'], 'Old process changed since cutover')
        if not inspection['running']:
            require(record and record['phase'] in ('stop_requested', 'stopped'), 'Old process stopped outside verified retirement')
            require(inspection['restart_policy'] == 'no', 'Stopped old process must remain disarmed for daemon reboot')
            record['phase'] = 'stopped'
            if identity not in self.state['stopped_processes']:
                self.state['stopped_processes'].append(identity)
            self.save('active')
            return
        self.identity(name, cut['old_identity'])
        status = self.api(name)
        deadline = self.sys.now() + timeout
        old_workers = {pid_key(x) for x in cut['workers']}
        if record is None:
            require(status['state'] == 'open' and not status['operation_id'], 'Old process already held by an unknown operation')
            record = {'phase': 'waiting_ingress', 'identity': identity, 'operation_id': None}
            self.state['retirements'][name] = record
            self.save('waiting_ingress')
        if record['phase'] == 'waiting_ingress':
            # HUP does not instantly remove old workers. Their existing HTTP/1,
            # HTTP/2 and WebSocket connections must retain an open application
            # gate until the entire captured generation and TCP ingress vanish.
            ingress_clean = 0
            while True:
                self.identity(name, record['identity'])
                status = self.api(name)
                require(status['state'] == 'open' and not status['operation_id'], 'Old gate changed while waiting for natural ingress drain')
                obs = self.sys.observe(self.c)
                require(pid_key(obs['master']) == pid_key(cut['master']), 'Nginx master changed while waiting for ingress')
                quiet = (not old_workers.intersection(pid_key(x) for x in obs['workers'])
                         and self.slot(name)['port'] not in obs['established_ports'])
                ingress_clean = ingress_clean + 1 if quiet else 0
                if ingress_clean >= 2:
                    break
                if self.sys.now() >= deadline:
                    self.save('waiting_ingress')
                    raise Deferred('Ingress retirement deferred: old workers or connections remain; old gate stays open and container running')
                self.sys.sleep(self.c['poll_seconds'])
            self.legacy_evidence(name, identity)
            self.ready(self.state['active_slot'])
            self.routes(self.state['active_slot'])
            # Persist the intent before POST; an uncertain response is never
            # attributed to this command merely because a draining state exists.
            record['phase'] = 'drain_requested'
            self.save('retiring')
            status = self.api(name, 'drain', {})
            require(status['state'] == 'draining' and status['operation_id'], 'Drain did not return an operation identity')
            record['operation_id'] = status['operation_id']
            record['phase'] = 'draining'
            self.save()
        elif not record.get('operation_id'):
            # Server-generated IDs cannot be safely attributed after a lost POST
            # response. Do not adopt or cancel someone else's drain operation.
            raise Deferred('Drain response was not durably recorded; preserve process and reconcile operation ownership')
        clean_samples = 0
        while True:
            self.identity(name, record['identity'])
            status = self.api(name)
            require(status['state'] == 'draining' and status['operation_id'] == record['operation_id'], 'Drain operation changed; process preserved')
            obs = self.sys.observe(self.c)
            require(pid_key(obs['master']) == pid_key(cut['master']), 'Nginx master changed; retirement needs reconciliation')
            background = status.get('background_jobs')
            if self.slot(name).get('preserve_mode_required', True):
                require(background is not None and background['state'] == 'held', 'Background jobs must be held before stopping')
            quiet = (all(status[x] == 0 for x in ('active_http', 'pending_usage', 'queued_http'))
                     and (background is None or background['active_tasks'] == 0)
                     and not old_workers.intersection(pid_key(x) for x in obs['workers'])
                     and self.slot(name)['port'] not in obs['established_ports'])
            clean_samples = clean_samples + 1 if quiet else 0
            if clean_samples >= 2:
                break
            if self.sys.now() >= deadline:
                record['phase'] = 'draining'
                self.save('retirement_deferred')
                raise Deferred('Retirement deferred: old requests, usage, jobs, workers or connections remain; container left running')
            self.sys.sleep(self.c['poll_seconds'])
        # Revalidate public ingress immediately before the only stop operation.
        self.sites_match()
        require(read_file(self.c['upstream_file']) == self.upstream(self.state['active_slot']), 'Active route changed before stop')
        self.ready(self.state['active_slot'])
        self.routes(self.state['active_slot'])
        self.identity(name, record['identity'])
        self.legacy_evidence(name, identity)
        # Route verification takes time. Recheck every retirement signal after
        # it, so late usage/jobs/connections cannot use a stale clean sample.
        if not self.retirement_quiet(name, record, cut):
            self.save('retirement_deferred')
            raise Deferred('Retirement signals changed before stop; process left running')
        # These asynchronous tasks are outside HTTP/usage and the coordinated
        # background gate. Query their real shared stores after natural ingress
        # and HTTP drain, on every version; a preflight zero is insufficient.
        self.inflight_evidence(identity)
        self.identity(name, record['identity'])
        if not self.retirement_quiet(name, record, cut):
            self.save('retirement_deferred')
            raise Deferred('Retirement signals changed during external checks; process preserved')
        # A stopped legacy process must not resurrect after a daemon/host reboot
        # and clear the new slot's shared state with its original startup logic.
        record['phase'] = 'restart_disable_requested'
        self.save('restart_disable_requested')
        self.sys.run(self.c['docker_command'] + ['update', '--restart=no', identity['container_id']])
        self.identity(name, record['identity'])
        require(self.inspect(name, running=False)['restart_policy'] == 'no', 'Retired process restart policy was not disabled; stop refused')
        if not self.retirement_quiet(name, record, cut):
            self.save('retirement_deferred')
            raise Deferred('Retirement signals changed while disabling restart; process left running')
        record['phase'] = 'stop_requested'
        self.save('retiring')
        self.sys.run(self.c['docker_command'] + ['stop', '--time', '30', identity['container_id']], timeout=40)
        require(not self.inspect(name, running=False)['running'], 'Old container did not stop; preserve retirement record')
        record['phase'] = 'stopped'
        self.state['stopped_processes'].append(identity)
        self.save('active')

    def retirement_quiet(self, name, record, cut):
        status = self.api(name)
        obs = self.sys.observe(self.c)
        bg = status.get('background_jobs')
        return (status['state'] == 'draining' and status['operation_id'] == record['operation_id']
                and all(status[x] == 0 for x in ('active_http', 'pending_usage', 'queued_http'))
                and (bg is None and not self.slot(name).get('preserve_mode_required', True)
                     or bg is not None and bg['active_tasks'] == 0 and bg['state'] == 'held')
                and pid_key(obs['master']) == pid_key(cut['master'])
                and not {pid_key(x) for x in cut['workers']}.intersection(pid_key(x) for x in obs['workers'])
                and self.slot(name)['port'] not in obs['established_ports'])

    def rollback(self, target):
        require(self.state['transition'] is None, 'Resume unresolved route transition first')
        cut = self.state.get('cutover', {})
        require(cut.get('from') == target, 'Rollback requires the previous slot')
        self.identity(target, cut['old_identity'])
        record = self.state['retirements'].get(target)
        status = self.api(target)
        if status['state'] != 'open':
            require(record and record.get('operation_id') and status['state'] == 'draining' and status['operation_id'] == record['operation_id'], 'Only the recorded process drain can be cancelled')
            record['phase'] = 'cancel_requested'
            self.save('rollback_cancel_requested')
            status = self.api(target, 'cancel', {'operation_id': record['operation_id']})
            require(status['state'] == 'open', 'Cancel did not reopen the old process')
        if record:
            record['operation_id'] = None
            record['phase'] = 'cancelled'
            self.save('active')
        self.ready(target)
        self.switch(target, rollback=True)

    def activate(self):
        require(self.state['transition'] is None, 'Resolve route transition before background activation')
        active = self.state['active_slot']
        cut = self.state.get('cutover', {})
        require(cut.get('to') == active, 'No verified cutover to activate')
        old = cut['from']
        record = self.state['retirements'].get(old, {})
        require(record.get('phase') == 'stopped', 'Background activation requires verified old-process retirement')
        old_inspection = self.inspect(old, running=False)
        require(not old_inspection['running'] and old_inspection['restart_policy'] == 'no', 'Old container must be stopped with automatic restart disabled')
        self.sites_match()
        require(read_file(self.c['upstream_file']) == self.upstream(active), 'Active route drift before background activation')
        # Restarted preserve-mode candidates serve HTTP with background held.
        # This explicit command revalidates the exact new process and both
        # routes, only after the previous process is durably retired/disarmed.
        identity, status = self.ready(active)
        self.routes(active, status)
        bg = status.get('background_jobs', {})
        require(status['state'] == 'open' and bg.get('mode') == 'coordinated', 'Active candidate lacks coordinated background control')
        self.state['background_activation'] = {'identity': identity, 'instance_id': bg['instance_id'], 'confirmed': False}
        self.save('background_activate_requested')
        if bg['state'] != 'active':
            self.identity(active, identity)
            status = self.api(active, 'background/activate', {'instance_id': bg['instance_id']})
            confirmed = status.get('background_jobs', {})
            require(confirmed.get('state') == 'active' and confirmed.get('instance_id') == bg['instance_id'], 'Background activation not confirmed for this process')
        self.state['background_activation']['confirmed'] = True
        self.save('active')

    def release(self, target, timeout):
        self.switch(target)
        cut = self.state.get('cutover', {})
        require(cut.get('to') == target, 'Release has no previous process to retire')
        self.retire(cut['from'], timeout)
        self.activate()

    def result(self):
        return {'active_slot': self.state['active_slot'], 'phase': self.state['phase'], 'installed': self.state['installed']}


def proc_identity(pid, root=Path('/proc')):
    raw = (root / str(pid) / 'stat').read_text()
    rest = raw[raw.rindex(')') + 2:].split()
    return {'pid': int(pid), 'start_time': rest[19]}, int(rest[1])


def observe(pid_file, ports, root=Path('/proc')):
    pid = int(Path(pid_file).read_text().strip())
    master, _ = proc_identity(pid, root)
    master_cmd = (root / str(pid) / 'cmdline').read_bytes()
    require(b'nginx: master process' in master_cmd, 'PID file does not identify an Nginx master')
    workers = []
    for entry in root.iterdir():
        if not entry.name.isdigit():
            continue
        try:
            identity, parent = proc_identity(int(entry.name), root)
            if parent == pid and b'nginx: worker process' in (entry / 'cmdline').read_bytes():
                workers.append(identity)
        except FileNotFoundError:
            continue  # process exited during the snapshot
    require(workers, 'No Nginx worker observed')
    connected = set()
    for name in ('tcp', 'tcp6'):
        for line in (root / 'net' / name).read_text().splitlines()[1:]:
            fields = line.split()
            if fields[3] == '01':
                for address in fields[1:3]:
                    port = int(address.rsplit(':', 1)[1], 16)
                    if port in ports:
                        connected.add(port)
    return {'master': master, 'workers': workers, 'established_ports': sorted(connected)}


def main():
    if len(sys.argv) > 1 and sys.argv[1] == 'observe':
        parser = argparse.ArgumentParser(description='Read-only Linux Nginx process and TCP observation')
        parser.add_argument('mode')
        parser.add_argument('--nginx-pid-file', required=True)
        parser.add_argument('--ports', required=True)
        args = parser.parse_args()
        print(json.dumps(observe(args.nginx_pid_file, {int(x) for x in args.ports.split(',')})))
        return 0
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    sub = parser.add_subparsers(dest='command', required=True)
    for command in ('install-route', 'status', 'activate'):
        sub.add_parser(command)
    for command in ('switch', 'rollback', 'retire', 'release'):
        p = sub.add_parser(command)
        p.add_argument('slot')
        if command in ('retire', 'release'):
            p.add_argument('--timeout', type=float, default=900)
    args = parser.parse_args()
    controller = None
    try:
        c = config_read(args.config)
        lock_path = Path(c['state_file'] + '.lock')
        require(not lock_path.is_symlink(), 'Unsafe lock path')
        lock_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        with open(lock_path, 'a+b') as lock:
            os.chmod(lock_path, 0o600)
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise ReleaseError('Another release command holds the lock') from None
            controller = Controller(c)
            if args.command == 'install-route':
                controller.install()
            elif args.command == 'switch':
                controller.switch(args.slot)
            elif args.command == 'rollback':
                controller.rollback(args.slot)
            elif args.command in ('retire', 'release'):
                require(0 <= args.timeout <= 86400, 'Invalid retirement timeout')
                getattr(controller, args.command)(args.slot, args.timeout)
            elif args.command == 'activate':
                controller.activate()
            print(json.dumps({'ok': True, **controller.result()}))
            return 0
    except Deferred as error:
        print(json.dumps({'ok': False, 'deferred': True, 'error': str(error), **(controller.result() if controller else {})}))
        return 3
    except (ReleaseError, OSError, ValueError, KeyError, TypeError, IndexError) as error:
        message = str(error) if isinstance(error, ReleaseError) else 'Release check failed; diagnostic payload withheld'
        print(json.dumps({'ok': False, 'error': message, **(controller.result() if controller else {})}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
