"""Background recovery guards with real durable writes and HTTP refresh boundary."""
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timedelta, timezone
import fcntl
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import threading
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


reconcile = module('background_reconcile', 'background-reconcile.py')
fixtures = module('release_fixtures', 'test_host_release.py')
host = reconcile.host


class ReconcileTests(unittest.TestCase):
    def setUp(self):
        fixture = fixtures.HostReleaseTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        fixture.controller.install()
        fixture.controller.release('green', 3)
        self.c, self.sys = fixture.c, fixture.sys
        self.config = fixture.config_path
        self.config.write_text(json.dumps(self.c))
        self.now = datetime.now(timezone.utc)
        self.pair_path = Path(fixture.tmp.name) / 'pair.json'
        self.pair = {'version': 1, 'access_token': 'synthetic-access-secret', 'refresh_token': 'rt_synthetic-refresh-secret', 'expires_at': (self.now + timedelta(hours=1)).isoformat()}
        self.write_pair()
        self.sys.statuses['green']['background_jobs']['state'] = 'held'
        self.sys.events = []
        self.requests = []
        self.refresh_code = 200
        self.refresh_reply = {'data': {'access_token': 'rotated-access-secret', 'refresh_token': 'rt_rotated-refresh-secret', 'expires_in': 3600, 'token_type': 'Bearer'}}
        self.status_401 = 0
        self.after_refresh = None
        request = self.sys.request
        def fake_request(base, path, token=None, host=None, body=None):
            self.requests.append((base, path, token, body))
            if path == '/api/v1/auth/refresh':
                if self.after_refresh:
                    self.after_refresh()
                return self.refresh_code, json.dumps(self.refresh_reply).encode()
            if path.endswith('/release/status') and self.status_401:
                self.status_401 -= 1
                return 401, b''
            return request(base, path, token, host, body)
        self.sys.request = fake_request

    def write_pair(self):
        self.pair_path.write_text(json.dumps(self.pair))
        self.pair_path.chmod(0o600)

    def run_reconcile(self, mode='reconcile'):
        return reconcile.run(str(self.config), str(self.pair_path), mode, self.sys)

    def refresh_requests(self):
        return [r for r in self.requests if r[1] == '/api/v1/auth/refresh']

    def activate_requests(self):
        return [r for r in self.requests if r[1].endswith('/background/activate')]

    def state(self):
        return json.loads(Path(self.c['state_file']).read_text())

    def save_state(self, state):
        Path(self.c['state_file']).write_text(json.dumps(state))

    def test_held_exact_current_instance_is_activated_without_other_mutations(self):
        result = self.run_reconcile()
        self.assertEqual(result['action'], 'background_activated')
        self.assertEqual(self.activate_requests()[0][3], {'instance_id': 'instance-green'})
        self.assertFalse(self.refresh_requests())
        self.assertFalse([e for e in self.sys.events if e[0] == 'run'])
        self.assertFalse([r for r in self.requests if r[3] is not None and not r[1].endswith('/background/activate')])
        self.assertEqual(self.state()['phase'], 'active')

    def test_already_active_does_not_post_or_rewrite_controller_state(self):
        self.sys.statuses['green']['background_jobs']['state'] = 'active'
        before = Path(self.c['state_file']).read_bytes()
        self.assertEqual(self.run_reconcile()['action'], 'already_active')
        self.assertFalse(self.activate_requests())
        self.assertEqual(Path(self.c['state_file']).read_bytes(), before)

    def test_incomplete_phases_and_transition_skip_before_credentials_or_network(self):
        original = self.state()
        self.pair_path.unlink()
        for phase in ('waiting_ingress', 'retiring', 'retirement_deferred', 'background_activate_requested', 'uninstalled'):
            with self.subTest(phase=phase):
                state = dict(original, phase=phase)
                self.save_state(state)
                self.assertEqual(self.run_reconcile()['action'], 'skipped_incomplete_release')
        self.save_state(dict(original, transition={'to': 'green'}))
        self.assertEqual(self.run_reconcile()['action'], 'skipped_incomplete_release')
        self.assertFalse(self.requests)

    def test_old_process_must_be_exact_stopped_and_disarmed(self):
        info = self.sys.info['blue']
        for field, value in [('Id', 'replacement'), ('StartedAt', 'new-start'), ('Running', True), ('restart', 'always')]:
            with self.subTest(field=field):
                if field == 'Id':
                    original = info[field]; info[field] = value
                elif field == 'restart':
                    original = info['HostConfig']['RestartPolicy']['Name']; info['HostConfig']['RestartPolicy']['Name'] = value
                else:
                    original = info['State'][field]; info['State'][field] = value
                with self.assertRaises(host.ReleaseError): self.run_reconcile()
                if field == 'Id': info[field] = original
                elif field == 'restart': info['HostConfig']['RestartPolicy']['Name'] = original
                else: info['State'][field] = original
        self.assertFalse(self.requests)

    def test_retirement_record_mismatch_refuses_before_network(self):
        state = self.state()
        state['retirements']['blue']['identity']['container_id'] = 'foreign'
        self.save_state(state)
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertFalse(self.requests)

    def test_late_old_identity_change_blocks_activation_after_route_probes(self):
        def changed(name, action, value):
            if name == 'green': self.sys.info['blue']['Id'] = 'replacement-old'
        self.sys.status_hook = changed
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertFalse(self.activate_requests())

    def test_restarted_preserve_candidate_activates_its_new_process_id(self):
        self.sys.info['green']['State']['StartedAt'] = '2026-10-21T00:00:00Z'
        self.sys.statuses['green']['background_jobs']['instance_id'] = 'new-process-instance'
        self.run_reconcile()
        self.assertEqual(self.activate_requests()[0][3], {'instance_id': 'new-process-instance'})
        self.assertEqual(self.state()['background_activation']['identity']['started_at'], '2026-10-21T00:00:00Z')

    def test_candidate_flags_image_or_process_restart_during_checks_refuse(self):
        info = self.sys.info['green']
        env = list(info['Config']['Env'])
        info['Config']['Env'] = [x for x in env if not x.startswith('GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE=')]
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertFalse(self.requests)
        info['Config']['Env'] = env
        def changed(name, action, value):
            if name == 'green': info['State']['StartedAt'] = 'restarted'
        self.sys.status_hook = changed
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertFalse(self.activate_requests())

    def test_http_migration_and_drain_never_activate_or_cancel(self):
        for state in ('draining', 'migrating', 'refreshing'):
            with self.subTest(state=state):
                self.sys.statuses['green']['state'] = state
                self.assertEqual(self.run_reconcile()['action'], 'skipped_http_gate')
        self.assertFalse([r for r in self.requests if r[3] is not None])
        self.assertEqual(self.state()['phase'], 'active')

    def test_both_domain_identity_must_match_before_activation(self):
        self.sys.wrong_route = True
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertFalse(self.activate_requests())

    def test_expiring_pair_rotates_only_through_direct_loopback_before_activation(self):
        self.pair['expires_at'] = (self.now + timedelta(seconds=30)).isoformat(); self.write_pair()
        self.assertEqual(self.run_reconcile()['action'], 'background_activated')
        request = self.refresh_requests()[0]
        self.assertEqual(request, ('http://127.0.0.1:30081', '/api/v1/auth/refresh', None, {'refresh_token': self.pair['refresh_token']}))
        persisted = json.loads(self.pair_path.read_text())
        self.assertEqual(persisted['access_token'], 'rotated-access-secret')
        self.assertEqual(Path(self.c['slots']['green']['admin_token_file']).read_text(), 'rotated-access-secret\n')
        self.assertEqual(self.pair_path.stat().st_mode & 0o777, 0o600)

    def test_authenticated_401_refreshes_once_and_retries_with_new_bearer(self):
        self.status_401 = 1
        self.run_reconcile()
        self.assertEqual(len(self.refresh_requests()), 1)
        self.assertEqual(self.activate_requests()[0][2], 'rotated-access-secret')

    def test_repeated_401_fails_without_second_rotation_or_activation(self):
        self.status_401 = 2
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertEqual(len(self.refresh_requests()), 1)
        self.assertFalse(self.activate_requests())

    def test_refresh_failure_and_uncertain_result_never_reuse_old_token(self):
        self.pair['expires_at'] = self.now.isoformat(); self.write_pair()
        self.refresh_code = 503
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertTrue(json.loads(self.pair_path.read_text())['refresh_pending'])
        self.refresh_code = 200
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertEqual(len(self.refresh_requests()), 1)
        self.assertFalse(self.activate_requests())

    def test_refresh_intent_is_durable_before_http_and_error_contains_no_credentials(self):
        self.pair['expires_at'] = self.now.isoformat(); self.write_pair()
        def lost_response():
            self.assertTrue(json.loads(self.pair_path.read_text())['refresh_pending'])
            raise OSError('synthetic-access-secret rt_synthetic-refresh-secret')
        self.after_refresh = lost_response
        with patch.object(reconcile.host, 'System', return_value=self.sys):
            out, error = io.StringIO(), io.StringIO()
            with redirect_stdout(out), redirect_stderr(error):
                result = reconcile.main(['--config', str(self.config), '--token-pair', str(self.pair_path)])
        self.assertEqual(result, 1)
        self.assertNotIn('synthetic-access-secret', out.getvalue() + error.getvalue())
        self.assertNotIn('rt_synthetic', out.getvalue() + error.getvalue())
        self.assertFalse(self.activate_requests())

    def test_crash_between_pair_and_token_write_repairs_without_another_refresh(self):
        self.pair['expires_at'] = self.now.isoformat(); self.write_pair()
        atomic_write = host.atomic_write
        token_path = self.c['slots']['green']['admin_token_file']
        def fail_token(path, data, mode=0o600):
            if str(path) == token_path: raise OSError('synthetic write failure')
            return atomic_write(path, data, mode)
        with patch.object(host, 'atomic_write', side_effect=fail_token):
            with self.assertRaises(OSError): self.run_reconcile()
        self.assertNotIn('refresh_pending', json.loads(self.pair_path.read_text()))
        self.assertEqual(self.run_reconcile()['action'], 'background_activated')
        self.assertEqual(len(self.refresh_requests()), 1)
        self.assertEqual(Path(token_path).read_text(), 'rotated-access-secret\n')

    def test_lost_rotated_pair_write_requires_login_without_reusing_refresh_token(self):
        self.pair['expires_at'] = self.now.isoformat(); self.write_pair()
        atomic_write = host.atomic_write
        def fail_pair(path, data, mode=0o600):
            if str(path) == str(self.pair_path) and b'refresh_pending' not in data:
                raise OSError('synthetic durability failure')
            return atomic_write(path, data, mode)
        with patch.object(host, 'atomic_write', side_effect=fail_pair):
            with self.assertRaises(OSError): self.run_reconcile()
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.assertEqual(len(self.refresh_requests()), 1)
        self.assertFalse(self.activate_requests())

    def test_invalid_refresh_payloads_preserve_uncertain_marker_and_refuse_activation(self):
        self.pair['expires_at'] = self.now.isoformat()
        original = dict(self.refresh_reply['data'])
        for change in ({'access_token': None}, {'refresh_token': ''}, {'expires_in': True}, {'expires_in': 0}, {'token_type': 'other'}):
            with self.subTest(change=change):
                self.write_pair()
                self.refresh_reply = {'data': {**original, **change}}
                with self.assertRaises(host.ReleaseError): self.run_reconcile()
                self.assertTrue(json.loads(self.pair_path.read_text())['refresh_pending'])
        self.assertFalse(self.activate_requests())

    def test_missing_malformed_or_insecure_pair_refuses(self):
        for changes in ({'version': True}, {'access_token': ''}, {'refresh_token': None}, {'expires_at': 'nope'}, {'expires_at': '2026-10-20T00:00:00'}, {'refresh_pending': True}):
            with self.subTest(changes=changes):
                self.pair_path.write_text(json.dumps({**self.pair, **changes}))
                with self.assertRaises((host.ReleaseError, ValueError)): self.run_reconcile()
        self.write_pair(); self.pair_path.chmod(0o644)
        with self.assertRaises(host.ReleaseError): self.run_reconcile()
        self.pair_path.unlink()
        with self.assertRaises(OSError): self.run_reconcile()
        self.assertFalse(self.requests)

    def test_refresh_only_command_never_activates_held_background(self):
        self.assertEqual(self.run_reconcile('refresh')['action'], 'token_current')
        self.pair['expires_at'] = self.now.isoformat(); self.write_pair()
        self.assertEqual(self.run_reconcile('refresh')['action'], 'token_refreshed')
        self.assertFalse(self.activate_requests())
        self.assertEqual(self.sys.statuses['green']['background_jobs']['state'], 'held')

    def test_token_pair_cannot_alias_access_token_or_be_a_symlink(self):
        with self.assertRaises(host.ReleaseError):
            reconcile.run(str(self.config), self.c['slots']['green']['admin_token_file'], system=self.sys)
        link = self.pair_path.with_name('link.json'); link.symlink_to(self.pair_path)
        with self.assertRaises(host.ReleaseError):
            reconcile.run(str(self.config), str(link), system=self.sys)
        self.assertFalse(self.requests)

    def test_real_shared_flock_excludes_timer_before_pair_read(self):
        with open(self.c['state_file'] + '.lock', 'a+b') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = subprocess.run([sys.executable, str(HERE / 'background-reconcile.py'), '--config', str(self.config), '--token-pair', str(self.pair_path)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(json.loads(result.stdout)['action'], 'skipped_release_locked')
        self.assertNotIn('secret', result.stdout + result.stderr)
        self.assertFalse(self.requests)

    def test_refresh_holds_same_controller_lock_through_rotation(self):
        self.pair['expires_at'] = self.now.isoformat(); self.write_pair()
        def during_refresh():
            with open(self.c['state_file'] + '.lock', 'a+b') as lock:
                with self.assertRaises(BlockingIOError): fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.after_refresh = during_refresh
        self.run_reconcile()


class HTTPBoundaryTests(unittest.TestCase):
    def test_real_refresh_http_uses_post_json_without_bearer_or_redirect_follow(self):
        requests = []
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_POST(self):
                body = self.rfile.read(int(self.headers['Content-Length']))
                requests.append((self.path, self.headers.get('Authorization'), json.loads(body)))
                self.send_response(302)
                self.send_header('Location', '/must-not-follow')
                self.end_headers()
            def do_GET(self):
                requests.append(('UNEXPECTED_REDIRECT', None, None))
                self.send_response(200); self.end_headers()
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            status, _ = host.System().request(f'http://127.0.0.1:{server.server_port}', '/api/v1/auth/refresh', body={'refresh_token': 'rt_synthetic'})
            self.assertEqual(status, 302)
            self.assertEqual(requests, [('/api/v1/auth/refresh', None, {'refresh_token': 'rt_synthetic'})])
        finally:
            server.shutdown(); server.server_close(); thread.join()


if __name__ == '__main__':
    unittest.main()
