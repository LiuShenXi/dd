# Sub2API Blue-Green Operations

This copy was recovered read-only from the BWH deployment on 2026-09-18.
It is retained for compatibility and rollback review, not automatically run by
the local upgrade. Read [the 0.2.6 upgrade record](../../docs/UPGRADE_0_2_6.md)
before future deployment; use an immutable candidate image and re-read the
live slot/configuration instead of reusing the generic `latest` example below.

This deployment keeps one PostgreSQL instance, one Redis instance, and the
existing `data/` directory. Only the application is duplicated. A small Nginx
router preserves both stable entry points:

- host Nginx to `127.0.0.1:8080`
- Monas/New API to `http://sub2api:8080`

The inactive slot remains stopped outside a deployment. Database migrations
run during candidate startup and are serialized by Sub2API's PostgreSQL
advisory lock. Migrations are forward-only, so every deployment starts with a
verified PostgreSQL, Redis, and application-data backup.

After bootstrap, do not use `docker compose -f docker-compose.local.yml up -d`:
that file still describes the retired single-application topology and is kept
only as the canonical service template. Use the scripts in this directory.
Never pass `--remove-orphans`; PostgreSQL and Redis are intentionally managed
outside the application-only blue-green Compose file.

## Server Layout

```text
/home/linuxuser/apps/sub2api/
  .env
  data/
  postgres_data/
  redis_data/
  docker-compose.local.yml
  blue-green/
  backups/automated/
  run/active-slot
```

## Commands

Initial migration from the single application container:

```bash
./blue-green/scripts/bootstrap.sh
```

Deploy a new immutable image into the inactive slot and switch after health
checks:

```bash
./blue-green/scripts/deploy.sh weishaw/sub2api:latest
```

Manual rollback to an already running healthy slot:

```bash
./blue-green/scripts/switch-slot.sh blue
```

The switch script does not start a stopped slot. Start and validate that slot
first. Do not run both slots indefinitely on a low-memory host.
