"""Explicit release operations for the existing Sub2API production deployment."""
import argparse
import base64
import hashlib
import hmac
import json
import os
import secrets
from pathlib import Path
import subprocess
import time
import urllib.parse
import urllib.request
import uuid
import datetime as dt
from decimal import Decimal

ROOT = Path('/home/linuxuser/apps/sub2api')
DEADLINE = None


def remaining(default=30):
    if DEADLINE is None:
        return default
    budget = DEADLINE - time.monotonic()
    if budget <= 0:
        raise TimeoutError('Release phase deadline reached')
    return min(default, budget)


def command(args, **kwargs):
    kwargs.setdefault('timeout', remaining())
    return subprocess.run(args, check=True, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, **kwargs).stdout


def docker(*args, **kwargs):
    return command(['sudo', '-n', 'docker', *args], **kwargs)


def query(sql):
    return docker('exec', 'sub2api-postgres', 'psql', '-X', '-U', 'sub2api',
                  '-d', 'sub2api', '-At', '-v', 'ON_ERROR_STOP=1', '-c', sql).decode().strip()


def private_json(path, value):
    with path.open('x', encoding='utf-8') as stream:
        os.chmod(path, 0o600)
        json.dump(value, stream, indent=2)


def inspect(name):
    return json.loads(docker('inspect', name))[0]


def env_of(container):
    return dict(item.split('=', 1) for item in container['Config']['Env'])


