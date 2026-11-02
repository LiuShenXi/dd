"""Merge the user-authorized architecture v2 without replacing implementation."""
import hashlib
import json
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REL = Path('.trellis/tasks/10-19-claude-weekly-quota-architecture')
SOURCE = Path('C:/WORK-SPACE/dd') / REL
DEST = ROOT / REL
EVIDENCE = Path(__file__).resolve().parent

def sha(data):
    return hashlib.sha256(data).hexdigest()

status = subprocess.check_output(['git', '-C', str(ROOT), 'status', '--porcelain', '--', str(REL)], text=True)
assert not status.strip(), 'Architecture has local changes; manual merge required'
manifest = json.loads((SOURCE / 'evidence/package-manifest.json').read_text(encoding='utf-8'))
for item in manifest['files']:
    raw = (SOURCE / item['path']).read_bytes()
    assert len(raw) == item['bytes'] and sha(raw) == item['sha256'], item['path']

files = []
for source in sorted(p for p in SOURCE.rglob('*') if p.is_file()):
    relative = source.relative_to(SOURCE)
    target = DEST / relative
    raw = source.read_bytes()
    previous = sha(target.read_bytes()) if target.is_file() else None
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(raw)
    assert sha(target.read_bytes()) == sha(raw)
    files.append({'path': relative.as_posix(), 'before_sha256': previous, 'after_sha256': sha(raw)})

tracker_path = EVIDENCE / 'coordinator-acceptance.json'
tracker = json.loads(tracker_path.read_text(encoding='utf-8-sig'))
existing = {x['id'] for x in tracker['tests']}
for line in (DEST / 'test-plan.md').read_text(encoding='utf-8').splitlines():
    match = re.match(r'\| (T\d+) \| (.*?) \| (.*?) \|$', line)
    if match and match[1] not in existing:
        tracker['tests'].append({'id': match[1], 'scenario': match[2], 'required_assertions': match[3], 'status': 'not_run', 'evidence': [], 'acceptance_note': 'Added by user-authorized v2 concurrency scope; architecture validation is not execution evidence.'})
assert len(tracker['tests']) == 74
tracker['scope'] = 'code, isolated synthetic tests, release/rollback rehearsal; user additionally authorized production deployment and remote branch push after acceptance'
tracker['architecture_version'] = 'v2'
tracker['concurrency_v2_status'] = 'implementation_required; previous I0-I3 acceptance remains v1 historical evidence only'
tracker['production_deployment_authorized'] = True
tracker['remote_push_authorized'] = True
tracker['production_card_issuance_authorized'] = False
tracker['production_model_consumption_authorized'] = False
tracker_path.write_text(json.dumps(tracker, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')

receipt = {'checked_at_utc': datetime.now(timezone.utc).isoformat(), 'action': 'non-destructive architecture v2 merge', 'source': str(SOURCE), 'destination': str(DEST), 'manifest_entries_verified': len(manifest['files']), 'files_copied': len(files), 'preserved_destination_only_files': [p.relative_to(DEST).as_posix() for p in DEST.rglob('*') if p.is_file() and not (SOURCE / p.relative_to(DEST)).exists()], 'prior_accepted_head': subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip(), 'acceptance_scenarios': len(tracker['tests']), 'business_code_modified': False, 'production_modified': False, 'files': files}
(EVIDENCE / 'coordinator-architecture-v2-merge.json').write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
print(json.dumps({k:v for k,v in receipt.items() if k != 'files'}, ensure_ascii=False, indent=2))
