#!/usr/bin/env python3
"""Read-only retirement evidence from the shared PostgreSQL/Redis stores.

Credentials stay in memory. JSON output contains only process identity and counts.
The count is deliberately global: a new-slot image job can defer old-slot cleanup.
"""
import argparse
from datetime import datetime, timezone
import json
import socket
import subprocess
import sys


def command(args):
    p = subprocess.run(args, capture_output=True, timeout=20)
    if p.returncode:
        raise RuntimeError('Read-only data-service command failed')
    return p.stdout


def inspect(name):
    return json.loads(command(['docker', 'inspect', name]))[0]


def environment(container):
    return dict(x.split('=', 1) for x in container['Config']['Env'] if '=' in x)


class RedisReadOnly:
    def __init__(self, host, port):
        self.socket = socket.create_connection((host, port), timeout=5)
        self.stream = self.socket.makefile('rb')

    def response(self):
        line = self.stream.readline(1024 * 1024)
        if not line.endswith(b'\r\n'):
            raise RuntimeError('Invalid Redis response')
        kind, payload = line[:1], line[1:-2]
        if kind == b'+':
            return payload
        if kind == b':':
            return int(payload)
        if kind == b'$':
            length = int(payload)
            if length == -1:
                return None
            if length < 0 or length > 64 * 1024 * 1024:
                raise RuntimeError('Unexpected Redis value size')
            raw = self.stream.read(length + 2)
            if len(raw) != length + 2 or not raw.endswith(b'\r\n'):
                raise RuntimeError('Truncated Redis response')
            return raw[:-2]
        if kind == b'*':
            count = int(payload)
            if count < 0 or count > 100000:
                raise RuntimeError('Unexpected Redis array size')
            return [self.response() for _ in range(count)]
        raise RuntimeError('Redis request failed; response withheld')

    def read(self, *parts):
        # Only the authenticated read-only sequence below uses this connection.
        encoded = [str(x).encode() if not isinstance(x, bytes) else x for x in parts]
        request = b'*' + str(len(encoded)).encode() + b'\r\n'
        for value in encoded:
            request += b'$' + str(len(value)).encode() + b'\r\n' + value + b'\r\n'
        self.socket.sendall(request)
        return self.response()

    def close(self):
        self.stream.close()
        self.socket.close()


def collect(args):
    app = inspect(args.container_id)
    if app['Id'] != args.container_id or app['State']['StartedAt'] != args.started_at or not app['State']['Running']:
        raise RuntimeError('Application process identity changed')
    pg = inspect(args.postgres_container)
    redis = inspect(args.redis_container)
    env, pg_env = environment(app), environment(pg)
    if not {'REDIS_DB', 'REDIS_PASSWORD'}.issubset(env):
        raise RuntimeError('Explicit running-container Redis database and authentication settings required')
    network = set(app['NetworkSettings']['Networks']) & set(redis['NetworkSettings']['Networks'])
    if len(network) != 1:
        raise RuntimeError('Expected one shared application/Redis network')
    redis_ip = redis['NetworkSettings']['Networks'][network.pop()]['IPAddress']
    user = pg_env.get('POSTGRES_USER', 'postgres')
    database = pg_env.get('POSTGRES_DB', user)
    sql = """SELECT json_build_object(
 'backup_running',(SELECT count(*) FROM settings s CROSS JOIN LATERAL jsonb_array_elements(s.value::jsonb) r WHERE s.key='backup_records' AND r->>'status'='running'),
 'restore_running',(SELECT count(*) FROM settings s CROSS JOIN LATERAL jsonb_array_elements(s.value::jsonb) r WHERE s.key='backup_records' AND r->>'restore_status'='running'),
 'batch_active',(SELECT count(*) FROM batch_image_jobs WHERE status NOT IN ('completed','failed','cancelled','output_deleted'))
);"""
    counts = json.loads(command(['docker', 'exec', args.postgres_container, 'psql', '-U', user, '-d', database, '-At', '-c', sql]))
    client = RedisReadOnly(redis_ip, int(env.get('REDIS_PORT', '6379')))
    try:
        if env['REDIS_PASSWORD']:
            client.read('AUTH', env['REDIS_PASSWORD'])
        client.read('SELECT', int(env['REDIS_DB']))
        cursor, seen, processing = b'0', set(), 0
        while True:
            cursor, keys = client.read('SCAN', cursor, 'MATCH', 'image_task:*', 'COUNT', '200')
            for key in keys:
                if key in seen:
                    continue
                seen.add(key)
                value = client.read('GET', key)
                if value is None:
                    continue
                record = json.loads(value)
                status = record.get('status')
                if status not in ('processing', 'completed', 'failed'):
                    raise RuntimeError('Unknown detached-image state; retirement refused')
                processing += status == 'processing'
            if cursor == b'0':
                break
        counts['detached_image_tasks'] = processing
    finally:
        client.close()
    counts.update({'version': 1, 'container_id': app['Id'], 'started_at': app['State']['StartedAt'],
                   'checked_at_utc': datetime.now(timezone.utc).isoformat()})
    return counts


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--container-id', required=True)
    p.add_argument('--started-at', required=True)
    p.add_argument('--postgres-container', default='sub2api2-postgres')
    p.add_argument('--redis-container', default='sub2api2-redis')
    args = p.parse_args()
    try:
        print(json.dumps(collect(args)))
        return 0
    except Exception:
        print(json.dumps({'ok': False, 'error': 'Could not verify shared in-flight tasks; credentials and payloads withheld'}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
