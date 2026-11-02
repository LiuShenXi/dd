"""Read current production metadata over SSH; never mutate production."""
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path

REMOTE = r'''
import hashlib,json,pathlib,subprocess,urllib.request,urllib.error
from datetime import datetime,timezone
def run(args):
    return subprocess.check_output(args,text=True).strip()
def inspect(kind,name):
    return json.loads(run(['docker',kind,'inspect',name]))[0]
v=inspect('container','sub2api2')
image=inspect('image',v['Image'])
db=inspect('container','sub2api2-postgres')
env=dict(x.split('=',1) for x in v['Config']['Env'] if '=' in x)
denv=dict(x.split('=',1) for x in db['Config']['Env'] if '=' in x)
def query(sql):
    return run(['docker','exec','sub2api2-postgres','psql','-X','-v','ON_ERROR_STOP=1','-U',denv.get('POSTGRES_USER','sub2api'),'-d',denv.get('POSTGRES_DB','sub2api'),'-Atc',sql])
labels=v['Config'].get('Labels') or {}
probes=[]
for url in ['http://127.0.0.1:29080/health','http://127.0.0.1:29080/v1/models','https://sub2api.monasapi.com/health','https://sub2api.monasapi.com/v1/models','https://teamsub2.monasapi.com/health','https://teamsub2.monasapi.com/v1/models']:
    try:
        with urllib.request.urlopen(url,timeout=4) as response:
            status=response.status
    except urllib.error.HTTPError as error:
        status=error.code
    except Exception:
        status='unavailable'
    probes.append({'url':url,'status':status})
flags={key:env.get(key) for key in ['RELEASE_DRAIN_START_HELD','CARPOOL_BACKGROUND_ENABLED','SUBSCRIPTION_QUOTA_ISSUE_ENABLED','SUBSCRIPTION_QUOTA_ADMISSION_ENABLED','SUBSCRIPTION_QUOTA_RECOVERY_ENABLED','DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY']}
compose=pathlib.Path(labels['com.docker.compose.project.config_files'])
result={'checked_at_utc':datetime.now(timezone.utc).isoformat(),'read_only':True,'target':'main-server/sub2api2','image':v['Config']['Image'],'image_id':v['Image'],'revision':(image['Config'].get('Labels') or {}).get('org.opencontainers.image.revision'),'binary_sha256':run(['docker','exec','sub2api2','sha256sum','/app/sub2api']).split()[0],'health':v['State'].get('Health',{}).get('Status'),'started_at':v['State']['StartedAt'],'restart_count':v['RestartCount'],'compose_project':labels.get('com.docker.compose.project'),'compose_dir':labels.get('com.docker.compose.project.working_dir'),'compose_file':str(compose),'compose_sha256':hashlib.sha256(compose.read_bytes()).hexdigest(),'ports':v['NetworkSettings']['Ports'],'network_names':list(v['NetworkSettings']['Networks']),'flag_whitelist':flags,'database_image':db['Config']['Image'],'database_version':query('SHOW server_version'),'migrations':json.loads(query("SELECT coalesce(json_agg(x ORDER BY filename),'[]'::json) FROM (SELECT filename,checksum FROM schema_migrations) x")),'usage_table_sizes':json.loads(query("SELECT coalesce(json_agg(x),'[]'::json) FROM (SELECT relname,pg_total_relation_size(oid) AS bytes,reltuples::bigint AS estimated_rows FROM pg_class WHERE relname IN ('usage_logs','usage_daily_aggregates','usage_hourly_aggregates','users','payment_orders')) x")),'service_inventory':[json.loads(line) for line in run(['docker','ps','-a','--format','{{json .}}']).splitlines() if 'sub2api' in line.lower()],'probes':probes}
for item in result['service_inventory']:
    for key in list(item):
        if key not in ['ID','Names','Image','State','Status','Ports']:
            del item[key]
print(json.dumps(result))
'''

evidence=Path(__file__).resolve().parent
proc=subprocess.run(['ssh','-o','BatchMode=yes','-o','ConnectTimeout=15','main-server','python3','-'],input=REMOTE,text=True,encoding='utf-8',capture_output=True,timeout=90)
if proc.returncode:
    raise SystemExit('Read-only probe failed; no production changes were performed. Exit '+str(proc.returncode))
data=json.loads(proc.stdout)
baseline=json.loads((evidence/'production-baseline-20261019.json').read_text(encoding='utf-8-sig'))
data['baseline_comparison']={key:data[key]==baseline[baseline_key] for key,baseline_key in [('image','app_image'),('image_id','image_id'),('revision','revision'),('binary_sha256','binary_sha256'),('migrations','migrations')]}
stamp=datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
path=evidence/f'coordinator-production-readonly-{stamp}.json'
path.write_text(json.dumps(data,indent=2)+'\n',encoding='utf-8')
print(json.dumps({'receipt':str(path),'image':data['image'],'revision':data['revision'],'health':data['health'],'restart_count_before_release':data['restart_count'],'migration_count':len(data['migrations']),'baseline_comparison':data['baseline_comparison'],'probes':data['probes']},indent=2))
