#!/usr/bin/env python3
"""Bounded metadata-only controller for Sub2API's Codex ticket owner."""
from __future__ import annotations

import argparse
import concurrent.futures
import contextlib
from dataclasses import dataclass
import datetime as dt
import http.client
import io
import json
import os
from pathlib import Path
import re
import signal
import socket
import sqlite3
import ssl
import sys
import threading
import time
import urllib.parse

MODEL = re.compile(r'[A-Za-z0-9][A-Za-z0-9._/-]{0,99}\Z')
VERSION = re.compile(r'(?:[a-f0-9]{64})?\Z')
REASONS = {'missing': 0, 'expiry': 1, 'turn_state_312': 2, 'model_mismatch': 3}
MAX_RESPONSE = 1024 * 1024


class SentinelError(Exception):
    """Only fixed, non-sensitive codes cross the logging boundary."""


def log(event, **fields):
    print(json.dumps({'at': dt.datetime.now(dt.timezone.utc).isoformat(),
                      'event': event, **fields}, separators=(',', ':')), flush=True)


@dataclass(frozen=True)
class Config:
    base_url: str
    token_file: str
    state_path: str
    status_path: str
    account_ids: tuple[int, ...] = (2,)
    models: tuple[str, ...] = ('gpt-6-astra', 'gpt-5.6-sol')
    poll_seconds: float = 2
    refresh_before_seconds: int = 600
    cooldown_seconds: int = 60
    max_backoff_seconds: int = 900
    max_attempts_per_hour: int = 6
    request_timeout_seconds: int = 40

    @classmethod
    def load(cls, path):
        try:
            raw = json.loads(Path(path).read_text(encoding='utf-8'))
            raw['account_ids'] = tuple(raw.get('account_ids', (2,)))
            raw['models'] = tuple(raw.get('models', ('gpt-6-astra', 'gpt-5.6-sol')))
            cfg = cls(**raw)
            cfg.validate()
            return cfg
        except (OSError, ValueError, TypeError, KeyError) as exc:
            raise SentinelError('invalid_config') from exc

    def validate(self):
        url = urllib.parse.urlsplit(self.base_url)
        if (url.scheme not in ('http', 'https') or not url.hostname or url.username
                or url.password or url.query or url.fragment or url.path not in ('', '/')):
            raise SentinelError('invalid_base_url')
        if not 1 <= len(self.account_ids) <= 32 or any(type(x) is not int or x <= 0 for x in self.account_ids):
            raise SentinelError('invalid_account_allowlist')
        if not 1 <= len(self.models) <= 8 or any(not isinstance(x, str) or not MODEL.fullmatch(x) for x in self.models):
            raise SentinelError('invalid_model_allowlist')
        limits = {'poll_seconds': (1, 60), 'refresh_before_seconds': (1, 1800),
                  'cooldown_seconds': (30, 3600), 'max_backoff_seconds': (60, 86400),
                  'max_attempts_per_hour': (1, 60), 'request_timeout_seconds': (5, 60)}
        for field, (low, high) in limits.items():
            value = getattr(self, field)
            if type(value) not in (int, float) or not low <= value <= high:
                raise SentinelError('invalid_limit')
        if type(self.max_attempts_per_hour) is not int or self.max_backoff_seconds < self.cooldown_seconds:
            raise SentinelError('invalid_limit')
        paths = [Path(p) for p in (self.token_file, self.state_path, self.status_path)]
        if any(not p.is_absolute() for p in paths) or len(set(paths)) != 3:
            raise SentinelError('invalid_state_paths')

    def permits(self, account, model):
        return account in self.account_ids and model in self.models


