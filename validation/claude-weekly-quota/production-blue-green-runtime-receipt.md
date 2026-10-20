# Actual isolated host-Nginx release receipt

**PASS**, fresh v4, 2026-10-20 03:54:34–03:55:12 UTC (38.29 seconds), command exit 0. This is an isolated synthetic runtime proof, not a production deployment receipt.

Machine-readable evidence: [office-bluegreen-runtime-20261020T035512Z.json](office-bluegreen-runtime-20261020T035512Z.json). The candidate is exactly the image in [production-20261020-build.json](production-20261020-build.json):

- Candidate image: `sha256:1ef6ac26fd2405baa50ca36d6b6629bd2d19c736e8e85702902220c6aab152c0`.
- Product source: `26926f6a20ed8bbe360c582aca51f3e491c54d61c5e6f18ddb16715c538b39cc`, commit `cace0104c6bbda896b321506ead009da4cfe8ac5`.
- Original R0 image: `sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677`.
- Controller: `13f2468b5d352429a316108675212a5161e86295f165c822fb23b2378cb63f03`.
- Harness: `4e4d2cfb636eb71e47009405c4a66eef94ca8426c35c8b70a9cd40900c08c32b`.
- Live in-flight collector: `46c389eef0dd2bbfb4d33ca7eded9ac4f7b0a0412eb39bdd601a36263a85aeb5`.

Controller, collector and harness hashes were unchanged from start to finish. Harness corrections after the product commit changed no product input or tested image.

```sh
python3 validation/claude-weekly-quota/blue-green-runtime-proof.py run \
  --private /Users/shenxi/.codex/private/claude-weekly-quota/office-bluegreen-v4-20261020 \
  --candidate sha256:1ef6ac26fd2405baa50ca36d6b6629bd2d19c736e8e85702902220c6aab152c0
```

## Observed behavior

1. Real old GPT HTTP, GPT SSE, GPT WebSocket, Claude SSE and TLS HTTP/2 requests were admitted before candidate startup. The candidate preserved every old account/user/WS Redis lease member. Its coordinated background jobs stayed held.
2. The real controller changed both Nginx virtual hosts. Readiness failure before cutover left route bytes unchanged. A deliberately misrouted second-host identity check after HUP restored both old routes while preserving already-admitted work on both processes. Normal switch, rollback, and switch again succeeded.
3. A one-second retirement budget returned exit 3 while old streams remained. The old gate stayed open and container stayed running. The original WebSocket completed its held turn and a second turn on the same connection. All held responses completed without provider cancellation.
4. An HTTP/1.1 connection was primed with GET, then reused for a POST whose body remained incomplete until after cutover. A real TLS HTTP/2 upload likewise reached the old application before cutover and delivered its remaining body afterward. Both completed successfully. This does not claim arbitrary new streams can be sent after Nginx GOAWAY.
5. A row lock on the synthetic legacy Claude user's balance held a real asynchronous usage-pool task after the HTTP response completed. At 03:55:02.305 UTC the actual status was `active_http=0`, `pending_usage=1`, gate `open`. The controller then entered `draining`; at 03:55:04.966 it still reported `pending_usage=1`, the process was running, and the retirement command had not completed. Only after releasing the lock did settlement finish and the old container stop at 03:55:10.031. Its restart policy was verified `no`.
6. Background activation succeeded only after verified old-process retirement. Fresh GPT traffic then completed on the candidate. Actual Nginx access logs contain successful model traffic for each host to each old/new upstream.

Final accounting: **7 GPT turns**, 7 billing rows, 7 dedup rows, one original cycle, exact total cost **USD 0.00012250** and matching ledger debits. **7 legacy Claude calls** deducted exactly **USD 0.0042**. Every provider call has one usage log; there were no cancelled provider calls. GPT native balance and all term-wallet usage totals were unchanged.

The original production in-flight collector performed real PostgreSQL reads and Redis SCAN/GET through a Desktop byte transport adapter. Its required counters were zero before final stop. Separate actual calibration proved `processing → 1`, `completed → 0`, and unknown status → refusal; the exact synthetic calibration key was deleted. See [office-bluegreen-inflight-calibration.json](office-bluegreen-inflight-calibration.json).

## Retained failures and scope

- [03:49:15](office-bluegreen-runtime-failed-20261020T034915Z.json): free space below the unchanged 2.5 GiB pre-run guard; no candidate started.
- [03:50:35](office-bluegreen-runtime-failed-20261020T035035Z.json): harness incorrectly expected an initial GPT SSE event before releasing the provider's content delta. R0 intentionally buffers `response.created` for failover. Corrected to prove held GPT SSE with actual provider admission plus Redis lease and require complete SSE afterward. Claude still requires its initial `message_start`.
- [03:52:07](office-bluegreen-runtime-failed-20261020T035207Z.json): harness locked the GPT cycle but expected an asynchronous pool counter. GPT carpool settlement is intentionally synchronous; the response waited correctly. Corrected the injected lock to the legacy Claude asynchronous usage path. Earlier substeps in that run are not substituted for the final full v4 PASS.
- [03:53:33](office-bluegreen-runtime-failed-20261020T035333Z.json): fresh v4 again stopped at the same disk guard before application startup. After precise task-resource/cache cleanup, the full v4 chain above passed.

Failed v3 logs and synthetic database dump remain private; [office-bluegreen-v3-cleanup.json](office-bluegreen-v3-cleanup.json) identifies the exact removed resources and backup hashes. Earlier completed v2 cleanup is similarly recorded. No unknown containers, volumes, images, production data, or module/toolchain source were removed.

The fresh schema seed uses the retained native driver's PostgreSQL 18.1 initializer, then starts exact PostgreSQL **18.6** on that owned same-major volume before runtime testing; it is not relabeled as a native 18.6 seed. Runtime version `180006` and binary identity were verified. Nginx is local arm64 1.27.1; the applications are the actual amd64 release images. The local Nginx network namespace models host-loopback routing. Production Nginx topology, configuration, backup and release execution have separate coordinator receipts.

This first R0 upgrade covers legacy Claude traffic. It does not claim a later term-enabled candidate-to-candidate Nginx cutover, real paid production requests, or production-representative database load. Private v4 evidence contains complete timeline, provider audit, controller logs/state, effective Nginx configuration, access logs and application logs; the JSON receipt records their hashes. The final image and native driver are retained.
