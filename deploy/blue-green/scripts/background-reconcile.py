#!/usr/bin/env python3
"""Reconcile only background ownership after a completed host release."""
import argparse
from datetime import datetime, timedelta, timezone
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import sys

_spec = importlib.util.spec_from_file_location('host_release', Path(__file__).with_name('host-release.py'))
host = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(host)


def token_text(value):
    return isinstance(value, str) and 0 < len(value) <= 32768 and all(33 <= ord(c) <= 126 for c in value)


def pair_read(path):
    value = json.loads(host.read_file(path, private=True))
    host.require(isinstance(value, dict) and type(value.get('version')) is int and value['version'] == 1, 'Invalid token-pair version')
    host.require(token_text(value.get('access_token')) and token_text(value.get('refresh_token')) and value['refresh_token'].startswith('rt_'), 'Missing or invalid token pair')
    host.require(isinstance(value.get('expires_at'), str), 'Missing token expiry')
    expires = datetime.fromisoformat(value['expires_at'].replace('Z', '+00:00'))
    host.require(expires.tzinfo is not None and expires.utcoffset() == timedelta(0), 'Token expiry must be UTC with timezone')
    host.require(type(value.get('refresh_pending', False)) is bool, 'Invalid token refresh state')
    host.require(not value.get('refresh_pending', False), 'Uncertain previous token refresh; replace the pair through normal login')
    return {k: value[k] for k in ('version', 'access_token', 'refresh_token', 'expires_at')}, expires


class TokenSession:
    def __init__(self, controller, system, pair_path, identity, now=None):
        self.controller, self.system, self.path = controller, system, pair_path
        self.active = controller.state['active_slot']
        self.identity = identity
        self.now = now or (lambda: datetime.now(timezone.utc))
        self.pair, self.expires = pair_read(pair_path)
        self.refreshed = False

    def sync_access(self):
        # The pair is authoritative. Repair a crash between the two atomic writes
        # without consuming the newly rotated refresh token a second time.
        path = self.controller.slot(self.active)['admin_token_file']
        expected = (self.pair['access_token'] + '\n').encode()
        if Path(path).exists():
            current = host.read_file(path, private=True)
            if current == expected:
                return
        host.atomic_write(path, expected, 0o600)

    def prepare(self):
        if self.expires <= self.now() + timedelta(seconds=120):
            self.refresh()
        else:
            self.sync_access()

    def refresh(self):
        host.require(not self.refreshed, 'Authentication failed after refresh; normal login required')
        self.controller.identity(self.active, self.identity)
        # Refresh tokens rotate on the server. A lost response cannot be retried
        # safely. Persist intent before sending and leave it on every failure.
        host.atomic_write(self.path, (json.dumps({**self.pair, 'refresh_pending': True}) + '\n').encode(), 0o600)
        self.refreshed = True
        started = self.now()
        code, raw = self.system.request(self.controller.direct(self.active), '/api/v1/auth/refresh', body={'refresh_token': self.pair['refresh_token']})
        host.require(code == 200, 'Token refresh refused; normal login required')
        value = json.loads(raw)['data']
        host.require(isinstance(value, dict) and value.get('token_type') == 'Bearer', 'Invalid refresh response; normal login required')
        host.require(token_text(value.get('access_token')) and token_text(value.get('refresh_token')) and value['refresh_token'].startswith('rt_'), 'Invalid rotated tokens; normal login required')
        host.require(type(value.get('expires_in')) is int and 0 < value['expires_in'] <= 2147483647, 'Invalid refreshed token lifetime')
        self.expires = started + timedelta(seconds=value['expires_in'])
        self.pair = {'version': 1, 'access_token': value['access_token'], 'refresh_token': value['refresh_token'], 'expires_at': self.expires.isoformat()}
        host.atomic_write(self.path, (json.dumps(self.pair) + '\n').encode(), 0o600)
        self.sync_access()


class AuthenticatedSystem:
    """Delegate existing probes/inspection; refresh only an authenticated 401."""
    def __init__(self, system, session):
        self.system, self.session = system, session

    def __getattr__(self, name):
        return getattr(self.system, name)

    def request(self, base, path, token=None, host=None, body=None):
        response = self.system.request(base, path, token=token, host=host, body=body)
        if response[0] == 401 and token is not None and path.startswith('/api/v1/admin/release/'):
            self.session.refresh()
            response = self.system.request(base, path, token=self.session.pair['access_token'], host=host, body=body)
        return response


