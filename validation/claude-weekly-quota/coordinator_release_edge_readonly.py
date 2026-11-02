"""Read-only, sanitized ingress and application-replica inventory."""
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path

REMOTE = r'''
import json,re,subprocess
def run(args):
    p=subprocess.run(args,text=True,capture_output=True)
    return p.returncode,p.stdout,p.stderr
code,out,_=run(['docker','ps','-q'])
if code: raise SystemExit('container inventory unavailable')
replicas=[]
for cid in out.split():
    code,raw,_=run(['docker','inspect',cid])
    if code: raise SystemExit('container inspect unavailable')
    c=json.loads(raw)[0]
    image=c['Config'].get('Image','')
    command=' '.join(c['Config'].get('Cmd') or [])
    if 'sub2api' not in image.lower() and '/app/sub2api' not in command: continue
    env=dict(v.split('=',1) for v in c['Config'].get('Env',[]) if '=' in v)
    labels=c['Config'].get('Labels',{})
    replicas.append({'name':c['Name'].lstrip('/'),'image':image,'image_id':c['Image'],
       'status':c['State']['Status'],'started_at':c['State']['StartedAt'],
       'compose_project':labels.get('com.docker.compose.project'),
       'compose_service':labels.get('com.docker.compose.service'),
       'release_start_held':env.get('RELEASE_DRAIN_START_HELD'),
       'carpool_background_enabled':env.get('CARPOOL_BACKGROUND_ENABLED'),
       'subscription_quota_environment_names':sorted(k for k in env if 'SUBSCRIPTION_QUOTA' in k)})
services={}
for name in ['nginx','caddy','haproxy','apache2']:
    code,out,_=run(['systemctl','is-active',name])
    services[name]=out.strip() or ('unavailable' if code else 'active')
code,out,err=run(['nginx','-T'])
ingress=[]
if not code:
    source=None
    for line in (out+'\n'+err).splitlines():
        if line.startswith('# configuration file '): source=line[len('# configuration file '):].rstrip(':')
        if re.match(r'^\s*(server_name|listen|proxy_pass|upstream)\s',line):
            clean=line.strip()
            if re.search(r'https?://[^\s/]+@',clean): clean='[credential-bearing upstream omitted]'
            ingress.append({'file':source,'directive':clean})
print(json.dumps({'read_only':True,'application_replicas':replicas,'edge_services':services,
 'nginx_inventory_available':not bool(code),'selected_nginx_directives':ingress,
 'production_mutation_performed':False}))
'''
proc = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', 'main-server', 'python3', '-'], input=REMOTE, text=True, encoding='utf-8', capture_output=True, timeout=60)
if proc.returncode:
    raise SystemExit('Read-only edge inventory failed; captured raw output is not printed.')
data = json.loads(proc.stdout)
data['checked_at_utc'] = datetime.now(timezone.utc).isoformat()
target = Path(__file__).with_name('coordinator-release-edge-readonly.json')
target.write_text(json.dumps(data, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'receipt':str(target),'application_replicas':data['application_replicas'], 'edge_services':data['edge_services'], 'nginx_inventory_available':data['nginx_inventory_available'], 'production_mutation_performed':False},indent=2))
