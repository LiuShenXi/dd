#!/usr/bin/env python3
"""Actual synthetic host-Nginx release proof; no production endpoints or Go build."""
import argparse
import base64
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import socketserver
import struct
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / 'validation/claude-weekly-quota'
PRIVATE_ROOT = Path.home() / '.codex/private/claude-weekly-quota'
DRIVER = PRIVATE_ROOT / 'office/repository-promotion-final3.test'
DRIVER_SHA = 'c228980169044eff0a381975577a2e717bd541cf197fb1baa84241e6cf3b8969'
PG = 'postgres@sha256:6c538e7206ea40ff740ef27883529390a690b6ead6ba96b44c67a9f7c638e8fd'
NGINX = 'sha256:a5127daff3d6f4606be3100a252419bfa84fd6ee5cd74d0feaca1a5068f97dcf'
R0 = 'sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677'
LABEL = 'com.codex.local-task=claude-weekly-quota'


def sha(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            value.update(chunk)
    return value.hexdigest()


def now():
    return datetime.now(timezone.utc).isoformat()


def write_json(path, data):
    path = Path(path)
    path.write_text(json.dumps(data, indent=2) + '\n')
    os.chmod(path, 0o600)


def command(args, *, data=None, timeout=90, check=True):
    result = subprocess.run(args, input=data, capture_output=True, timeout=timeout)
    if check and result.returncode:
        # Arguments or stderr can contain synthetic keys. Private caller logs may
        # retain the output separately; never include them in public exceptions.
        raise RuntimeError('command failed: ' + Path(args[0]).name + ' ' + str(result.returncode))
    return result


def docker(*args, **kw):
    return command(['docker', *args], **kw).stdout.decode().strip()


def inspect(container):
    return json.loads(docker('inspect', container))[0]


def eventually(callback, seconds=40, detail='condition'):
    deadline = time.monotonic() + seconds
    last = None
    while time.monotonic() < deadline:
        try:
            value = callback()
            if value:
                return value
        except (OSError, http.client.HTTPException) as error:
            last = type(error).__name__
        time.sleep(0.2)
    raise AssertionError('timeout: ' + detail + (': ' + last if last else ''))


def json_http(port, method, path, body=None, token=None, host=None):
    conn = http.client.HTTPConnection('127.0.0.1', port, timeout=8)
    headers = {'Content-Type': 'application/json', 'Connection': 'close'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    if host:
        headers['Host'] = host
    raw = json.dumps(body).encode() if body is not None else None
    try:
        conn.request(method, path, raw, headers)
        response = conn.getresponse()
        content = response.read(2 << 20)
        payload = json.loads(content) if content else {}
        return response.status, payload.get('data', payload)
    finally:
        conn.close()


class ByteBridge(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, container, target_port, local_port=0):
        self.container, self.target_port = container, target_port
        super().__init__(('127.0.0.1', local_port), BridgeHandler)
        threading.Thread(target=self.serve_forever, daemon=True).start()


class BridgeHandler(socketserver.BaseRequestHandler):
    def handle(self):
        proc = subprocess.Popen(['docker', 'exec', '-i', self.server.container,
                                 'nc', '-w', '120', '127.0.0.1', str(self.server.target_port)],
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        def upload():
            try:
                while data := self.request.recv(65536):
                    proc.stdin.write(data)
                    proc.stdin.flush()
            except (OSError, BrokenPipeError):
                pass
            finally:
                try:
                    proc.stdin.close()
                except OSError:
                    pass
        threading.Thread(target=upload, daemon=True).start()
        try:
            while data := os.read(proc.stdout.fileno(), 65536):
                self.request.sendall(data)
        except OSError:
            pass
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()


class HeldHTTP:
    def __init__(self, port, host, key, *, gpt, stream):
        self.first = threading.Event()
        self.done = threading.Event()
        self.status = None
        self.error = None
        self.body = bytearray()
        self.started = now()
        self.finished = None
        self.gpt, self.stream = gpt, stream
        def run():
            conn = http.client.HTTPConnection('127.0.0.1', port, timeout=120)
            try:
                body = {'model': 'gpt-5.2' if gpt else 'claude-sonnet-4-5', 'stream': stream}
                body.update({'input': 'isolated blue green runtime', 'max_output_tokens': 64} if gpt else
                            {'messages': [{'role': 'user', 'content': 'isolated blue green runtime'}], 'max_tokens': 64})
                conn.request('POST', '/v1/responses' if gpt else '/v1/messages', json.dumps(body),
                             {'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json',
                              'Host': host, 'Connection': 'close'})
                response = conn.getresponse()
                self.status = response.status
                while chunk := response.readline():
                    self.body.extend(chunk)
                    if len(self.body) > 2 << 20:
                        raise AssertionError('unexpected response size')
                    if b'response.created' in self.body or b'message_start' in self.body:
                        self.first.set()
                if not stream:
                    self.first.set()
            except Exception as error:
                self.error = type(error).__name__
            finally:
                conn.close()
                self.finished = now()
                self.done.set()
        self.thread = threading.Thread(target=run, daemon=True)
        self.thread.start()

    def assert_complete(self):
        assert self.done.wait(30), 'held HTTP did not complete'
        assert self.error is None, self.error
        assert self.status == 200, 'held HTTP not successful'
        if self.stream:
            assert (b'response.completed' if self.gpt else b'message_stop') in self.body
            assert b'event: error' not in self.body
        else:
            assert json.loads(self.body)['status'] == 'completed'
        return {'status': self.status, 'stream': self.stream, 'gpt': self.gpt,
                'started': self.started, 'finished': self.finished, 'body_sha256': hashlib.sha256(self.body).hexdigest()}


class WebSocket:
    """Small RFC6455 test client: validates handshake, masks sends, handles ping/fragmentation."""
    def __init__(self, port, host, token):
        self.socket = socket.create_connection(('127.0.0.1', port), timeout=120)
        nonce = base64.b64encode(os.urandom(16)).decode()
        request = (f'GET /v1/responses HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\n'
                   f'Connection: Upgrade\r\nSec-WebSocket-Key: {nonce}\r\nSec-WebSocket-Version: 13\r\n'
                   f'Authorization: Bearer {token}\r\n\r\n')
        self.socket.sendall(request.encode())
        raw = bytearray()
        while not raw.endswith(b'\r\n\r\n'):
            raw.extend(self.exact(1))
            assert len(raw) < 32768
        lines = raw.decode().split('\r\n')
        assert lines[0].split()[1] == '101', 'WS upgrade rejected'
        headers = dict(line.lower().split(': ', 1) for line in lines[1:] if ': ' in line)
        expected = base64.b64encode(hashlib.sha1((nonce + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
        # Header names are insensitive; values (especially accept) are not.
        accept = next(line.split(':', 1)[1].strip() for line in lines[1:] if line.lower().startswith('sec-websocket-accept:'))
        assert accept == expected
        assert headers.get('upgrade') == 'websocket'

    def exact(self, count):
        result = bytearray()
        while len(result) < count:
            chunk = self.socket.recv(count - len(result))
            if not chunk:
                raise EOFError('WebSocket closed before completion')
            result.extend(chunk)
        return bytes(result)

    def send(self, value, opcode=1):
        raw = json.dumps(value).encode() if opcode == 1 else value
        mask = os.urandom(4)
        length = len(raw)
        head = bytes([0x80 | opcode])
        head += bytes([0x80 | length]) if length < 126 else bytes([0x80 | 126]) + struct.pack('!H', length)
        self.socket.sendall(head + mask + bytes(x ^ mask[i % 4] for i, x in enumerate(raw)))

    def event(self):
        message = bytearray()
        while True:
            a, b = self.exact(2)
            assert not b & 0x80, 'server frames must not be masked'
            length = b & 127
            if length == 126:
                length = struct.unpack('!H', self.exact(2))[0]
            elif length == 127:
                length = struct.unpack('!Q', self.exact(8))[0]
            assert length <= 2 << 20
            body = self.exact(length)
            opcode = a & 15
            if opcode == 8:
                raise EOFError('WebSocket close before expected completion')
            if opcode == 9:
                self.send(body, 10)
                continue
            if opcode == 10:
                continue
            assert opcode in (0, 1)
            message.extend(body)
            if a & 0x80:
                event = json.loads(message)
                assert event.get('type') != 'error', 'actual WS error event'
                return event

    def begin_turn(self, text):
        self.send({'type': 'response.create', 'model': 'gpt-5.2', 'max_output_tokens': 64, 'input': text})
        # The real HTTP-bridge mode may emit created only after its provider
        # response arrives. Confirm admission using provider audit + Redis lease
        # while the gate is held; waiting for that event here would deadlock.

    def finish_turn(self):
        kinds = []
        while True:
            event = self.event()
            kinds.append(event['type'])
            if event['type'] == 'response.completed':
                assert event['response']['status'] == 'completed'
                return kinds

    def close(self):
        try:
            self.send(struct.pack('!H', 1000), 8)
        finally:
            self.socket.close()


class HeldH2:
    """System curl negotiates real TLS HTTP/2; full stream is retained privately."""
    def __init__(self, runtime):
        prefix = runtime.private / 'old-h2'
        self.body = prefix.with_suffix('.body')
        self.headers = prefix.with_suffix('.headers')
        config = prefix.with_suffix('.curl')
        host, port = runtime.hosts[1], runtime.ports['tls']
        body = json.dumps({'model': 'claude-sonnet-4-5', 'stream': True, 'max_tokens': 64,
                           'messages': [{'role': 'user', 'content': 'actual old HTTP2 stream across release'}]})
        fields = [('url', f'https://{host}:{port}/v1/messages'), ('resolve', f'{host}:{port}:127.0.0.1'),
                  ('cacert', str(runtime.private / 'nginx/certificate.pem')),
                  ('header', 'Authorization: Bearer ' + runtime.e['ConversionKey']),
                  ('header', 'Content-Type: application/json'), ('data-binary', body),
                  ('output', str(self.body)), ('dump-header', str(self.headers)), ('write-out', '%{http_version}'), ('noproxy', '*')]
        config.write_text('http2\nno-buffer\nsilent\nshow-error\nmax-time = 300\n' +
                          ''.join(k + ' = ' + json.dumps(v) + '\n' for k, v in fields))
        os.chmod(config, 0o600)
        self.proc = subprocess.Popen(['curl', '--config', str(config)], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.started = now()

    def first(self):
        eventually(lambda: self.body.exists() and b'message_start' in self.body.read_bytes(), seconds=15,
                   detail='actual HTTP2 initial stream event')
        assert 'HTTP/2 200' in self.headers.read_text(), 'must negotiate real HTTP2'

    def finish(self):
        stdout, stderr = self.proc.communicate(timeout=30)
        assert self.proc.returncode == 0 and stdout == b'2', 'HTTP2 stream failed or downgraded'
        assert b'message_stop' in self.body.read_bytes()
        return {'http_version': 2, 'started': self.started, 'finished': now(), 'body_sha256': sha(self.body),
                'headers_sha256': sha(self.headers), 'client_stderr_sha256': hashlib.sha256(stderr).hexdigest()}


class LateBodyHTTP:
    """Reuse an actual keepalive socket; finish its next body after cutover."""
    def __init__(self, port, host, key):
        self.body = json.dumps({'model': 'claude-sonnet-4-5', 'stream': False, 'max_tokens': 64,
                                'messages': [{'role': 'user', 'content': 'late body on original connection'}]}).encode()
        self.conn = http.client.HTTPConnection('127.0.0.1', port, timeout=120)
        self.conn.request('GET', '/health', headers={'Host': host, 'Connection': 'keep-alive'})
        prime = self.conn.getresponse()
        prime.read()
        assert prime.status == 200 and self.conn.sock is not None
        self.original_socket = self.conn.sock
        self.conn.putrequest('POST', '/v1/messages', skip_host=True)
        for key, value in {'Host': host, 'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json',
                           'Content-Length': str(len(self.body)), 'Connection': 'keep-alive'}.items():
            self.conn.putheader(key, value)
        self.conn.endheaders(self.body[:1])
        assert self.conn.sock is self.original_socket, 'late request must reuse the original keepalive socket'

    def deliver_body(self):
        self.conn.send(self.body[1:])

    def finish(self):
        response = self.conn.getresponse()
        body = response.read()
        self.conn.close()
        assert response.status == 200
        value = json.loads(body)
        assert value['type'] == 'message' and value['stop_reason'] == 'end_turn'
        return {'status': response.status, 'reused_original_keepalive_socket': True,
                'body_sha256': hashlib.sha256(body).hexdigest()}


class LateBodyH2:
    """Actual TLS H2 upload keeps one old-worker stream open across HUP."""
    def __init__(self, runtime):
        prefix = runtime.private / 'late-h2'
        self.body_path = prefix.with_suffix('.body')
        self.headers = prefix.with_suffix('.headers')
        config = prefix.with_suffix('.curl')
        host, port = runtime.hosts[1], runtime.ports['tls']
        self.body = json.dumps({'model': 'claude-sonnet-4-5', 'stream': False, 'max_tokens': 64,
                               'messages': [{'role': 'user', 'content': 'late body on original TLS H2 stream'}]}).encode()
        fields = [('url', f'https://{host}:{port}/v1/messages'), ('resolve', f'{host}:{port}:127.0.0.1'),
                  ('cacert', str(runtime.private / 'nginx/certificate.pem')),
                  ('header', 'Authorization: Bearer ' + runtime.e['ConversionKey']),
                  ('header', 'Content-Type: application/json'), ('header', 'Content-Length: ' + str(len(self.body))),
                  ('header', 'Expect:'), ('request', 'POST'), ('upload-file', '-'),
                  ('output', str(self.body_path)), ('dump-header', str(self.headers)), ('write-out', '%{http_version}'), ('noproxy', '*')]
        config.write_text('http2\nno-buffer\nsilent\nshow-error\nmax-time = 300\n' +
                          ''.join(k + ' = ' + json.dumps(v) + '\n' for k, v in fields))
        os.chmod(config, 0o600)
        self.proc = subprocess.Popen(['curl', '--config', str(config)], stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.proc.stdin.write(self.body[:1])
        self.proc.stdin.flush()

    def deliver_body(self):
        self.proc.stdin.write(self.body[1:])
        self.proc.stdin.flush()
        self.proc.stdin.close()
        self.proc.stdin = None

    def finish(self):
        stdout, stderr = self.proc.communicate(timeout=30)
        assert self.proc.returncode == 0 and stdout == b'2', 'late H2 upload failed or downgraded'
        assert 'HTTP/2 200' in self.headers.read_text()
        value = json.loads(self.body_path.read_bytes())
        assert value['type'] == 'message' and value['stop_reason'] == 'end_turn'
        return {'status': 200, 'http_version': 2, 'body_sha256': sha(self.body_path),
                'client_stderr_sha256': hashlib.sha256(stderr).hexdigest()}


def observe(nginx, watched_ports=(39080, 39081)):
    """Real Linux process and socket observer for the host-release controller."""
    info = inspect(nginx)
    assert info['Config']['Labels'].get('com.codex.local-task') == 'claude-weekly-quota'
    pids = docker('exec', nginx, 'pidof', 'nginx').split()
    records = []
    for pid in pids:
        result = command(['docker', 'exec', nginx, 'cat', '/proc/' + pid + '/stat'], check=False)
        if result.returncode:
            continue  # A graceful worker can exit between pidof and stat.
        raw = result.stdout.decode()
        fields = raw[raw.rindex(')') + 2:].split()
        records.append({'pid': int(pid), 'ppid': int(fields[1]), 'start_time': fields[19]})
    master = next(r for r in records if r['pid'] == 1)
    ports = set()
    for name in ('tcp', 'tcp6'):
        for row in docker('exec', nginx, 'cat', '/proc/net/' + name).splitlines()[1:]:
            fields = row.split()
            if fields[3] == '01':
                ports.update(p for value in fields[1:3] if (p := int(value.rsplit(':', 1)[1], 16)) in watched_ports)
    return {'master': {'pid': master['pid'], 'start_time': master['start_time']},
            'workers': [{'pid': r['pid'], 'start_time': r['start_time']} for r in records if r['ppid'] == master['pid']],
            'established_ports': sorted(ports)}


def prepare(private):
    assert not private.exists(), 'never overwrite an isolated fixture'
    assert sha(DRIVER) == DRIVER_SHA
    assert shutil.disk_usage(ROOT).free >= 2.5 * 1024**3
    private.mkdir(mode=0o700)
    build = json.loads((OUT / 'office-I6-build-F4.json').read_text())
    run = 'cwq-env-bluegreen-' + datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S')
    network = docker('network', 'create', '--internal', '--label', LABEL, run)
    write_json(private / 'prepare-state.json', {'network': network, 'network_name': run})
    env = os.environ | {'CI': 'true', 'GOMAXPROCS': '3', 'CWQ_RUNTIME_STAGE': 'I6',
          'SUB2API_TEST_POSTGRES_IMAGE': 'postgres:18.1-alpine3.23', 'CWQ_I4_PRIVATE_DIR': str(private),
          'CWQ_LOOPBACK_BRIDGE': str(PRIVATE_ROOT / 'office/loopback-bridge'),
          'CWQ_I4_IMAGE': build['r2_image'], 'CWQ_I4_SOURCE': build['source_sha256'],
          'CWQ_I4_PROVIDER_IMAGE': build['provider_tag'], 'CWQ_I4_NETWORK': run,
          'CWQ_I4_PUBLIC_MANIFEST': str(private / 'prepare-manifest.json')}
    log = private / 'prepare.log'
    with log.open('x') as handle:
        result = subprocess.run([str(DRIVER), '-test.v', '-test.parallel=1', '-test.timeout=5m',
                                 '-test.run=^TestSubscriptionQuotaI4PrepareEnvironment$'],
                                cwd=ROOT / 'backend/internal/repository', env=env,
                                stdout=handle, stderr=subprocess.STDOUT, timeout=330)
    assert result.returncode == 0, 'isolated preparation failed; private log retained'
    fixture = json.loads((private / 'synthetic-fixture.json').read_text())
    app = inspect(fixture['Application'])
    write_json(private / 'app-environment.json', app['Config']['Env'])
    docker('stop', '--time', '20', fixture['Application'])
    docker('rm', fixture['Application'])
    # The retained native prepare helper asserts the literal 18.1 image name.
    # Keep that seed identity honest; upgrade only this fresh named volume to
    # the exact same-major 18.6 runtime before application acceptance.
    old_pg = inspect(fixture['Postgres'])
    assert old_pg['Config']['Labels'].get('com.codex.local-task') == 'claude-weekly-quota'
    docker('stop', '--time', '20', fixture['Postgres'])
    docker('rm', fixture['Postgres'])
    fixture['Postgres'] = docker('run', '-d', '--pull', 'never', '--name', fixture['RunID'] + '-postgres',
            '--label', LABEL, '--network', run, '--network-alias', fixture['RunID'] + '-pg',
            '--memory', '768m', '--cpus', '1', '--mount', 'type=volume,source=' + fixture['Volume'] + ',target=/var/lib/postgresql',
            '--env', 'POSTGRES_HOST_AUTH_METHOD=trust', '--env', 'POSTGRES_DB=' + fixture['Database'], PG)
    eventually(lambda: command(['docker', 'exec', fixture['Postgres'], 'pg_isready', '-U', 'postgres'], check=False).returncode == 0,
               detail='18.6 minor upgrade ready')
    version = docker('exec', fixture['Postgres'], 'psql', '-X', '-At', '-U', 'postgres', '-d', fixture['Database'],
                     '-c', 'SHOW server_version_num')
    assert version == '180006'
    pg_sha = docker('exec', fixture['Postgres'], 'sha256sum', '/usr/local/bin/postgres').split()[0]
    assert pg_sha == '51b088c1df230893fc90182ce286d5e7caa5020ce9c01158dab462e1ec4b619a'
    write_json(private / 'synthetic-fixture.json', fixture)
    write_json(private / 'prepare-receipt.json', {'pass': True, 'stage': 'synthetic data preparation only',
               'driver_sha256': DRIVER_SHA, 'raw_sha256': sha(log), 'network': run,
               'seed_image': build['r2_image_id'], 'postgres_seed_version': '18.1',
               'postgres_acceptance_version_num': version, 'postgres_acceptance_binary_sha256': pg_sha,
               'finished': now()})
    print(json.dumps({'prepared': True, 'private': str(private), 'network': run}), flush=True)


class Runtime:
    def __init__(self, private):
        self.private = private
        self.e = json.loads((private / 'synthetic-fixture.json').read_text())
        self.bridges = []
        timeline = private / 'timeline.json'
        self.events = json.loads(timeline.read_text()) if timeline.exists() else []
        self.event_lock = threading.Lock()
        self.started = now()
        self.state_path = private / 'runtime.json'
        self.state = json.loads(self.state_path.read_text()) if self.state_path.exists() else {}
        self.hosts = ['sub2api.synthetic.invalid', 'teamsub2.synthetic.invalid']
        self.controller = ROOT / 'deploy/blue-green/scripts/host-release.py'

    def save(self):
        write_json(self.state_path, self.state)

    def event(self, kind, **values):
        with self.event_lock:
            self.events.append({'time': now(), 'event': kind, **values})
            write_json(self.private / 'timeline.json', self.events)
        print(json.dumps({'event': kind, 'time': now()}), flush=True)

    def sql(self, sql):
        return docker('exec', '-i', self.e['Postgres'], 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                      '-U', 'postgres', '-d', self.e['Database'], data=sql.encode())

    def gate(self, *, hold=None, sequence=None):
        body = {'release_sequence': sequence} if sequence else {'hold': hold}
        docker('exec', self.e['Provider'], 'wget', '-q', '-O', '-', '--header=Content-Type: application/json',
               '--post-data=' + json.dumps(body), 'http://127.0.0.1:8081/__control')

    def audit(self):
        return json.loads(docker('exec', self.e['Provider'], 'wget', '-q', '-O', '-', 'http://127.0.0.1:8081/__audit')) or []

    def wait_call(self, after):
        rows = eventually(lambda: (a if len(a := self.audit()) == after + 1 else None), detail='one actual provider admission')
        assert rows[-1]['active'] and not rows[-1]['cancelled']
        return rows[-1]['sequence']

    def bridge(self, port):
        saved_ports = self.state.setdefault('bridge_ports', {})
        bridge = ByteBridge(self.state['nginx'], port, saved_ports.get(str(port), 0))
        self.bridges.append(bridge)
        actual = bridge.server_address[1]
        saved_ports[str(port)] = actual
        self.save()
        return actual

    def start_app(self, slot, image):
        assert slot not in self.state, 'never recreate an existing release process'
        port = 39080 if slot == 'blue' else 39081
        source_env = json.loads((self.private / 'app-environment.json').read_text())
        env = dict(item.split('=', 1) for item in source_env if '=' in item)
        env.update({'SERVER_PORT': str(port), 'RELEASE_DRAIN_START_HELD': 'false',
                    'SUBSCRIPTION_QUOTA_ISSUE_ENABLED': 'false', 'SUBSCRIPTION_QUOTA_ADMISSION_ENABLED': 'false',
                    'SUBSCRIPTION_QUOTA_RECOVERY_ENABLED': 'true', 'CARPOOL_BACKGROUND_ENABLED': 'true',
                    'BATCH_IMAGE_QUEUE_ENABLED': 'false', 'REDIS_DB': '0', 'REDIS_PASSWORD': ''})
        if slot == 'green':
            env.update({'GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE': 'preserve',
                        'GATEWAY_BACKGROUND_JOB_MODE': 'coordinated', 'GATEWAY_BACKGROUND_JOBS_START_HELD': 'true'})
        argv = ['run', '-d', '--pull', 'never', '--platform', 'linux/amd64',
                '--name', self.e['RunID'] + '-bg-' + slot, '--label', LABEL,
                '--network', 'container:' + self.state['nginx'], '--read-only', '--user', '1000:1000',
                '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '--memory', '1536m', '--cpus', '2',
                '--tmpfs', '/app/data:rw,size=128m,uid=1000,gid=1000', '--tmpfs', '/tmp:rw,size=64m,uid=1000,gid=1000']
        for key, value in env.items():
            argv.extend(['--env', key + '=' + value])
        self.state[slot] = docker(*argv, image)
        self.save()
        self.event('application_started', slot=slot, id=self.state[slot], image=inspect(self.state[slot])['Image'])

    def ready(self, port):
        def check():
            code, _ = json_http(port, 'GET', '/health')
            return code == 200
        eventually(check, seconds=90, detail='real application health')
        code, _ = json_http(port, 'GET', '/v1/models')
        assert code == 401

    def login(self, port):
        code, data = json_http(port, 'POST', '/api/v1/auth/login', {'email': self.e['AdminEmail'], 'password': self.e['Password']})
        assert code == 200
        token = data['access_token']
        code, data = json_http(port, 'GET', '/api/v1/admin/compliance', token=token)
        assert code == 200
        if data.get('required'):
            code, _ = json_http(port, 'POST', '/api/v1/admin/compliance/accept',
                                {'phrase': data['ack_phrase_en'], 'language': 'en'}, token)
            assert code == 200
        path = self.private / 'admin-token'
        path.write_text(token)
        os.chmod(path, 0o600)
        self.token = token

    def status(self, slot):
        code, data = json_http(self.ports[slot], 'GET', '/api/v1/admin/release/status', token=self.token)
        assert code == 200
        return data

    def control(self, *args, expected=0):
        started = now()
        result = command([sys.executable, str(self.controller), '--config', str(self.private / 'release-config.json'), *args],
                         check=False, timeout=120)
        path = self.private / ('control-' + str(len(self.events)) + '.log')
        path.write_bytes(result.stdout + result.stderr)
        os.chmod(path, 0o600)
        self.event('controller', args=list(args), exit_code=result.returncode, log_sha256=sha(path), started=started)
        assert result.returncode == expected, 'release controller returned unexpected outcome: ' + str(result.returncode)
        return result

    def start_old(self):
        if 'nginx' not in self.state:
            nginx_dir = self.private / 'nginx'
            conf_dir = nginx_dir / 'conf.d'
            conf_dir.mkdir(parents=True, mode=0o700)
            (nginx_dir / 'status-fault.conf').write_text('')
            (nginx_dir / 'nginx.conf').write_text('''worker_processes 2;
pid /var/run/nginx.pid;
error_log /dev/stderr notice;
events { worker_connections 1024; }
http {
 log_format proof '$msec $pid $host $request_method $uri $status $upstream_addr';
 access_log /tmp/bluegreen-access.log proof;
 map $http_upgrade $proof_upgrade { default upgrade; '' ''; }
 include /proof/conf.d/*.conf;
}
''')
            for i, host in enumerate(self.hosts):
                (conf_dir / ('site-' + str(i) + '.conf')).write_text('''server {
 listen 8080;
 server_name ''' + host + ''';
 ''' + ('include /proof/status-fault.conf;' if i == 1 else '') + '''
 location / {
  proxy_pass http://127.0.0.1:39080;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header Upgrade $http_upgrade;
  proxy_set_header Connection $proof_upgrade;
  proxy_buffering off;
  proxy_request_buffering off;
  proxy_read_timeout 1200s;
  proxy_send_timeout 1200s;
 }
}
''')
            self.state['nginx'] = docker('run', '-d', '--pull', 'never', '--name', self.e['RunID'] + '-bg-nginx',
                    '--label', LABEL, '--network', self.e['Network'], '--memory', '128m',
                    '--mount', 'type=bind,source=' + str(nginx_dir) + ',target=/proof,readonly',
                    NGINX, 'nginx', '-c', '/proof/nginx.conf', '-g', 'daemon off;')
            self.save()
        self.enable_h2()
        if 'blue' not in self.state:
            self.start_app('blue', R0)
        self.ports = {'blue': self.bridge(39080), 'green': self.bridge(39081), 'front': self.bridge(8080), 'tls': self.bridge(8443)}
        self.ready(self.ports['blue'])
        self.login(self.ports['blue'])
        self.event('old_ready', release_status=self.status('blue'))

    def enable_h2(self):
        directory = self.private / 'nginx'
        certificate = directory / 'certificate.pem'
        if certificate.exists():
            return
        command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
                 '-keyout', str(directory / 'key.pem'), '-out', str(certificate),
                 '-subj', '/CN=sub2api.synthetic.invalid', '-addext',
                 'subjectAltName=DNS:sub2api.synthetic.invalid,DNS:teamsub2.synthetic.invalid'])
        os.chmod(directory / 'key.pem', 0o600)
        for i in range(2):
            path = directory / ('conf.d/site-' + str(i) + '.conf')
            value = path.read_text().replace(' listen 8080;', ' listen 8080;\n listen 8443 ssl http2;\n ssl_certificate /proof/certificate.pem;\n ssl_certificate_key /proof/key.pem;')
            path.write_text(value)
        docker('exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf', '-t')
        docker('exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf', '-s', 'reload')

    def configure_controller(self, candidate_image):
        slots = {}
        for slot, port, image in [('blue', 39080, R0), ('green', 39081, candidate_image)]:
            slots[slot] = {'container': self.e['RunID'] + '-bg-' + slot, 'port': port, 'image_id': image,
                           'admin_token_file': str(self.private / 'admin-token'), 'preserve_mode_required': slot == 'green',
                           'direct_url': 'http://127.0.0.1:' + str(self.ports[slot])}
        config = {'version': 1, 'state_file': str(self.private / 'release-state.json'),
                  'upstream_file': str(self.private / 'nginx/conf.d/release-upstream.conf'),
                  'upstream_name': 'bluegreen_active', 'initial_slot': 'blue', 'slots': slots,
                  'nginx_sites': [str(self.private / ('nginx/conf.d/site-' + str(i) + '.conf')) for i in range(2)],
                  'routes': [{'url': 'http://127.0.0.1:' + str(self.ports['front']), 'host': host} for host in self.hosts],
                  'docker_command': ['docker'],
                  'nginx_command': ['docker', 'exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf'],
                  'observer_command': [sys.executable, str(Path(__file__).resolve()), 'observe', '--nginx', self.state['nginx']],
                  'inflight_evidence_command': [sys.executable, str(Path(__file__).resolve()), 'inflight', '--private', str(self.private)],
                  'poll_seconds': 0.2, 'reload_timeout_seconds': 15}
        write_json(self.private / 'release-config.json', config)
        config['slots']['blue']['legacy_background_evidence_file'] = str(self.private / 'legacy-background.json')
        write_json(self.private / 'release-config.json', config)
        self.legacy_evidence()

    def legacy_evidence(self):
        counts = json.loads(self.sql("""SELECT json_build_object(
          'scheduled_enabled',(SELECT count(*) FROM scheduled_test_plans WHERE enabled),
          'channel_enabled',(SELECT count(*) FROM channel_monitors WHERE enabled),
          'batch_jobs',(SELECT count(*) FROM batch_image_jobs WHERE status NOT IN ('completed','failed','cancelled','output_deleted') OR (status='completed' AND settled_at IS NULL)),
          'backup_running',(SELECT count(*) FROM jsonb_array_elements(coalesce((SELECT value::jsonb FROM settings WHERE key='backup_records'),'[]'::jsonb)) x WHERE x->>'status'='running'),
          'restore_running',(SELECT count(*) FROM jsonb_array_elements(coalesce((SELECT value::jsonb FROM settings WHERE key='backup_records'),'[]'::jsonb)) x WHERE x->>'restore_status'='running'),
          'backup_schedule_enabled',coalesce((SELECT (value::jsonb->>'enabled')::boolean FROM settings WHERE key='backup_schedule'),false),
          'cn_active_accounts',(SELECT count(*) FROM accounts WHERE status='active' AND deleted_at IS NULL AND platform IN ('kimi','deepseek','zhipu','minimax')));"""))
        assert counts['backup_schedule_enabled'] is False
        assert all(value == 0 for key, value in counts.items() if key != 'backup_schedule_enabled')
        identity = inspect(self.state['blue'])
        write_json(self.private / 'legacy-background.json', counts | {'version': 1, 'container_id': identity['Id'],
                'started_at': identity['State']['StartedAt'], 'image_id': identity['Image'], 'checked_at_utc': now()})

    def members(self):
        keys = [f"concurrency:user:{self.e['A']}", f"concurrency:user:{self.e['B']}",
                f"concurrency:account:{self.e['GPTAccount']}", f"concurrency:account:{self.e['Account']}",
                f"concurrency:openai_ws_ingress:api_key:{self.e['GPTKeyID']}"]
        return {key: docker('exec', self.e['Redis'], 'redis-cli', '--raw', 'ZRANGE', key, '0', '-1').splitlines() for key in keys}

    def fresh_http(self, *, gpt=True, port=None, host=None):
        before = len(self.audit())
        client = HeldHTTP(port or self.ports['front'], host or self.hosts[0],
                          self.e['GPTKey'] if gpt else self.e['ConversionKey'], gpt=gpt, stream=not gpt)
        sequence = self.wait_call(before)
        self.gate(sequence=sequence)
        value = client.assert_complete()
        self.event('fresh_request_completed', sequence=sequence, **value)
        return value

    def baseline(self):
        return json.loads(self.sql(f"""SELECT json_build_object(
          'request_max',(SELECT coalesce(max(id),0) FROM carpool_billing_requests),
          'usage_max',(SELECT coalesce(max(id),0) FROM usage_logs),
          'gpt_balance',(SELECT base_balance_usd::text FROM carpool_cycles WHERE id={self.e['GPTCycle']}),
          'native_a',(SELECT balance::text FROM users WHERE id={self.e['A']}),
          'native_b',(SELECT balance::text FROM users WHERE id={self.e['B']}),
          'claude_used',(SELECT sum(used_usd)::text FROM subscription_quota_terms));"""))

    def assert_settlement(self, before, audit_start):
        audit = self.audit()[audit_start:]
        assert audit and all(not row['active'] and not row['cancelled'] for row in audit)
        gpt = sum(row['model'] == 'gpt-5.2' for row in audit)
        claude = len(audit) - gpt
        rid, uid = before['request_max'], before['usage_max']
        eventually(lambda: self.sql(f"SELECT count(*) FROM carpool_billing_requests WHERE id>{rid} AND status='settled'") == str(gpt),
                   seconds=45, detail='every real GPT turn settled exactly once')
        eventually(lambda: self.sql(f"SELECT count(*) FROM usage_logs WHERE id>{uid}") == str(len(audit)),
                   seconds=15, detail='one usage log per completed provider call')
        evidence = json.loads(self.sql(f"""SELECT json_build_object(
          'gpt_completed',{gpt},'claude_completed',{claude},
          'new_gpt_rows',(SELECT count(*) FROM carpool_billing_requests WHERE id>{rid}),
          'original_cycle_only',NOT EXISTS(SELECT 1 FROM carpool_billing_requests WHERE id>{rid} AND cycle_id<>{self.e['GPTCycle']}),
          'gpt_cost',(SELECT sum(actual_cost_usd)::text FROM carpool_billing_requests WHERE id>{rid}),
          'gpt_balance_exact',(SELECT base_balance_usd={before['gpt_balance']}::numeric-(SELECT sum(actual_cost_usd) FROM carpool_billing_requests WHERE id>{rid}) FROM carpool_cycles WHERE id={self.e['GPTCycle']}),
          'gpt_ledger_exact',NOT EXISTS(SELECT 1 FROM carpool_billing_requests r WHERE r.id>{rid} AND (SELECT coalesce(-sum(delta_usd),0) FROM carpool_ledger l WHERE l.request_id=r.request_id AND l.api_key_id=r.api_key_id)<>r.actual_cost_usd),
          'gpt_dedup_count',(SELECT count(*) FROM usage_billing_dedup d JOIN carpool_billing_requests r ON r.request_id=d.request_id AND r.api_key_id=d.api_key_id WHERE r.id>{rid}),
          'gpt_native_unchanged',(SELECT balance={before['native_a']}::numeric FROM users WHERE id={self.e['A']}),
          'legacy_claude_balance_exact',(SELECT balance={before['native_b']}::numeric-({claude}::numeric*0.0006) FROM users WHERE id={self.e['B']}),
          'term_wallets_unchanged',(SELECT sum(used_usd)={before['claude_used']}::numeric FROM subscription_quota_terms));"""))
        assert evidence['new_gpt_rows'] == evidence['gpt_dedup_count'] == gpt
        assert all(evidence[name] is True for name in ['original_cycle_only', 'gpt_balance_exact', 'gpt_ledger_exact',
                   'gpt_native_unchanged', 'legacy_claude_balance_exact', 'term_wallets_unchanged']), 'settlement conservation failed'
        self.event('exact_settlement_verified', **evidence)
        return evidence

    def retire_with_pending_usage(self, held):
        client, sequence = held
        argv = ['docker', 'exec', '-i', self.e['Postgres'], 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                '-U', 'postgres', '-d', self.e['Database']]
        lock = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        # GPT carpool settlement is intentionally synchronous. Legacy Claude
        # uses the real usage pool; lock only its user's debit row after auth.
        lock.stdin.write(f"BEGIN; SELECT id FROM users WHERE id={self.e['B']} FOR UPDATE; SELECT 'LOCK_READY';\n".encode())
        lock.stdin.flush()
        while lock.stdout.readline().strip() != b'LOCK_READY':
            assert lock.poll() is None, 'failed to lock the isolated billing row'
        unlocked = threading.Event()
        unlock_guard = threading.Lock()
        def unlock():
            with unlock_guard:
                if not unlocked.is_set():
                    unlocked.set()
                    lock.stdin.write(b'COMMIT;\n')
                    lock.stdin.flush()
                    lock.stdin.close()
        # Real task timeout is five seconds. Keep the injected row lock below
        # that deadline; this test does not change any product timeout.
        timer = threading.Timer(4.0, unlock)
        timer.start()
        retired = threading.Event()
        failure = []
        try:
            self.gate(sequence=sequence)
            self.event('old_held_response_completed', sequence=sequence, **client.assert_complete())
            status = eventually(lambda: (v if (v := self.status('blue'))['pending_usage'] > 0 else None),
                                seconds=2, detail='actual asynchronous legacy Claude usage pending after HTTP complete')
            self.event('actual_usage_pending_after_http_complete', status=status, provider='legacy_claude',
                       user_balance_while_locked=self.sql(f"SELECT balance::text FROM users WHERE id={self.e['B']}"))
            def retire():
                try:
                    self.control('retire', 'blue', '--timeout', '40')
                except Exception as error:
                    failure.append(error)
                finally:
                    retired.set()
            threading.Thread(target=retire, daemon=True).start()
            draining = eventually(lambda: (v if (v := self.status('blue'))['state'] == 'draining' and v['pending_usage'] > 0 else None),
                                  seconds=2.5, detail='actual controller drain while usage remains pending')
            time.sleep(0.3)
            assert inspect(self.state['blue'])['State']['Running'], 'old process stopped while usage was pending'
            still_pending = self.status('blue')
            assert still_pending['pending_usage'] > 0 and not retired.is_set(), 'pending-usage refusal must be observed before settlement'
            self.event('retirement_preserved_process_during_pending_usage', first_draining_status=draining, old_status=still_pending)
            unlock()
            timer.cancel()
            assert retired.wait(60), 'retirement did not finish after releasing real billing row lock'
            if failure:
                raise failure[0]
        finally:
            unlock()
            timer.cancel()
            lock.wait(timeout=5)

    def run(self, candidate):
        assert 'green' not in self.state, 'full proof requires a fresh candidate process'
        assert shutil.disk_usage(ROOT).free >= 2.5 * 1024**3, 'insufficient isolated runtime headroom'
        self.controller_before = sha(self.controller)
        self.harness_before = sha(Path(__file__))
        collector = ROOT / 'deploy/blue-green/scripts/main-server-inflight.py'
        collector_before = sha(collector)
        candidate_meta = json.loads(docker('image', 'inspect', candidate))[0]
        candidate_id = candidate_meta['Id']
        self.start_old()
        self.configure_controller(candidate_id)
        self.control('install-route')
        before = self.baseline()
        audit_start = len(self.audit())
        self.gate(hold=True)
        held = []
        for gpt, stream, host in [(True, False, self.hosts[0]), (True, True, self.hosts[1]), (False, True, self.hosts[0])]:
            count = len(self.audit())
            client = HeldHTTP(self.ports['front'], host, self.e['GPTKey'] if gpt else self.e['ConversionKey'], gpt=gpt, stream=stream)
            sequence = self.wait_call(count)
            # OpenAI failover intentionally buffers response.created until a
            # content delta. This provider holds that delta, so prove GPT SSE
            # admission with provider audit and the real lease below; require
            # complete SSE output after release. Claude emits message_start now.
            if stream and not gpt:
                assert client.first.wait(10), 'initial SSE event must precede candidate startup'
            held.append((client, sequence))
        ws = WebSocket(self.ports['front'], self.hosts[1], self.e['GPTKey'])
        count = len(self.audit())
        ws.begin_turn('old session turn held across candidate startup')
        ws_sequence = self.wait_call(count)
        count = len(self.audit())
        h2 = HeldH2(self)
        h2_sequence = self.wait_call(count)
        h2.first()
        active_before_late = self.status('blue')['active_http']
        late = LateBodyHTTP(self.ports['front'], self.hosts[0], self.e['ConversionKey'])
        eventually(lambda: self.status('blue')['active_http'] == active_before_late + 1,
                   detail='old application receives incomplete request before cutover')
        late_h2 = LateBodyH2(self)
        eventually(lambda: self.status('blue')['active_http'] == active_before_late + 2,
                   detail='old application receives incomplete TLS H2 request before cutover')
        members = self.members()
        assert len(members[f"concurrency:user:{self.e['A']}"]) == 3
        assert len(members[f"concurrency:user:{self.e['B']}"]) == 2
        assert len(members[f"concurrency:openai_ws_ingress:api_key:{self.e['GPTKeyID']}"]) == 1
        self.event('http_sse_ws_and_real_h2_admitted_before_new_start', members=members, old_status=self.status('blue'))
        self.start_app('green', candidate_id)
        self.ready(self.ports['green'])
        assert self.members() == members, 'candidate startup deleted original ownership'
        assert all(row['active'] and not row['cancelled'] for row in self.audit()[audit_start:])
        assert self.status('green')['background_jobs']['state'] == 'held'
        self.event('candidate_startup_preserved_all_leases', old_status=self.status('blue'), new_status=self.status('green'))

        # A held business gate must reject cutover although health remains 200.
        path = self.private / 'nginx/conf.d/release-upstream.conf'
        original = path.read_bytes()
        code, drain = json_http(self.ports['green'], 'POST', '/api/v1/admin/release/drain', {}, self.token)
        assert code == 200
        self.control('switch', 'green', expected=1)
        assert path.read_bytes() == original and inspect(self.state['blue'])['State']['Running']
        code, _ = json_http(self.ports['green'], 'POST', '/api/v1/admin/release/cancel', {'operation_id': drain['operation_id']}, self.token)
        assert code == 200
        self.event('unready_candidate_refused_without_route_mutation')

        # Real proxy misroute on only the second virtual host makes process
        # identity verification fail after HUP; its old target works on rollback.
        count = len(self.audit())
        candidate_held = HeldHTTP(self.ports['green'], self.hosts[1], self.e['ConversionKey'], gpt=False, stream=True)
        candidate_sequence = self.wait_call(count)
        assert candidate_held.first.wait(10)
        fault = self.private / 'nginx/status-fault.conf'
        fault.write_text('location = /api/v1/admin/release/status { proxy_pass http://127.0.0.1:39080; }\n')
        docker('exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf', '-t')
        docker('exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf', '-s', 'reload')
        self.control('switch', 'green', expected=1)
        assert path.read_bytes() == original
        assert inspect(self.state['blue'])['State']['Running'] and inspect(self.state['green'])['State']['Running']
        assert not candidate_held.done.is_set(), 'failed cutover cancelled admitted candidate work'
        fault.write_text('')
        docker('exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf', '-s', 'reload')
        self.event('failed_post_reload_identity_restored_both_routes')
        self.gate(sequence=candidate_sequence)
        self.event('admitted_candidate_stream_survived_failed_cutover', **candidate_held.assert_complete())

        self.control('switch', 'green')
        self.fresh_http(host=self.hosts[0])
        self.fresh_http(gpt=False, host=self.hosts[1])
        new_ws = WebSocket(self.ports['front'], self.hosts[1], self.e['GPTKey'])
        count = len(self.audit())
        new_ws.begin_turn('new connection on new slot')
        seq = self.wait_call(count)
        self.gate(sequence=seq)
        self.event('new_websocket_completed', events=new_ws.finish_turn())
        new_ws.close()
        self.legacy_evidence()
        self.control('retire', 'blue', '--timeout', '1', expected=3)
        assert inspect(self.state['blue'])['State']['Running']
        assert self.status('blue')['state'] == 'open', 'deadline must preserve late old ingress'
        assert all(not client.done.is_set() for client, _ in held)
        assert h2.proc.poll() is None
        self.event('drain_deadline_kept_old_open_and_streams_alive')
        count = len(self.audit())
        late.deliver_body()
        sequence = self.wait_call(count)
        self.gate(sequence=sequence)
        self.event('late_body_on_original_http_connection_completed_after_cutover', **late.finish())
        count = len(self.audit())
        late_h2.deliver_body()
        sequence = self.wait_call(count)
        self.gate(sequence=sequence)
        self.event('late_body_on_original_tls_h2_stream_completed_after_cutover', **late_h2.finish())
        self.control('rollback', 'blue')
        self.fresh_http(gpt=False, host=self.hosts[1])
        self.control('switch', 'green')

        # Existing WS session continues on the original process after final HUP.
        self.gate(sequence=ws_sequence)
        self.event('old_websocket_first_turn_completed', events=ws.finish_turn())
        count = len(self.audit())
        ws.begin_turn('old established session second turn after cutover')
        seq = self.wait_call(count)
        self.gate(sequence=seq)
        self.event('old_websocket_second_turn_completed', events=ws.finish_turn())
        ws.close()
        self.gate(sequence=h2_sequence)
        self.event('old_http2_response_completed', **h2.finish())
        for client, seq in held[:2]:
            self.gate(sequence=seq)
            self.event('old_held_response_completed', sequence=seq, **client.assert_complete())
        self.event('before_retirement', old_status=self.status('blue'), observation=observe(self.state['nginx']))
        self.legacy_evidence()
        self.retire_with_pending_usage(held[2])
        assert not inspect(self.state['blue'])['State']['Running']
        settlement = self.assert_settlement(before, audit_start)
        self.control('activate')
        assert self.status('green')['background_jobs']['state'] == 'active'
        self.fresh_http(host=self.hosts[0])
        settlement = self.assert_settlement(before, audit_start)
        self.gate(hold=False)
        assert sha(self.controller) == self.controller_before, 'controller changed during actual proof'
        assert sha(Path(__file__)) == self.harness_before, 'harness changed during actual proof'
        assert sha(collector) == collector_before, 'in-flight collector changed during actual proof'
        access = docker('exec', self.state['nginx'], 'cat', '/tmp/bluegreen-access.log')
        (self.private / 'nginx-access.log').write_text(access)
        route_counts = {}
        for host in self.hosts:
            for port in (39080, 39081):
                count = sum(len(parts := line.split()) == 7 and parts[2] == host and
                            parts[3] in ('POST', 'GET') and parts[4] in ('/v1/responses', '/v1/messages') and
                            parts[5] in ('200', '101') and parts[6] == '127.0.0.1:' + str(port)
                            for line in access.splitlines())
                assert count > 0, 'both domains must record real completed model traffic to both slots'
                route_counts[host + ':' + str(port)] = count
        effective_nginx = command(['docker', 'exec', self.state['nginx'], 'nginx', '-c', '/proof/nginx.conf', '-T'])
        (self.private / 'nginx-effective.conf').write_bytes(effective_nginx.stdout)
        write_json(self.private / 'final-provider-audit.json', self.audit())
        processes = {}
        for slot in ('blue', 'green', 'nginx'):
            result = command(['docker', 'logs', self.state[slot]])
            (self.private / (slot + '.log')).write_bytes(result.stdout + result.stderr)
            info = inspect(self.state[slot])
            processes[slot] = {'id': info['Id'], 'image': info['Image'], 'started_at': info['State']['StartedAt'],
                               'finished_at': info['State']['FinishedAt'], 'running': info['State']['Running'],
                               'restart_policy': info['HostConfig']['RestartPolicy']['Name']}
        self.event('actual_runtime_proof_passed')
        receipt = {'pass': True, 'started': self.started, 'finished': now(), 'synthetic_only': True,
                   'production_modified': False, 'candidate_image': candidate_id, 'old_image': R0,
                   'candidate_labels': candidate_meta['Config'].get('Labels', {}),
                   'postgres_version_num': '180006', 'nginx_image': NGINX,
                   'controller_sha256': sha(self.controller), 'harness_sha256': sha(Path(__file__)),
                   'inflight_collector_sha256': sha(collector),
                   'timeline_sha256': sha(self.private / 'timeline.json'), 'private_evidence': str(self.private),
                   'processes': processes, 'completed_model_upstream_counts': route_counts,
                   'nginx_effective_sha256': sha(self.private / 'nginx-effective.conf'),
                   'nginx_access_sha256': sha(self.private / 'nginx-access.log'),
                   'provider_audit_sha256': sha(self.private / 'final-provider-audit.json'),
                   'controller_state_sha256': sha(self.private / 'release-state.json'),
                   'settlement': settlement, 'limitations': ['Synthetic HTTP/1.1, real TLS HTTP/2 and WebSocket transports; no production requests or production data',
                    'R0 upgrade exercises legacy Claude balance, not term-quota conversion',
                    'Nginx local image is arm64 1.27.1; production binary topology checked separately']}
        output = OUT / ('office-bluegreen-runtime-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '.json')
        write_json(output, receipt)
        print(json.dumps({'pass': True, 'receipt': str(output)}), flush=True)


def inflight_adapter(args):
    """Use the production collector unchanged through actual Desktop byte transport."""
    fixture = json.loads((args.private / 'synthetic-fixture.json').read_text())
    script = ROOT / 'deploy/blue-green/scripts/main-server-inflight.py'
    spec = importlib.util.spec_from_file_location('real_retirement_collector', script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    original_inspect = module.inspect
    original_redis = module.RedisReadOnly

    def shared_namespace_inspect(name):
        info = original_inspect(name)
        mode = info['HostConfig'].get('NetworkMode', '')
        if mode.startswith('container:'):
            # Docker suppresses Networks on a container sharing another's netns.
            # Resolve its actual holder; do not invent a network membership.
            holder = original_inspect(mode.split(':', 1)[1])
            assert holder['Config']['Labels'].get('com.codex.local-task') == 'claude-weekly-quota'
            info['NetworkSettings']['Networks'] = holder['NetworkSettings']['Networks']
        return info

    class RedisTransport(original_redis):
        def __init__(self, host, port):
            actual = original_inspect(fixture['Redis'])
            assert actual['Config']['Labels'].get('com.codex.local-task') == 'claude-weekly-quota'
            assert host in [value['IPAddress'] for value in actual['NetworkSettings']['Networks'].values()]
            assert port == 6379
            self.bridge = ByteBridge(fixture['Redis'], port)
            super().__init__('127.0.0.1', self.bridge.server_address[1])

        def close(self):
            super().close()
            self.bridge.shutdown()
            self.bridge.server_close()

    module.inspect = shared_namespace_inspect
    module.RedisReadOnly = RedisTransport
    result = module.collect(argparse.Namespace(container_id=args.container_id, started_at=args.started_at,
                                              postgres_container=fixture['Postgres'], redis_container=fixture['Redis']))
    write_json(args.private / 'last-inflight-evidence.json', result)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='mode', required=True)
    p = sub.add_parser('prepare')
    p.add_argument('--private', type=Path, required=True)
    p = sub.add_parser('observe')
    p.add_argument('--nginx', required=True)
    p.add_argument('--ports', default='')
    p = sub.add_parser('smoke-old')
    p.add_argument('--private', type=Path, required=True)
    p = sub.add_parser('run')
    p.add_argument('--private', type=Path, required=True)
    p.add_argument('--candidate', required=True)
    p = sub.add_parser('inflight')
    p.add_argument('--private', type=Path, required=True)
    p.add_argument('--container-id', required=True)
    p.add_argument('--started-at', required=True)
    args = parser.parse_args()
    if args.mode == 'observe':
        print(json.dumps(observe(args.nginx, tuple(int(p) for p in args.ports.split(',')) if args.ports else (39080, 39081))))
    elif args.mode == 'inflight':
        assert args.private.resolve().is_relative_to(PRIVATE_ROOT.resolve())
        print(json.dumps(inflight_adapter(args)))
    elif args.mode == 'prepare':
        assert args.private.resolve().is_relative_to(PRIVATE_ROOT.resolve())
        prepare(args.private.resolve())
    elif args.mode == 'run':
        assert args.private.resolve().is_relative_to(PRIVATE_ROOT.resolve())
        runtime = Runtime(args.private.resolve())
        try:
            runtime.run(args.candidate)
        except Exception as error:
            runtime.event('runtime_proof_failed', error_type=type(error).__name__, reason=str(error))
            write_json(OUT / ('office-bluegreen-runtime-failed-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '.json'),
                       {'pass': False, 'started': runtime.started, 'finished': now(), 'candidate': args.candidate,
                        'error_type': type(error).__name__, 'reason': str(error), 'synthetic_only': True,
                        'production_modified': False, 'private_evidence': str(runtime.private),
                        'timeline_sha256': sha(runtime.private / 'timeline.json')})
            raise
    else:
        assert args.private.resolve().is_relative_to(PRIVATE_ROOT.resolve())
        runtime = Runtime(args.private.resolve())
        runtime.start_old()
        runtime.gate(hold=True)
        runtime.fresh_http()
        runtime.fresh_http(gpt=False, host=runtime.hosts[1])
        ws = WebSocket(runtime.ports['front'], runtime.hosts[0], runtime.e['GPTKey'])
        before = len(runtime.audit())
        ws.begin_turn('original R0 WebSocket fixture smoke')
        sequence = runtime.wait_call(before)
        runtime.gate(sequence=sequence)
        events = ws.finish_turn()
        ws.close()
        before = len(runtime.audit())
        h2 = HeldH2(runtime)
        sequence = runtime.wait_call(before)
        h2.first()
        runtime.gate(sequence=sequence)
        runtime.event('old_real_http2_smoke_completed', **h2.finish())
        runtime.gate(hold=False)
        runtime.event('old_websocket_smoke_completed', events=events)
        runtime.event('old_smoke_passed')


if __name__ == '__main__':
    main()
