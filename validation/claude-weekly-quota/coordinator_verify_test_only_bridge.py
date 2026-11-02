"""Verify a test-only source change without relabeling the existing F2 image."""
import hashlib
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
BEFORE = 'environment-I4-F2-aa71-source-freeze.json'
AFTER = 'I4-main-F3-test-sync-source.json'
BUILD = 'environment-I4-F2-20261019T175928Z-build-receipt.json'
ENVIRONMENT = 'environment-I4-20261019T180032Z-active.json'
ALLOWED = ['backend/internal/service/usage_cleanup_service_test.go']

def read(name):
    return json.loads((HERE / name).read_text(encoding='utf-8-sig'))

before, after, build, environment = map(read, [BEFORE, AFTER, BUILD, ENVIRONMENT])
old = {item['path']: item['sha256'] for item in before['files']}
new = {item['path']: item['sha256'] for item in after['files']}
for manifest, inventory in [(before, old), (after, new)]:
    encoded = ''.join(path + '\0' + inventory[path] + '\n' for path in sorted(inventory)).encode('utf-8')
    assert hashlib.sha256(encoded).hexdigest() == manifest['source_sha256'], 'Invalid aggregate source hash'
assert old.keys() == new.keys(), 'File inventory changed beyond the approved test fix'
changed = sorted(path for path in old if old[path] != new[path])
assert changed == ALLOWED, 'A non-approved source change invalidates this bridge'
assert before['source_sha256'] == build['source_sha256'] == environment['source_sha256']
assert before['frontend_dist_sha256'] == after['frontend_dist_sha256'] == build['frontend_dist_sha256']
for relative, expected in new.items():
    assert hashlib.sha256((REPO / relative).read_bytes()).hexdigest() == expected, relative
provider = hashlib.sha256((HERE / 'environment-I4-provider/main.go').read_bytes()).hexdigest()
recipe = hashlib.sha256((HERE / 'environment-I4-runtime.Dockerfile').read_bytes()).hexdigest()
assert provider == build['provider_source_sha256']
assert recipe == build['runtime_recipe_sha256']
docker = 'C:/Program Files/Docker/Docker/resources/bin/docker.exe'
runtime_image = subprocess.check_output([docker,'--host','npipe:////./pipe/docker_engine','inspect',
    '--format','{{.Image}}',environment['application']],text=True,timeout=30).strip()
assert runtime_image == build['image_id'] == environment['image_id']
receipt = {
    'approved':True,'checked_at_utc':datetime.now(timezone.utc).isoformat(),
    'purpose':'Reuse the unchanged F2 application image with an explicitly identified F3 test driver',
    'image_source_sha256':before['source_sha256'],
    'driver_source_sha256':after['source_sha256'],
    'candidate_image_id':runtime_image,'changed_files':changed,
    'before_manifest':BEFORE,'after_manifest':AFTER,'compared_files':len(new),
    'frontend_dist_sha256':after['frontend_dist_sha256'],
    'provider_source_sha256':provider,'runtime_recipe_sha256':recipe,
    'unchanged_non_test_source':True,'current_source_files_rehashed':True,
    'running_image_verified':True,'production_modified':False,
    'reason':'Only a preexisting unit-test stub counter was made atomic; Go application builds exclude *_test.go files.',
    'limitations':'Does not replace the failed F2 race result. Repeat the existing race inventory on F3; preserve each earlier receipt source hash. Any other source/provider/frontend change invalidates this bridge.',
}
target = HERE / 'coordinator-I4-F2-F3-test-only-bridge.json'
target.write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
print(json.dumps(receipt,indent=2))
