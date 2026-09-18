"""Crash, concurrency and transaction boundaries using only temporary local state."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import sentinel as s

MODEL, VERSION = 'gpt-6-astra', 'a' * 64


def snapshot(enabled=True):
    return {'instance_id': 'instance-1', 'enabled': enabled,
            'latest_seq': 1, 'oldest_seq': 1, 'dropped_events': 0,
            'targets': [{'account_id': 2, 'model': MODEL, 'eligible': True,
                         'ready': True, 'ticket_version': VERSION, 'remaining_seconds': 3600}],
            'events': [{'seq': 1, 'account_id': 2, 'model': MODEL,
                        'kind': 'model_mismatch', 'expected_ticket_version': VERSION}]}


class FailureBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.cfg = s.Config('http://127.0.0.1', str(root / 'token'), str(root / 'state.sqlite'),
                           str(root / 'status.json'))
        self.store = s.Store(self.cfg.state_path, self.cfg)
        self.addCleanup(self.store.db.close)

    def test_job_and_cursor_rollback_together_on_ingest_failure(self):
        original = self.store.enqueue

        def fail_after_enqueue(*args, **kwargs):
            original(*args, **kwargs)
            raise s.SentinelError('synthetic_crash_before_commit')

        with patch.object(self.store, 'enqueue', side_effect=fail_after_enqueue):
            with self.assertRaises(s.SentinelError):
                self.store.ingest(snapshot())
        self.assertEqual(self.store.meta('cursor', '0'), '0')
        self.assertEqual(self.store.summary()['pending'], 0)
        self.store.ingest(snapshot())
        self.assertEqual(self.store.meta('cursor'), '1')
        self.assertIsNotNone(self.store.claim())

    def test_master_off_blocks_failed_inflight_retry(self):
        self.store.ingest(snapshot())
        job = self.store.claim()
        self.store.ingest(snapshot(enabled=False))
        self.store.finish(job, {'status': 'failed'})
        with patch.object(self.store, 'clock', return_value=time.time() + 3600):
            self.assertIsNone(self.store.claim())
        self.store.ingest(snapshot(enabled=False))
        self.assertEqual(self.store.summary()['pending'], 0)

    def test_two_connections_cannot_claim_the_same_job(self):
        self.store.ingest(snapshot())
        start = threading.Barrier(2)

        def worker():
            store = s.Store(self.cfg.state_path, self.cfg)
            try:
                # Before the fix, each connection could read the pending job and
                # its empty attempts before either implicit INSERT transaction.
                def slow_insert(statement):
                    if statement.startswith('INSERT INTO attempts'):
                        time.sleep(0.05)
                store.db.set_trace_callback(slow_insert)
                start.wait(timeout=3)
                return store.claim()
            finally:
                store.db.close()

        with ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(worker) for _ in range(2)]
            results = [future.result(timeout=5) for future in futures]
        self.assertEqual(sum(job is not None for job in results), 1)
        self.assertEqual(self.store.db.execute('SELECT count(*) FROM attempts').fetchone()[0], 1)

    def lock_subprocess_code(self, block=False):
        return (
            "import sys; sys.path.insert(0, sys.argv[1]); import sentinel as s\n"
            "try:\n"
            " with s.controller_lock(sys.argv[2]):\n"
            "  print('locked', flush=True)\n"
            + ("  sys.stdin.read()\n" if block else '') +
            "except s.SentinelError as exc:\n"
            " print(str(exc), flush=True); sys.exit(2)\n"
        )

    def test_second_process_is_rejected_without_touching_live_state(self):
        self.store.ingest(snapshot())
        self.store.claim()
        command = [sys.executable, '-c', self.lock_subprocess_code(),
                   str(Path(s.__file__).parent), self.cfg.state_path]
        with s.controller_lock(self.cfg.state_path):
            second = subprocess.run(command, capture_output=True, text=True, timeout=5)
            self.assertEqual(second.returncode, 2, second.stderr)
            self.assertEqual(second.stdout.strip(), 'controller_already_running')
            self.assertEqual(self.store.summary()['inflight'], 1)
        third = subprocess.run(command, capture_output=True, text=True, timeout=5)
        self.assertEqual(third.returncode, 0, third.stderr)
        self.assertEqual(third.stdout.strip(), 'locked')

    def test_process_crash_releases_lock_without_deleting_lock_file(self):
        command = [sys.executable, '-c', self.lock_subprocess_code(block=True),
                   str(Path(s.__file__).parent), self.cfg.state_path]
        child = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, text=True)
        try:
            # Bound the readiness read as well as child teardown.
            with ThreadPoolExecutor(max_workers=1) as pool:
                ready = pool.submit(child.stdout.readline)
                try:
                    self.assertEqual(ready.result(timeout=5).strip(), 'locked')
                except BaseException:
                    child.kill()
                    raise
            child.kill()
            child.wait(timeout=5)
            with s.controller_lock(self.cfg.state_path):
                self.assertTrue(Path(self.cfg.state_path + '.lock').exists())
        finally:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=5)
            for handle in (child.stdin, child.stdout, child.stderr):
                handle.close()


if __name__ == '__main__':
    unittest.main()
