# Final release preparation — not executed

Deployment and remote push are explicitly authorized after acceptance. This
document is a preparation checklist; no deployment or production enablement
is represented here. The root coordinator executes the final production step.

- Recheck the actual main-server/sub2api2 image, revision, executable hash,
  migration list, Compose digest, runtime flags, replica/worker inventory,
  ports and business readiness immediately before release. Current observed
  deployment is /opt/sub2api2/docker-compose.migrated.yml, project sub2api2,
  loopback 29080, not the historical BWH blue-green scripts.
- Retain immutable final R1 and R2 images and receipts. R1 must contain the
  final v2 concurrency protocol and I5 paid-order recovery, with new issuance,
  sales and admission disabled and recovery enabled. Early I3 R1 is insufficient.
  Prefer the verified production runtime image as the final base, replacing
  only reviewed executable artifacts; record actual compiler/runtime versions.
  `coordinator-r0-provenance.json` proves this base. Production Docker 29.8's
  containerd store and local Docker 25 classic storage can report index versus
  config digests; verify the exported manifest/config chain and binary on the
  destination rather than assuming their displayed image IDs must be identical.
- Back up current database and Compose/configuration privately and verify
  readability. No credential or production-data copy goes to Git. Backups are
  for disaster recovery, not overwriting newer billing data during rollback.
- Use an execution mechanism that survives a local Codex/SSH interruption,
  with durable remote phase/receipt files and bounded failure handling. The
  crash recovery demonstrated that tool-launched Windows child processes may
  disappear with their host Job; do not depend on the local chat staying alive
  during a production drain or Compose mutation.
- Reuse the actual release API and authenticated operator path without logging
  credentials. Wait for active_http and pending_usage to reach zero, with a
  bounded drain and state-aware cancel before mutation if it cannot finish.
  Never force-close active GPT streams or clear Redis user/WS slots to pass.
- Production Claude default concurrency is explicitly 3. Preserve every
  user's existing GPT/legacy setting and optional Claude override. No real
  cards, group conversion, user entitlements or model consumption are implied
  by deployment authorization. Existing groups retain their stored mode.
- Verify actual final container image/revision/binary/frontend assets, forward
  migration checksums, recovery flags, healthy status, release gate open and
  exact unauthenticated /v1/models 401 via both loopback and public entrances.
  Capture restart count relative to the pre-release count of 5, rather than
  claiming an original zero count. Read-only wallet/term/GPT guards supplement
  health; use no real model call without an explicit test identity authorization.
- Publish only the reviewed codex/claude-weekly-quota branch to origin, verify
  its remote SHA and record it. origin/main diverges from the production-based
  branch; do not force-push or overwrite unrelated main commits.

Read-only Compose inventory is recorded in
`coordinator-release-inventory-readonly.json`: postgres, redis and sub2api are
the three services in the actual project. The optional global administrator
API key is absent; no release-control request or production mutation was made.
The authorized release must use the existing administrator authentication
path without persisting or logging a password/token. This is preparation,
not proof that the application drain is currently open or empty.

I4-v2, I5, I6 acceptance, the full 74-case evidence map, realistic migration
timing and final rollback proof are still required before executing this list.

Additional read-only edge/replica inventory is in
`coordinator-release-edge-readonly.json`: one running application container,
Nginx active, with `/etc/nginx/conf.d/sub2api-migrated.conf` and
`/etc/nginx/conf.d/teamsub2.conf` both proxying to 127.0.0.1:29080. The production
image, start time and explicit false startup hold remain unchanged.

Release control is process-local. An operation ID from the drained/locked old
process is not a resume token for a freshly recreated process. Normal candidates
start with RELEASE_DRAIN_START_HELD=false and complete startup migrations before
serving. The final rehearsal/runbook must distinguish old-process drain/lock,
container replacement and new-process readiness/open-state verification. If an
external maintenance gate is required, it must be explicitly rehearsed against
the actual ingress; never substitute an unsafe start-held configuration or clear
GPT Redis state. No edge configuration or container has been changed here.

The exact production PostgreSQL 18.6 image is now available privately for I6.
See `coordinator-postgres-runtime-export-20261019T180857Z.json` and
`coordinator-postgres-runtime-provenance.json`: image-only export, verified OCI
index/platform/config graph and nine production RootFS diff IDs. The archive
has no RepoTags and contains no database volume or rows. It has not been loaded
or started locally. Use an isolated named tag, then verify the program hash and
version before the realistic migration rehearsal; archive provenance alone is
not a PostgreSQL 18.6 migration test pass.