def verify_retired(controller):
    state, active = controller.state, controller.state['active_slot']
    cut = state.get('cutover', {})
    host.require(cut.get('to') == active and cut.get('from') in controller.c['slots'] and cut['from'] != active, 'No completed cutover to reconcile')
    old = cut['from']
    record = state['retirements'].get(old, {})
    host.require(record.get('phase') == 'stopped' and record.get('identity') == cut.get('old_identity'), 'Old retirement identity is not confirmed')
    actual = controller.inspect(old, running=False)
    expected = record['identity']
    host.require(all(actual[k] == expected[k] for k in ('container_id', 'started_at', 'image_id')), 'Old process identity changed; reconciliation refused')
    host.require(not actual['running'] and actual['restart_policy'] == 'no', 'Old process must remain stopped with restart disabled')


def reconcile_locked(controller, pair_path, mode='reconcile', now=None):
    state = controller.state
    if state.get('phase') != 'active' or state.get('transition') is not None:
        return {'ok': True, 'action': 'skipped_incomplete_release'}
    host.require(state.get('installed') is True, 'No installed host route')
    verify_retired(controller)
    active = state['active_slot']
    host.require(controller.slot(active).get('preserve_mode_required', True), 'Background reconciliation requires preserve mode')
    identity = controller.identity(active)
    original_system = controller.sys
    session = TokenSession(controller, original_system, pair_path, identity, now)
    session.prepare()
    controller.sys = AuthenticatedSystem(original_system, session)
    try:
        status = controller.api(active)
        if status['state'] != 'open':
            return {'ok': True, 'action': 'skipped_http_gate'}
        background = status.get('background_jobs', {})
        host.require(background.get('mode') == 'coordinated', 'Coordinated background control is required')
        controller.ready(active)
        controller.routes(active, status)
        controller.identity(active, identity)
        if mode == 'refresh':
            return {'ok': True, 'action': 'token_refreshed' if session.refreshed else 'token_current'}
        if background.get('state') == 'active':
            return {'ok': True, 'action': 'already_active'}
        host.require(background.get('state') == 'held', 'Unknown background state')
        verify_retired(controller)
        controller.activate()
        return {'ok': True, 'action': 'background_activated'}
    finally:
        controller.sys = original_system


def run(config_path, pair_path, mode='reconcile', system=None):
    c = host.config_read(config_path)
    pair_path = str(Path(pair_path))
    reserved = [config_path, c['state_file'], c['state_file'] + '.lock', c['upstream_file'], *c['nginx_sites'], *(s['admin_token_file'] for s in c['slots'].values())]
    host.require(Path(pair_path).is_absolute() and Path(pair_path).resolve() not in {Path(p).resolve() for p in reserved}, 'Token pair must have a distinct absolute private path')
    # Same inode/lock protocol as the release CLI. The state check happens only
    # after acquiring this lock, and it remains held through refresh/activation.
    lock_path = Path(c['state_file'] + '.lock')
    host.require(not lock_path.is_symlink(), 'Unsafe lock path')
    with open(lock_path, 'a+b') as lock:
        os.chmod(lock_path, 0o600)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return {'ok': True, 'action': 'skipped_release_locked'}
        return reconcile_locked(host.Controller(c, system), pair_path, mode)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    parser.add_argument('--token-pair', required=True)
    parser.add_argument('mode', nargs='?', choices=('reconcile', 'refresh'), default='reconcile')
    args = parser.parse_args(argv)
    try:
        print(json.dumps(run(args.config, args.token_pair, args.mode)))
        return 0
    except (host.ReleaseError, OSError, ValueError, KeyError, TypeError, IndexError, AttributeError, OverflowError) as error:
        # Never stringify network/JSON/filesystem errors: their values can
        # contain tokens, response bodies or private paths.
        message = str(error) if isinstance(error, host.ReleaseError) else 'Background reconciliation failed; private diagnostics withheld'
        print(json.dumps({'ok': False, 'error': message}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
