"""HTTP acceptance against only the freshly created synthetic candidate."""
import argparse
import datetime
import decimal
import json
import secrets
import subprocess
import urllib.error
import urllib.request

from runtime import BASE, EVIDENCE, PREFIX, PRIVATE, own, save_json

def request(path, *, method='GET', token=None, body=None, key=None, expected=200, envelope=True):
    headers = {'Accept': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    if body is not None:
        headers['Content-Type'] = 'application/json'
    if key:
        headers['Idempotency-Key'] = key
    req = urllib.request.Request(BASE + path, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            status, raw = response.status, response.read()
    except urllib.error.HTTPError as error:
        status, raw = error.code, error.read()
    if status != expected and not (expected == 200 and status == 201):
        raise RuntimeError(f'{method} {path} returned HTTP {status}; expected {expected}')
    data = json.loads(raw)
    if envelope and expected == 200:
        if 'data' not in data:
            raise RuntimeError(f'{method} {path} lacks success data')
        return data['data']
    return data

def admin_login():
    private = json.loads((PRIVATE / 'app-credentials.private.json').read_text())
    if private['source'] != 'synthetic-local-only' or private['base_url'] != BASE:
        raise RuntimeError('refusing non-synthetic or non-local credential file')
    auth = request('/api/v1/auth/login', method='POST', body=private['admin'])
    assert auth['user']['role'] == 'admin'
    # A fixture acknowledgement is stored only in this disposable synthetic DB.
    # It exercises local onboarding, not an external service or production user.
    status = request('/api/v1/admin/compliance', token=auth['access_token'])
    if status['required']:
        accepted = request('/api/v1/admin/compliance/accept', method='POST', token=auth['access_token'],
                           body={'phrase': status['ack_phrase_en'], 'language': 'en'})
        assert accepted['required'] is False
    return auth['access_token']

def sql(query):
    own('container', PREFIX + '-postgres')
    result = subprocess.run(['docker', 'exec', '-i', PREFIX + '-postgres', 'psql', '-X', '-U', 'upgrade026', '-d', 'upgrade026', '-At', '-v', 'ON_ERROR_STOP=1'], input=query.encode(), capture_output=True)
    if result.returncode:
        raise RuntimeError('synthetic database query failed')
    return result.stdout.decode().strip()

def verify_settings():
    token = admin_login()
    started = own('container', PREFIX + '-server')['State']['StartedAt']
    assert sql("SELECT count(*) FROM accounts WHERE type = 'oauth';") == '0'
    settings = request('/api/v1/admin/settings', token=token)
    assert settings['openai_codex_ticket_enabled'] is False
    updated = request('/api/v1/admin/settings', method='PUT', token=token, body={'openai_codex_ticket_enabled': True})
    assert updated['openai_codex_ticket_enabled'] is True
    assert request('/api/v1/admin/settings', token=token)['openai_codex_ticket_enabled'] is True
    secret = secrets.token_hex(20)
    proxy = 'socks5h://synthetic:' + secret + '@ticket-proxy.invalid:1080'
    key = 'openai_codex_ticket_harvest_proxy_url'
    updated = request('/api/v1/admin/settings', method='PUT', token=token, body={key: proxy})
    assert secret not in json.dumps(updated)
    assert updated['openai_codex_ticket_harvest_proxy_configured'] is True
    masked = request('/api/v1/admin/settings', token=token)[key]
    assert secret not in masked and 'ticket-proxy.invalid' in masked
    request('/api/v1/admin/settings', method='PUT', token=token, body={key: masked})
    invalid = request('/api/v1/admin/settings', method='PUT', token=token, body={key: 'ftp://synthetic:' + secret + '@ticket-proxy.invalid:21'}, expected=400, envelope=False)
    assert secret not in json.dumps(invalid)
    after_invalid = request('/api/v1/admin/settings', token=token)
    assert after_invalid[key] == masked
    request('/api/v1/admin/settings', method='PUT', token=token, body={'openai_codex_ticket_enabled': False})
    final = request('/api/v1/admin/settings', token=token)
    assert final['openai_codex_ticket_enabled'] is False
    assert own('container', PREFIX + '-server')['State']['StartedAt'] == started
    assert sql("SELECT count(*) FROM accounts WHERE type = 'oauth';") == '0'
    evidence = {'default_disabled': True, 'enable_visible_without_restart': True, 'disable_visible_without_restart': True,
                'proxy_configured': True, 'proxy_response_masks_password': True, 'masked_roundtrip_preserves_proxy': True,
                'invalid_scheme_status': 400, 'invalid_scheme_preserves_existing_proxy': True, 'invalid_response_omits_password': True,
                'oauth_accounts': 0, 'final_enabled': False, 'container_started_at_unchanged': True,
                'boundary': 'HTTP live settings persistence and response masking; no real ticket harvest or upstream call'}
    save_json(EVIDENCE / 'ticket-settings-http.json', evidence)
    print('TICKET_SETTINGS_HTTP_PASS default off, live toggle, proxy masking, invalid scheme rejection', flush=True)

def iso(when):
    return when.isoformat(timespec='milliseconds').replace('+00:00', 'Z')


def verify_redeem_pagination():
    token = admin_login()
    fixtures = json.loads((PRIVATE / 'browser-fixtures.private.json').read_text())
    assert fixtures['source'] == 'synthetic-local-only' and fixtures['base_url'] == BASE
    ordinary = next(u for u in fixtures['users'] if u['name'] == 'ordinary')
    before = request(f"/api/v1/admin/users/{ordinary['id']}", token=token)
    # Concurrency codes add history without changing the balance isolation fixture.
    for index in range(3):
        code = 'upgrade026-page-' + fixtures['nonce'] + '-' + str(index)
        request('/api/v1/admin/redeem-codes/create-and-redeem', method='POST', token=token,
                key=code, body={'code': code, 'type': 'concurrency', 'value': 1, 'user_id': ordinary['id'],
                                'notes': 'Synthetic local pagination acceptance'})
    auth = request('/api/v1/auth/login', method='POST', body={'email': ordinary['email'], 'password': ordinary['password']})
    user_token = auth['access_token']
    legacy = request('/api/v1/redeem/history', token=user_token)
    assert isinstance(legacy, list) and len(legacy) == 3
    first = request('/api/v1/redeem/history?page=1&page_size=2', token=user_token)
    second = request('/api/v1/redeem/history?page=2&page_size=2', token=user_token)
    assert first['total'] == second['total'] == 3
    assert len(first['items']) == 2 and len(second['items']) == 1
    assert [item['id'] for item in first['items'] + second['items']] == [item['id'] for item in legacy]
    assert len(set(item['id'] for item in first['items'] + second['items'])) == 3
    request('/api/v1/redeem/history?page=0&page_size=2', token=user_token, expected=400, envelope=False)
    after = request(f"/api/v1/admin/users/{ordinary['id']}", token=token)
    assert after['balance'] == before['balance'] == ordinary['ordinary_balance']
    save_json(EVIDENCE / 'redeem-history-pagination.json', {'source': 'three synthetic concurrency redemptions through HTTP',
              'user_id': ordinary['id'], 'legacy_array_preserved': True, 'total': 3, 'page_sizes': [2, 1],
              'no_overlapping_items': True, 'paginated_order_matches_legacy': True, 'invalid_page_status': 400,
              'ordinary_balance_unchanged': True})
    print('REDEEM_HISTORY_PAGINATION_PASS legacy array + two pages + invalid page + unchanged balance', flush=True)

def seed():
    token = admin_login()
    fixture_path = PRIVATE / 'browser-fixtures.private.json'
    if fixture_path.exists():
        state = json.loads(fixture_path.read_text())
    else:
        state = {'source': 'synthetic-local-only', 'base_url': BASE, 'nonce': secrets.token_hex(4), 'groups': {}, 'users': []}
    nonce = state['nonce']
    for kind in ('carpool', 'standard'):
        if kind not in state['groups']:
            group = request('/api/v1/admin/groups', method='POST', token=token, key='upgrade026-group-' + kind + '-' + nonce, body={
                'name': 'Upgrade 0.2.6 ' + kind + ' synthetic', 'description': 'Synthetic local browser acceptance',
                'platform': 'openai', 'rate_multiplier': 1, 'is_exclusive': True, 'subscription_type': kind,
                'model_allowlist': {'enabled': True, 'models': ['gpt-5.1', 'gpt-4o-mini']},
            })
            state['groups'][kind] = {'id': group['id'], 'name': group['name']}
            save_json(fixture_path, state)
    plans = request('/api/v1/admin/carpool/plans', token=token)
    plan = next(p for p in plans if p['code'] == 'four_seat' and p['enabled'] and p['is_latest'])
    assert plan['reset_mode'] == 'rolling'
    state['plan_id'] = plan['plan_id']
    now = datetime.datetime.now(datetime.timezone.utc)
    day = datetime.timedelta(days=1)
    cases = [('active', 123, now-day, 'carpool'), ('future', 45, now+2*day, 'carpool'),
             ('expired', 9, now-29*day, 'carpool'), ('ordinary', 77, None, 'standard')]
    for name, balance, starts, kind in cases:
        existing = next((u for u in state['users'] if u['name'] == name), None)
        if existing is None:
            email, password = f'upgrade026-{name}-{nonce}@example.invalid', secrets.token_hex(24)
            group_id = state['groups'][kind]['id']
            user = request('/api/v1/admin/users', method='POST', token=token, body={
                'email': email, 'password': password, 'username': 'Upgrade candidate ' + name,
                'notes': 'Synthetic local acceptance, no real user data', 'role': 'user', 'balance': balance,
                'concurrency': 5, 'rpm_limit': 0, 'allowed_groups': list({group_id, state['groups']['standard']['id']}), 'restrict_public_groups': True,
            })
            existing = {'name': name, 'id': user['id'], 'email': email, 'password': password, 'group_id': group_id,
                        'ordinary_balance': balance, 'starts_at': iso(starts) if starts else None}
            state['users'].append(existing)
            save_json(fixture_path, state)
        if kind == 'carpool' and 'term_id' not in existing:
            body = {'plan_id': plan['plan_id'], 'starts_at': existing['starts_at'], 'mode': 'new', 'takeover': None}
            preview = request(f"/api/v1/admin/users/{existing['id']}/carpool/preview", method='POST', token=token, body=body)
            term = request(f"/api/v1/admin/users/{existing['id']}/carpool/terms", method='POST', token=token,
                           key='upgrade026-term-' + name + '-' + nonce, body={**body, 'group_id': existing['group_id'], 'payment': None, 'notes': 'Synthetic local HTTP opening'})
            assert datetime.datetime.fromisoformat(term['expires_at'].replace('Z', '+00:00')) - datetime.datetime.fromisoformat(term['starts_at'].replace('Z', '+00:00')) == 28*day
            assert term['plan_snapshot']['reset_mode'] == 'rolling'
            assert preview['next_natural_reset_at'] == term['next_natural_reset_at']
            existing.update({'term_id': term['id'], 'expires_at': term['expires_at'], 'reset_mode': 'rolling',
                             'preview_matches_open': True, 'next_natural_reset_at': term['next_natural_reset_at']})
            save_json(fixture_path, state)
        after = request(f"/api/v1/admin/users/{existing['id']}", token=token)
        assert decimal.Decimal(str(after['balance'])) == decimal.Decimal(str(balance))
        auth = request('/api/v1/auth/login', method='POST', body={'email': existing['email'], 'password': existing['password']})
        if 'api_key' not in existing:
            # Carpool binding is administrator-only. First create a normal key
            # in an allowed standard group, then exercise the admin bind API.
            request(f"/api/v1/admin/users/{existing['id']}", method='PUT', token=token,
                    body={'allowed_groups': list({existing['group_id'], state['groups']['standard']['id']})})
            created = request('/api/v1/keys', method='POST', token=auth['access_token'], body={'name': 'Synthetic browser acceptance', 'group_id': state['groups']['standard']['id']})
            existing['api_key'], existing['api_key_id'] = created['key'], created['id']
            save_json(fixture_path, state)
        if kind == 'carpool' and not existing.get('admin_bound'):
            request(f"/api/v1/admin/api-keys/{existing['api_key_id']}", method='PUT', token=token, body={'group_id': existing['group_id']})
            existing['admin_bound'] = True
            save_json(fixture_path, state)
        if kind == 'carpool':
            details = request('/api/v1/user/carpool/details', token=auth['access_token'])
            existing['details_status'] = details['term']['status'] if details.get('term') else 'no_term'
            if name == 'active':
                usage = request('/v1/usage', token=existing['api_key'], envelope=False)
                assert usage['billing_type'] == 'carpool'
                assert 'balance' not in usage
                assert decimal.Decimal(str(usage['remaining'])) == decimal.Decimal(str(details['quota']['available_usd']))
                existing['remaining'] = usage['remaining']
                existing['usage_omits_ordinary_balance'] = True
        else:
            usage = request('/v1/usage', token=existing['api_key'], envelope=False)
            assert decimal.Decimal(str(usage['balance'])) == decimal.Decimal(str(balance))
            existing['usage_balance'] = usage['balance']
        save_json(fixture_path, state)
    public = {'source': 'fresh synthetic data via real HTTP admin/user API', 'plan': {'id': plan['plan_id'], 'reset_mode': plan['reset_mode']},
              'groups': state['groups'], 'users': [{k:v for k,v in u.items() if k not in ('email','password','api_key')} for u in state['users']],
              'ordinary_balances_preserved': True, 'active_carpool_uses_quota_not_ordinary_balance': True, 'all_terms_28_days': True}
    save_json(EVIDENCE / 'http-fixtures.json', public)
    print('FIXTURES_READY active/future/expired carpool and ordinary user; 28-day rolling snapshots and balance isolation verified', flush=True)
    print('Private browser fixtures: ' + str(fixture_path), flush=True)

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('settings', 'seed', 'redeem'))
    args = parser.parse_args()
    try:
        {'settings': verify_settings, 'seed': seed, 'redeem': verify_redeem_pagination}[args.action]()
    except Exception as error:
        print(type(error).__name__ + ': ' + str(error))
        raise SystemExit(1)