def http(path, token=None, body=None, port=28080):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request('http://127.0.0.1:%d%s' % (port, path),
                                 data=None if body is None else json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(req, timeout=remaining(35)) as response:
        return json.load(response)


def verify_target():
    active = command(['sudo', '-n', 'cat', str(ROOT / 'run/active-slot')]).decode().strip()
    green = inspect('sub2api-green')
    if active != 'green' or green['Config']['Image'] != 'sub2api:0.2.1-refundfix-36266f512776':
        raise RuntimeError('Production baseline changed; refresh release inventory')
    if green['State'].get('Health', {}).get('Status') != 'healthy':
        raise RuntimeError('Original production is not healthy')
    return green


def backup(work):
    green = verify_target()
    before = json.loads(query("SELECT jsonb_build_object('users',(SELECT count(*) FROM users WHERE deleted_at IS NULL),'keys',(SELECT count(*) FROM api_keys WHERE deleted_at IS NULL),'migration',(SELECT max(filename) FROM schema_migrations),'time',clock_timestamp())"))
    if before['users'] != 24 or before['keys'] != 29 or not before['migration'].startswith('234_'):
        raise RuntimeError('Production inventory differs from approved release')
    private_json(work / 'green-inspect.private.json', green)
    private_json(work / 'blue-inspect.private.json', inspect('sub2api-blue'))
    for relative in ['.env', 'blue-green/slot-images.env', 'blue-green/docker-compose.yml',
                     'blue-green/router/conf.d/default.conf']:
        data = command(['sudo', '-n', 'cat', str(ROOT / relative)])
        path = work / (relative.replace('/', '_') + '.backup')
        with path.open('xb') as stream:
            os.chmod(path, 0o600)
            stream.write(data)
    dump = work / 'production-before.dump'
    with dump.open('xb') as stream:
        os.chmod(dump, 0o600)
        result = subprocess.run(['sudo', '-n', 'docker', 'exec', 'sub2api-postgres',
                                 'pg_dump', '-U', 'sub2api', '-d', 'sub2api', '-Fc', '--no-owner'],
                                stdout=stream, stderr=subprocess.PIPE, timeout=180)
    if result.returncode or dump.stat().st_size < 1024:
        raise RuntimeError('Production backup failed')
    before['backup_bytes'] = dump.stat().st_size
    before['backup_sha256'] = hashlib.file_digest(dump.open('rb'), 'sha256').hexdigest()
    private_json(work / 'backup-receipt.json', before)
    print(json.dumps(before))


def auth(work):
    green = verify_target()
    environment = env_of(green)
    secret = environment.get('JWT_SECRET', '')
    if not secret:
        raise RuntimeError('No fixed JWT signing secret; session continuity requires review')
    # A short-lived operator token uses the existing administrator identity and
    # signing key. It neither rotates a Key nor changes any account/session row.
    binding = query("SELECT COALESCE((SELECT value FROM settings WHERE key='session_binding_enabled'),'false')")
    if binding != 'false':
        raise RuntimeError('Session binding requires an existing bound operator session')
    user = json.loads(query("SELECT jsonb_build_object('id',id,'email',email,'password_hash',password_hash,'role',role,'status',status) FROM users WHERE id=1 AND deleted_at IS NULL"))
    if user['role'] != 'admin' or user['status'] != 'active':
        raise RuntimeError('Administrator identity changed')
    material = (user['email'].strip().lower() + '\n' + user['password_hash']).encode()
    version = int.from_bytes(hashlib.sha256(material).digest()[:8], 'big') & 0x7fffffffffffffff
    now = int(time.time())
    claims = dict(user_id=1, email=user['email'], role='admin', token_version=version,
                  iat=now, nbf=now-5, exp=now+7200)
    encode = lambda value: base64.urlsafe_b64encode(value).rstrip(b'=')
    signed = encode(b'{"alg":"HS256","typ":"JWT"}') + b'.' + encode(json.dumps(claims,separators=(',',':')).encode())
    token = (signed+b'.'+encode(hmac.new(secret.encode(),signed,hashlib.sha256).digest())).decode()
    result = http('/api/v1/admin/users/1', token)
    if result.get('code') != 0 or result.get('data',{}).get('role') != 'admin':
        raise RuntimeError('Existing administrator authentication failed')
    private_json(work / 'operator.private.json', dict(token=token, expires_at=claims['exp']))
    print('Existing administrator verified; temporary operator token stored privately')


def prepare_stage(work, image):
    green = verify_target()
    if not image.startswith('sub2api:carpool-production-'):
        raise RuntimeError('Expected immutable release image tag')
    environment = env_of(green)
    environment.update(RELEASE_DRAIN_START_HELD='true', CARPOOL_BACKGROUND_ENABLED='false',
                       BATCH_IMAGE_QUEUE_ENABLED='false')
    override = {'services': {'sub2api-blue': {'image': image, 'environment': environment}}}
    private_json(work / 'stage-override.private.json', override)
    print('Standby override prepared from exact live environment; no container changed')


def release_dsn(environment, user=None, password=None):
    username = user or environment['DATABASE_USER']
    secret = password if password is not None else environment['DATABASE_PASSWORD']
    return 'postgres://%s:%s@%s:%s/%s?sslmode=disable' % (
        urllib.parse.quote(username,safe=''),urllib.parse.quote(secret,safe=''),
        environment['DATABASE_HOST'],environment.get('DATABASE_PORT','5432'),environment['DATABASE_DBNAME'])


def run_cli(work, image, mode, dsn, token='', operation='', suffix=''):
    evidence=work/'cli'
    evidence.mkdir(mode=0o700,exist_ok=True)
    if mode!='schema' and not (evidence/'manifest.json').exists():
        private_json(evidence/'manifest.json',json.loads((work/'manifest.json').read_text()))
    # Secrets enter the short-lived process through stdin, never Docker arguments.
    script = 'read -r CARPOOL_RELEASE_DATABASE_URL; read -r CARPOOL_RELEASE_ADMIN_TOKEN; read -r CARPOOL_RELEASE_OPERATION_ID; export CARPOOL_RELEASE_DATABASE_URL CARPOOL_RELEASE_ADMIN_TOKEN CARPOOL_RELEASE_OPERATION_ID; export CARPOOL_RELEASE_APP_URL=http://sub2api-blue:8080; exec /app/carpool-release "$@"'
    container_name='carpool-release-cli-'+mode+'-'+uuid.uuid4().hex[:12]
    args = ['run','--rm','--name',container_name,'-i','--network','sub2api_sub2api-network','--memory','256m',
            '--cpus','0.5','--user',str(os.getuid()),'-v',str(evidence)+':/release',
            '--entrypoint','/bin/sh',image,'-c',script,'release-cli','-mode',mode]
    if mode != 'schema':
        args += ['-manifest','/release/manifest.json','-result','/release/'+mode+suffix+'.json']
    private_json(work/(container_name+'.json'),{'container':container_name,'mode':mode,'started_at':time.time()})
    result = docker(*args,input=(dsn+'\n'+token+'\n'+operation+'\n').encode(),timeout=remaining(65)).decode()
    private_json(work / (mode+suffix+'-command.json'),{'output':result,'finished_at':time.time()})
    print(result.strip())


def schema(work,image):
    green = verify_target()
    if not (work / 'backup-receipt.json').exists():
        raise RuntimeError('A verified production backup is required')
    run_cli(work,image,'schema',release_dsn(env_of(green)))
    print(query("SELECT max(filename) FROM schema_migrations"))


def compose_args(work):
    return ['compose','--env-file',str(ROOT/'.env'),'--env-file',str(ROOT/'blue-green/slot-images.env'),
            '-f',str(ROOT/'blue-green/docker-compose.yml'),'-f',str(work/'stage-override.private.json')]


def inbound(container):
    rows = docker('exec',container,'cat','/proc/net/tcp','/proc/net/tcp6').decode().splitlines()
    return sum(1 for line in rows if len(line.split())>3 and line.split()[1].endswith(':1F90') and line.split()[3]=='01')


def stage(work,image):
    green = verify_target()
    if not (work / 'stage-override.private.json').exists():
        raise RuntimeError('Reviewed standby override is missing')
    if inbound('sub2api-blue'):
        raise RuntimeError('Existing standby has active connections; do not replace it')
    result = docker(*compose_args(work),'up','-d','--no-deps','sub2api-blue').decode()
    private_json(work/'stage-command.json',{'output':result})
    for _ in range(90):
        try:
            if http('/health',port=18080).get('status')=='ok':
                break
        except Exception:
            pass
        time.sleep(1)
    else:
        raise RuntimeError('New standby health did not become ready; old route remains active')
    blue=inspect('sub2api-blue')
    expected=env_of(green)
    actual=env_of(blue)
    overrides={'RELEASE_DRAIN_START_HELD':'true','CARPOOL_BACKGROUND_ENABLED':'false','BATCH_IMAGE_QUEUE_ENABLED':'false'}
    expected.update(overrides)
    if any(actual.get(k)!=v for k,v in expected.items()) or blue['Config']['Image']!=image:
        raise RuntimeError('Standby does not preserve the reviewed runtime environment')
    if [(m['Source'],m['Destination']) for m in blue['Mounts']] != [(m['Source'],m['Destination']) for m in green['Mounts']]:
        raise RuntimeError('Standby shared data mount differs from original')
    binary=docker('exec','sub2api-blue','sha256sum','/app/sub2api').decode().split()[0]
    label=blue['Config']['Labels'].get('sub2api.binary.sha256')
    if binary != label:
        raise RuntimeError('Running binary does not match release label')
    token=json.loads((work/'operator.private.json').read_text())['token']
    state=http('/api/v1/admin/release/status',token,port=18080)['data']
    if state['state']!='migrating' or state['active_http'] or state['pending_usage']:
        raise RuntimeError('New standby is not safely held')
    private_json(work/'stage-receipt.json',{'image':image,'image_id':blue['Image'],'binary_sha256':binary,'state':state})
    print(json.dumps({'standby':'blue','state':state['state'],'binary_sha256':binary,'original_route':'green'}))


def resume_standby(work):
    verify_target()
    if query('SELECT count(*) FROM carpool_billing_bindings')!='0':
        raise RuntimeError('Cannot use pre-import standby resume after ledger activation')
    token=json.loads((work/'operator.private.json').read_text())['token']
    state=http('/api/v1/admin/release/status',token,port=18080)['data']
    manifest=json.loads((work/'manifest.json').read_text())
    users=[manifest['admin_user_id']]+[m['user_id'] for m in manifest['members']]
    result=http('/api/v1/admin/release/resume',token,{'operation_id':state['operation_id'],'user_ids':users,'group_ids':[2]},port=18080)
    if result.get('data',{}).get('state')!='open':
        raise RuntimeError('Standby gate failed to open')
    print('Standby serves the unchanged original ledger; production router still green')


def provision(work,image):
    green=verify_target()
    role='carpool_release_20260908'
    if query("SELECT count(*) FROM pg_roles WHERE rolname='carpool_release_20260908'")!='0':
        raise RuntimeError('Migration role already exists; inspect the prior operation')
    password=secrets.token_hex(32)
    sql=(work/'restricted-role.sql').read_bytes()
    script='read -r CARPOOL_RELEASE_PASSWORD; export CARPOOL_RELEASE_PASSWORD; exec psql -X -U sub2api -d sub2api -v release_role=carpool_release_20260908 -f -'
    result=docker('exec','-i','sub2api-postgres','/bin/sh','-c',script,input=password.encode()+b'\n'+sql)
    private_json(work/'migration-role.private.json',{'role':role,'password':password})
    private_json(work/'role-command.json',{'output':result.decode()})
    dsn=release_dsn(env_of(green),role,password)
    run_cli(work,image,'preview',dsn)
    print('Restricted migration role created and current production preview passed')


def workers():
    output=docker('top','sub2api-router','-eo','pid,args').decode()
    return [line.split()[0] for line in output.splitlines() if 'nginx: worker process' in line]


def switch(work):
    verify_target()
    token=json.loads((work/'operator.private.json').read_text())['token']
    if http('/api/v1/admin/release/status',token,port=18080)['data']['state']!='open':
        raise RuntimeError('Standby must be verified and open before routing')
    if query('SELECT count(*) FROM carpool_billing_bindings')!='0':
        raise RuntimeError('This first switch is only for unchanged original billing')
    old=workers()
    if not old:
        raise RuntimeError('No original router workers found')
    private_json(work/'old-router-workers.json',{'pids':old,'captured_at':time.time()})
    result=command(['sudo','-n','bash',str(ROOT/'blue-green/scripts/switch-slot.sh'),'blue']).decode()
    private_json(work/'switch-command.json',{'output':result,'finished_at':time.time()})
    print(result.strip())


def require_blue(work):
    active=command(['sudo','-n','cat',str(ROOT/'run/active-slot')]).decode().strip()
    receipt=json.loads((work/'stage-receipt.json').read_text())
    blue=inspect('sub2api-blue')
    if active!='blue' or blue['Image']!=receipt['image_id'] or blue['State'].get('Health',{}).get('Status')!='healthy':
        raise RuntimeError('Verified new application is not the healthy active slot')
    return blue


def redis_call(environment,*args):
    script='IFS= read -r REDISCLI_AUTH; export REDISCLI_AUTH; exec redis-cli --json --no-auth-warning "$@"'
    result=docker('exec','-i','sub2api-redis','/bin/sh','-c',script,'release-redis','-n',environment.get('REDIS_DB','0'),*args,
                  input=(environment.get('REDIS_PASSWORD','')+'\n').encode())
    return json.loads(result)


def processing_tasks(environment):
    cursor='0'
    seen=set()
    processing=0
    while True:
        cursor,keys=redis_call(environment,'SCAN',cursor,'MATCH','image_task:*','COUNT','100')
        for key in keys:
            if key in seen:
                continue
            seen.add(key)
            if len(seen)>5000:
                raise RuntimeError('Unexpectedly large async task inventory')
            value=redis_call(environment,'GET',key)
            if value is not None and json.loads(value).get('status')=='processing':
                processing+=1
        if str(cursor)=='0':
            return processing


def old_status(work):
    blue=require_blue(work)
    original=json.loads((work/'old-router-workers.json').read_text())['pids']
    present=set(workers())
    old=inspect('sub2api-green')
    connected=inbound('sub2api-green') if old['State']['Running'] else 0
    tasks=processing_tasks(env_of(blue))
    remaining=[pid for pid in original if pid in present]
    return {'old_workers':remaining,'old_established':connected,'async_processing':tasks,
            'ready':not remaining and not connected and not tasks,'old_running':old['State']['Running']}


def stop_old(work):
    status=old_status(work)
    if not status['ready']:
        print(json.dumps(status))
        raise RuntimeError('Original requests or asynchronous work remain; keep the old application alive')
    stop_time=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
    docker('stop','--timeout','-1','sub2api-green',timeout=None)
    old=inspect('sub2api-green')
    if old['State']['Running'] or old['State']['ExitCode']!=0 or old['State']['OOMKilled']:
        raise RuntimeError('Old process did not finish with a clean normal exit')
    result=subprocess.run(['sudo','-n','docker','logs','--since',stop_time,'sub2api-green'],
                          stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True,timeout=30)
    logs=result.stdout.decode(errors='replace')
    private_json(work/'old-stop-receipt.json',{'state':old['State'],'logs':logs})
    verify_cleanup(logs)
    print('Original process drained and exited normally; new application still serves original billing')


def verify_cleanup(logs):
    required=['[Cleanup] UsageRecordWorkerPool succeeded','[Cleanup] Redis succeeded','[Cleanup] Ent succeeded']
    if not all(marker in logs for marker in required) or 'Server forced to shutdown' in logs:
        raise RuntimeError('Shutdown did not prove complete usage settlement')
    if any('[Cleanup]' in line and ' failed:' in line for line in logs.splitlines()):
        raise RuntimeError('A cleanup step failed; reconcile before importing')


def persistent_work(environment):
    counts=json.loads(query("SELECT jsonb_build_object('frozen',(SELECT count(*) FROM users WHERE COALESCE(frozen_balance,0)<>0),'batch',(SELECT count(*) FROM batch_image_jobs WHERE status NOT IN ('completed','failed','cancelled','output_deleted') OR (status='completed' AND settled_at IS NULL)),'payments',(SELECT count(*) FROM payment_orders WHERE status IN ('PENDING','PAID','RECHARGING','FAILED')))"))
    counts['async_processing']=processing_tasks(environment)
    if any(counts.values()):
        raise RuntimeError('Persistent billing work remains: '+json.dumps(counts))
    return counts


def protected_snapshot():
    return json.loads(query("SELECT jsonb_build_object('users',(SELECT jsonb_agg(jsonb_build_object('id',id,'balance',balance::text,'created_at',created_at) ORDER BY id) FROM users WHERE deleted_at IS NULL),'keys',(SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM api_keys k WHERE deleted_at IS NULL),'groups',(SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM groups g),'account_groups',(SELECT jsonb_agg(to_jsonb(a) ORDER BY account_id,group_id) FROM account_groups a),'session_secret',(SELECT md5(value) FROM security_secrets WHERE key='jwt_secret'))"))


def cutover(work,image):
    global DEADLINE
    blue=require_blue(work)
    image_data=json.loads(docker('image','inspect',image))[0]
    if image_data['Id']!=blue['Image']:
        raise RuntimeError('Migration CLI image differs from verified serving image')
    image=image_data['Id']
    old=inspect('sub2api-green')
    if old['State']['Running'] or old['State']['ExitCode']!=0 or old['State']['OOMKilled']:
        raise RuntimeError('Original application must be cleanly stopped first')
    verify_cleanup(json.loads((work/'old-stop-receipt.json').read_text())['logs'])
    environment=env_of(blue)
    if environment.get('CARPOOL_BACKGROUND_ENABLED')!='false' or environment.get('RELEASE_DRAIN_START_HELD')!='true':
        raise RuntimeError('Unexpected cutover runtime flags')
    operator=json.loads((work/'operator.private.json').read_text())
    if operator['expires_at'] < time.time()+300:
        raise RuntimeError('Operator authentication expires too soon')
    token=operator['token']
    role=json.loads((work/'migration-role.private.json').read_text())
    dsn=release_dsn(environment,role['role'],role['password'])
    manifest=json.loads((work/'manifest.json').read_text())
    users=[manifest['admin_user_id']]+[m['user_id'] for m in manifest['members']]
    persistent_work(environment)
    if query('SELECT count(*) FROM carpool_billing_bindings')!='0':
        raise RuntimeError('Import already started; use explicit reconciliation')
    quiet_deadline=time.monotonic()+600
    print('Waiting for a naturally idle admission boundary before draining',flush=True)
    while True:
        state=http('/api/v1/admin/release/status',token,port=18080)['data']
        if state['state']!='open':
            raise RuntimeError('Application is already held; reconcile prior operation')
        if state['active_http']==0 and state['pending_usage']==0:
            break
        if time.monotonic()>=quiet_deadline:
            raise RuntimeError('No idle window observed; traffic remains open and no import was attempted')
        time.sleep(0.25)
    started=time.monotonic()
    DEADLINE=started+45
    operation=None
    locked=False
    attempt=uuid.uuid4().hex[:12]
    try:
        state=http('/api/v1/admin/release/drain',token,{},port=18080)['data']
        operation=state['operation_id']
        private_json(work/('cutover-start-'+attempt+'.json'),{'operation_id':operation,'started_at':time.time()})
        while True:
            state=http('/api/v1/admin/release/status',token,port=18080)['data']
            if state['state']!='draining' or state['operation_id']!=operation:
                raise RuntimeError('Drain operation changed')
            if state['active_http']==0 and state['pending_usage']==0:
                persistent_work(environment)
                break
            time.sleep(min(0.25,remaining()))
        # An uncertain lock response is treated as locked, never auto-cancelled.
        locked=True
        state=http('/api/v1/admin/release/lock',token,{'operation_id':operation},port=18080)['data']
        if state['state']!='migrating':
            raise RuntimeError('Migration gate did not lock')
        DEADLINE=started+105
        persistent_work(environment)
        before=protected_snapshot()
        private_json(work/('protected-before-'+attempt+'.private.json'),before)
        run_cli(work,image,'apply',dsn,token,operation,suffix='-'+attempt)
        run_cli(work,image,'verify',dsn,token,operation,suffix='-'+attempt)
        after=protected_snapshot()
        if before!=after:
            private_json(work/('protected-after-'+attempt+'.private.json'),after)
            raise RuntimeError('Protected balances, Keys, routing or session signing state changed during import')
        result=http('/api/v1/admin/release/resume',token,
                    {'operation_id':operation,'user_ids':users,'group_ids':[2]},port=18080)['data']
        if result['state']!='open':
            raise RuntimeError('Verified migration remains held')
        receipt={'members':20,'state':'open','elapsed_seconds':round(time.monotonic()-started,3),
                 'operation_id':operation,'finished_at':time.time(),'group_id':2,'attempt':attempt}
        private_json(work/'cutover-receipt.json',receipt)
        print(json.dumps(receipt))
    except Exception:
        DEADLINE=None
        if not locked:
            state=http('/api/v1/admin/release/status',token,port=18080)['data']
            if state['state']=='draining' and (operation is None or state['operation_id']==operation):
                http('/api/v1/admin/release/cancel',token,{'operation_id':state['operation_id']},port=18080)
                print('Drain cancelled before locking; original billing remains active')
            elif state['state']!='open':
                print('Unexpected gate state; explicit reconciliation required')
        else:
            print('Cutover needs reconciliation; no automatic unlock or rollback was attempted')
        raise
    finally:
        DEADLINE=None


def require_import(work):
    receipt=json.loads((work/'cutover-receipt.json').read_text())
    if receipt['state']!='open' or query('SELECT count(*) FROM carpool_billing_bindings')!='20':
        raise RuntimeError('Verified subscription import is required')
    return receipt


def final_stage(work,image):
    import yaml
    require_import(work)
    blue=require_blue(work)
    if inspect('sub2api-green')['State']['Running']:
        raise RuntimeError('Final green slot must be stopped before replacement')
    if json.loads(docker('image','inspect',image))[0]['Id']!=blue['Image']:
        raise RuntimeError('Final stage image differs from accepted image')
    original=json.loads((work/'green-inspect.private.json').read_text())
    environment=env_of(original)
    environment.update(RELEASE_DRAIN_START_HELD='false',CARPOOL_BACKGROUND_ENABLED='true')
    # Preserve Compose extension tags while using a structured YAML edit.
    class Tagged:
        def __init__(self,tag,value): self.tag,self.value=tag,value
    class Loader(yaml.SafeLoader): pass
    class Dumper(yaml.SafeDumper): pass
    def tagged(loader,node):
        if isinstance(node,yaml.SequenceNode): value=loader.construct_sequence(node)
        elif isinstance(node,yaml.MappingNode): value=loader.construct_mapping(node)
        else: value=loader.construct_scalar(node)
        return Tagged(node.tag,value)
    def represent(dumper,value):
        node=dumper.represent_data(value.value)
        node.tag=value.tag
        return node
    for tag in ['!override','!reset']:
        Loader.add_constructor(tag,tagged)
    Dumper.add_representer(Tagged,represent)
    compose_path=ROOT/'blue-green/docker-compose.yml'
    current=command(['sudo','-n','cat',str(compose_path)])
    config=yaml.load(current,Loader=Loader)
    for slot in ['blue','green']:
        config['services']['sub2api-'+slot]['environment']=environment.copy()
    encoded=yaml.dump(config,Dumper=Dumper,sort_keys=False).encode()
    candidate=work/'final-compose.private.yml'
    candidate.write_bytes(encoded)
    os.chmod(candidate,0o600)
    slots=('SUB2API_BLUE_IMAGE='+image+'\nSUB2API_GREEN_IMAGE='+image+'\n').encode()
    private_json(work/'final-config-receipt.json',{'image':image,'sha256':hashlib.sha256(encoded).hexdigest(),'background_enabled':True,'start_held':False})
    command(['sudo','-n','install','-m','600',str(candidate),str(compose_path)])
    slot_candidate=work/'final-slot-images.env'
    slot_candidate.write_bytes(slots)
    command(['sudo','-n','install','-m','600',str(slot_candidate),str(ROOT/'blue-green/slot-images.env')])
    base=['compose','--env-file',str(ROOT/'.env'),'--env-file',str(ROOT/'blue-green/slot-images.env'),'-f',str(compose_path)]
    resolved=json.loads(docker(*base,'config','--format','json'))
    if any(resolved['services']['sub2api-'+s]['environment']!=environment for s in ['blue','green']):
        raise RuntimeError('Persisted Compose environment differs from accepted runtime')
    docker(*base,'up','-d','--no-deps','sub2api-green',timeout=90)
    for _ in range(90):
        try:
            if http('/health',port=28080).get('status')=='ok': break
        except Exception: pass
        time.sleep(1)
    else: raise RuntimeError('Final green health check failed; blue still serves production')
    green=inspect('sub2api-green')
    if green['Image']!=blue['Image'] or env_of(green)!=environment:
        raise RuntimeError('Final green artifact or environment differs')
    if [(m['Source'],m['Destination']) for m in green['Mounts']]!=[(m['Source'],m['Destination']) for m in blue['Mounts']]:
        raise RuntimeError('Final green mount differs')
    binary=docker('exec','sub2api-green','sha256sum','/app/sub2api').decode().split()[0]
    if binary!=green['Config']['Labels']['sub2api.binary.sha256']:
        raise RuntimeError('Final green runtime binary mismatch')
    token=json.loads((work/'operator.private.json').read_text())['token']
    state=http('/api/v1/admin/release/status',token,port=28080)['data']
    if state['state']!='open': raise RuntimeError('Final green did not start open')
    private_json(work/'final-stage-receipt.json',{'image_id':green['Image'],'binary_sha256':binary,'gate':state,'environment_preserved':True})
    print('Final green healthy on exact accepted image; original session configuration preserved; maintenance enabled')


def final_switch(work):
    require_import(work)
    require_blue(work)
    receipt=json.loads((work/'final-stage-receipt.json').read_text())
    green=inspect('sub2api-green')
    if green['Image']!=receipt['image_id'] or green['State'].get('Health',{}).get('Status')!='healthy':
        raise RuntimeError('Final green is not healthy')
    old=workers()
    private_json(work/'final-old-router-workers.json',{'pids':old,'captured_at':time.time()})
    result=command(['sudo','-n','bash',str(ROOT/'blue-green/scripts/switch-slot.sh'),'green']).decode()
    private_json(work/'final-switch-receipt.json',{'output':result,'finished_at':time.time()})
    print(result.strip())


def final_old_status(work):
    if command(['sudo','-n','cat',str(ROOT/'run/active-slot')]).decode().strip()!='green':
        raise RuntimeError('Expected final green route')
    blue=inspect('sub2api-blue')
    original=json.loads((work/'final-old-router-workers.json').read_text())['pids']
    present=set(workers())
    token=json.loads((work/'operator.private.json').read_text())['token']
    state=http('/api/v1/admin/release/status',token,port=18080)['data'] if blue['State']['Running'] else {'active_http':0,'pending_usage':0}
    result={'old_workers':[p for p in original if p in present],
            'connections':inbound('sub2api-blue') if blue['State']['Running'] else 0,
            'active_http':state['active_http'],'pending_usage':state['pending_usage'],
            'async_processing':processing_tasks(env_of(blue)),'running':blue['State']['Running']}
    result['ready']=not any(v for k,v in result.items() if k!='running')
    return result


def final_stop(work):
    status=final_old_status(work)
    if not status['ready']:
        print(json.dumps(status))
        raise RuntimeError('Compatible blue still has live work; retain it')
    stop_time=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
    docker('stop','--timeout','-1','sub2api-blue',timeout=None)
    blue=inspect('sub2api-blue')
    result=subprocess.run(['sudo','-n','docker','logs','--since',stop_time,'sub2api-blue'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True,timeout=30)
    logs=result.stdout.decode(errors='replace')
    private_json(work/'final-stop-receipt.json',{'state':blue['State'],'logs':logs})
    verify_cleanup(logs)
    if blue['State']['Running'] or blue['State']['ExitCode']!=0 or blue['State']['OOMKilled']:
        raise RuntimeError('Compatible blue did not exit cleanly')
    print('Compatible blue drained and stopped normally; final green is the sole active application')


def validate(work,port):
    receipt=require_import(work)
    manifest=json.loads((work/'manifest.json').read_text())
    before=json.loads((work/('protected-before-'+receipt['attempt']+'.private.json')).read_text())
    applied=json.loads((work/'cli'/('apply-'+receipt['attempt']+'.json')).read_text())
    current=protected_snapshot()
    for field in ['groups','account_groups','session_secret']:
        if before[field]!=current[field]: raise RuntimeError('Protected routing/session state changed: '+field)
    mutable={'last_used_at','updated_at','quota_used','usage_5h','usage_1d','usage_7d','window_5h_start','window_1d_start','window_7d_start'}
    stable=lambda keys:[{k:v for k,v in item.items() if k not in mutable} for item in keys]
    if stable(before['keys'])!=stable(current['keys']): raise RuntimeError('Original Key identity, configuration or group changed')
    users={u['id']:u for u in current['users']}
    opening={u['id']:u for u in before['users']}
    approved={m['user_id']:m for m in manifest['members']}
    instant=lambda v:dt.datetime.fromisoformat(v.replace('Z','+00:00'))
    for member in applied['members']:
        uid=member['user_id']
        policy=approved[uid]
        if instant(member['starts_at'])!=instant(users[uid]['created_at']): raise RuntimeError('Registration time changed')
        if instant(member['expires_at'])-instant(member['starts_at'])!=dt.timedelta(days=policy['duration_days']): raise RuntimeError('Term duration mismatch')
        anchor=instant(manifest['cycle_anchor'])
        if instant(member['cycle_starts_at'])!=anchor or instant(member['cycle_ends_at'])!=min(anchor+dt.timedelta(days=7),instant(member['expires_at'])): raise RuntimeError('Current cycle anchor mismatch')
        if Decimal(member['plan_snapshot']['weekly_quota_usd'])!=Decimal(str(policy['weekly_quota_usd'])) or member['plan_snapshot']['code']!=policy['plan_code']: raise RuntimeError('Special tier/quota mismatch')
        if Decimal(member['opening_balance_usd'])!=Decimal(opening[uid]['balance']) or users[uid]['balance']!=opening[uid]['balance']: raise RuntimeError('Member original balance or opening amount changed')
    counts=json.loads(query("SELECT jsonb_build_object('terms',(SELECT count(*) FROM carpool_terms),'bindings',(SELECT count(*) FROM carpool_billing_bindings),'opening_rows',(SELECT count(*) FROM carpool_ledger WHERE event_type='takeover_opening'),'bad_boost',(SELECT count(*) FROM carpool_terms WHERE boost_used<>0),'admin_terms',(SELECT count(*) FROM carpool_terms WHERE user_id=1),'extra_cycles',(SELECT count(*) FROM carpool_cycles)-20,'billing_exceptions',(SELECT count(*) FROM carpool_billing_requests WHERE status<>'settled' AND (status='reconcile_required' OR last_error IS NOT NULL)))"))
    if any(counts[k]!=20 for k in ['terms','bindings','opening_rows']) or any(counts[k] for k in ['bad_boost','admin_terms','extra_cycles','billing_exceptions']): raise RuntimeError('Unexpected ledger inventory: '+json.dumps(counts))
    excluded=manifest['excluded_user_ids']
    if any(Decimal(users[uid]['balance'])!=0 or any(k['user_id']==uid for k in current['keys']) for uid in excluded): raise RuntimeError('Unactivated account changed')
    identities=json.loads(query("SELECT jsonb_agg(jsonb_build_object('id',id,'email',email,'password_hash',password_hash,'role',role) ORDER BY id) FROM users WHERE deleted_at IS NULL"))
    secret=query("SELECT value FROM security_secrets WHERE key='jwt_secret'")
    def member_token(user):
        material=(user['email'].strip().lower()+'\n'+user['password_hash']).encode()
        version=int.from_bytes(hashlib.sha256(material).digest()[:8],'big')&0x7fffffffffffffff
        now=int(time.time())
        claims=dict(user_id=user['id'],email=user['email'],role=user['role'],token_version=version,iat=now,nbf=now-5,exp=now+600)
        enc=lambda b:base64.urlsafe_b64encode(b).rstrip(b'=')
        signed=enc(b'{"alg":"HS256","typ":"JWT"}')+b'.'+enc(json.dumps(claims,separators=(',',':')).encode())
        return (signed+b'.'+enc(hmac.new(secret.encode(),signed,hashlib.sha256).digest())).decode()
    proof=[]
    for user in identities:
        if user['id'] not in approved: continue
        data=http('/api/v1/user/carpool/details',member_token(user),port=port)['data']
        if data['billing_mode']!='carpool' or data['term'] is None or data['quota'] is None: raise RuntimeError('Member HTTP contract failed')
        proof.append({'user_id':user['id'],'details':data})
    unrestricted=next(k for k in current['keys'] if k['user_id'] in approved and k['status']=='active' and not any(k[n] for n in ['quota','rate_limit_5h','rate_limit_1d','rate_limit_7d']))
    member_usage=http('/v1/usage',unrestricted['key'],port=port)
    admin=next(k for k in current['keys'] if k['id']==6 and k['user_id']==1 and k['group_id']==2 and k['status']=='active')
    admin_usage=http('/v1/usage',admin['key'],port=port)
    if member_usage.get('billing_type')!='carpool' or 'balance' in member_usage: raise RuntimeError('Member usage exposed original wallet')
    if admin_usage.get('billing_type')=='carpool' or 'balance' not in admin_usage: raise RuntimeError('Administrator ordinary billing lost')
    operator=json.loads((work/'operator.private.json').read_text())['token']
    if http('/api/v1/admin/users/1',operator,port=port)['data']['role']!='admin': raise RuntimeError('Pre-release administrator session failed')
    stamp=uuid.uuid4().hex[:10]
    private_json(work/('validation-'+stamp+'.private.json'),{'members':proof,'member_usage':member_usage,'admin_usage':admin_usage})
    result={'members':len(proof),'days_7':2,'days_28':18,'group_id':2,'keys_preserved':len(current['keys']),
            'member_balances_preserved':True,'original_session_valid':True,'member_usage_carpool':True,
            'admin_usage_standard':True,'counts':counts,'port':port,'checked_at':time.time()}
    private_json(work/('validation-'+stamp+'.json'),result)
    print(json.dumps(result))


def probe(work, port):
    if port not in (8080, 18080, 28080):
        raise RuntimeError('Unexpected application probe port')
    key = query("SELECT key FROM api_keys WHERE id=6 AND user_id=1 AND group_id=2 AND status='active' AND deleted_at IS NULL")
    if not key:
        raise RuntimeError('Original administrator Key is unavailable')
    request_id = 'release-' + uuid.uuid4().hex
    headers = {'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json',
               'User-Agent': 'codex_cli_rs/0.110.0', 'X-Request-ID': request_id}
    base = 'http://127.0.0.1:%d' % port
    req = urllib.request.Request(base + '/v1/models', headers=headers)
    with urllib.request.urlopen(req,timeout=20) as response:
        catalog = json.load(response)
    models = [item['id'] for item in catalog.get('data',[])]
    candidates = ['gpt-5.6-luna', 'gpt-5.6-sol']
    model = next((item for item in candidates if item in models),None)
    if not model:
        raise RuntimeError('No reviewed minimal-probe model in the current group catalog')
    payload = {'model':model,'input':[{'role':'user','content':[{'type':'input_text','text':'Reply exactly OK.'}]}],
               'instructions':'Follow the user request briefly.','max_output_tokens':64,'stream':True,'store':False,
               'reasoning':{'effort':'low'}}
    req = urllib.request.Request(base+'/v1/responses',data=json.dumps(payload).encode(),headers=headers)
    started = time.monotonic()
    text_parts = []
    completed = False
    usage = None
    with urllib.request.urlopen(req,timeout=120) as response:
        for line in response:
            if not line.startswith(b'data: '):
                continue
            raw = line[6:].strip()
            if raw == b'[DONE]':
                continue
            event = json.loads(raw)
            if event.get('type') == 'response.output_text.delta':
                text_parts.append(event.get('delta',''))
            if event.get('type') == 'response.completed':
                completed = True
                usage = event.get('response',{}).get('usage')
    if not completed or not ''.join(text_parts).strip():
        raise RuntimeError('Real upstream probe did not complete with text')
    receipt = {'port':port,'api_key_id':6,'group_id':2,'model':model,
               'completed':completed,'text':''.join(text_parts),'usage':usage,
               'elapsed_seconds':round(time.monotonic()-started,3),'request_id':request_id}
    private_json(work / ('probe-%d-%s.json' % (port,request_id)),receipt)
    print(json.dumps(receipt))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=['backup', 'auth', 'prepare-stage', 'probe', 'schema', 'stage', 'resume-standby', 'provision', 'switch', 'old-status', 'stop-old', 'cutover','final-stage','final-switch','final-old-status','final-stop','validate','status'])
    parser.add_argument('--workdir', required=True)
    parser.add_argument('--image')
    parser.add_argument('--port', type=int, default=8080)
    args = parser.parse_args()
    os.umask(0o077)
    work = Path(args.workdir)
    if work.parent != ROOT or not work.name.startswith('production-release-20260908-'):
        raise RuntimeError('Unexpected release directory')
    work.mkdir(mode=0o700, exist_ok=True)
    if args.operation == 'backup':
        backup(work)
    elif args.operation == 'auth':
        auth(work)
    elif args.operation == 'probe':
        probe(work,args.port)
    elif args.operation == 'schema':
        schema(work,args.image or '')
    elif args.operation == 'stage':
        stage(work,args.image or '')
    elif args.operation == 'resume-standby':
        resume_standby(work)
    elif args.operation == 'provision':
        provision(work,args.image or '')
    elif args.operation == 'switch':
        switch(work)
    elif args.operation == 'old-status':
        print(json.dumps(old_status(work)))
    elif args.operation == 'stop-old':
        stop_old(work)
    elif args.operation == 'cutover':
        cutover(work,args.image or '')
    elif args.operation == 'final-stage':
        final_stage(work,args.image or '')
    elif args.operation == 'final-switch':
        final_switch(work)
    elif args.operation == 'final-old-status':
        print(json.dumps(final_old_status(work)))
    elif args.operation == 'final-stop':
        final_stop(work)
    elif args.operation == 'validate':
        validate(work,args.port)
    elif args.operation == 'status':
        token=json.loads((work/'operator.private.json').read_text())['token']
        print(json.dumps({'gate':http('/api/v1/admin/release/status',token,port=18080)['data'],
                          'bindings':query('SELECT count(*) FROM carpool_billing_bindings'),
                          'cutover_receipt':(work/'cutover-receipt.json').exists()}))
    else:
        prepare_stage(work,args.image or '')
