"""Read-only release-control capability and sanitized actual Compose inventory."""
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path

REMOTE = r'''
import hashlib,json,pathlib,subprocess,urllib.request,urllib.error
def run(args): return subprocess.check_output(args,text=True).strip()
def inspect(name): return json.loads(run(['docker','inspect',name]))[0]
app=inspect('sub2api2'); db=inspect('sub2api2-postgres')
env=dict(v.split('=',1) for v in db['Config']['Env'] if '=' in v)
def query(sql): return run(['docker','exec','sub2api2-postgres','psql','-X','-v','ON_ERROR_STOP=1','-U',env.get('POSTGRES_USER','sub2api'),'-d',env.get('POSTGRES_DB','sub2api'),'-Atc',sql])
labels=app['Config']['Labels']; compose=pathlib.Path(labels['com.docker.compose.project.config_files'])
merged=json.loads(run(['docker','compose','--project-directory',labels['com.docker.compose.project.working_dir'],'-f',str(compose),'config','--format','json']))
services={}
for name,svc in merged['services'].items():
    services[name]={k:svc[k] for k in ['image','container_name','ports','networks','depends_on','restart'] if k in svc}
    services[name]['environment_names']=sorted(svc.get('environment',{}))
    services[name]['volume_targets']=[v.get('target') for v in svc.get('volumes',[]) if isinstance(v,dict)]
key=query("SELECT value FROM settings WHERE key='admin_api_key'")
control={'existing_admin_credential_available':bool(key),'http_status':None,'status':None}
if key:
    req=urllib.request.Request('http://127.0.0.1:29080/api/v1/admin/release/status',headers={'x-api-key':key})
    try:
        with urllib.request.urlopen(req,timeout=4) as r:
            control['http_status']=r.status
            payload=json.load(r)
        data=payload.get('data',{})
        control['status']={k:data[k] for k in ['state','active_http','pending_usage','queued_http'] if k in data}
    except urllib.error.HTTPError as e: control['http_status']=e.code
    except Exception: control['http_status']='unavailable'
del key
print(json.dumps({'read_only':True,'compose_file':str(compose),'compose_sha256':hashlib.sha256(compose.read_bytes()).hexdigest(),'compose_project':merged.get('name'),'services':services,'release_control':control,'production_mutation_performed':False}))
'''
proc = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=15', 'main-server', 'python3', '-'], input=REMOTE, text=True, encoding='utf-8', capture_output=True, timeout=60)
if proc.returncode:
    raise SystemExit('Read-only release inventory failed; captured stderr is not printed to avoid credential disclosure.')
data = json.loads(proc.stdout)
data['checked_at_utc'] = datetime.now(timezone.utc).isoformat()
root = Path(__file__).resolve().parent
target = root / 'coordinator-release-inventory-readonly.json'
target.write_text(json.dumps(data, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'receipt':str(target),'compose_project':data['compose_project'],'service_names':list(data['services']),'release_control':data['release_control'],'production_mutation_performed':False},indent=2))
