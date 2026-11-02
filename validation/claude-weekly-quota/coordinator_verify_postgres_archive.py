"""Validate production image archive provenance without starting PostgreSQL."""
import hashlib
import json
import tarfile
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
source = HERE / 'coordinator-postgres-runtime-export-20261019T180857Z.json'
record = json.loads(source.read_text(encoding='utf-8'))
archive = Path(record['private_archive'])
with archive.open('rb') as stream:
    assert hashlib.file_digest(stream,'sha256').hexdigest() == record['archive_sha256']
with tarfile.open(archive,'r:gz') as tf:
    def blob(digest):
        algorithm, value = digest.split(':',1)
        assert algorithm == 'sha256'
        data = tf.extractfile('blobs/sha256/'+value).read()
        assert hashlib.sha256(data).hexdigest() == value
        return json.loads(data)
    top = json.load(tf.extractfile('index.json'))
    expected = record['metadata']['image_id']
    assert len(top['manifests']) == 1 and top['manifests'][0]['digest'] == expected
    root = blob(expected)
    selected = [m for m in root['manifests'] if m.get('platform',{}).get('os')=='linux'
                and m.get('platform',{}).get('architecture')=='amd64']
    assert len(selected) == 1
    manifest_digest = selected[0]['digest']
    manifest = blob(manifest_digest)
    config_digest = manifest['config']['digest']
    config = blob(config_digest)
    assert config['rootfs']['diff_ids'] == record['metadata']['rootfs_diff_ids']
    legacy = json.load(tf.extractfile('manifest.json'))
    assert len(legacy) == 1
    assert legacy[0]['Config'] == 'blobs/sha256/'+config_digest.split(':',1)[1]
    tags = legacy[0].get('RepoTags') or []
result = {
    'checked_at_utc':datetime.now(timezone.utc).isoformat(),'export_receipt':source.name,
    'archive_sha256':record['archive_sha256'],'production_oci_index_digest':expected,
    'platform_manifest_digest':manifest_digest,'config_digest':config_digest,
    'rootfs_diff_ids_match_production':True,'rootfs_layer_count':len(config['rootfs']['diff_ids']),
    'runtime_version_observed_in_production':record['metadata']['program_version'],
    'runtime_program_sha256_observed_in_production':record['metadata']['program_sha256'],
    'archive_repo_tags':tags,'archive_provenance_verified':True,
    'local_image_loaded':False,'isolated_postgres_started':False,
    'database_rows_copied':False,'production_modified':False,'I6_migration_test_performed':False,
}
target=HERE/'coordinator-postgres-runtime-provenance.json'
target.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
print(json.dumps(result,indent=2))
