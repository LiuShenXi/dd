"""Verify exported OCI index -> platform manifest -> imported classic image.

Only selected non-secret image metadata is persisted. The local container runs
sha256sum with networking disabled; it never starts the application.
"""
import hashlib
import json
import subprocess
import tarfile
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent
EXPECTED = 'sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677'
TAG = 'sub2api:2.8.13-monas-bps.13'
export = json.loads((ROOT / 'coordinator-r0-export-20261019T170157Z.json').read_text())
archive = Path(export['private_archive'])
with archive.open('rb') as f:
    assert hashlib.file_digest(f, 'sha256').hexdigest() == export['archive_sha256']

with tarfile.open(archive, 'r:gz') as tf:
    def blob(digest):
        algorithm, value = digest.split(':', 1)
        assert algorithm == 'sha256'
        raw = tf.extractfile('blobs/sha256/' + value).read()
        assert hashlib.sha256(raw).hexdigest() == value
        return json.loads(raw)

    top = json.load(tf.extractfile('index.json'))
    assert top['manifests'][0]['digest'] == EXPECTED
    index = blob(EXPECTED)
    platforms = [m for m in index['manifests'] if m.get('platform', {}).get('os') == 'linux' and m.get('platform', {}).get('architecture') == 'amd64']
    assert len(platforms) == 1
    manifest = blob(platforms[0]['digest'])
    config_digest = manifest['config']['digest']
    config = blob(config_digest)
    legacy = json.load(tf.extractfile('manifest.json'))
    assert legacy[0]['Config'] == 'blobs/sha256/' + config_digest.split(':')[1]

def output(args):
    return subprocess.check_output(args, text=True, encoding='utf-8', timeout=90).strip()

local = json.loads(output(['docker', 'image', 'inspect', TAG]))[0]
assert local['Id'] == config_digest
assert local['RootFS']['Layers'] == config['rootfs']['diff_ids']
remote_script = '''import json,subprocess
def run(a): return subprocess.check_output(a,text=True).strip()
v=json.loads(run(['docker','image','inspect','sub2api:2.8.13-monas-bps.13']))[0]
c=json.loads(run(['docker','container','inspect','sub2api2']))[0]
print(json.dumps({'image_id':v['Id'],'container_image_id':c['Image'],'descriptor':v.get('Descriptor'),'rootfs':v['RootFS'],'revision':v['Config'].get('Labels',{}).get('org.opencontainers.image.revision'),'binary_sha256':run(['docker','exec','sub2api2','sha256sum','/app/sub2api']).split()[0],'engine_version':run(['docker','version','--format','{{.Server.Version}}'])}))
'''
proc = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', 'main-server', 'python3', '-'], input=remote_script, text=True, encoding='utf-8', capture_output=True, timeout=90, check=True)
remote = json.loads(proc.stdout)
assert remote['image_id'] == remote['container_image_id'] == EXPECTED
assert remote['descriptor']['digest'] == EXPECTED
assert remote['rootfs']['Layers'] == local['RootFS']['Layers']
binary = output(['docker', 'run', '--rm', '--network', 'none', '--entrypoint', 'sha256sum', config_digest, '/app/sub2api']).split()[0]
assert binary == remote['binary_sha256'] == '85d991720e15993813fdfdddc408852fefe90c54314e760d68765cf5ba0767cd'
revision = (local['Config'].get('Labels') or {}).get('org.opencontainers.image.revision')
assert revision == remote['revision'] == '8d869d41f40d6eedbe2ee05aaf8973145cccbe4d'
receipt = {
    'checked_at_utc': datetime.now(timezone.utc).isoformat(),
    'archive_sha256': export['archive_sha256'],
    'production_oci_index_digest': EXPECTED,
    'platform': platforms[0]['platform'],
    'platform_manifest_digest': platforms[0]['digest'],
    'config_digest_and_classic_import_id': config_digest,
    'production_engine': remote['engine_version'],
    'local_engine': output(['docker', 'version', '--format', '{{.Server.Version}}']),
    'rootfs_diff_ids_identical': True,
    'rootfs_layer_count': len(local['RootFS']['Layers']),
    'revision': revision,
    'binary_sha256_identical': binary,
    'application_started': False,
    'local_probe_network': 'none',
    'production_modified': False,
    'provenance_verified': True,
    'id_difference_explanation': 'The production containerd store reports the OCI index digest; the classic Docker image store reports its linux/amd64 image configuration digest. The exported OCI graph, local RootFS, labels and executable match production.',
}
path = ROOT / 'coordinator-r0-provenance.json'
path.write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
print(json.dumps(receipt, indent=2))
