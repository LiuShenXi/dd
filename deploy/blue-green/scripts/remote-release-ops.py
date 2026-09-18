#!/usr/bin/env python3
"""Retired one-off carpool release entrypoint; configuration cleanup only."""
import argparse
import datetime
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile

ROOT = Path('/home/linuxuser/apps/sub2api')
FLAG = 'RELEASE_DRAIN_START_HELD'


def clear_stage(work):
    """Disarm stage files without changing containers or resuming in-memory gates."""
    if work.is_symlink():
        raise ValueError('Symlinked release directories are not allowed')
    files = sorted(work.glob('stage-override.private.json*'))
    parsed, original = {}, {}
    for path in files:
        if path.is_symlink() or not path.is_file() or path.resolve().parent != work.resolve():
            raise ValueError('Unsafe stage path')
        original[path] = path.read_bytes()
        data = json.loads(original[path])
        if not isinstance(data.get('services'), dict):
            raise ValueError('Invalid stage override')
        for service in data['services'].values():
            env = service.get('environment', {})
            if isinstance(env, list):
                env = dict(item.split('=', 1) if '=' in item else (item, None) for item in env)
                service['environment'] = env
            if not isinstance(env, dict):
                raise ValueError('Invalid stage environment')
            if FLAG in env:
                env[FLAG] = 'false'
        parsed[path] = data
    canonical = work / 'stage-override.private.json'
    if not files:
        return {'changed': False, 'running_containers_modified': False}
    if len(files) == 1 and canonical in original:
        old = json.loads(original[canonical])
        if old == parsed[canonical]:
            return {'changed': False, 'running_containers_modified': False}
    archive_dir = work / 'release-guard-archive'
    if archive_dir.is_symlink():
        raise ValueError('Unsafe archive directory')
    archive_dir.mkdir(mode=0o700, exist_ok=True)
    os.chmod(archive_dir, 0o700)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    archive = archive_dir / ('stage-overrides-' + stamp + '.tar.gz')
    with archive.open('xb') as stream:
        os.chmod(archive, 0o600)
        with tarfile.open(fileobj=stream, mode='w:gz') as tar:
            for path, content in original.items():
                info = tarfile.TarInfo(path.name)
                info.size, info.mode = len(content), 0o600
                tar.addfile(info, io.BytesIO(content))
        stream.flush()
        os.fsync(stream.fileno())
    with tarfile.open(archive, 'r:gz') as tar:
        for path, content in original.items():
            if tar.extractfile(path.name).read() != content:
                raise ValueError('Backup verification failed')
    for path, content in original.items():
        if path.read_bytes() != content:
            raise ValueError('Stage file changed during cleanup; abort and recheck')
    if canonical in parsed:
        fd, tmp = tempfile.mkstemp(prefix='.stage-safe-', dir=work)
        try:
            with os.fdopen(fd, 'w') as stream:
                json.dump(parsed[canonical], stream, indent=2)
                stream.write('\n')
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(tmp, canonical)
        finally:
            if os.path.exists(tmp):
                os.unlink(tmp)
    for path in files:
        if path != canonical:
            path.unlink()
    return {'changed': True, 'archive': str(archive), 'archived_files': len(files),
            'removed_runnable_backup_files': sum(p != canonical for p in files),
            'running_containers_modified': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['clear-stage', 'backup', 'auth', 'prepare-stage', 'probe'])
    parser.add_argument('--workdir', required=True)
    parser.add_argument('--image')
    parser.add_argument('--port', type=int, default=8080)
    args = parser.parse_args()
    if args.operation != 'clear-stage':
        parser.error('Legacy one-off release operations are retired. No override, token, container, or upstream request was created. Use the guarded standard deployment flow; migration requires separate approval.')
    work = Path(args.workdir)
    if work.parent != ROOT or not work.name.startswith('production-release-20260908-') or work.resolve() != work:
        parser.error('Unexpected release directory')
    os.umask(0o077)
    print(json.dumps(clear_stage(work)))


if __name__ == '__main__':
    main()
