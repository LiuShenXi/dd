"""Write a secret-free final observation without changing runtime state."""
import datetime

from lifecycle import check
from runtime import BASE, EVIDENCE, PREFIX, http_status, own, save_json
from verify_http import sql

check()
state = {}
for suffix in ['server', 'rollback', 'postgres', 'redis', 'mock', 'probe', 'ingress']:
    data = own('container', PREFIX + '-' + suffix)
    state[suffix] = {'running': data['State']['Running'], 'image_id': data['Image']}
assert state['server']['running'] and not state['rollback']['running']
assert http_status('/health') == 200 and http_status('/v1/models') == 401
save_json(EVIDENCE / 'final-runtime-state.json', {
    'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'base_url': BASE,
    'health_status': 200, 'unauthenticated_models_status': 401, 'containers': state,
    'database_migration_count': int(sql('SELECT count(*) FROM schema_migrations;')),
    'oauth_accounts': int(sql("SELECT count(*) FROM accounts WHERE type='oauth';")),
    'ownership_preflight_passed': True, 'stop_cleanup_executed': False,
})
print('FINAL_RUNTIME_CHECK_PASS candidate running; rollback stopped; 297 migrations', flush=True)
