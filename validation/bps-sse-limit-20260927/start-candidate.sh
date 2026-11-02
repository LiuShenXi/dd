#!/usr/bin/env bash
set -Eeuo pipefail
r=/home/linuxuser/apps/sub2api
w=/root/bps-sse-limit-20260927
exec 9>"$r/run/deploy.lock"
flock -n 9
test "$(cat "$r/run/active-slot")" = green
test "$(docker inspect sub2api-green --format '{{.Image}}')" = sha256:6618da177d57996b7c81e33331c172d96a1f63a9074d960e8aff74b9ef54df26
test "$(docker image inspect sub2api:2.8.13-monas-bps.6 --format '{{.Id}}')" = sha256:6618da177d57996b7c81e33331c172d96a1f63a9074d960e8aff74b9ef54df26
test "$(docker inspect sub2api-blue --format '{{.State.Running}}')" = false
(cd "$r/backups/automated/20260927T054235Z" && sha256sum -c SHA256SUMS)
mkdir -p "$w/candidate"
tar -xzf "$w/candidate.tar.gz" -C "$w/candidate"
(cd "$w/candidate" && sha256sum -c SHA256SUMS)
docker build --pull=false --network=none -t sub2api:2.8.13-monas-bps.7 "$w/candidate"
docker run --rm --network=none --entrypoint /app/sub2api sub2api:2.8.13-monas-bps.7 -version
test ! -e "$w/slot-images.before.env"
cp -p "$r/blue-green/slot-images.env" "$w/slot-images.before.env"
cp -p "$r/blue-green/router/conf.d/default.conf" "$w/router.before.conf"
python3 - <<'PY'
from pathlib import Path
import json, subprocess
r='/home/linuxuser/apps/sub2api'
c=json.loads(subprocess.check_output(['docker','compose','--env-file',r+'/.env','--env-file',r+'/blue-green/slot-images.env','-f',r+'/blue-green/docker-compose.yml','config','--format','json'],text=True))['services']['sub2api-blue']
a=json.loads(subprocess.check_output(['docker','inspect','sub2api-green'],text=True))[0]
live=dict(v.split('=',1) for v in a['Config']['Env'])
assert all(str(v)==live.get(k) for k,v in c['environment'].items()), 'candidate environment differs'
assert str(c['mem_limit'])==str(a['HostConfig']['Memory']), 'memory limit differs'
p=Path(r+'/blue-green/slot-images.env')
lines=p.read_text().splitlines()
assert 'SUB2API_GREEN_IMAGE=sub2api:2.8.13-monas-bps.6' in lines
assert 'SUB2API_BLUE_IMAGE=sub2api:2.8.13-monas-bps.5' in lines
p.write_text('\n'.join('SUB2API_BLUE_IMAGE=sub2api:2.8.13-monas-bps.7' if x.startswith('SUB2API_BLUE_IMAGE=') else x for x in lines)+'\n')
print('candidate environment and memory limit match production')
PY
source "$r/blue-green/scripts/common.sh"
compose config -q
compose up -d --no-deps --force-recreate sub2api-blue
wait_for_slot blue
python3 "$r/blue-green/scripts/release-guard.py" probe http://127.0.0.1:18080
docker image inspect sub2api:2.8.13-monas-bps.7 --format '{{.Id}}' > "$w/candidate-image-id.txt"
