#!/usr/bin/env bash
set -Eeuo pipefail
r=/home/linuxuser/apps/sub2api
w=/root/bps-sse-limit-20260927
exec 9>"$r/run/deploy.lock"
flock -n 9
test "$(cat "$r/run/active-slot")" = green
test "$(docker inspect sub2api-green --format '{{.Image}}')" = sha256:6618da177d57996b7c81e33331c172d96a1f63a9074d960e8aff74b9ef54df26
test "$(docker inspect sub2api-blue --format '{{.Image}}')" = "$(cat "$w/candidate-image-id.txt")"
python3 - <<'PY'
import json
from pathlib import Path
w=Path('/root/bps-sse-limit-20260927')
for kind in ('function','custom'):
    p=json.loads((w/('candidate-'+kind+'.json')).read_text())
    assert p['passed'] and p['http']==200 and p['event']=='response.completed'
PY
rollback_on_error() {
 local rc=$?
 trap - ERR
 if [ "$(cat "$r/run/active-slot")" = blue ]; then
  bash "$r/blue-green/scripts/switch-slot.sh" green
 fi
 date -u +%Y-%m-%dT%H:%M:%SZ > "$w/rollback-at.txt"
 exit "$rc"
}
trap rollback_on_error ERR
bash "$r/blue-green/scripts/switch-slot.sh" blue
date -u +%Y-%m-%dT%H:%M:%SZ > "$w/cutover-at.txt"
for kind in function custom; do
 python3 "$w/probe-tools.py" https://sub2api.monasapi.com "$kind" > "$w/production-$kind.json"
 cat "$w/production-$kind.json"
done
python3 "$r/blue-green/scripts/release-guard.py" monitor
trap - ERR
date -u +%Y-%m-%dT%H:%M:%SZ > "$w/verified-at.txt"
