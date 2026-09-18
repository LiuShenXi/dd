# Publish and deploy the compatible 0.2.6 release

The user explicitly requested publishing this integrated version as remote main
and deploying it to BWH production. This authorizes the concrete publication,
backup, brief maintenance, drain/lock, application replacement and route change.

- Publish only to LiuShenXi/dd; preserve the existing independent operations
  commit and a remote backup branch. No v* tag or automatic Release workflow.
- Build the formal root Dockerfile with a fixed source revision, server and
  carpool-release CLI. Validate the exact image against isolated synthetic data.
- Preserve the prior completed compatibility tests and fix publication CI
  findings without weakening billing or request-context behavior.
- BWH has about 1 GiB RAM. Do not weaken the generic dual-slot 700 MiB gate;
  perform an explicitly serialized maintenance release instead.
- Pre-stage the verified image. Stop new ingress, drain active HTTP/WS and pending
  usage to zero, lock, then stop old green. Back up PostgreSQL, Redis, app data and
  configuration privately before starting blue and applying six migrations.
- Compare all 291 prior migration checksums/timestamps, protected users/keys/
  groups/explicit quotas/accounts/carpool tables/usage, then route only after the
  candidate is ready. Codex ticket collection stays disabled.
- Verify direct, router and public health 200 plus unauthenticated models 401,
  actual image identity, open gate, 297 migrations and stopped old application.
- Retain old image/config for application rollback. Never restore an old database
  snapshot after new production billing resumes. Preserve private backup files
  on the server; commit only sanitized deployment evidence.
