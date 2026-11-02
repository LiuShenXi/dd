# BPS native tool namespace repair — 2026-09-26

## Incident evidence and limits

The user reported unsupported native tool / no tool was executed in the task
查找3D冒险岛技术方案, ID 01a0dcd8-1eb8-7f03-b715-95b622356d14.
The task record and production operations log agree on six failures from
2026-09-26 17:57:55 through 18:01:49 Asia/Shanghai. First request:
99b64724-db45-4119-831c-ba9580af6b3a; last request:
57434a4f-0b48-4394-addb-6dd43fb880b6.

The model's completed tool batch failed native-to-client catalog resolution.
The stream adapter emitted response.failed without dispatching that batch.
This does not undo earlier successful commands in the same task. The final
user message in the affected task concerned running computer use in the
background; this repair did not resume that task or operate its desktop.

Old diagnostics do not retain native tool names or namespaces. Therefore the
exact identity behind the six production failures cannot be established from
those logs. The reproduced defect below generates the same error but is NOT
claimed to be proven as the cause of every reported failure. The original task
needs a user-initiated retry to confirm recovery.

## Reproduced defects and fix

Before the fix, a declared mcp__node_repl.js function returned as name=js and
namespace=mcp__node_repl was rejected with the exact reported message. A second
test showed that an explicit conflicting namespace could be ignored and a
same-named global tool selected. Both regression tests failed on the baseline
and pass after the change.

Resolve the exact namespace-plus-name identity first, preserve consistent
fully qualified names and the established functions host display prefix, and
reject explicit namespace mismatches. Do not guess by a short-name/suffix match.
Preserve function arguments, large JSON integers, custom raw input, call IDs
and transport-wrapped history replay. No tool execution occurs on the server.

Rejected unknown tools now include a fixed known-native category, namespace
state, name length and identity digest. Arbitrary names, arguments and user
content remain redacted. This makes a future differing failure diagnosable
without silently routing an undeclared tool.

## Verification

- Full basispoints package tests passed, including new split-namespace,
  conflicting-namespace, custom input, replay, streaming and privacy regressions.
- Focused Excel BPS gateway service tests passed. git diff --check passed.
- Candidate real function call: 9454f6a1-a446-4eef-8345-2dc1e6222cce.
- Candidate real custom call: 5daacfd8-e331-4782-bbf9-a894f506056a.
- Candidate screenshot regression: 890d6752-fba1-4717-a505-2b31fdcc88bd;
  response.completed and correct LEFT=blue; RIGHT=yellow result.
- Public function call: 0527cb63-bc36-423d-9121-c50b135fa2a0.
- Public custom call: e91da9cf-c88b-4ee7-9eb3-7aedff19b8d6.
  Both returned HTTP 200 / response.completed with the exact expected tool
  identity and arguments. Probe scripts inspect calls and never execute them.
- Split namespace at the raw native-response boundary is proven by the
  deterministic decoder/stream regressions; live probes verify gateway behavior
  without claiming that the model necessarily chose that raw wire shape.
- Direct/router/public readiness passed: /health=200, unauthenticated
  /v1/models=401. Active container healthy, no OOM and zero restarts.
- Post-cutover audit: zero unsupported-native-tool errors in the observed
  window. One separate request-stage HTTP 400 was recorded as Failed to read
  request body; no additional cause is inferred from that generic message.

## Release provenance and rollback

- Deployed at 2026-09-26T10:17:51Z (18:17:51 Asia/Shanghai).
- Active slot green, version 2.8.13-monas-bps.6.
- Source fix: 9d2414f3f57e6c8de368595ccaaa9d89f10cca6b.
- Baseline: 4ebd89296, preserving the production JPEG and inline-image fixes.
- Binary SHA256: 9611fa382170bae141c5f445d7d5c3c518809176d5092766d7adcf9eac742693.
- Image ID: sha256:6618da177d57996b7c81e33331c172d96a1f63a9074d960e8aff74b9ef54df26.
- Backup: /home/linuxuser/apps/sub2api/backups/automated/20260926T101341Z.
- Remote evidence: /root/bps-native-namespace-20260926.
- Local probes: C:/WORK-SPACE/dd/validation/bps-native-namespace-20260926.
- Exact former production image used as runtime base; native binary built
  off-host. Environment/memory settings checked against the active container.
  No migrations, credential changes, frontend changes or task restarts.
- Previous blue slot stopped gracefully only after old router workers drained.
  Its .5 image remains available. For rollback, start blue, pass readiness, then
  use the existing guarded switch-slot.sh blue procedure. Do not restore old
  database snapshots over live billing.

Prevention: test both combined and split tool identifiers and conflicting
namespaces. Keep whole-batch validation before dispatch and safe structural
diagnostics. Distinguish a reproducible compatibility defect from an incident
whose original tool identity was never recorded.
