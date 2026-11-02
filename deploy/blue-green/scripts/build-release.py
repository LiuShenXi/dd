#!/usr/bin/env python3
"""Build a committed amd64 release against the retained, verified R0 runtime."""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[3]
VALIDATION = ROOT / 'validation/claude-weekly-quota'
spec = importlib.util.spec_from_file_location('source_snapshot', VALIDATION / 'office-source-snapshot.py')
snapshot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(snapshot)


def sha(path):
    with path.open('rb') as handle:
        return hashlib.file_digest(handle, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-dir', type=Path, required=True)
    parser.add_argument('--receipt', type=Path, required=True)
    parser.add_argument('--version', default='2.8.13-monas-claude-weekly.1')
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]+', args.version):
        parser.error('Invalid release version')
    private = args.private_dir.resolve()
    allowed = Path.home() / '.codex/private/claude-weekly-quota'
    if not private.is_relative_to(allowed) or private.exists():
        parser.error('Use a fresh directory inside the task private directory')
    changed = subprocess.check_output(['git', 'status', '--porcelain', '--', 'backend', 'frontend', 'docs/legal', 'deploy/blue-green', 'deploy/docker-entrypoint.sh'], cwd=ROOT)
    if changed:
        parser.error('Commit application and release tooling changes before building')
    private.mkdir(mode=0o700, parents=True)
    before = snapshot.capture(ROOT)
    (private / 'source-before.json').write_text(json.dumps(before, indent=2) + '\n')
    created = datetime.now(timezone.utc).isoformat()
    receipt = {'started_utc': created, 'head': before['head'], 'source_sha256': before['source_sha256'],
               'frontend_dist_sha256': before['frontend_dist_sha256'], 'version': args.version,
               'target': 'linux/amd64', 'builder_sha256': sha(Path(__file__)), 'status': 'building', 'operations': []}
    def run(name, command, cwd=ROOT, env=None):
        log = private / (name + '.log')
        with log.open('w') as handle:
            p = subprocess.run(command, cwd=cwd, env=env, stdout=handle, stderr=subprocess.STDOUT, timeout=2400)
        receipt['operations'].append({'name': name, 'exit_code': p.returncode, 'log_sha256': sha(log)})
        print(json.dumps({'operation': name, 'exit_code': p.returncode}), flush=True)
        p.check_returncode()
    def inspect(tag):
        return json.loads(subprocess.check_output(['docker', 'image', 'inspect', tag]))[0]
    try:
        base_tag = 'sub2api:2.8.13-monas-bps.13'
        base = inspect(base_tag)
        provenance = json.loads((VALIDATION / 'office-r0-provenance.json').read_text())
        assert provenance['provenance_verified'] and base['Id'] == provenance['local_image_id']
        assert len(base['RootFS']['Layers']) == provenance['rootfs_layer_count']
        receipt['runtime_base_image_id'] = base['Id']
        recipe = ROOT / 'deploy/blue-green/Dockerfile.runtime'
        receipt['runtime_recipe_sha256'] = sha(recipe)
        receipt['go_version'] = subprocess.check_output(['go', 'version'], cwd=ROOT / 'backend', text=True).strip()
        candidate = private / 'candidate'
        candidate.mkdir()
        env = os.environ | {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '0', 'GOMAXPROCS': '3'}
        flags = f"-s -w -X main.Version={args.version} -X main.Commit={before['head']} -X main.Date={created} -X main.BuildType=release"
        run('server', ['go','build','-p=1','-tags=embed','-trimpath','-buildvcs=false','-ldflags='+flags,'-o',str(candidate/'sub2api'),'./cmd/server'], ROOT/'backend', env)
        run('release-cli', ['go','build','-p=1','-trimpath','-buildvcs=false','-o',str(candidate/'carpool-release'),'./cmd/carpool-release'], ROOT/'backend', env)
        shutil.copytree(ROOT/'backend/resources', candidate/'resources')
        shutil.copy2(recipe, candidate/'Dockerfile')
        tag = 'sub2api:' + args.version + '-' + before['head'][:12]
        run('image', ['docker','build','--platform=linux/amd64','--network=none','--pull=false','-t',tag,'--build-arg','RUNTIME_BASE='+base_tag,'--build-arg','REVISION='+before['head'],'--build-arg','SOURCE_SHA256='+before['source_sha256'],'--build-arg','BUILD_DATE='+created,str(candidate)])
        image = inspect(tag)
        assert image['RootFS']['Layers'][:len(base['RootFS']['Layers'])] == base['RootFS']['Layers']
        run('version', ['docker','run','--rm','--platform=linux/amd64','--network=none','--read-only','--user=1000:1000','--entrypoint=/app/sub2api',tag,'--version'])
        after = snapshot.capture(ROOT)
        (private/'source-after.json').write_text(json.dumps(after,indent=2)+'\n')
        assert before['source_sha256'] == after['source_sha256'] and before['head'] == after['head'], 'Source changed during build'
        assert receipt['runtime_recipe_sha256'] == sha(recipe), 'Image recipe changed during build'
        receipt.update({'image':tag,'image_id':image['Id'],'binaries':{name:sha(candidate/name) for name in ['sub2api','carpool-release']},'source_after_sha256':after['source_sha256'],'runtime_base_layers_retained':True,'status':'built_pending_runtime_acceptance'})
    except Exception:
        receipt['status'] = 'failed'
        raise
    finally:
        receipt['finished_utc'] = datetime.now(timezone.utc).isoformat()
        args.receipt.write_text(json.dumps(receipt,indent=2)+'\n')
        print(json.dumps({'status':receipt['status'],'receipt':str(args.receipt)}),flush=True)


if __name__ == '__main__':
    main()
