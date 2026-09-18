"""Run the recovered production executable against only the synthetic upgraded DB.

Run only after other runtime probes finish. Restores candidate in finally.
"""
import datetime
import json
import shutil

from runtime import BASE, EVIDENCE, IMAGE, LABEL, PREFIX, PRIVATE, SCRIPT, digest, http_status, inspect, own, run, save_json, wait_ready
from verify_http import admin_login, request, sql

BASELINE = SCRIPT.parents[3] / 'sub2api-bwh-baseline-20260918/runtime'
OLD_SHA = '7c8ac352ca44541b770c13bdcce913586cf138a75013b6477866e30af92b0bc1'
OLD_IMAGE = PREFIX + ':recovered-production'


def details(user):
    auth = request('/api/v1/auth/login', method='POST', body={'email': user['email'], 'password': user['password']})
    data = request('/api/v1/user/carpool/details', token=auth['access_token'])
    term = data['term']
    immutable = json.loads(sql(f"SELECT json_build_object('id',id,'plan_snapshot',plan_snapshot) FROM carpool_terms WHERE id={int(user['term_id'])};"))
    return {'starts_at': term['starts_at'], 'expires_at': term['expires_at'], 'reset_mode': term['reset_mode'],
            'next_natural_reset_at': term['next_natural_reset_at'], 'immutable_snapshot': immutable, 'quota': data['quota']}


def readiness():
    wait_ready(lambda: http_status('/health') == 200, 120, 'local binary readiness')
    assert http_status('/v1/models') == 401


def switch_ingress():
    own('container', PREFIX + '-ingress')
    run(['docker', 'restart', PREFIX + '-ingress'], 'switch-ingress')
    readiness()


def main():
    fixtures = json.loads((PRIVATE / 'browser-fixtures.private.json').read_text())
    assert fixtures['source'] == 'synthetic-local-only' and fixtures['base_url'] == BASE
    active = next(user for user in fixtures['users'] if user['name'] == 'active')
    group_id = fixtures['groups']['standard']['id']
    candidate_name, rollback_name = PREFIX + '-server', PREFIX + '-rollback'
    candidate = own('container', candidate_name)
    assert candidate['State']['Running']
    assert list(candidate['NetworkSettings']['Networks']) == [PREFIX + '-internal']
    assert own('network', PREFIX + '-internal')['Internal']
    if inspect('container', rollback_name) is not None:
        raise RuntimeError('rollback container already exists; refusing replacement')
    assert digest(BASELINE / 'sub2api') == OLD_SHA
    before = details(active)
    token = admin_login()
    group_before = request(f'/api/v1/admin/groups/{group_id}', token=token)['model_allowlist']
    assert sql("SELECT count(*) FROM accounts WHERE type = 'oauth';") == '0'
    context = PRIVATE / 'rollback-context'
    context.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(BASELINE / 'sub2api', context / 'sub2api')
    shutil.copytree(BASELINE / 'resources', context / 'resources', dirs_exist_ok=True)
    shutil.copyfile(SCRIPT / 'Dockerfile', context / 'Dockerfile')
    run(['docker', 'build', '--network=none', '--label', 'com.codex.local-task=' + LABEL, '-t', OLD_IMAGE, str(context)], 'build-rollback-image')
    evidence = {'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'source': 'downloaded production binary, synthetic local database only',
                'old_binary_sha256': OLD_SHA, 'candidate_image_id': candidate['Image'], 'group_id': group_id,
                'same_database_and_config': True, 'apps_run_sequentially': True, 'production_touched': False}
    stopped = False
    try:
        run(['docker', 'stop', '--time', '30', candidate_name], 'stop-candidate-for-rollback')
        stopped = True
        assert not own('container', candidate_name)['State']['Running']
        run(['docker', 'run', '-d', '--name', rollback_name, '--network', PREFIX + '-internal', '--network-alias', 'upgrade026-app',
             '--label', 'com.codex.local-task=' + LABEL, '--env-file', str(PRIVATE / 'app.private.env'),
             '-v', PREFIX + '-data:/app/data', '--read-only', '--tmpfs', '/tmp:rw,nosuid,noexec,size=64m',
             '--tmpfs', '/var/lib/postgresql:rw,nosuid,noexec,size=1m', OLD_IMAGE], 'start-old-binary')
        switch_ingress()
        old_token = admin_login()
        old_details = details(active)
        assert before == old_details
        old_usage = request('/v1/usage', token=active['api_key'], envelope=False)
        assert old_usage['billing_type'] == 'carpool' and 'balance' not in old_usage
        assert old_usage['remaining'] == active['remaining']
        old_group = request(f'/api/v1/admin/groups/{group_id}', token=old_token)
        assert old_group['models_list_config'] == group_before
        marker = {'enabled': True, 'models': ['gpt-5.1', 'gpt-4o-mini', 'rollback-synthetic']}
        written = request(f'/api/v1/admin/groups/{group_id}', method='PUT', token=old_token, body={'models_list_config': marker})
        assert written['models_list_config'] == marker
        columns = json.loads(sql(f"SELECT json_build_object('old',models_list_config,'new',model_allowlist) FROM groups WHERE id={int(group_id)};"))
        assert columns['old'] == marker and columns['new'] == marker
        evidence.update({'old_health_status': 200, 'old_unauthenticated_models_status': 401, 'old_admin_login': True,
                         'old_carpool_snapshot_and_quota_equal': True, 'old_api_key_carpool_usage': True, 'old_group_api_reads_new_config': True,
                         'old_group_api_write_synchronizes_both_columns': True, 'old_image_id': own('container', rollback_name)['Image']})
    finally:
        rollback = own('container', rollback_name)
        if rollback is not None and rollback['State']['Running']:
            run(['docker', 'stop', '--time', '30', rollback_name], 'stop-old-binary')
        if stopped:
            assert own('container', rollback_name) is None or not own('container', rollback_name)['State']['Running']
            run(['docker', 'start', candidate_name], 'restore-candidate')
            switch_ingress()
    token = admin_login()
    current = request(f'/api/v1/admin/groups/{group_id}', token=token)
    assert current['model_allowlist'] == marker
    assert details(active) == before
    new_usage = request('/v1/usage', token=active['api_key'], envelope=False)
    assert new_usage['billing_type'] == 'carpool' and 'balance' not in new_usage
    assert new_usage['remaining'] == active['remaining']
    # Restore the fixture's original allowlist through the new API, testing reverse sync.
    request(f'/api/v1/admin/groups/{group_id}', method='PUT', token=token, body={'model_allowlist': group_before})
    columns = json.loads(sql(f"SELECT json_build_object('old',models_list_config,'new',model_allowlist) FROM groups WHERE id={int(group_id)};"))
    assert columns['old'] == group_before and columns['new'] == group_before
    evidence.update({'candidate_restored': True, 'candidate_health_status': 200, 'candidate_unauthenticated_models_status': 401,
                     'candidate_admin_login': True, 'candidate_reads_old_api_write': True, 'candidate_write_reverse_sync': True,
                     'candidate_carpool_snapshot_and_quota_equal': True, 'candidate_api_key_carpool_usage_after_old_cache': True,
                     'original_group_allowlist_restored': True,
                     'final_runtime': 'candidate', 'base_url': BASE})
    save_json(EVIDENCE / 'old-binary-rollback.json', evidence)
    from lifecycle import register
    register()
    print('OLD_BINARY_ROLLBACK_PASS old startup/auth/carpool/config write, candidate restored and consistent', flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(type(error).__name__ + ': ' + str(error), flush=True)
        raise SystemExit(1)
