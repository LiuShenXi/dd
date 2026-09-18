# Codex ticket production configuration — 2026-09-19

At 01:42:52 Asia/Shanghai, ticket collection/injection was enabled on the existing
`0.2.6-carpool-20260919` production instance without restarting the application.
The user selected the existing `US-static-car8` proxy (ID 3) for collection.
Business account egress, account scheduling flags and other settings were preserved.

## Verified observations

- Before enabling, two bounded requests through the selected proxy, using active
  OAuth account 2, each returned HTTP 200 and a 292-character `x-codex-turn-state`.
- Requested `gpt-6-astra` completed as `gpt-6-astra`; requested `gpt-5.6-sol`
  completed as `gpt-5.6-sol`. Each request used 16 input and 5 output tokens.
  These are two small successful samples, not a general model-quality guarantee.
- The two real tickets were persisted and the scheduler account snapshot was
  refreshed before the global switch was enabled, avoiding an empty-ticket gate.
- At 01:44:05, the administrator API showed both account 2 tickets as 292 chars,
  `ready=true`, `blocked=false`, with about 55 minutes remaining.
- The native background loop was observed running. Paused accounts 5 and 6 had
  no valid tickets: account 5 returned length 312; account 6 returned HTTP 400.
  Their pre-existing `schedulable=false` values were not changed. The collector
  currently also probes active-status accounts that are paused for scheduling.
- Public `/health` returned 200 and unauthenticated `/v1/models` returned 401.
  Application start time and restart count were unchanged.

## Mechanism and limits

Defaults: target length 292, local lifetime 3600 seconds, refresh when 600 seconds
remain, six seconds between completed probe cycles, 25-second probe timeout.
Tickets are keyed by account/model, held in application memory and persisted in
`accounts.extra`. The administrator API redacts the ticket material.

The production `fail_closed=true` default remains: an enabled, gated OAuth
account/model without a valid ticket is excluded or rejected. API-key framework
accounts are outside this mechanism. The harvest proxy does not change the
normal business request proxy.

The code applies tickets to ordinary HTTP forwarding and initial WS handshakes.
There is no direct outbound-header capture in this validation, so acquisition
and readiness must not be described as wire-level proof of automatic injection.
No user API key or customer carpool billing was used for the diagnostic probes.

There is no implemented model-reroute-triggered refresh or explicit 312-revocation
handling. Some WS reconnect paths reuse the upstream handshake state, including
non-292 values, instead of reapplying a newly harvested ticket. This configuration
therefore does not establish that degradation or reconnection problems are fixed.

The [official Codex client](https://github.com/openai/codex/blob/main/codex-rs/core/src/client.rs)
describes turn-state as sticky routing within a single turn and warns against
reusing it between different turns. Sub2API's account/model-wide reuse is an
experimental third-party behavior, not an official priority or quality guarantee.

## Recovery and evidence

The independent collection switch can be disabled in administrator settings;
the settings cache expires within five seconds. No deployment or database
rollback is required to disable collection/injection.

Private before-state and raw ticket files are retained with restricted permissions
under `/home/linuxuser/apps/sub2api/releases/codex-ticket-20260919`; never publish
them. Public-safe configuration and verification receipts are in
`validation/upgrade-026/evidence/codex-ticket-*-receipt.json`.

The release-time ticket-OFF observation in `RELEASE_0_2_6_20260919.md` is historical;
this document records the subsequently authorized configuration change.
