import tempfile
import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from sentinel import Config, Store
from test_sentinel import event, snapshot


class CrossKindCoalescingTests(unittest.TestCase):
    def test_newer_lower_priority_event_survives_old_stale_completion(self):
        with tempfile.TemporaryDirectory() as root:
            now = [10000.0]
            cfg = Config('http://sub2api:8080', str(Path(root) / 'token'),
                         str(Path(root) / 'state.sqlite'), str(Path(root) / 'status.json'))
            store = Store(cfg.state_path, cfg, lambda: now[0])
            try:
                store.ingest(snapshot(events=[event(seq=1, kind='model_mismatch')], seq=1))
                old_job = store.claim()
                # A same-blob native renewal occurs while this job is in flight.
                # A later request then observes 312 against the newer generation.
                store.ingest(snapshot(events=[event(seq=2, kind='turn_state_312')], seq=2))
                store.finish(old_job, {'status': 'stale'})
                self.assertEqual(store.summary()['pending'], 1)
                self.assertIsNone(store.claim())
                now[0] += cfg.cooldown_seconds + 1
                job = store.claim()
                self.assertEqual(job['event_id'], 'instance-1:2')
                self.assertEqual(job['reason'], 'turn_state_312')
            finally:
                store.db.close()


if __name__ == '__main__':
    unittest.main()
