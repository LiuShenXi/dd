# BPS inline-image production repair — 2026-09-26

Production: sub2api.monasapi.com on SSH host bwh. Cutover completed at
2026-09-26T07:02:38Z (15:02:38 Asia/Shanghai). Active slot: green.
Source fix commit: 84e7aca682c58ebf923b9c509fb91645601339a9.
Version: 2.8.13-monas-bps.4; source baseline: d1466639e (the existing custom
production fork, including its carpool/billing/Sentinel changes).

## Root cause and implementation

The deployed BPS adapter explicitly rejected data:image/base64 during local
request validation, before any upstream inference. This produced the reported
input[6].content[2] error. The official BPS attachments endpoint accepts the
image using the selected account's existing authentication and proxy and returns
openai_file_id. The gateway now uploads validated inline images and substitutes
that account-scoped reference before inference. HTTPS image inputs still work.

Real integration testing also found that BPS rejects image arrays in
function_call_output.output with HTTP 422. Preserve the entire tool result as
JSON text and supply its images in an adjacent message explicitly marked as
untrusted tool output. Preserve text, order, call_id, detail, task and turn
identity. No raw Base64 is put into a text field or sent to inference.

Decoded images are limited to 20 MiB each. The bounded 512-entry, one-hour cache
stores hashes/file IDs only, scoped by tenant key, thread, account and credential.
Uploads use cancellation, a 45-second timeout, disabled redirects and bounded
response reads. Failed uploads do not submit inference or change scheduling.
No public relay, new credentials, database migration or frontend changes.
Temporary exploratory relay settings were removed and verified absent.

## Verification

- Full basispoints package tests passed. Focused Excel BPS service tests passed.
- Coverage includes streaming, non-streaming, compact forwarding, repeated images,
  scoped/concurrent cache behavior, cancellation, invalid input, upload failures,
  tool provenance and HTTPS tool images. git diff --check passed.
- Official attachment upload and inference both returned HTTP 200; the model
  correctly identified a synthetic red/blue image.
- Final candidate user images, tool images and non-streaming images all returned
  response.completed and correctly identified LEFT=blue; RIGHT=yellow.
- Public production user-image request fa1c7857-f922-4a18-b20f-66832dad507d and
  tool-image request 22927264-1941-4dd6-988e-b32742629b10 both returned 200,
  response.completed and the correct blue/yellow result.
- Public text request 9b19ea73-5ea9-4694-8512-c0a0e18d9506 returned TEXT_OK.
- Direct, router and public readiness: /health=200; unauthenticated /v1/models=401.
  Final audit showed zero post-cutover errors, zero image rejections, zero restarts
  and no OOM. This is the observed verification window, not a future guarantee.
- Old nginx workers drained naturally; blue was then stopped gracefully.

## Artifacts and rollback

- Binary SHA256: be8bece42aef3837c1481655d6586ecbf1dc6231e24c01cd6359ab613fdf962b
- Deployed image ID: sha256:9918c3e11d4bb1658016714d0abd3fb6d95f091346890f521d94d98efbb740ac
- Build stamp: 2026-09-26T06:59:54Z; build commit label: d1466639e-imagefix.
- Runtime image layers on the exact former production image; native Linux/amd64
  binary built off-host. Frontend assets copied from the exact baseline build.
- Verified backup: /home/linuxuser/apps/sub2api/backups/automated/20260926T065333Z
- Remote receipts/probes: /root/bps-image-fix-20260926
- Local probes/binary: C:/WORK-SPACE/dd/validation/bps-image-20260926
- Previous image and stopped blue container remain available as
  sub2api:2.8.13-monas-bps.3. Roll back by starting blue, verifying readiness, then
  running the existing guarded switch-slot.sh blue procedure. Never restore an
  old database snapshot over resumed traffic and billing.

## Prevention

Root-cause categories: cross-layer contract mismatch and test coverage gap.
Mock tests that merely preserve image arrays cannot prove upstream compatibility.
Keep synthetic real-image checks for user input AND tool output, require terminal
completion plus correct image interpretation, and verify production source/image
provenance before assuming a local upstream tag matches a deployed custom build.
