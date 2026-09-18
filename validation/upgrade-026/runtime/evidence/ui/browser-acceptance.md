# Isolated candidate browser acceptance

Date: 2026-09-19 (Asia/Shanghai). Candidate source: `8c3275d597cdcf381a30dee87fe0e2c9fa2773ef`; served at `http://127.0.0.1:38626`. Testing started only after the runtime owner reported the real old-binary rollback complete and the candidate restored. No production tab, endpoint, credential or data was used. Existing synthetic administrator and active/ordinary user fixtures were read from private local files without printing passwords or API keys.

## Completed browser behavior checks

| Area | Observed result |
|---|---|
| Version / brand | Admin sidebar shows v0.2.6; home, login, app title and navigation show 水上列车. The retained generic upstream onboarding text still names Sub2API. |
| Ticket toggle | Initially OFF. Clicked ON, saved using the real UI, reloaded and observed ON. Clicked OFF, saved, reloaded and observed OFF. No server restart occurred. |
| Proxy masking | Field loads `socks5h://synthetic:%2A%2A%2A@ticket-proxy.invalid:1080`, with the password-hidden status. Saving the settings while switching tickets preserves the masked proxy value. The input remains editable when the feature is OFF, allowing configuration before enablement. No real credential was entered or shown. |
| Admin carpool | Real loaded list contains active quota 550, future quota 0, expired quota 0 with the correct status labels; active cycle expands to 550 and its scheduled period. Existing probe fixtures were also present. |
| Active user | Real login shows header and dashboard 550 carpool quota, with independent-balance wording; the underlying synthetic ordinary balance 123 is absent. Contract details show fixed term expiry, next natural refill, current period, 2/2 boosts on the dashboard and zero usage. |
| Assigned Key | Edit dialog shows carpool group as text with no group selection control. Other editable fields remain available. Dialog canceled without changing the Key. |
| Self-service Key creation | Provider selector is present; expanding the group selector offers the synthetic standard group only, excluding carpool. Dialog canceled, no new credential created. |
| Mobile navigation / identity | At requested 390×844, API Keys changes to cards. Mobile menu reaches carpool details, user menu shows 550, logout and ordinary login work. Ordinary dashboard shows 77 and no residual 550. |
| Ordinary user | Desktop and narrow dashboard show 77 ordinary balance, no stale carpool quota, and disabled boost with 未开通拼车月卡. |
| Redemption history | Ordinary user sees 3 real seeded redemption entries; total 3, previous/next disabled for a single page. Changed page size 20→50 successfully. No new redemption submitted. Multi-page HTTP cases were covered separately by the runtime worker; this UI fixture does not require a second page. |

## Layout evidence and limitations

Read-only DOM geometry measured the rendered local UI through the documented browser API. `document.documentElement.scrollWidth` equals `clientWidth` after layout settled:

- Requested desktop 1280×900: active dashboard and ordinary dashboard 1272 / 1272 (vertical scrollbar accounted for).
- Requested narrow 390×844: Keys 390 / 390; carpool details, ordinary dashboard and redemption 382 / 382.
- An immediate post-resize measurement of Keys briefly read 451 / 382 while changing from table to mobile cards. The settled measurement was 390 / 390, and no visible element exceeded the viewport. It was not treated as a persistent overflow defect.

No candidate product error was observed in the captured browser console. Four error entries were from the existing `chrome-extension://.../content_main.js`, reporting `postMessage` target origin `null`; these are recorded in `browser-console-errors.json` and are not attributed to application code.

Screenshots could not be captured: the documented browser screenshot API repeatedly timed out waiting for `Page.captureScreenshot` at 5 seconds, including after using the documented recovery guidance and setting the desktop viewport. Further retries stopped. Thus behavior, accessibility state and DOM geometry were accepted in this scope; pixel-level appearance, clipping/overlap and screenshot evidence remain unverified. A few CDP click/evaluate operations also timed out; the current page state was inspected before retrying, and completion was confirmed from the actual UI. No lower-level or alternate browser automation was used.

Evidence files here contain synthetic page identities and masked Key fragments only:

- `active-carpool-desktop.ax.txt`
- `active-carpool-390.ax.txt`
- `carpool-key-readonly.ax.txt`
- `ordinary-dashboard-390.ax.txt`
- `ordinary-redemption-390.ax.txt`
- `ordinary-dashboard-desktop.ax.txt`
- `browser-console-errors.json`

## Final state and untested scope

Ticket switch was restored OFF and verified after reload. Site type was never changed. Temporary viewport override was reset. The local preview remains on the synthetic ordinary user's dashboard. No account, Key, payment, boost, quota or contract was created/changed by this browser pass; only the reversible ticket toggle was saved ON then restored OFF, and the redemption page-size UI was changed.

Not covered by this browser pass: live ticket harvesting/injection, real provider generation, real payment/refund, all vendor admin forms, three-site-mode switching, future/expired user logins, account export redaction, or all WebSocket scenarios. Source, unit, HTTP/WS and isolated SQL evidence are separate layers and do not expand this browser acceptance scope. Production remains unchanged.

Repository copies of the accessibility captures normalize trailing line whitespace only; original captures remain in the external UI evidence directory.
