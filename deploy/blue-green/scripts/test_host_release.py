"""State-machine and fault tests; real Nginx/streams have a separate runtime gate."""
import copy
import fcntl
from datetime import datetime, timedelta, timezone
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('host_release', HERE / 'host-release.py')
host = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host)


class FakeSystem(host.System):
    def __init__(self, c):
        self.c = c
        self.clock = 0
        self.generation = 100
        self.workers = [{'pid': 100, 'start_time': '100'}]
        self.master = {'pid': 10, 'start_time': '10'}
        self.keep_workers = False
        self.ports = []
        self.events = []
        self.fail_probe = None
        self.wrong_route = False
        self.fail_nginx_test = 0
        self.crash_reload = False
        self.no_new_workers = False
        self.forced_shutdown = False
        self.crash_drain_response = False
        self.crash_stop_response = False
        self.restart_update_effect = True
        self.external_counts = {'backup_running':0,'restore_running':0,'detached_image_tasks':0,'batch_active':0}
        self.status_hook = None
        self.info = {}
        self.statuses = {}
        for name, slot in c['slots'].items():
            env = ['RELEASE_DRAIN_START_HELD=false']
            bg = None
            if slot.get('preserve_mode_required', True):
                env += ['GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE=preserve', 'GATEWAY_BACKGROUND_JOB_MODE=coordinated', 'GATEWAY_BACKGROUND_JOBS_START_HELD=true']
                bg = {'mode': 'coordinated', 'state': 'held', 'active_tasks': 0, 'instance_id': 'instance-' + name}
            self.info[name] = {'Id': 'container-' + name, 'Image': slot['image_id'], 'State': {'Running': True, 'StartedAt': '2026-10-20T00:00:00Z'}, 'Config': {'Env': env}, 'HostConfig': {'RestartPolicy': {'Name': 'always'}}}
            self.statuses[name] = {'state': 'open', 'operation_id': '', 'active_http': 0, 'pending_usage': 0, 'queued_http': 0}
            if bg:
                self.statuses[name]['background_jobs'] = bg

    def phase(self):
        path = Path(self.c['state_file'])
        return json.loads(path.read_text())['phase'] if path.exists() else None

    def inspect(self, c, slot):
        name = next(n for n, s in c['slots'].items() if s['container'] == slot['container'])
        return copy.deepcopy(self.info[name])

    def run(self, argv, timeout=10):
        self.events.append(('run', argv, self.phase()))
        if argv[0] == 'inflight-evidence':
            return json.dumps({'version':1,'container_id':argv[argv.index('--container-id')+1], 'started_at':argv[argv.index('--started-at')+1], 'checked_at_utc':datetime.now(timezone.utc).isoformat(), **self.external_counts}).encode()
        if argv[-1] == '-T' and self.forced_shutdown:
            return b'worker_shutdown_timeout 10s;\n'
        if argv[0] == 'nginx' and argv[-1] == '-t' and self.fail_nginx_test:
            self.fail_nginx_test -= 1
            raise host.ReleaseError('synthetic nginx validation failure')
        if argv[-2:] == ['-s', 'reload']:
            if self.crash_reload:
                self.crash_reload = False
                raise KeyboardInterrupt()
            if not self.no_new_workers:
                self.generation += 1
                workers = [{'pid': self.generation, 'start_time': str(self.generation)}]
                self.workers = self.workers + workers if self.keep_workers else workers
        if 'stop' in argv:
            ident = argv[-1]
            for info in self.info.values():
                if info['Id'] == ident:
                    info['State']['Running'] = False
            if self.crash_stop_response:
                self.crash_stop_response = False
                raise KeyboardInterrupt()
        if '--restart=no' in argv and self.restart_update_effect:
            for info in self.info.values():
                if info['Id'] == argv[-1]:
                    info['HostConfig']['RestartPolicy']['Name'] = 'no'
        return b''

    def observe(self, c):
        return {'master': copy.deepcopy(self.master), 'workers': copy.deepcopy(self.workers), 'established_ports': list(self.ports)}

    def probe(self, route):
        self.events.append(('probe', route['url'], route.get('host')))
        if self.fail_probe == route['url']:
            raise host.ReleaseError('synthetic probe failure')

    def request(self, base, path, token=None, host=None, body=None):
        if base.startswith('http://127.0.0.1:30'):
            port = int(base.rsplit(':', 1)[1])
            name = next(n for n, s in self.c['slots'].items() if s['port'] == port)
        else:
            data = Path(self.c['upstream_file']).read_text()
            name = next(n for n, s in self.c['slots'].items() if ':' + str(s['port']) + ';' in data)
            if self.wrong_route and name == 'green':
                name = 'blue'
        action = path.rsplit('/release/', 1)[1]
        self.events.append(('api', name, action, copy.deepcopy(body), self.phase()))
        value = self.statuses[name]
        if action == 'drain':
            if value['state'] != 'open':
                return 409, b''
            value['state'] = 'draining'
            value['operation_id'] = 'drain-' + name
            if 'background_jobs' in value:
                value['background_jobs']['state'] = 'held'
            if self.crash_drain_response:
                self.crash_drain_response = False
                raise KeyboardInterrupt()
        elif action == 'cancel':
            if value['state'] != 'draining' or value['operation_id'] != body['operation_id']:
                return 409, b''
            value['state'] = 'open'
            value['operation_id'] = ''
        elif action == 'background/activate':
            assert body == {'instance_id': value['background_jobs']['instance_id']}
            value['background_jobs']['state'] = 'active'
        if self.status_hook:
            self.status_hook(name, action, value)
        return 200, json.dumps({'data': value}).encode()

    def sleep(self, seconds):
        self.clock += seconds

    def now(self):
        return self.clock


class HostReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        token = root / 'token'
        token.write_text('synthetic-private-token')
        token.chmod(0o600)
        self.c = {'version': 1, 'state_file': str(root / 'state.json'), 'upstream_file': str(root / 'upstream.conf'), 'upstream_name': 'sub2api_release', 'initial_slot': 'blue', 'nginx_sites': [str(root / 'one.conf'), str(root / 'two.conf')], 'routes': [{'url': 'https://one.synthetic.invalid'}, {'url': 'http://router.synthetic.invalid', 'host': 'two.synthetic.invalid'}], 'slots': {}, 'docker_command': ['docker'], 'nginx_command': ['nginx'], 'observer_command': ['observe'], 'poll_seconds': 1, 'reload_timeout_seconds': 2}
        self.c['inflight_evidence_command'] = ['inflight-evidence']
        for name, port, digit in [('blue', 30080, 'a'), ('green', 30081, 'b')]:
            self.c['slots'][name] = {'container': 'app-' + name, 'port': port, 'image_id': 'sha256:' + digit * 64, 'admin_token_file': str(token), 'preserve_mode_required': name != 'blue'}
        for path in self.c['nginx_sites']:
            Path(path).write_text('server {\n location / {\n  proxy_pass http://127.0.0.1:30080;\n  proxy_read_timeout 1200s;\n }\n}\n')
        self.config_path = root / 'private.json'
        self.config_path.write_text(json.dumps(self.c))
        self.config_path.chmod(0o600)
        self.sys = FakeSystem(self.c)
        self.controller = host.Controller(self.c, self.sys)
        self.evidence_path = root / 'legacy.json'
        self.c['slots']['blue']['legacy_background_evidence_file'] = str(self.evidence_path)
        self.write_evidence()

    def write_evidence(self, **changes):
        info = self.sys.info['blue']
        value = {'version': 1, 'container_id': info['Id'], 'started_at': info['State']['StartedAt'], 'image_id': info['Image'], 'checked_at_utc': datetime.now(timezone.utc).isoformat(), 'scheduled_enabled': 0, 'channel_enabled': 0, 'batch_jobs': 0, 'backup_running': 0, 'restore_running': 0, 'cn_active_accounts': 0}
        value.update(changes)
        value.setdefault('backup_schedule_enabled', False)
        self.evidence_path.write_text(json.dumps(value))
        self.evidence_path.chmod(0o600)

    def installed(self):
        self.controller.install()
        self.sys.events.clear()

    def switched(self):
        self.installed()
        self.controller.switch('green')
        self.sys.events.clear()

    def stops(self):
        return [x for x in self.sys.events if x[0] == 'run' and 'stop' in x[1]]

    def test_install_preserves_old_upstream_and_both_original_nonroute_settings(self):
        self.installed()
        self.assertEqual(Path(self.c['upstream_file']).read_bytes(), self.controller.upstream('blue'))
        for site in self.c['nginx_sites']:
            self.assertIn('proxy_pass http://sub2api_release;', Path(site).read_text())
            self.assertIn('proxy_read_timeout 1200s;', Path(site).read_text())
        self.assertTrue(self.controller.state['installed'])
        self.assertFalse(self.stops())

    def test_duplicate_proxy_rejected_before_any_file_mutation(self):
        site = Path(self.c['nginx_sites'][1])
        site.write_text(site.read_text() + 'proxy_pass http://127.0.0.1:30080;\n')
        originals = [Path(p).read_bytes() for p in self.c['nginx_sites']]
        with self.assertRaises(host.ReleaseError):
            self.controller.install()
        self.assertEqual(originals, [Path(p).read_bytes() for p in self.c['nginx_sites']])
        self.assertFalse(Path(self.c['upstream_file']).exists())

    def test_nginx_install_failure_restores_both_sites(self):
        originals = [Path(p).read_bytes() for p in self.c['nginx_sites']]
        self.sys.fail_nginx_test = 1
        with self.assertRaises(host.ReleaseError):
            self.controller.install()
        self.assertEqual(originals, [Path(p).read_bytes() for p in self.c['nginx_sites']])
        self.assertFalse(self.controller.state['installed'])
        self.assertFalse(self.stops())

    def test_interrupted_install_resumes_its_durable_original_files(self):
        self.sys.crash_reload = True
        with self.assertRaises(KeyboardInterrupt):
            self.controller.install()
        resumed = host.Controller(self.c, self.sys)
        self.assertEqual(resumed.state['phase'], 'install_reload_requested')
        resumed.install()
        self.assertTrue(resumed.state['installed'])
        self.assertFalse(self.stops())

    def test_running_image_and_startup_contract_rejected_before_route_write(self):
        self.installed()
        original = Path(self.c['upstream_file']).read_bytes()
        for mutation in ['image', 'held', 'cleanup', 'background_mode', 'background_start']:
            with self.subTest(mutation=mutation):
                info = copy.deepcopy(self.sys.info['green'])
                if mutation == 'image':
                    self.sys.info['green']['Image'] = 'sha256:' + 'c' * 64
                else:
                    key = {'held': 'RELEASE_DRAIN_START_HELD', 'cleanup': 'GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE', 'background_mode': 'GATEWAY_BACKGROUND_JOB_MODE', 'background_start': 'GATEWAY_BACKGROUND_JOBS_START_HELD'}[mutation]
                    self.sys.info['green']['Config']['Env'] = [x for x in info['Config']['Env'] if not x.startswith(key + '=')]
                with self.assertRaises(host.ReleaseError):
                    self.controller.switch('green')
                self.assertEqual(Path(self.c['upstream_file']).read_bytes(), original)
                self.sys.info['green'] = info
        self.assertFalse(self.stops())

    def test_direct_readiness_failure_does_not_switch_or_drain(self):
        self.installed()
        self.sys.fail_probe = 'http://127.0.0.1:30081'
        with self.assertRaises(host.ReleaseError):
            self.controller.switch('green')
        self.assertEqual(self.controller.state['active_slot'], 'blue')
        self.assertFalse(any(e[0] == 'api' and e[2] == 'drain' for e in self.sys.events))

    def test_instance_route_mismatch_restores_old_without_drain_or_stop(self):
        self.installed()
        self.sys.wrong_route = True
        with self.assertRaises(host.ReleaseError):
            self.controller.switch('green')
        self.assertEqual(Path(self.c['upstream_file']).read_bytes(), self.controller.upstream('blue'))
        self.assertEqual(self.controller.state['active_slot'], 'blue')
        self.assertFalse(self.stops())
        self.assertEqual(self.sys.statuses['blue']['state'], 'open')

    def test_cutover_persists_before_hup_and_resume_is_safe(self):
        self.installed()
        self.sys.crash_reload = True
        with self.assertRaises(KeyboardInterrupt):
            self.controller.switch('green')
        resumed = host.Controller(self.c, self.sys)
        self.assertEqual(resumed.state['phase'], 'switch_reload_requested')
        resumed.switch('green')
        self.assertEqual(resumed.state['active_slot'], 'green')
        self.assertFalse(self.stops())
        self.assertTrue(all(e[2] in ('switch_reload_requested', 'switch_restore_requested') for e in self.sys.events if e[0] == 'run' and e[1][-2:] == ['-s', 'reload']))

    def test_no_new_worker_generation_does_not_claim_success(self):
        self.installed()
        self.sys.no_new_workers = True
        with self.assertRaises(host.ReleaseError):
            self.controller.switch('green')
        self.assertEqual(self.controller.state['active_slot'], 'blue')
        self.assertFalse(self.stops())

    def test_nginx_forced_shutdown_deadline_rejected_before_cutover(self):
        self.installed()
        self.sys.forced_shutdown = True
        original = Path(self.c['upstream_file']).read_bytes()
        with self.assertRaises(host.ReleaseError):
            self.controller.switch('green')
        self.assertEqual(Path(self.c['upstream_file']).read_bytes(), original)
        self.assertFalse(self.stops())

    def test_each_busy_retirement_signal_defers_without_stopping(self):
        self.switched()
        old_workers = self.controller.state['cutover']['workers']
        for signal in ['active_http', 'pending_usage', 'queued_http', 'worker', 'socket']:
            with self.subTest(signal=signal):
                self.sys.statuses['blue'].update(active_http=0, pending_usage=0, queued_http=0)
                self.sys.workers = [{'pid': 999, 'start_time': '999'}]
                self.sys.ports = []
                if signal == 'worker':
                    self.sys.workers += old_workers
                elif signal == 'socket':
                    self.sys.ports = [30080]
                else:
                    self.sys.statuses['blue'][signal] = 1
                with self.assertRaises(host.Deferred):
                    self.controller.retire('blue', 1)
                self.assertFalse(self.stops())
                self.assertFalse(any(x[0]=='run' and '--restart=no' in x[1] for x in self.sys.events))
                self.assertTrue(self.sys.info['blue']['State']['Running'])
        drains = [x for x in self.sys.events if x[0] == 'api' and x[2] == 'drain']
        self.assertEqual(len(drains), 1)

    def test_background_active_task_blocks_stop(self):
        self.switched()
        self.sys.statuses['blue']['background_jobs'] = {'mode': 'legacy', 'state': 'held', 'active_tasks': 1, 'instance_id': 'legacy-test'}
        with self.assertRaises(host.Deferred):
            self.controller.retire('blue', 1)
        self.assertFalse(self.stops())

    def test_old_ingress_stays_open_until_workers_and_connections_disappear(self):
        self.switched()
        self.sys.workers += self.controller.state['cutover']['workers']
        self.sys.ports = [30080]
        with self.assertRaises(host.Deferred):
            self.controller.retire('blue', 1)
        self.assertEqual(self.controller.state['phase'], 'waiting_ingress')
        self.assertEqual(self.sys.statuses['blue']['state'], 'open')
        self.assertFalse(any(e[0]=='api' and e[2]=='drain' for e in self.sys.events))
        # A late old-worker HTTP request is still admitted by the open gate;
        # actual network/HTTP2 behavior is proved by the isolated runtime gate.
        self.sys.statuses['blue']['active_http'] = 1
        resumed = host.Controller(self.c, self.sys)
        with self.assertRaises(host.Deferred):
            resumed.retire('blue', 1)
        self.assertEqual(self.sys.statuses['blue']['state'], 'open')
        self.sys.statuses['blue']['active_http'] = 0
        self.sys.workers = [{'pid':999,'start_time':'999'}]
        self.sys.ports = []
        resumed.retire('blue', 3)
        self.assertEqual(len(self.stops()), 1)

    def test_legacy_evidence_strict_counts_and_freshness(self):
        self.switched()
        for changes in [{'scheduled_enabled': True}, {'channel_enabled': None}, {'batch_jobs': -1}, {'backup_schedule_enabled': True}, {'backup_schedule_enabled': None}, {'cn_active_accounts': True}, {'cn_active_accounts': None}, {'container_id': 'foreign'}, {'checked_at_utc': (datetime.now(timezone.utc)-timedelta(hours=1)).isoformat()}]:
            with self.subTest(changes=changes):
                self.write_evidence(**changes)
                with self.assertRaises(host.ReleaseError):
                    self.controller.retire('blue', 3)
                self.assertFalse(self.stops())
        self.write_evidence(cn_active_accounts=None, background_balance_enabled=False)
        self.controller.retire('blue', 3)
        self.assertEqual(len(self.stops()), 1)

    def test_late_usage_after_route_verification_prevents_stop(self):
        self.switched()
        polls = 0
        def hook(name, action, value):
            nonlocal polls
            if name == 'blue' and action == 'status' and value['state'] == 'draining':
                polls += 1
                if polls >= 3:
                    value['pending_usage'] = 1
        self.sys.status_hook = hook
        with self.assertRaises(host.Deferred):
            self.controller.retire('blue', 3)
        self.assertFalse(self.stops())

    def test_live_external_counts_block_every_retirement_before_restart_update(self):
        self.switched()
        for key in self.sys.external_counts:
            with self.subTest(key=key):
                self.sys.external_counts[key] = 1
                with self.assertRaises(host.Deferred):
                    self.controller.retire('blue', 3)
                self.assertFalse(self.stops())
                self.assertFalse(any(e[0]=='run' and '--restart=no' in e[1] for e in self.sys.events))
                self.sys.external_counts[key] = 0
        self.controller.retire('blue', 3)
        self.assertEqual(len(self.stops()), 1)

    def test_external_evidence_missing_or_noninteger_is_not_zero(self):
        self.switched()
        for value in (None, True, -1, 'unknown'):
            with self.subTest(value=value):
                self.sys.external_counts['detached_image_tasks'] = value
                with self.assertRaises(host.ReleaseError):
                    self.controller.retire('blue', 3)
                self.assertFalse(self.stops())
                self.assertFalse(any(e[0]=='run' and '--restart=no' in e[1] for e in self.sys.events))
        self.c.pop('inflight_evidence_command')
        with self.assertRaises(host.ReleaseError):
            self.controller.retire('blue', 3)
        self.assertFalse(self.stops())

    def test_release_stops_only_after_cutover_and_then_activates_jobs(self):
        self.installed()
        self.controller.release('green', 3)
        self.assertEqual(len(self.stops()), 1)
        stop_index = next(i for i,e in enumerate(self.sys.events) if e[0]=='run' and 'stop' in e[1])
        update_index = next(i for i,e in enumerate(self.sys.events) if e[0]=='run' and '--restart=no' in e[1])
        activate_index = next(i for i,e in enumerate(self.sys.events) if e[0]=='api' and e[2]=='background/activate')
        self.assertLess(update_index, stop_index)
        self.assertLess(stop_index, activate_index)
        self.assertEqual(self.sys.info['blue']['HostConfig']['RestartPolicy']['Name'], 'no')
        self.assertEqual(self.stops()[0][2], 'retiring')
        self.assertEqual(self.sys.statuses['green']['background_jobs']['state'], 'active')
        self.controller.release('green', 3)
        self.assertEqual(len(self.stops()), 1)


    def test_unconfirmed_restart_disable_refuses_stop(self):
        self.switched()
        self.sys.restart_update_effect = False
        with self.assertRaises(host.ReleaseError):
            self.controller.retire('blue', 3)
        self.assertFalse(self.stops())
        self.assertTrue(self.sys.info['blue']['State']['Running'])
        self.assertEqual(self.controller.state['retirements']['blue']['phase'], 'restart_disable_requested')

    def test_stop_response_crash_resumes_by_inspection_without_second_stop(self):
        self.installed()
        self.sys.crash_stop_response = True
        with self.assertRaises(KeyboardInterrupt):
            self.controller.release('green', 3)
        resumed = host.Controller(self.c, self.sys)
        self.assertEqual(resumed.state['retirements']['blue']['phase'], 'stop_requested')
        resumed.release('green', 3)
        self.assertEqual(len(self.stops()), 1)
        self.assertEqual(self.sys.info['blue']['HostConfig']['RestartPolicy']['Name'], 'no')
        self.assertEqual(self.sys.statuses['green']['background_jobs']['state'], 'active')

    def test_lost_drain_response_preserves_process_without_adopting_operation(self):
        self.switched()
        self.sys.crash_drain_response = True
        with self.assertRaises(KeyboardInterrupt):
            self.controller.retire('blue', 1)
        resumed = host.Controller(self.c, self.sys)
        self.assertIsNone(resumed.state['retirements']['blue']['operation_id'])
        with self.assertRaises(host.Deferred):
            resumed.retire('blue', 1)
        with self.assertRaises(host.ReleaseError):
            resumed.rollback('blue')
        self.assertFalse(self.stops())

    def test_rollback_cancels_exact_operation_before_readiness_and_switch(self):
        self.switched()
        self.sys.statuses['blue']['active_http'] = 1
        with self.assertRaises(host.Deferred):
            self.controller.retire('blue', 2)
        self.controller.rollback('blue')
        cancels = [x for x in self.sys.events if x[0] == 'api' and x[2] == 'cancel']
        self.assertEqual(cancels[0][3], {'operation_id': 'drain-blue'})
        self.assertEqual(self.controller.state['active_slot'], 'blue')
        self.assertFalse(self.stops())

    def test_rollback_rejects_foreign_operation_or_restarted_or_stopped_old(self):
        self.switched()
        for mutation in ['foreign_operation', 'restarted', 'stopped']:
            with self.subTest(mutation=mutation):
                before_info = copy.deepcopy(self.sys.info['blue'])
                before_status = copy.deepcopy(self.sys.statuses['blue'])
                if mutation == 'foreign_operation':
                    self.sys.statuses['blue'].update(state='draining', operation_id='foreign')
                elif mutation == 'restarted':
                    self.sys.info['blue']['State']['StartedAt'] = 'new-start'
                else:
                    self.sys.info['blue']['State']['Running'] = False
                with self.assertRaises(host.ReleaseError):
                    self.controller.rollback('blue')
                self.assertEqual(self.controller.state['active_slot'], 'green')
                self.sys.info['blue'] = before_info
                self.sys.statuses['blue'] = before_status

    def test_activation_requires_old_verified_stopped(self):
        self.switched()
        with self.assertRaises(host.ReleaseError):
            self.controller.activate()
        self.assertEqual(self.sys.statuses['green']['background_jobs']['state'], 'held')

    def test_explicit_activation_revalidates_restarted_candidate_after_old_retirement(self):
        self.installed()
        self.controller.release('green', 3)
        self.sys.info['green']['State']['StartedAt'] = 'new-candidate-start'
        self.sys.statuses['green']['background_jobs'].update(state='held', instance_id='new-instance')
        self.controller.activate()
        activation = self.controller.state['background_activation']
        self.assertEqual(activation['identity']['started_at'], 'new-candidate-start')
        self.assertEqual(activation['instance_id'], 'new-instance')
        self.assertTrue(activation['confirmed'])
        self.assertEqual(self.sys.statuses['green']['background_jobs']['state'], 'active')

    def test_external_site_edit_blocks_retirement(self):
        self.switched()
        Path(self.c['nginx_sites'][1]).write_text('proxy_pass http://foreign;\n')
        with self.assertRaises(host.ReleaseError):
            self.controller.retire('blue', 1)
        self.assertFalse(self.stops())

    def test_private_config_permissions_and_invalid_counts(self):
        self.assertEqual(host.config_read(self.config_path)['version'], 1)
        self.config_path.chmod(0o644)
        with self.assertRaises(host.ReleaseError):
            host.config_read(self.config_path)

    def test_flock_refuses_concurrent_command_without_secret_output(self):
        lock_path = Path(self.c['state_file'] + '.lock')
        with open(lock_path, 'a+b') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = subprocess.run([sys.executable, str(HERE/'host-release.py'), '--config', str(self.config_path), 'status'], capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn(b'holds the lock', result.stderr)
        self.assertNotIn(b'synthetic-private-token', result.stdout + result.stderr)
        self.assertFalse(Path(self.c['state_file']).exists())

    def test_legacy_deploy_exits_before_any_helpers_or_side_effects(self):
        result = subprocess.run(['bash', str(HERE/'deploy.sh')], capture_output=True, env=os.environ | {'DEPLOY_ROOT': str(Path(self.tmp.name)/'must-not-exist')})
        self.assertEqual(result.returncode, 2)
        self.assertFalse((Path(self.tmp.name)/'must-not-exist').exists())


class RealBoundaryTests(unittest.TestCase):
    def test_host_header_probe_requires_exact_status_without_redirect(self):
        system = host.System()
        for bad in (200, 302, 403, 500, 503):
            with self.subTest(bad=bad), patch.object(system, 'request', side_effect=[(200,b''),(bad,b'')]), self.assertRaises(host.ReleaseError):
                system.probe({'url':'http://127.0.0.1:1','host':'synthetic.invalid'})

    def test_linux_observer_tracks_start_time_and_either_tcp_direction(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for pid,parent,command in [(10,1,b'nginx: master process'),(20,10,b'nginx: worker process is shutting down'),(30,1,b'unrelated')]:
                d=root/str(pid); d.mkdir()
                # State is field 3; starttime is field 22.
                fields=['S',str(parent)]+['0']*17+[str(pid*100)]
                (d/'stat').write_text(f'{pid} (nginx) '+' '.join(fields))
                (d/'cmdline').write_bytes(command)
            (root/'net').mkdir()
            (root/'net/tcp').write_text('header\n 0: 0100007F:7570 0100007F:ABCD 01 x\n')
            (root/'net/tcp6').write_text('header\n 0: 000000:ABCD 000000:7571 01 x\n')
            pidfile=root/'nginx.pid'; pidfile.write_text('10')
            result=host.observe(pidfile,{30064,30065},root)
            self.assertEqual(result['workers'],[{'pid':20,'start_time':'2000'}])
            self.assertEqual(result['established_ports'],[30064,30065])


if __name__ == '__main__':
    unittest.main()