def remaining_time(deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError('control_deadline')
    return remaining


class DeadlineReader(io.RawIOBase):
    """Every actual header/body recv uses the remaining total request budget."""
    def __init__(self, sock, deadline):
        super().__init__()
        self.sock, self.deadline = sock, deadline
        # Preserve the normal makefile socket reference when HTTPConnection
        # closes its reference after a Connection: close response header.
        self.raw = sock.makefile('rb', buffering=0)

    def readable(self):
        return True

    def readinto(self, buffer):
        self.sock.settimeout(remaining_time(self.deadline))
        return self.raw.readinto(buffer)

    def close(self):
        try:
            self.raw.close()
        finally:
            super().close()


class DeadlineSocket:
    def __init__(self, sock, deadline):
        self.sock, self.deadline = sock, deadline

    def sendall(self, data):
        self.sock.settimeout(remaining_time(self.deadline))
        return self.sock.sendall(data)

    def makefile(self, mode):
        if mode != 'rb':
            raise ValueError('unsupported_socket_file_mode')
        return io.BufferedReader(DeadlineReader(self.sock, self.deadline))

    def close(self):
        self.sock.close()


def connect_control(url, deadline):
    """Direct one-request connection: no environment proxies or pooling.

    DNS uses the host resolver (Docker's local DNS in production). Once it
    returns, all address attempts, TLS and HTTP I/O share one deadline.
    """
    port = url.port or (443 if url.scheme == 'https' else 80)
    addresses = socket.getaddrinfo(url.hostname, port, type=socket.SOCK_STREAM)
    last_error = OSError('no_control_address')
    for family, kind, proto, _, address in addresses:
        sock = socket.socket(family, kind, proto)
        try:
            sock.settimeout(remaining_time(deadline))
            sock.connect(address)
            if url.scheme == 'https':
                context = ssl.create_default_context()
                sock = context.wrap_socket(sock, server_hostname=url.hostname,
                                           do_handshake_on_connect=False)
                sock.settimeout(remaining_time(deadline))
                sock.do_handshake()
            remaining_time(deadline)
            connection = http.client.HTTPConnection(url.hostname, port)
            connection.sock = DeadlineSocket(sock, deadline)
            return connection
        except (OSError, ValueError) as exc:
            last_error = exc
            sock.close()
    raise last_error


class Client:
    def __init__(self, cfg):
        self.cfg = cfg

    def call(self, route, payload=None):
        connection = None
        try:
            deadline = time.monotonic() + self.cfg.request_timeout_seconds
            token = Path(self.cfg.token_file).read_text(encoding='ascii').strip()
            if not 32 <= len(token) <= 256 or not re.fullmatch(r'[A-Za-z0-9_-]+', token):
                raise SentinelError('invalid_control_token')
            data = None if payload is None else json.dumps(payload, separators=(',', ':')).encode()
            connection = connect_control(urllib.parse.urlsplit(self.cfg.base_url), deadline)
            connection.request('GET' if payload is None else 'POST',
                               '/internal/codex-sentinel/' + route, body=data,
                               headers={'Authorization': 'Bearer ' + token,
                                        'Accept': 'application/json',
                                        'Content-Type': 'application/json',
                                        'Connection': 'close'})
            with connection.getresponse() as response:
                if 300 <= response.status < 400:
                    raise SentinelError('redirect_refused')
                if response.status in (401, 403):
                    raise SentinelError('control_auth_rejected')
                if response.status not in (200, 429, 503):
                    raise SentinelError('control_http_error')
                raw = response.read(MAX_RESPONSE + 1)
                remaining_time(deadline)
                if len(raw) > MAX_RESPONSE:
                    raise SentinelError('control_response_too_large')
                obj = json.loads(raw)
                if not isinstance(obj, dict):
                    raise SentinelError('control_invalid_json')
                return response.status, obj
        except SentinelError:
            raise
        except (OSError, ValueError, http.client.HTTPException) as exc:
            raise SentinelError('control_unavailable') from exc
        finally:
            if connection is not None:
                connection.close()

    def status(self, after):
        status, data = self.call('status?after=' + str(after))
        if status != 200:
            raise SentinelError('status_unavailable')
        return validate_status(data)

    def refresh(self, job):
        _, result = self.call('refresh', {k: job[k] for k in
            ('account_id', 'model', 'reason', 'expected_ticket_version', 'event_id')})
        allowed = {'refreshed', 'stale', 'disabled', 'ineligible', 'cooldown', 'failed'}
        if result.get('status') not in allowed:
            raise SentinelError('refresh_invalid_result')
        if result['status'] == 'refreshed' and (result.get('persisted') is not True or result.get('ready') is not True
                or type(result.get('account_id')) is not int or result['account_id'] != job['account_id']
                or result.get('model') != job['model']
                or not isinstance(result.get('ticket_version'), str) or not re.fullmatch('[a-f0-9]{64}', result['ticket_version'])
                or result['ticket_version'] == job['expected_ticket_version']):
            raise SentinelError('refresh_unverified')
        return result


def validate_status(obj):
    """One strict boundary decoder; no consumer reparses arbitrary payloads."""
    def require(condition):
        if not condition:
            raise SentinelError('invalid_status_contract')

    try:
        require(type(obj['protocol_version']) is int and obj['protocol_version'] == 1)
        require(isinstance(obj['instance_id'], str) and re.fullmatch(r'[A-Za-z0-9_-]{1,100}', obj['instance_id']))
        require(type(obj['enabled']) is bool)
        for key in ('latest_seq', 'oldest_seq', 'dropped_events'):
            require(type(obj[key]) is int and obj[key] >= 0)
        require(obj['oldest_seq'] <= obj['latest_seq'] + 1)
        require(isinstance(obj['targets'], list) and len(obj['targets']) <= 256)
        require(isinstance(obj['events'], list) and len(obj['events']) <= 1024)
        targets = set()
        for item in obj['targets']:
            require(type(item['account_id']) is int and item['account_id'] > 0)
            require(isinstance(item['model'], str) and MODEL.fullmatch(item['model']))
            require(type(item['eligible']) is bool and type(item['ready']) is bool)
            require(isinstance(item['ticket_version'], str) and VERSION.fullmatch(item['ticket_version']))
            require(type(item['remaining_seconds']) is int)
            key = (item['account_id'], item['model'])
            require(key not in targets)
            targets.add(key)
        previous = -1
        for item in obj['events']:
            require(type(item['seq']) is int and previous < item['seq'] <= obj['latest_seq'])
            previous = item['seq']
            require(type(item['account_id']) is int and item['account_id'] > 0)
            require(isinstance(item['model'], str) and MODEL.fullmatch(item['model']))
            require(item['kind'] in ('turn_state_312', 'model_mismatch'))
            require(isinstance(item['expected_ticket_version'], str) and VERSION.fullmatch(item['expected_ticket_version']))
        return obj
    except (KeyError, TypeError, ValueError) as exc:
        raise SentinelError('invalid_status_contract') from exc


class Store:
    def __init__(self, path, cfg, clock=time.time):
        self.cfg, self.clock = cfg, clock
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        self.db = sqlite3.connect(path, timeout=5)
        self.db.row_factory = sqlite3.Row
        self.db.executescript('''
        PRAGMA journal_mode=WAL;
        PRAGMA synchronous=FULL;
        CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS jobs (
            account_id INTEGER, model TEXT, expected_ticket_version TEXT NOT NULL,
            reason TEXT NOT NULL, event_id TEXT NOT NULL, next_at REAL NOT NULL,
            failures INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'pending',
            lease_until REAL NOT NULL DEFAULT 0, PRIMARY KEY(account_id,model));
        CREATE TABLE IF NOT EXISTS attempts (
            id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL, model TEXT NOT NULL,
            started_at REAL NOT NULL, outcome TEXT NOT NULL DEFAULT 'uncertain');
        CREATE INDEX IF NOT EXISTS attempts_target_time ON attempts(account_id,model,started_at);
        ''')
        with self.db:
            self.db.execute("UPDATE jobs SET status='pending', next_at=MAX(next_at,lease_until) WHERE status='inflight'")

    def meta(self, key, default=''):
        row = self.db.execute('SELECT value FROM meta WHERE key=?', (key,)).fetchone()
        return row[0] if row else default

    def set_meta(self, key, value):
        self.db.execute('INSERT INTO meta VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value', (key, str(value)))

    def enqueue(self, account, model, version, reason, event_id, now):
        old = self.db.execute('SELECT * FROM jobs WHERE account_id=? AND model=?', (account, model)).fetchone()
        if old and old['expected_ticket_version'] == version:
            if REASONS[reason] > REASONS[old['reason']]:
                self.db.execute('UPDATE jobs SET reason=?,event_id=? WHERE account_id=? AND model=?',
                                (reason, event_id, account, model))
            return
        self.db.execute('''INSERT OR REPLACE INTO jobs
            (account_id,model,expected_ticket_version,reason,event_id,next_at) VALUES (?,?,?,?,?,?)''',
            (account, model, version, reason, event_id, now))

    def ingest(self, snapshot):
        now = self.clock()
        cursor = int(self.meta('cursor', '0')) if self.meta('instance') == snapshot['instance_id'] else 0
        gaps = int(snapshot['oldest_seq'] > cursor + 1)
        targets = {(t['account_id'], t['model']): t for t in snapshot['targets']
                   if self.cfg.permits(t['account_id'], t['model']) and t['eligible']}
        with self.db:
            if not snapshot['enabled']:
                self.db.execute("DELETE FROM jobs WHERE status='pending'")
            else:
                for old in self.db.execute('SELECT * FROM jobs').fetchall():
                    t = targets.get((old['account_id'], old['model']))
                    if old['status'] == 'pending' and (not t or t['ticket_version'] != old['expected_ticket_version']):
                        self.db.execute('DELETE FROM jobs WHERE account_id=? AND model=?', (old['account_id'], old['model']))
                for event in snapshot['events']:
                    t = targets.get((event['account_id'], event['model']))
                    if event['seq'] <= cursor or not t or t['ticket_version'] != event['expected_ticket_version']:
                        continue
                    self.enqueue(event['account_id'], event['model'], event['expected_ticket_version'],
                                 event['kind'], snapshot['instance_id'] + ':' + str(event['seq']), now)
                for (account, model), target in targets.items():
                    if not target['ready'] or target['remaining_seconds'] <= self.cfg.refresh_before_seconds:
                        reason = 'expiry' if target['ready'] else 'missing'
                        self.enqueue(account, model, target['ticket_version'], reason, 'periodic', now)
            self.set_meta('instance', snapshot['instance_id'])
            self.set_meta('cursor', snapshot['latest_seq'])
            self.set_meta('enabled', int(snapshot['enabled']))
            self.set_meta('last_poll_at', now)
            self.set_meta('event_gaps', int(self.meta('event_gaps', '0')) + gaps)
            self.db.execute('DELETE FROM attempts WHERE started_at<?', (now - 86400,))
        return gaps

    def claim(self):
        now = self.clock()
        with self.db:
            # SELECT otherwise runs before SQLite's implicit write transaction,
            # allowing two connections to admit the same pending job.
            self.db.execute('BEGIN IMMEDIATE')
            if self.meta('enabled', '0') != '1':
                return None
            for row in self.db.execute("SELECT * FROM jobs WHERE status='pending' AND next_at<=? ORDER BY next_at,account_id,model", (now,)).fetchall():
                recent = self.db.execute('SELECT started_at FROM attempts WHERE account_id=? AND model=? AND started_at>? ORDER BY started_at',
                                         (row['account_id'], row['model'], now - 3600)).fetchall()
                due = now
                if recent:
                    due = max(due, recent[-1][0] + self.cfg.cooldown_seconds)
                if len(recent) >= self.cfg.max_attempts_per_hour:
                    due = max(due, recent[0][0] + 3601)
                if due > now:
                    self.db.execute('UPDATE jobs SET next_at=? WHERE account_id=? AND model=?', (due, row['account_id'], row['model']))
                    continue
                attempt = self.db.execute('INSERT INTO attempts(account_id,model,started_at) VALUES (?,?,?)',
                                          (row['account_id'], row['model'], now)).lastrowid
                self.db.execute("UPDATE jobs SET status='inflight',lease_until=? WHERE account_id=? AND model=?",
                                (now + self.cfg.request_timeout_seconds + self.cfg.cooldown_seconds, row['account_id'], row['model']))
                return {**dict(row), 'attempt_id': attempt}
        return None

    def finish(self, job, result):
        outcome = result.get('status', 'failed')
        success = outcome == 'refreshed' and result.get('persisted') is True and result.get('ready') is True
        if outcome == 'refreshed' and not success:
            outcome = 'failed'
        with self.db:
            self.db.execute('UPDATE attempts SET outcome=? WHERE id=?', (outcome, job['attempt_id']))
            if success or outcome in ('stale', 'disabled', 'ineligible'):
                self.db.execute('DELETE FROM jobs WHERE account_id=? AND model=? AND expected_ticket_version=?',
                                (job['account_id'], job['model'], job['expected_ticket_version']))
            else:
                failures = min(job['failures'] + 1, 16)
                retry = result.get('retry_after_seconds', 0)
                retry = retry if type(retry) is int and 0 <= retry <= 86400 else 0
                delay = max(min(self.cfg.max_backoff_seconds, self.cfg.cooldown_seconds * 2 ** (failures - 1)), retry)
                self.db.execute("UPDATE jobs SET status='pending',failures=?,next_at=?,lease_until=0 WHERE account_id=? AND model=? AND expected_ticket_version=?",
                                (failures, self.clock() + delay, job['account_id'], job['model'], job['expected_ticket_version']))
        return outcome

    def summary(self):
        return {'pending': self.db.execute("SELECT count(*) FROM jobs WHERE status='pending'").fetchone()[0],
                'inflight': self.db.execute("SELECT count(*) FROM jobs WHERE status='inflight'").fetchone()[0],
                'last_poll_at': float(self.meta('last_poll_at', '0')),
                'event_gaps': int(self.meta('event_gaps', '0')),
                'enabled': self.meta('enabled', '0') == '1'}


def write_status(path, data):
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)
    temp = target.with_suffix('.tmp')
    temp.write_text(json.dumps(data), encoding='utf-8')
    os.replace(temp, target)


