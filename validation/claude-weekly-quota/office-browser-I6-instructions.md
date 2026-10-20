# Office I5/I6 browser fixture

Status: coordinator CUA is complete through F4 on 2026-10-20. The final browser candidate was `sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`, source `8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`, at **http://127.0.0.1:59645** during acceptance. The coordinator owns private diagnostics and synthetic cleanup; do not assume this URL remains available. `office-I6-browser-receipt.json` records each F1–F4 observation and screenshot's exact candidate. Fixture helpers and this closeout did not change product source.

## Isolation and provenance

- Initial preparation used `sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483`. The final F4 browser application is `sha256:3d3c392d424663235dce2a7130ac4abe272a2e04ca3540d1ea0746840a84f122`, with source `8a1629916bd4ee338ffdfa51a368f20a347012281be914242c669e12138b20af`, checked against `office-I6-build-F4.json` and immutable image labels. The public browser manifest retains each earlier identity in its upgrade records.
- The main applications keep running. A transaction-consistent `pg_dump` is restored into a fresh `cwq_browser_i6_<random>` database in the same synthetic PostgreSQL instance. SQL helpers reject every other database name. Only the copied replica registry is cleared.
- Redis uses a separate container from the main Redis image ID, on its native architecture. Neither `CLIENT PAUSE` nor Pub/Sub can cross into this browser Redis. No main Redis command, key mutation, flush, application stop, or container removal is performed.
- The new application, independent fake payment provider and Redis share the existing internal Docker network, publish no ports, and have the task label. The application has a fresh synthetic TOTP encryption key and declares only `browser-1` in its replica roster. The provider has its own synthetic payment key.
- PostgreSQL remains a shared process, so concurrent PostgreSQL-wide stop/restart or fault injection would also interrupt browser preparation. Ordinary main-fixture queries and streaming acceptance can continue.
- All credentials, tokens, SQL dumps, provider config, HTTP bodies and raw logs are written below the task's private directory with private permissions. The public output contains provenance, local URL and synthetic IDs only. These fixture secrets are never copied into this document.

## Run after review

From the repository root, keep this command in a persistent foreground exec session:

The command below documents initial preparation. Do not rerun it for the existing
browser fixture; it intentionally rejects an existing private directory. The
current fixture and its CUA data must be preserved and use `upgrade` when needed.

```sh
python3 validation/claude-weekly-quota/office-browser-I6.py prepare --serve
```

The default private directory is `/Users/shenxi/.codex/private/claude-weekly-quota/office-browser-I6-20261020-v1`. It must not already exist; failed preparation is retained for inspection and is never automatically repeated. A reviewed repeat must explicitly choose a fresh `--private-dir` below the task private root, before the subcommand.

Read the resulting URL and IDs from `validation/claude-weekly-quota/office-I6-browser-environment.json`. The synthetic admin and two new user credentials live only in the private `browser-fixture.json`: `admin_email` / `admin_password`, and `users[]` with purposes `buyer` and `manual`. The cloned administrator inherits the main fixture's acknowledged compliance state. The native bridge stays alive because `--serve` keeps the parent process running; do not rely on a detached child surviving an exited exec session.

The script performs actual admin/user HTTP login, creates users and keys, configures the provider and plans through admin APIs, enters maintenance through the rollout API, and obtains a preview with explicit `allocations: []`. SQL is used only for the fresh database restore, copied registry cleanup, two preboot allowlisted groups, and one synthetic historical terminal order. No rollout conversion is pre-applied.

It also creates and completes one actual synthetic purchase through the regular order endpoint and a valid signed EasyPay webhook. This makes an immutable completed-order snapshot available immediately. The fake provider never charges money or sends callbacks automatically.

## Browser acceptance sequence

