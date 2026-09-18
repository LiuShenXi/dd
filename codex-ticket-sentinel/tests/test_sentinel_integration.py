"""Local-only HTTP/controller contract tests; all credentials and tickets are synthetic."""
import contextlib
from dataclasses import replace
import http.server
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

MODULE_PATH = Path(__file__).resolve().parents[1] / 'sentinel.py'
SPEC = importlib.util.spec_from_file_location('sentinel_integration_subject', MODULE_PATH)
s = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = s
SPEC.loader.exec_module(s)

TOKEN = 'synthetic_local_only_control_token_00000000'
MODEL = 'gpt-6-astra'
OLD, NEW = 'a' * 64, 'b' * 64


@contextlib.contextmanager
def backend(responder):
    received = []

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def handle_request(self):
            length = int(self.headers.get('Content-Length', '0'))
            body = self.rfile.read(length) if length else b''
            received.append({'method': self.command, 'path': self.path,
                             'auth': self.headers.get('Authorization'),
                             'body': json.loads(body) if body else None})
            responder(self)

        do_GET = handle_request
        do_POST = handle_request

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    worker = threading.Thread(target=server.serve_forever, kwargs={'poll_interval': 0.02}, daemon=True)
    worker.start()
    try:
        yield 'http://127.0.0.1:' + str(server.server_port), received
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=2)


def reply(handler, payload, code=200, headers=None):
    data = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
    handler.send_response(code)
    handler.send_header('Content-Length', str(len(data)))
    handler.send_header('Content-Type', 'application/json')
    for key, value in (headers or {}).items():
        handler.send_header(key, value)
    handler.end_headers()
    handler.wfile.write(data)


def snapshot(kind='model_mismatch', version=OLD, event_version=OLD):
    return {'protocol_version': 1, 'instance_id': 'local-instance', 'enabled': True,
            'latest_seq': 1, 'oldest_seq': 1, 'dropped_events': 0,
            'events': [{'seq': 1, 'account_id': 2, 'model': MODEL, 'kind': kind,
                        'expected_ticket_version': event_version, 'actual_model': 'gpt-5.6-luna'}],
            'targets': [{'account_id': 2, 'model': MODEL, 'eligible': True,
                         'ready': True, 'ticket_version': version, 'remaining_seconds': 3600}]}


class HTTPIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.token = root / 'token'
        self.token.write_text(TOKEN, encoding='ascii')
        self.cfg = s.Config(base_url='http://127.0.0.1', token_file=str(self.token),
                            state_path=str(root / 'state.sqlite'), status_path=str(root / 'status.json'))

    def test_auth_token_is_read_from_file_on_each_call_and_never_written(self):
        with backend(lambda h: reply(h, snapshot())) as (url, requests):
            client = s.Client(replace(self.cfg, base_url=url))
            client.status(0)
            rotated = TOKEN + '_rotated'
            self.token.write_text(rotated, encoding='ascii')
            client.status(1)
            self.assertEqual([r['auth'] for r in requests], ['Bearer ' + TOKEN, 'Bearer ' + rotated])
            self.assertEqual(self.token.read_text(), rotated)
            self.assertEqual([r['path'] for r in requests],
                             ['/internal/codex-sentinel/status?after=0', '/internal/codex-sentinel/status?after=1'])

    def test_cross_origin_redirects_never_forward_bearer(self):
        with backend(lambda h: reply(h, {})) as (destination, forwarded):
            for code in (301, 302, 303, 307, 308):
                with self.subTest(code=code):
                    with backend(lambda h: reply(h, b'', code, {'Location': destination + '/sink'})) as (url, _):
                        with self.assertRaisesRegex(s.SentinelError, '^redirect_refused$'):
                            s.Client(replace(self.cfg, base_url=url)).status(0)
            self.assertEqual(forwarded, [])

    def test_auth_failure_does_not_include_body_or_credentials(self):
        for code in (401, 403):
            with self.subTest(code=code):
                with backend(lambda h: reply(h, {'private': TOKEN}, code)) as (url, _):
                    with self.assertRaisesRegex(s.SentinelError, '^control_auth_rejected$'):
                        s.Client(replace(self.cfg, base_url=url)).status(0)

    def test_environment_proxy_is_not_used(self):
        with backend(lambda h: reply(h, snapshot())) as (url, requests):
            with patch.dict(os.environ, {'http_proxy': 'http://127.0.0.1:1',
                                          'HTTP_PROXY': 'http://127.0.0.1:1', 'NO_PROXY': '', 'no_proxy': ''}):
                self.assertEqual(s.Client(replace(self.cfg, base_url=url)).status(0)['protocol_version'], 1)
            self.assertEqual(len(requests), 1)

    def test_refreshed_receipt_must_match_target_and_change_version(self):
        job = {'account_id': 2, 'model': MODEL, 'reason': 'model_mismatch',
               'expected_ticket_version': OLD, 'event_id': 'local-instance:1'}
        valid = {'status': 'refreshed', 'account_id': 2, 'model': MODEL,
                 'ticket_version': NEW, 'persisted': True, 'ready': True}
        for delta in ({'account_id': 3}, {'account_id': True}, {'model': 'gpt-5.6-sol'},
                      {'ticket_version': OLD}, {'ticket_version': 'opaque-ticket'},
                      {'persisted': False}, {'ready': False}):
            with self.subTest(delta=delta):
                with backend(lambda h: reply(h, {**valid, **delta})) as (url, _):
                    with self.assertRaisesRegex(s.SentinelError, '^refresh_unverified$'):
                        s.Client(replace(self.cfg, base_url=url)).refresh(job)

    def test_response_byte_budget_is_enforced(self):
        with backend(lambda h: reply(h, b' ' * 1025)) as (url, _):
            with patch.object(s, 'MAX_RESPONSE', 1024):
                with self.assertRaisesRegex(s.SentinelError, '^control_response_too_large$'):
                    s.Client(replace(self.cfg, base_url=url)).status(0)

    def test_terminal_events_refresh_then_duplicate_snapshot_is_noop(self):
        for kind in ('model_mismatch', 'turn_state_312'):
            with self.subTest(kind=kind):
                def responder(h):
                    if h.command == 'GET':
                        reply(h, snapshot(kind))
                    else:
                        reply(h, {'status': 'refreshed', 'account_id': 2, 'model': MODEL,
                                  'ticket_version': NEW, 'persisted': True, 'ready': True})
                with backend(responder) as (url, requests):
                    cfg = replace(self.cfg, base_url=url, state_path=self.cfg.state_path + kind)
                    client, store = s.Client(cfg), s.Store(cfg.state_path, cfg)
                    try:
                        store.ingest(client.status(0))
                        job = store.claim()
                        self.assertIsNotNone(job)
                        self.assertEqual(store.finish(job, client.refresh(job)), 'refreshed')
                        store.ingest(client.status(1))
                        self.assertIsNone(store.claim())
                        posts = [r for r in requests if r['method'] == 'POST']
                        self.assertEqual(len(posts), 1)
                        self.assertEqual(posts[0]['body'], {'account_id': 2, 'model': MODEL,
                            'reason': kind, 'expected_ticket_version': OLD, 'event_id': 'local-instance:1'})
                    finally:
                        store.db.close()

    def test_stale_event_does_not_refresh_new_ticket(self):
        with backend(lambda h: reply(h, snapshot(version=NEW))) as (url, requests):
            cfg = replace(self.cfg, base_url=url)
            store = s.Store(cfg.state_path, cfg)
            try:
                store.ingest(s.Client(cfg).status(0))
                self.assertIsNone(store.claim())
                self.assertEqual(store.meta('cursor'), '1')
                self.assertFalse(any(r['method'] == 'POST' for r in requests))
            finally:
                store.db.close()

    def test_server_stale_result_completes_attempt_without_retry(self):
        def responder(h):
            reply(h, snapshot() if h.command == 'GET' else {'status': 'stale'})
        with backend(responder) as (url, requests):
            cfg = replace(self.cfg, base_url=url)
            client, store = s.Client(cfg), s.Store(cfg.state_path, cfg)
            try:
                store.ingest(client.status(0))
                job = store.claim()
                self.assertEqual(store.finish(job, client.refresh(job)), 'stale')
                self.assertEqual(store.summary()['pending'], 0)
                self.assertEqual(store.summary()['inflight'], 0)
                self.assertEqual(len([r for r in requests if r['method'] == 'POST']), 1)
            finally:
                store.db.close()

    def test_continuous_response_bytes_cannot_extend_request_deadline(self):
        def drip(h):
            h.send_response(200)
            h.send_header('Content-Length', '10')
            h.end_headers()
            try:
                for _ in range(8):
                    h.wfile.write(b' ')
                    h.wfile.flush()
                    time.sleep(0.075)
                h.wfile.write(b'{}')
            except OSError:
                pass
        with backend(drip) as (url, _):
            client = s.Client(replace(self.cfg, base_url=url, request_timeout_seconds=0.2))
            started = time.monotonic()
            try:
                client.call('status?after=0')
            except s.SentinelError:
                pass
            self.assertLess(time.monotonic() - started, 0.45,
                            'A socket inactivity timeout is not a total request deadline')

    def test_continuous_header_bytes_cannot_extend_request_deadline(self):
        def drip_headers(h):
            try:
                h.wfile.write(b'HTTP/1.1 200 OK\r\nX-Slow: ')
                for _ in range(8):
                    h.wfile.write(b'a')
                    h.wfile.flush()
                    time.sleep(0.075)
                h.wfile.write(b'\r\nContent-Length: 2\r\n\r\n{}')
            except OSError:
                pass
        with backend(drip_headers) as (url, _):
            client = s.Client(replace(self.cfg, base_url=url, request_timeout_seconds=0.2))
            started = time.monotonic()
            with self.assertRaisesRegex(s.SentinelError, '^control_unavailable$'):
                client.call('status?after=0')
            self.assertLess(time.monotonic() - started, 0.45)


if __name__ == '__main__':
    unittest.main()
