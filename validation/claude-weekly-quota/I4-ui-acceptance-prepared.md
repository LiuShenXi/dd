# Prepared browser acceptance script — not executed

`I4-ui-acceptance.mjs` is a standalone, write-only preparation. No browser installation, launch, connection or Playwright import has been performed. Human authorization for an alternative browser is still required; the command flag does not grant that authorization.

Update2026-10-19 23:11UTC: the user has opened an authorized Chrome surface and root is operating it through CUA. Use `I4-CUA-browser-checklist.md` and the task-only54551 proxy for that acceptance. Standalone Playwright remains **unauthorized and unexecuted**; the script below is a prepared reference, not the chosen browser execution route.

After that authorization, the coordinator may run an already-installed Playwright runtime with:

```powershell
node validation/claude-weekly-quota/I4-ui-acceptance.mjs --execute-authorized --receipt C:\absolute\path\environment-I4-<current>-active.json --out C:\absolute\path\private-browser-results
```

An existing runtime can be supplied with `--playwright-module C:\absolute\path\playwright\index.mjs`. Do not install a browser or package as part of script preparation. The script accepts only a synthetic/no-egress receipt, matching private fixture and loopback application. It reads synthetic credentials privately, never prints them, never writes storage state, HAR, trace or full-page HTML, and blocks external HTTP requests. Only isolated quota-card crops and a sanitized per-case receipt are saved. Do not enable general Playwright protocol/network debug logging.

The environment owner must provide a **fresh dedicated C** in the private fixture: `C` positive user ID distinct from A/B, `CEmail`, shared synthetic `Password`, optional `CGroup` (otherwise `Group`). C must start with GPT concurrency5, inherited Claude default3, no override and no cards/history in that group. C is never created or given a password by this script. Do not rerun a partly completed management case on the same C; prepare another isolated C rather than deleting audit facts.

Real page/API cases cover immediate login→Keys profile bootstrap, server default3/A override2/GPT5, B inherited3, A100/B300 canonical cards, two Keys sharing one card, GPT's own projection, admin display/keyboard focus/Escape, English→Chinese and390px layout. Quota and policy values are assertions from the agreed synthetic fixture; rerun only against a freshly coherent fixture after concurrent load tests have ended.

Two additional cases perform **real isolated management writes only for C**: UI updates Claude override3→4 then actual GET proves GPT5 unchanged; UI opens100, drops its already-committed real HTTP response, retries through the UI with the same key/payload and verifies replay returns the same card, then schedules renewal75. Actual API responses and final GET verify exact amounts, both168h intervals, original100 still current and75 queued at its end. No A/B policy/card is changed, no model is called and no wallet consumption is generated. Receipt flags distinguish these real writes from the read-only cases. These management facts complement the separate actual generation/settlement matrix; they do not replace it.

Slow preview A→B (late success and error/finally), additional exact retry operation identity,409 draft preservation and held server-clock expiry use **mocked browser HTTP responses** with real embedded DOM. The A/B mutation cases are intercepted and aborted/conflicted. These scenarios are UI evidence, never actual server settlement evidence. Actual Router/Redis/PG/image financial assertions remain in the independent backend receipts.

The script has not been browser-tested, so selectors and candidate prerequisites must be checked by the authorized operator. Do not silently skip a case, reinterpret a script failure as pass, or use an old candidate receipt. No production endpoint, external model or unsupported Live provider is allowed.
