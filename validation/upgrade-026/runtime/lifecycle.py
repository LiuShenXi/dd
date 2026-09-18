"""Manage only recorded, labelled resources of this isolated acceptance runtime."""
import argparse
import datetime
import json
import subprocess

from runtime import LABEL, PREFIX, PRIVATE, own, run, save_json

if not __debug__:
    raise RuntimeError('Runtime lifecycle requires Python assertions; optimized mode is refused')

REGISTRY = PRIVATE / 'owned-resources.json'
ALLOWED = {
    'container': {'postgres', 'redis', 'server', 'ingress', 'mock', 'rollback', 'probe'},
    'network': {'internal', 'ingress-net'},
    'volume': {'pgdata', 'data'},
}


def identity(kind, data):
    if kind == 'volume':
        return {'name': data['Name'], 'created_at': data['CreatedAt'], 'mountpoint': data['Mountpoint']}
    return {'id': data['Id']}


def validate_name(kind, name):
    assert name.startswith(PREFIX + '-') and name[len(PREFIX) + 1:] in ALLOWED[kind], 'unrecognized runtime resource'


def load():
    state = json.loads(REGISTRY.read_text())
    assert state['label'] == LABEL and state['prefix'] == PREFIX
    return state


def register():
    state = load() if REGISTRY.exists() else {'label': LABEL, 'prefix': PREFIX, 'resources': []}
    for kind, suffixes in ALLOWED.items():
        for suffix in sorted(suffixes):
            name = PREFIX + '-' + suffix
            data = own(kind, name)
            if data is None:
                continue
            entry = {'kind': kind, 'name': name, 'identity': identity(kind, data)}
            if kind == 'container':
                # postgres:18-alpine declares this volume even when its entrypoint
                # is replaced. Record its exact identity but never delete an
                # unlabelled anonymous volume as part of scoped cleanup.
                entry['retained_anonymous_volumes'] = []
                for mount in data.get('Mounts', []):
                    if mount['Type'] == 'volume' and mount['Name'] not in (PREFIX + '-data', PREFIX + '-pgdata'):
                        assert mount['Destination'] == '/var/lib/postgresql' and len(mount['Name']) == 64
                        from runtime import inspect
                        volume = inspect('volume', mount['Name'])
                        entry['retained_anonymous_volumes'].append(identity('volume', volume))
            previous = next((v for v in state['resources'] if v['kind'] == kind and v['name'] == name), None)
            if previous is not None:
                assert previous['identity'] == entry['identity'], 'resource identity changed; refusing registration'
                if 'retained_anonymous_volumes' in previous:
                    assert previous == entry, 'recorded anonymous volume association changed'
                else:
                    previous.update(entry)
            else:
                state['resources'].append(entry)
    state['recorded_at'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    save_json(REGISTRY, state)
    check()
    print('RESOURCES_REGISTERED identities recorded; no resources stopped or deleted', flush=True)


def check():
    state = load()
    existing = []
    known_names = {entry['name'] for entry in state['resources'] if entry['kind'] == 'container'}
    known_ids = {entry['identity']['id'] for entry in state['resources'] if entry['kind'] == 'container'}
    for entry in state['resources']:
        kind, name = entry['kind'], entry['name']
        validate_name(kind, name)
        data = own(kind, name)
        if data is None:
            continue
        assert identity(kind, data) == entry['identity'], 'recorded resource identity changed'
        if kind == 'network':
            for container_id, endpoint in data.get('Containers', {}).items():
                assert container_id in known_ids and endpoint['Name'] in known_names, 'foreign network endpoint; refusing lifecycle action'
        elif kind == 'volume':
            result = subprocess.run(['docker', 'ps', '-a', '-q', '--no-trunc', '--filter', 'volume=' + name], capture_output=True, check=True)
            assert set(result.stdout.decode().split()) <= known_ids, 'foreign volume reference; refusing lifecycle action'
        elif kind == 'container':
            assert all(network.startswith(PREFIX + '-') and network[len(PREFIX) + 1:] in ALLOWED['network']
                       for network in data['NetworkSettings']['Networks']), 'container has foreign network; refusing lifecycle action'
            for mount in data.get('Mounts', []):
                if mount['Type'] == 'volume':
                    if mount['Name'] not in (PREFIX + '-data', PREFIX + '-pgdata'):
                        from runtime import inspect
                        volume = inspect('volume', mount['Name'])
                        assert mount['Destination'] == '/var/lib/postgresql'
                        assert identity('volume', volume) in entry.get('retained_anonymous_volumes', []), 'container has unrecorded foreign volume'
                        refs = subprocess.run(['docker', 'ps', '-a', '-q', '--no-trunc', '--filter', 'volume=' + mount['Name']], capture_output=True, check=True)
                        assert set(refs.stdout.decode().split()) == {data['Id']}, 'inherited anonymous volume has foreign references'
        existing.append(entry)
    # Refuse unrecorded known runtime containers before any destructive operation.
    for suffix in ALLOWED['container']:
        name = PREFIX + '-' + suffix
        if own('container', name) is not None:
            assert name in known_names, 'register the newly created runtime container before lifecycle action'
    return existing


def stop():
    entries = check()  # Preflight the complete set before touching any resource.
    order = ['ingress', 'probe', 'rollback', 'server', 'mock', 'redis', 'postgres']
    by_name = {entry['name']: entry for entry in entries if entry['kind'] == 'container'}
    for suffix in order:
        name = PREFIX + '-' + suffix
        if name not in by_name:
            continue
        data = own('container', name)
        assert identity('container', data) == by_name[name]['identity']
        if data['State']['Running']:
            run(['docker', 'stop', '--time', '30', by_name[name]['identity']['id']], 'lifecycle-stop-' + suffix)
    print('OWNED_RUNTIME_STOPPED', flush=True)


def cleanup():
    check()
    stop()
    for kind in ('container', 'network', 'volume'):
        entries = [entry for entry in check() if entry['kind'] == kind]
        for entry in entries:
            data = own(kind, entry['name'])
            assert data is not None and identity(kind, data) == entry['identity']
            target = entry['name'] if kind == 'volume' else entry['identity']['id']
            run(['docker', kind, 'rm', target], 'lifecycle-remove-' + entry['name'])
    archived = PRIVATE / ('owned-resources.cleaned-' + datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '.json')
    REGISTRY.rename(archived)
    print('OWNED_RUNTIME_REMOVED containers/networks/volumes only; private artifacts and images retained', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('register', 'check', 'stop', 'cleanup'))
    args = parser.parse_args()
    try:
        {'register': register, 'check': lambda: (check(), print('OWNERSHIP_PREFLIGHT_PASS')), 'stop': stop, 'cleanup': cleanup}[args.action]()
    except Exception as error:
        print(type(error).__name__ + ': ' + str(error), flush=True)
        raise SystemExit(1)
