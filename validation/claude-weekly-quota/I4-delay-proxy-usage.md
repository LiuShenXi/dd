The task-only delay proxy forwards solely to the exact synthetic loopback candidate recorded in its receipt. It binds `127.0.0.1`, runs outside the tool Job via WMI ShowWindow0, logs no payloads, and has no business-server hooks. Ordinary requests and static resources pass through unchanged.

Read `environment-I4-delay-proxy-20261019T180232Z.json` for the current proxy URL. Use the same private synthetic login fixture as the direct candidate. Browser acceptance remains separate from proxy availability.

Control endpoint `GET /__cwq/control` reports counters and the armed path. To hold one exact API response, POST JSON to that endpoint:

```json
{"path":"/api/v1/admin/users/2/subscription-quota/terms/preview","method":"POST","hold":true}
```

Replace the example path with the actual observed canonical preview URL and synthetic A ID; do not assume the example resource spelling. Only one hold may be armed at a time. The proxy obtains the real backend response before delaying its delivery. Other paths, including B's preview, proceed normally. After closing A and opening B, release A with:

```json
{"release":true}
```

For a delayed error instead of the original response, arm with an additional `"error_status":503`. The resulting error is explicitly marked `SYNTHETIC_PROXY_DELAYED_ERROR`. This is a local transport fault and is not a business-server response. Each hold has a two-minute ceiling.

The same mechanism can hold the exact synthetic boundary user's summary GET response while its full 168-hour current card naturally expires. Create that fixture immediately before the browser observation, and leave the A/B ledger fixtures unchanged. Observe that the UI waits for a real server projection rather than locally rotating cards or altering balances.

Record browser steps, actual armed path, `held_responses`/`released_responses`, screenshots/assertions, and direct candidate image/source identity. Proxy startup alone does not prove the UI race.
