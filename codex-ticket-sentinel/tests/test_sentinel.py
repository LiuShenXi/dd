import dataclasses
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from sentinel import Config, SentinelError, Store, validate_status

V1, V2 = 'a' * 64, 'b' * 64


def snapshot(version=V1, remaining=3600, events=None, seq=0, enabled=True, eligible=True, instance='instance-1'):
    return {'protocol_version': 1, 'instance_id': instance, 'enabled': enabled,
            'latest_seq': seq, 'oldest_seq': 1, 'dropped_events': 0,
            'events': events or [], 'targets': [{'account_id': 2, 'model': 'gpt-6-astra',
                'ticket_version': version, 'eligible': eligible, 'ready': bool(version),
                'remaining_seconds': remaining}]}


def event(seq=1, version=V1, kind='model_mismatch', account=2, model='gpt-6-astra'):
    return {'seq': seq, 'account_id': account, 'model': model, 'kind': kind,
            'expected_ticket_version': version, 'actual_model': 'gpt-5.6-luna'}


class StateTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.now = 10000.
        self.cfg = Config('http://sub2api:8080', str(self.root / 'token'), str(self.root / 'state.sqlite'), str(self.root / 'status.json'))
        self.store = Store(self.cfg.state_path, self.cfg, lambda: self.now)

    def tearDown(self):
        self.store.db.close()
        self.tmp.cleanup()

    def signal(self, **kwargs):
        data = snapshot(events=[event(**kwargs)], seq=kwargs.get('seq', 1))
        self.store.ingest(validate_status(data))

    def test_duplicate_signals_commit_cursor_and_only_one_job(self):
        self.signal()
        self.signal()
        self.assertEqual(self.store.meta('cursor'), '1')
        job = self.store.claim()
        self.assertEqual(job['reason'], 'model_mismatch')
        self.assertIsNone(self.store.claim())
        self.assertEqual(self.store.db.execute('SELECT count(*) FROM attempts').fetchone()[0], 1)

    def test_old_version_event_cannot_refresh_new_ticket(self):
        self.store.ingest(validate_status(snapshot(version=V2, events=[event(version=V1)], seq=1)))
        self.assertIsNone(self.store.claim())
        self.assertEqual(self.store.meta('cursor'), '1')

    def test_ineligible_and_nonallowlisted_events_do_not_refresh(self):
        self.store.ingest(snapshot(events=[event()], seq=1, eligible=False))
        self.assertIsNone(self.store.claim())
        self.store.ingest(snapshot(events=[event(seq=2, account=9)], seq=2))
        self.assertIsNone(self.store.claim())

    def test_missing_and_near_expiry_trigger_but_healthy_does_not(self):
        self.store.ingest(snapshot())
        self.assertIsNone(self.store.claim())
        self.store.ingest(snapshot(remaining=600))
        self.assertEqual(self.store.claim()['reason'], 'expiry')
        self.store.db.execute('DELETE FROM jobs')
        self.store.db.execute('DELETE FROM attempts')
        self.store.db.commit()
        self.store.ingest(snapshot(version='', remaining=0))
        job = self.store.claim()
        self.assertEqual((job['reason'], job['expected_ticket_version']), ('missing', ''))

    def test_disable_drops_pending_without_starting_refresh(self):
        self.signal()
        self.store.ingest(snapshot(enabled=False, seq=1))
        self.assertIsNone(self.store.claim())
        self.assertEqual(self.store.summary()['pending'], 0)

    def test_failed_refresh_backoff_survives_duplicate_signal_and_restart(self):
        self.signal()
        job = self.store.claim()
        self.store.finish(job, {'status': 'failed'})
        self.now += 20
        self.signal(seq=2)
        self.assertIsNone(self.store.claim())
        self.store.db.close()
        self.store = Store(self.cfg.state_path, self.cfg, lambda: self.now)
        self.assertIsNone(self.store.claim())
        self.now += 40
        second = self.store.claim()
        self.assertIsNotNone(second)
        self.store.finish(second, {'status': 'failed'})
        self.now += 119
        self.assertIsNone(self.store.claim())
        self.now += 1
        self.assertIsNotNone(self.store.claim())

    def test_uncertain_inflight_restart_keeps_lease_and_attempt(self):
        self.signal()
        self.store.claim()
        self.store.db.close()
        self.store = Store(self.cfg.state_path, self.cfg, lambda: self.now)
        self.assertIsNone(self.store.claim())
        self.now += self.cfg.request_timeout_seconds + self.cfg.cooldown_seconds
        self.assertIsNotNone(self.store.claim())
        self.assertEqual(self.store.db.execute('SELECT count(*) FROM attempts').fetchone()[0], 2)

    def test_hourly_attempt_budget_survives_new_ticket_versions(self):
        self.store.cfg = dataclasses.replace(self.cfg, max_attempts_per_hour=2)
        for i, version in enumerate((V1, V2)):
            self.store.ingest(snapshot(version=version, events=[event(seq=i + 1, version=version)], seq=i + 1))
            job = self.store.claim()
            self.assertIsNotNone(job)
            self.store.finish(job, {'status': 'refreshed', 'persisted': True, 'ready': True})
            self.now += 61
        self.store.ingest(snapshot(version=V1, events=[event(seq=3)], seq=3))
        self.assertIsNone(self.store.claim())
        self.now = 13601.
        self.assertIsNotNone(self.store.claim())

    def test_nonpersisted_success_is_failure(self):
        self.signal()
        job = self.store.claim()
        self.assertEqual(self.store.finish(job, {'status': 'refreshed', 'persisted': False, 'ready': True}), 'failed')
        self.assertEqual(self.store.summary()['pending'], 1)

    def test_refresh_of_old_version_cannot_delete_new_pending_signal(self):
        self.signal()
        old = self.store.claim()
        self.store.ingest(snapshot(version=V2, events=[event(seq=2, version=V2)], seq=2))
        self.store.finish(old, {'status': 'refreshed', 'persisted': True, 'ready': True})
        current = self.store.db.execute('SELECT * FROM jobs').fetchone()
        self.assertEqual(current['expected_ticket_version'], V2)
        self.assertIsNone(self.store.claim())  # independent cooldown still applies

    def test_restart_instance_resets_cursor_and_reports_ring_gap(self):
        self.signal(seq=10)
        data = snapshot(instance='instance-2', events=[event(seq=2)], seq=2)
        data['oldest_seq'] = 2
        self.assertEqual(self.store.ingest(data), 1)
        self.assertEqual(self.store.meta('cursor'), '2')
        self.assertEqual(self.store.summary()['event_gaps'], 1)

    def test_periodic_checks_cannot_downgrade_pending_anomaly(self):
        self.signal()
        self.store.ingest(snapshot(seq=1, remaining=100))
        self.assertEqual(self.store.claim()['reason'], 'model_mismatch')


class BoundaryTests(unittest.TestCase):
    def test_rejects_invalid_statuses(self):
        for mutator in [lambda x: x.update(protocol_version=2),
                        lambda x: x['targets'][0].update(ticket_version='secret-like-opaque-value'),
                        lambda x: x['targets'][0].update(eligible='true'),
                        lambda x: x.update(events=[event(seq=2), event(seq=1)], latest_seq=2)]:
            data = snapshot()
            mutator(data)
            with self.assertRaises(SentinelError):
                validate_status(data)

    def test_config_rejects_credentials_and_unbounded_limits(self):
        cfg = Config('http://user:password@localhost', '/token', '/state', '/status')
        with self.assertRaises(SentinelError):
            cfg.validate()
        with self.assertRaises(SentinelError):
            dataclasses.replace(cfg, base_url='http://localhost', cooldown_seconds=0).validate()
        with self.assertRaises(SentinelError):
            dataclasses.replace(cfg, base_url='http://localhost', account_ids=()).validate()


if __name__ == '__main__':
    unittest.main()
