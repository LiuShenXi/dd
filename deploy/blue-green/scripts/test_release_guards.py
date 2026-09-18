import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import urllib.error

HERE = Path(__file__).resolve().parent


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


guard = load('release_guard', 'release-guard.py')
ops = load('release_ops', 'remote-release-ops.py')


class GuardTests(unittest.TestCase):
    def test_held_env_rejected(self):
        for value in ['true', 'TRUE', '1', 'yes', 'unexpected']:
            with self.subTest(value=value), self.assertRaises(guard.GuardError):
                guard.check_environment({guard.FLAG: value})

    def test_open_or_unset_env_allowed(self):
        for env in [{}, {guard.FLAG: 'false'}, [guard.FLAG+'=false'], {guard.FLAG: False}]:
            guard.check_environment(env)

    def test_merged_override_cannot_enable_hold(self):
        config = {'services': {name: {'image': 'test', 'environment': {guard.FLAG: 'false'}}
                              for name in ['sub2api-blue', 'sub2api-green']}}
        guard.check_compose(config)
        config['services']['sub2api-blue']['environment'][guard.FLAG] = 'true'
        with self.assertRaises(guard.GuardError):
            guard.check_compose(config)

    def test_container_resumed_but_restart_held_is_rejected(self):
        info = {'State': {'Running': True}, 'Config': {'Env': [guard.FLAG+'=true']}}
        with patch.object(guard, 'docker_json', return_value=[info]), self.assertRaises(guard.GuardError):
            guard.check_container('sub2api-blue')

    def test_image_drift_rejected(self):
        info = {'State': {'Running': True}, 'Config': {'Env': []}, 'Image': 'new-image'}
        with patch.object(guard, 'docker_json', side_effect=[[info], [{'Id': 'old-image'}]]):
            with self.assertRaises(guard.GuardError):
                guard.check_container('sub2api-blue', 'configured-image')

    def test_probe_healthy_but_business_timeout(self):
        from unittest.mock import MagicMock
        opener = MagicMock()
        response = MagicMock()
        response.__enter__.return_value.status = 200
        opener.open.side_effect = [response, TimeoutError()]
        with patch.object(guard.urllib.request, 'build_opener', return_value=opener):
            with self.assertRaises(guard.GuardError):
                guard.probe('http://synthetic.invalid')

    def test_probe_requires_exact_401(self):
        from unittest.mock import MagicMock
        for code in [200, 302, 403, 500, 503]:
            opener = MagicMock()
            response = MagicMock()
            response.__enter__.return_value.status = 200
            bad = urllib.error.HTTPError('synthetic', code, 'test', {}, None)
            opener.open.side_effect = [response, bad]
            with patch.object(guard.urllib.request, 'build_opener', return_value=opener):
                with self.assertRaises(guard.GuardError):
                    guard.probe('http://synthetic.invalid')

    def test_clear_stage_archives_exact_bytes_and_preserves_other_settings(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            canonical = root / 'stage-override.private.json'
            backup = root / 'stage-override.private.json.bak-test'
            data = {'services': {'sub2api-blue': {'image': 'original-image',
                    'environment': {guard.FLAG: 'true', 'SYNTHETIC_SETTING': 'preserved'}}}}
            original = json.dumps(data).encode()
            canonical.write_bytes(original)
            backup.write_bytes(original)
            result = ops.clear_stage(root)
            self.assertTrue(result['changed'])
            with tarfile.open(result['archive']) as archive:
                self.assertEqual(archive.extractfile(backup.name).read(), original)
            self.assertFalse(backup.exists())
            actual = json.loads(canonical.read_text())['services']['sub2api-blue']
            self.assertEqual(actual['image'], 'original-image')
            self.assertEqual(actual['environment'][guard.FLAG], 'false')
            self.assertEqual(actual['environment']['SYNTHETIC_SETTING'], 'preserved')
            self.assertFalse(ops.clear_stage(root)['changed'])

    def test_clear_stage_invalid_json_is_nonmutating(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / 'stage-override.private.json'
            target.write_text('{broken')
            with self.assertRaises(ValueError):
                ops.clear_stage(root)
            self.assertEqual(target.read_text(), '{broken')
            self.assertFalse((root/'release-guard-archive').exists())

    def test_legacy_operations_fail_without_side_effects(self):
        with tempfile.TemporaryDirectory() as directory:
            for operation in ['prepare-stage', 'backup', 'auth', 'probe']:
                result = subprocess.run([sys.executable, str(HERE/'remote-release-ops.py'),
                    operation, '--workdir', str(Path(directory)/'never-created')], capture_output=True)
                self.assertEqual(result.returncode, 2)
                self.assertFalse((Path(directory)/'never-created').exists())

    @unittest.skipIf(sys.platform == 'win32', 'Runs on the Linux deployment host')
    def test_conditional_shell_function_does_not_mask_probe_failure(self):
        script = '''source "$1"
python3() { return 1; }
docker() { return 0; }
if verify_stable_route; then exit 99; else exit 0; fi
'''
        result = subprocess.run(['bash', '-c', script, 'test', str(HERE/'common.sh')], capture_output=True)
        self.assertEqual(result.returncode, 0)

    @unittest.skipIf(sys.platform == 'win32', 'Runs on the Linux deployment host')
    def test_switch_refuses_before_touching_router(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_guard = root/'guard.py'
            fake_guard.write_text('raise SystemExit(1)\n')
            images = root/'images.env'
            images.write_text('SUB2API_BLUE_IMAGE=synthetic\n')
            router = root/'router.conf'
            router.write_text('unchanged router\n')
            active = root/'active-slot'
            active.write_text('green\n')
            result = subprocess.run(['bash',str(HERE/'switch-slot.sh'),'blue'],capture_output=True,
                env=os.environ | {'RELEASE_GUARD':str(fake_guard),'SLOT_IMAGES_FILE':str(images),
                'ROUTER_CONF':str(router),'ACTIVE_SLOT_FILE':str(active)})
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(router.read_text(),'unchanged router\n')
            self.assertEqual(active.read_text(),'green\n')

    @unittest.skipIf(sys.platform == 'win32', 'Runs on the Linux deployment host')
    def test_compose_refuses_mutation_when_merged_config_is_held(self):
        config={'services': {name:{'image':'synthetic','environment':{guard.FLAG:'true'}}
                            for name in ['sub2api-blue','sub2api-green']}}
        script='''source "$1"
docker() {
  if [[ "$*" == *"config --format json"* ]]; then printf '%s' "$TEST_CONFIG"; return 0; fi
  echo UNEXPECTED_MUTATION
  return 0
}
if compose up -d sub2api-blue; then exit 99; else exit 0; fi
'''
        result=subprocess.run(['bash','-c',script,'test',str(HERE/'common.sh')],capture_output=True,
            env=os.environ | {'RELEASE_GUARD':str(HERE/'release-guard.py'),'TEST_CONFIG':json.dumps(config)})
        self.assertEqual(result.returncode,0)
        self.assertNotIn(b'UNEXPECTED_MUTATION',result.stdout)


if __name__ == '__main__':
    unittest.main()
