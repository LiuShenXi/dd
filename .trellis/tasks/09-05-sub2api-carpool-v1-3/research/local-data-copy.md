# Isolated Local Database Copy

## Authorization and Source

The user authorized a read-only copy of remote Sub2API data for local testing, not production writes or deployment. The configured knowledge-base directory contained no Sub2API database record; the existing `newapi-vultr` SSH alias and narrowly scoped live container metadata were used to verify the target.

Verified application: `sub2api-green`, image `sub2api:0.2.1-refundfix-36266f512776`. Database: `sub2api-postgres`, PostgreSQL 18.6, database `sub2api`. Export used pg_dump custom format, no owner/ACL, and a short lock-wait timeout. No remote files, schemas, rows, service configuration, or running containers were modified.

## Archive and Restore Evidence

- Compressed archive: 15,811,942 bytes.
- SHA256: `82DE61661C539F1F37DD339F642E95768D54F3FEE7BB49B6010FB93E6223D639`.
- Private location outside git: `C:/Users/Administrator/AppData/Local/Codex/PrivateTests/sub2api-carpool-v1.3-20260905/production-snapshot.dump.dpapi`.
- The archive is Windows DPAPI CurrentUser encrypted, with user/SYSTEM-only inherited ACLs disabled. Decryption round-trip hash was verified. Original plaintext host and container dump files were removed after successful restore/encryption. Recovery requires the same Windows user DPAPI context; no plaintext production secret appears in this task.
- Docker endpoint was verified as local Docker Desktop (`npipe:////./pipe/docker_engine`), not the remote server.
- Fresh local database: container `carpool-v13-test-postgres`, volume `carpool-v13-test-pgdata`, database/user `carpool_test`, PostgreSQL 18.3 (same major as source).
- Fresh empty Redis: `carpool-v13-test-redis`; no production Redis data was copied.
- Restore completed without errors. Restored counts: users 13, usage_logs 41,154, usage_billing_dedup 41,154, schema_migrations 281. These are snapshot counts, not claims about the current live database.

## Isolation Gate

Both containers join only `carpool-v13-test-net`, inspected `Internal=true`, with no published database/cache host ports. In-container wget exists; probes to `https://example.com` and `http://host.docker.internal:18080/health` both failed. Existing Docker services and volumes were not changed.

The sanitization and conservation gate passed at 2026-09-06 00:26 Asia/Shanghai, followed by outbox hardening and another 109-metric comparison. The director may now connect the application only under the isolation requirements below. Application startup must retain the internal-only network, disabled external jobs, fresh local JWT/TOTP configuration, and loopback-only web ports. Real identities and credentials must never appear in browser screenshots or reports.

## Copied User Dates and Tier Mapping

The user requires local copied-user test terms to start at `users.created_at`, expire exactly 30 days later, and retain normal 7/7/7/7/2 cycles. Registration dates must remain untouched during sanitization. At the check time, all 12 nondeleted copied users were within their registration-based 30 days.

The source copy has only an OpenAI standard group with weekly_limit_usd=0, zero subscription plans, and zero nondeleted native subscriptions. There is no verified three-tier carpool mapping. Do not infer a plan from balance or convert the standard group wholesale. Use copied records for compatibility/regression and registration-based test openings only after a plan is explicitly selected; use synthetic identities for all three plan acceptance workflows.

## Independent Pre-Sanitization Fingerprints

These hashes cover only stable financial/identity-reference/time fields, not plaintext personal data or credentials. A second check after sanitization must match.

| Table | Rows | Fingerprint |
| --- | ---: | --- |
| users | 13 | 21b9342c995463eb97558c520975e9bd |
| api_keys | 18 | 659fd176f5ac32ffae42599e70ce37ef |
| usage_logs | 41154 | 398a6e2b53f20db5958fe92191d45801 |
| usage_billing_dedup | 41154 | 73718d97a59097e34837b03382223862 |

The original dedup fingerprint above includes request identifiers/fingerprints that are deliberately pseudonymized. Its post-sanitization value is not expected to match. An independently captured stable-field hash (id, api_key_id, created_at) matched before/after as `c1ebe3024b0ef941bc7d3d0834f5d21c`. All 41,154 usage-to-dedup associations still match. Users, keys, and usage-log financial/time/reference hashes in the table matched unchanged.

## Completed Sanitization Gate

- `sanitize-test-database.ps1` verified the local Docker endpoint, internal-only network and absence of an attached application, then ran the reviewed SQL with ON_ERROR_STOP in one transaction.
- The first attempt rolled back because API-key credential rotation enqueued cache-invalidations and violated the exact row-count guard. No partial updates survived. The corrected transaction suppresses only `api_keys.trg_api_keys_auth_cache_invalidation` during key rotation and restores it before commit; all other triggers and every constraint remain active. The old and new Redis environments are unrelated, so no production cache invalidation is required.
- PostgreSQL's OpenAI account trigger retains its noncredential `openai_long_context_billing_enabled` boolean. The safety assertion permits only that single boolean in otherwise-empty account extra JSON, preserving this billing flag while removing all secrets.
- Successful main transaction SHA256: `DC648E4EA136CD1418D8A8D85395F4AE4A5753B1A6B2F1C774FF4C2319532691`. A subsequent `finalize-local-copy.sql` additionally replaced existing cache-key hashes with local ID-derived hashes and cleared residual leases; that change is also included in the current main SQL for future restores.
- Independent verifier compared 109 exact metrics: every public-table row count except settings (which gains explicit fail-closed keys), stable financial/time/reference hashes, and cross-table usage/payment relationships. All matched again after final hardening. Payment tables were empty in this snapshot, so payment relationship checks are schema/sanitizer coverage, not evidence of populated historical payment data.
- Read-only postchecks returned zero unsafe users, active keys/accounts/providers/monitors/plugins, pending payments, or disabled user triggers. All 12 nondeleted users still satisfy their original registration-based 30-day window at check time.
- Fresh Redis responds PONG. Domain and host.docker.internal probes fail resolution; direct `http://1.1.1.1` fails with Network unreachable, confirming denial independently of DNS.

## Application Handoff

Use only network `carpool-v13-test-net`; PostgreSQL alias `carpool-db:5432`, database/user `carpool_test`, and Redis alias `carpool-redis:6379`. The generated local-only password is in private `postgres.env` beside the encrypted archive; consume it programmatically without printing it or embedding it in git. No database or Redis host port is published. The planned web address is `http://127.0.0.1:38088`; port 18080 belongs to another service and must not be used. Application and mock upstream must have no second network, no host mapping/proxy, and only loopback web exposure. Apply every explicit environment/DB/config gate in `local-test-isolation.md` before startup. The original encrypted archive must never be mounted into the app. Application startup and feature acceptance remain pending, not passed by this data-readiness gate.
