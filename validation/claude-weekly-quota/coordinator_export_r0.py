"""Read-only production image export to a private local test-artifact folder."""
import hashlib
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path

EXPECTED = 'sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677'
TAG = 'sub2api:2.8.13-monas-bps.13'
SSH = ['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', 'main-server']
stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
root = Path(os.environ['LOCALAPPDATA']) / 'Codex/PrivateTests/claude-weekly-quota/r0-image'
root.mkdir(parents=True, exist_ok=True)
partial = root / f'production-r0-{stamp}.tar.gz.partial'
target = partial.with_suffix('')
actual = subprocess.check_output(SSH + ['docker image inspect ' + TAG + " --format '{{.Id}}'"], text=True).strip()
assert actual == EXPECTED
print(json.dumps({'phase': 'read_only_export_started', 'expected_image_id': EXPECTED, 'private_output': str(target)}), flush=True)
with partial.open('xb') as stream:
    result = subprocess.run(SSH + ["bash -o pipefail -c 'docker image save " + TAG + " | gzip -1'"], stdout=stream, stderr=subprocess.PIPE, timeout=1800)
if result.returncode:
    raise SystemExit('Image export failed; partial file retained privately; production unchanged')
with partial.open('rb') as stream:
    digest = hashlib.file_digest(stream, 'sha256').hexdigest()
partial.rename(target)
receipt = {'checked_at_utc': datetime.now(timezone.utc).isoformat(), 'production_action': 'read-only docker image inspect/save', 'tag': TAG, 'image_id': actual, 'private_archive': str(target), 'archive_bytes': target.stat().st_size, 'archive_sha256': digest, 'local_load_verified': False, 'production_modified': False}
evidence = Path(__file__).resolve().parent / ('coordinator-r0-export-' + stamp + '.json')
evidence.write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
print(json.dumps(receipt, indent=2), flush=True)
