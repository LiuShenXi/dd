#!/usr/bin/env python3
"""Fail-closed deployment checks. Never resumes gates or modifies containers."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import time
import urllib.error
import urllib.request

FLAG = 'RELEASE_DRAIN_START_HELD'


class GuardError(RuntimeError):
    pass


def environment(value):
    if isinstance(value, dict):
        return value
    if isinstance(value, list):
        return dict(item.split('=', 1) if '=' in item else (item, None) for item in value)
    raise GuardError('Invalid environment representation')


def check_environment(value):
    setting = environment(value).get(FLAG)
    if setting is not None and str(setting).strip().lower() not in ('', 'false', '0', 'f'):
        raise GuardError('Start-held application configuration is forbidden')


def check_compose(config):
    for slot in ('blue', 'green'):
        name = 'sub2api-' + slot
        service = config.get('services', {}).get(name)
        if not service or not service.get('image'):
            raise GuardError('Missing slot or image: ' + name)
        check_environment(service.get('environment', {}))


def docker_json(*args):
    result = subprocess.run(['docker', *args], capture_output=True, timeout=5)
    if result.returncode:
        raise GuardError('Docker read-only inspection failed')
    return json.loads(result.stdout)


def check_container(name, expected_image=None):
    info = docker_json('inspect', name)[0]
    if not info['State']['Running']:
        raise GuardError('Target container is not running: ' + name)
    check_environment(info['Config'].get('Env', []))
    if expected_image:
        expected_id = docker_json('image', 'inspect', expected_image)[0]['Id']
        if info['Image'] != expected_id:
            raise GuardError('Running container differs from configured slot image: ' + name)
    return {'container': name, 'image_id': info['Image'], 'started_at': info['State']['StartedAt']}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def probe(base, timeout=2):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    results = {}
    for path, expected in (('/health', 200), ('/v1/models', 401)):
        started = time.monotonic()
        try:
            with opener.open(base.rstrip('/') + path, timeout=timeout) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
        except (OSError, TimeoutError):
            raise GuardError('Business readiness timeout or connection failure: ' + path) from None
        if status != expected:
            raise GuardError('Unexpected business readiness status: ' + path + ' ' + str(status))
        results[path] = {'status': status, 'seconds': round(time.monotonic() - started, 3)}
    return results


def image_for_slot(path, slot):
    key = 'SUB2API_' + slot.upper() + '_IMAGE'
    entries = [line.split('=', 1)[1].strip() for line in path.read_text().splitlines()
               if line.startswith(key + '=')]
    if len(entries) != 1 or not entries[0]:
        raise GuardError('Missing or duplicate configured slot image')
    return entries[0]


def monitor(root):
    active_path = root / 'run/active-slot'
    slot = active_path.read_text().strip()
    if slot not in ('blue', 'green'):
        raise GuardError('Invalid active slot')
    expected = image_for_slot(root / 'blue-green/slot-images.env', slot)
    result = check_container('sub2api-' + slot, expected)
    port = 18080 if slot == 'blue' else 28080
    result['direct'] = probe('http://127.0.0.1:' + str(port))
    result['router'] = probe('http://127.0.0.1:8080')
    if active_path.read_text().strip() != slot:
        raise GuardError('Active slot changed during readiness check; retry required')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='mode', required=True)
    sub.add_parser('compose-config')
    p = sub.add_parser('container')
    p.add_argument('name', choices=['sub2api-blue', 'sub2api-green'])
    p.add_argument('--image')
    p = sub.add_parser('probe')
    p.add_argument('url')
    p = sub.add_parser('monitor')
    p.add_argument('--root', type=Path, default=Path('/home/linuxuser/apps/sub2api'))
    args = parser.parse_args()
    try:
        if args.mode == 'compose-config':
            check_compose(json.load(sys.stdin))
            result = {'compose_safe': True}
        elif args.mode == 'container':
            result = check_container(args.name, args.image)
        elif args.mode == 'probe':
            result = probe(args.url)
        else:
            result = monitor(args.root)
        print(json.dumps({'ok': True, 'result': result}))
        return 0
    except (GuardError, OSError, ValueError, KeyError, subprocess.TimeoutExpired) as error:
        # Do not print Docker/config payloads, environment values, or HTTP bodies.
        message = str(error) if isinstance(error, GuardError) else 'Readiness check could not be completed'
        print(json.dumps({'ok': False, 'error': message}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