1. **Plan editing:** log in as the private synthetic administrator at `/login`; open `/admin/orders/plans`. Find the public manifest's `sale_plan_id`. Its sale price is **USD 12.34**, quota is the separate exact string **USD 125.12345678**, and term is **7 days / 168 hours**. Confirm the term mode and fixed duration. The separate `manual_plan_id` is not for sale and has default quota **USD 300.00000000**.
2. **Purchase and frozen order:** log in as `buyer`, open `/purchase`, and inspect the sale card/confirmation. The configured CNY conversion is 7, so payment is **CNY 86.38**, independently of the allowance. `/orders` already contains `sample_order_id`, completed with the original **125.12345678** snapshot. Create a second order through the UI if checking the payment flow. Complete only that synthetic order with the command below, then revisit the UI.
3. **Snapshot stability:** as admin, edit the sale plan's default quota to a distinguishable positive value. Reopen the completed sample order in both `/orders` and `/admin/orders`; the recorded allowance and 168-hour duration must stay at their purchased snapshot. The plan may be restored after observing this.
4. **Manual default and override:** open `/admin/users`, select the new `manual` user's Claude weekly-card action, and choose the new term group. Select the unsold manual plan and use its **300.00000000** default. Verify the preview before confirming. Repeat with a deliberate override such as **333.00000003**; the preview must show that override, not the plan default. Confirming a second card exercises renewal/queue display.
5. **Controlled conversion:** open `/admin/groups`, find `rollout_group_id`, and open its Claude rollout action. It starts in maintenance with two new Key owners and **no inferred allocations**. The replica list must show only `browser-1`. Historical terminal orders show **1** with a nonblocking warning: a later payment needs manual review and does not automatically infer allowance or issue a card. Add explicit user/quota pairs, preview them, edit a value to confirm the old preview is invalidated, then obtain a fresh preview and confirm conversion. Conversion keeps maintenance enabled until explicit release.
6. **Accessibility and language:** repeat key forms/modal scrolling at a narrow width (approximately 390 px), inspect the English labels as well as Chinese, and check that disabled actions, validation messages, exact decimal values, long IDs and replica details remain legible. Existing GPT/windows groups in the cloned database remain available as regression comparison; use only the new browser IDs for mutations.

For a browser-created sale order, the coordinator may invoke:

```sh
python3 validation/claude-weekly-quota/office-browser-I6.py complete-order ORDER_ID
```

The helper checks the application image, loopback target, newly created user IDs, sale plan, provider instance and term group before delivering a signed synthetic callback. It verifies `COMPLETED` and unchanged snapshot afterward. It refuses foreign orders and never retries failed callbacks automatically. A custom private directory must also be supplied when one was used for preparation.

## Upgrade an existing browser application

After the coordinator supplies and authorizes a new immutable candidate, use the
explicit command below. Substitute only the reviewed image ID, source digest,
and successful build receipt; the command rejects inconsistent provenance.

```sh
python3 -B validation/claude-weekly-quota/office-browser-I6.py upgrade \
  --image-id REVIEWED_IMAGE_ID \
  --source-sha256 REVIEWED_SOURCE_SHA256 \
  --build-receipt REVIEWED_BUILD_RECEIPT \
  --serve
```

The helper validates the existing browser application's exact identity,
separate database/Redis and private environment, retains its stopped container
for logs, then starts only a replacement application. It checks internal health
before replacing the precise native bridge PID at the same loopback port.
Database, Redis, provider, credentials, plans and cards are preserved. It makes
no SQL calls and waits for the previous browser replica heartbeat to expire
naturally. The existing completed purchase snapshot must stay unchanged.

On success the private fixture and public manifest record the new application,
image, source and bridge PID, along with the preserved previous identities.
The new foreground exec session replaces the old bridge's session and must stay
alive. Upgrade failure retains its private record and does not automatically
restart or recreate resources. `complete-order` subsequently verifies the
updated fixture/manifest/application identities before any synthetic callback.

## Cleanup and completed acceptance

This script intentionally has no automatic cleanup. Retain private diagnostics and the public ID manifest while acceptance is in progress. Stop/remove only the browser application, provider, Redis and bridge, and drop only its explicitly named copied database after the coordinator finishes; preserve the main applications, main Redis, PostgreSQL container and main database.

Static syntax/help and isolated helper checks passed. Actual preparation also passed: real logins, API-created provider/plans/users/keys, maintained rollout preview with explicit empty allocations and no blockers, one historical terminal-order warning, and a genuine synthetic order plus signed webhook yielding `COMPLETED` with unchanged quota snapshot. The generated public manifest records the exact IDs and source. Preparation alone did not establish browser layout or interaction quality. Coordinator CUA subsequently passed the scoped F1–F4 flows in `office-I6-browser-receipt.json`, including final actual admin order list/detail on F4. Other refund statuses and asynchronous races remain explicitly identified as component-test coverage.