@contextlib.contextmanager
def controller_lock(state_path):
    """One controller per local state volume; OS releases the lock on crash."""
    path = Path(str(Path(state_path).resolve()) + '.lock')
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('a+b') as handle:
        if path.stat().st_size == 0:
            handle.write(b'\0')
            handle.flush()
        handle.seek(0)
        try:
            if os.name == 'nt':
                import msvcrt
                msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as exc:
            raise SentinelError('controller_already_running') from exc
        try:
            yield
        finally:
            handle.seek(0)
            if os.name == 'nt':
                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
            # Never unlink this file: a waiting process may hold its inode.


def run(cfg):
    os.umask(0o077)
    with controller_lock(cfg.state_path):
        run_locked(cfg)


def run_locked(cfg):
    store, client = Store(cfg.state_path, cfg), Client(cfg)
    stopping = threading.Event()
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, lambda *_: stopping.set())
    executor = concurrent.futures.ThreadPoolExecutor(max_workers=1, thread_name_prefix='refresh')
    future, job = None, None
    last_error = None
    try:
        while not stopping.is_set():
            if future is not None and future.done():
                try:
                    result = future.result()
                except Exception:
                    result = {'status': 'failed'}
                outcome = store.finish(job, result)
                log('refresh_result', account_id=job['account_id'], model=job['model'], outcome=outcome)
                future, job = None, None
            try:
                snapshot = client.status(int(store.meta('cursor', '0')))
                if store.meta('instance') and snapshot['instance_id'] != store.meta('instance'):
                    snapshot = client.status(0)
                gaps = store.ingest(snapshot)
                if gaps:
                    log('event_gap', latest_seq=snapshot['latest_seq'], oldest_seq=snapshot['oldest_seq'])
                if last_error:
                    log('control_recovered')
                last_error = None
                if future is None:
                    job = store.claim()
                    if job:
                        log('refresh_started', account_id=job['account_id'], model=job['model'], reason=job['reason'])
                        future = executor.submit(client.refresh, job)
            except SentinelError as exc:
                code = str(exc)
                if code != last_error:
                    log('control_error', code=code)
                last_error = code
            write_status(cfg.status_path, {'running': True, 'updated_at': time.time(),
                                           'control_error': last_error, **store.summary()})
            stopping.wait(cfg.poll_seconds)
    finally:
        executor.shutdown(wait=True, cancel_futures=True)
        if future is not None and future.done() and not future.cancelled():
            try:
                store.finish(job, future.result())
            except Exception:
                pass  # Persisted lease prevents an immediate uncertain retry after restart.
        write_status(cfg.status_path, {'running': False, 'updated_at': time.time(), **store.summary()})
        store.db.close()


def healthcheck(path, max_age):
    try:
        status = json.loads(Path(path).read_text())
        age = time.time() - status['last_poll_at']
        return status.get('running') is True and status.get('control_error') is None and 0 <= age < max_age
    except (OSError, ValueError, KeyError, TypeError):
        return False


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config')
    parser.add_argument('--healthcheck')
    parser.add_argument('--max-age', type=float, default=30)
    args = parser.parse_args()
    if args.healthcheck:
        return 0 if healthcheck(args.healthcheck, args.max_age) else 1
    if not args.config:
        parser.error('--config is required')
    try:
        run(Config.load(args.config))
    except SentinelError as exc:
        log('fatal', code=str(exc))
        return 1
    except Exception:
        log('fatal', code='runtime_failure')
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
