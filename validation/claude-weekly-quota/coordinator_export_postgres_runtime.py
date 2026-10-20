"""Read-only copy of the production PostgreSQL image for later isolated I6 tests.

Exports image layers only, never volumes or database rows. The private archive
is not loaded automatically, so existing local image tags remain unchanged.
"""
import hashlib
import json
import os
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

SSH = ['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', 'main-server']
READ = r'''
import json,subprocess
def run(args): return subprocess.check_output(args,text=True).strip()
c=json.loads(run(['docker','inspect','sub2api2-postgres']))[0]
i=json.loads(run(['docker','image','inspect',c['Image']]))[0]
print(json.dumps({'container_image':c['Image'],'configured_tag':c['Config']['Image'],
 'image_id':i['Id'],'rootfs_diff_ids':i['RootFS']['Layers'],
 'program_version':run(['docker','exec','sub2api2-postgres','postgres','--version']),
 'program_sha256':run(['docker','exec','sub2api2-postgres','sha256sum','/usr/local/bin/postgres']).split()[0]}))
'''
proc = subprocess.run(SSH + ['python3', '-'], input=READ, text=True,
                      encoding='utf-8', capture_output=True, timeout=45)
if proc.returncode:
    raise SystemExit('Read-only PostgreSQL image inspection failed; no database action performed.')
metadata = json.loads(proc.stdout)
digest = metadata['image_id']
if not re.fullmatch(r'sha256:[0-9a-f]{64}', digest):
    raise SystemExit('Unexpected image digest; export refused.')
if metadata['container_image'] != digest or metadata['program_version'] != 'postgres (PostgreSQL) 18.6':
    raise SystemExit('Production PostgreSQL runtime drifted; inspect before preparing I6.')
stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
folder = Path(os.environ['LOCALAPPDATA']) / 'Codex/PrivateTests/claude-weekly-quota/pg-production-image'
folder.mkdir(parents=True, exist_ok=True)
partial = folder / f'production-postgres-{stamp}.tar.gz.partial'
target = partial.with_suffix('')
print(json.dumps({'phase':'image_only_export_started','version':metadata['program_version'],'image_id':digest}),flush=True)
with partial.open('xb') as stream:
    result = subprocess.run(SSH + ["bash -o pipefail -c 'docker image save " + digest + " | gzip -1'"],
                            stdout=stream,stderr=subprocess.PIPE,timeout=900)
if result.returncode:
    raise SystemExit('Image export failed; private partial retained; production unchanged.')
with partial.open('rb') as stream:
    archive_digest = hashlib.file_digest(stream,'sha256').hexdigest()
partial.rename(target)
receipt = {
    'checked_at_utc':datetime.now(timezone.utc).isoformat(),
    'purpose':'Exact production PostgreSQL runtime for later synthetic I6 migration rehearsal',
    'metadata':metadata,'private_archive':str(target),'archive_bytes':target.stat().st_size,
    'archive_sha256':archive_digest,'production_action':'read-only image inspect/save and program version/hash',
    'database_rows_copied':False,'production_modified':False,'local_image_loaded':False,
    'I6_migration_test_performed':False,
}
path=Path(__file__).resolve().parent / f'coordinator-postgres-runtime-export-{stamp}.json'
path.write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
print(json.dumps({'receipt':str(path),'archive_bytes':receipt['archive_bytes'],'archive_sha256':archive_digest,
                  'production_modified':False,'I6_migration_test_performed':False}),flush=True)
