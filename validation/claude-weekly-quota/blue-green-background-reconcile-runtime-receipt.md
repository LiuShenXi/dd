# Background reconciliation: actual runtime receipt

**PASS**, 2026-10-20 04:00:52–04:00:57 UTC, exit 0. This separate synthetic proof modifies neither production nor the original v4 PASS receipt.

[Machine-readable receipt](office-bluegreen-background-reconcile-20261020T040057Z.json) records source identities, original evidence hashes and exact old/new instance IDs. The helper tested was `background-reconcile.py` SHA-256 `69fe83fead918542e9925194bd521c5b8310c3f3be50dd928fccb26232f57a8f`; helper, controller and proof-source hashes were unchanged throughout.

```sh
python3 validation/claude-weekly-quota/blue-green-background-reconcile-proof.py \
  --private /Users/shenxi/.codex/private/claude-weekly-quota/office-bluegreen-v4-20261020 \
  --evidence /Users/shenxi/.codex/private/claude-weekly-quota/office-bluegreen-reconcile-20261020
```

- Verified every synthetic model call had completed, old R0 remained stopped with restart disabled, and the candidate had no active HTTP, usage or background tasks.
- Saved a complete private snapshot of original v4 files, including the controller state whose hash matches the original PASS receipt. The original public receipt stayed byte-for-byte unchanged.
- Logged in normally through the actual candidate, saving its access/refresh pair with mode `0600`; no credential was printed.
- Restarted only the exact synthetic green container. Its container/image stayed the same, process start time and background instance ID changed, HTTP reopened, and background jobs were held with no leadership.
- Executed the unmodified helper with the existing private controller configuration. It returned `background_activated`; both Nginx domains reported the exact new instance, active with leadership.
- Changed only the pair file's local `expires_at` scheduling metadata to 30 seconds ahead. Access and refresh token bytes were unchanged before invoking the helper. Its real `/api/v1/auth/refresh` call returned `token_refreshed` and rotated both tokens. The new access token authenticated on both domains; the access file and pair remained `0600` and consistent.
- Executed reconciliation again: `already_active`. Controller state, route file, token pair, process identity, financial baselines and provider audit were unchanged. Old R0 stayed stopped with restart disabled.

Private evidence and original snapshot are retained under `office-bluegreen-reconcile-20261020`. This proves restart recovery and real refresh rotation against the tested release image, not elapsed natural JWT expiry or a production timer execution. No Go build, product-source change, or production operation was performed.

After the coordinator confirmed production completion, the final synthetic database was dumped privately (compressed size 85,649 bytes), and current logs, state, provider audit and both pre/post-reconcile snapshots were saved with file hashes. [Cleanup receipt](office-bluegreen-v4-final-cleanup.json) records removal of exactly six v4 containers, their two volumes, the v4 network and the empty failed-initialization network whose ID matches its private prepare record. Candidate/R0 images, the native driver, all public receipts and private backups were verified retained. No production or unrelated Docker resource was touched.
