# Director UI Acceptance

Final local acceptance is recorded in director-final-acceptance.md; earlier pending statements below describe their historical capture point. All browser interactions use synthetic identities; no copied-user rows or credentials are captured.

## V3 Restricted User Review

SOURCE granted restricted SAFEUI for the fresh HTTP three identity, including its own boost and popup read mark. A separate narrow approval allowed dismissing the already-logged-in old synthetic three popup solely to log out. Reset-suite identities and administrator/global/reset mutations were not touched. The director then logged in with the fresh V3 HTTP three fixture and forced a full dashboard navigation; the document script was /assets/index-Dy77EOSL.js, matching the V3 frontend build.

- Desktop dashboard rendered at 3440x1271 and 1440x1000. Ordinary balance remained zero. Actual boost click changed remaining from 2/2 to 1/2, amount70USD, cumulative usage2USD unchanged.
- Desktop details at1440x1000: document width1440; time-rail segment widths257.594/257.594/257.609/257.594/73.609px, matching7:7:7:7:2 with subpixel rounding. Segment text is only1..5; aria labels contain cycle/status and no quota; no title attribute is present.
- Menu order is dashboard, keys, usage, subscriptions, carpool, redeem, profile. Mobile menu opened and navigated to details successfully.
- Details at390x844: document width390; rail widths79.797/79.797/79.797/79.797/22.797px. Full-page screenshot has no overlapping text or horizontal page overflow. Period dates are identical before and after boost; reset remains scheduled with absolute Shanghai timestamp and countdown.
- Mobile dashboard at390x844 exposed a real defect:133px balance-card inner width retained a92px nonshrinking button, compressing Chinese boost copy into a vertical single-character column. This screenshot is diagnostic, not a visual pass.

Screenshots in this directory:

- director-v3-user-dashboard-desktop.png
- director-v3-user-dashboard-1440-after-boost.png
- director-v3-user-details-desktop.png
- director-v3-user-dashboard-390.png
- director-v3-user-details-390.png

## V4 Build And Real-Page Review

Frontend corrected only the boost row wrapping/text minimum/action alignment in UserDashboardStats.vue, with a focused layout test. Director reviewed the actual code. Fresh full typecheck, full lint, frontend production build and265files/1903tests passed. Whole backend build also passed; backend source is unchanged from V3.

Source identity before and after verification is E27EC3B41A1DDC3FE78520B33E93CA811821C9F5D8DCEA72D81E2F8F1B2812D4. Dist identity is2B88660EFE8991B65B380713D96DDBFCC9E67BE523080393BB27563EFCED9508. director-code-ready-final-image-v4.json was finalized before submission and passed Assert-DirectorCodeReady with6CA4C8B23F69CD87A32B9AFE667A1D8F1DDAC80247D69C240DC5183E5258D490. It authorizes source-owned local start only, not final acceptance.

SOURCE rebuilt V4 image sha256:0ad4aa72ab397139643253a22f8b1643f7593edc8fef9624a0db6ac9a3408ef8 and granted FULL SAFEUI after HTTP67/67, strictWS11/11 and coherent pending_merge reset22/22. The director independently reviewed the actual reports and their matching image/approval/config/environment identities, isolation and conservation results. V4 is not final acceptance because the following real-page review found remaining fixed-enum localization omissions.

### User UI

- Final-image script /assets/index-BJ4cz2Uy.js was verified in the browser. At390x844, boost copy is133x50px and ends at y237; the92x36px action starts at y245. The corrected copy is horizontal and does not overlap the action. The screenshot was actually viewed.
- Synthetic three122 clicked boost once: remaining2/2 to1/2, grant70USD, ordinary balance0 unchanged. Touch help fits390px and explains30days, two claims,70USD and current-cycle expiry.
- Details screenshots at1440x1000 and390x844 were visually inspected. Mobile page width382 within390viewport; the rail is approximately77.93px four times and22.28px, matching7:7:7:7:2. Segments contain only1..5 with cycle/status aria text, no quota/title. Period remains09/06 02:48 to10/06 02:48, cumulative usage2USD, reset count0.
- Synthetic reset member136 saw only the synthetic announcement10 popup with both the relative phrase for tonight and absolute2026-09-06 22:00:00 Shanghai date, then marked its own announcement read. No original announcement bodies were captured.

Screenshots: director-v4-user-announcement.png, director-v4-user-dashboard-390.png, director-v4-user-boost-help-390.png, director-v4-user-details-390.png and director-v4-user-details-desktop.png.

### Administrator UI

- All six tabs loaded. User filter2147483647 showed a proper empty state, not a blank-page crash. Filter122 showed one active term and five cycles; current total768USD =700 initial +70 boost -2 actual usage. Ledger shows one initial grant, two1USD usage debits and one70USD boost. Exceptions filtered122 are empty.
- Renewal and payment dialogs opened without financial submits. Reset page shows preserved batch1, scheduled2026/09/06 22:00:00; observer list is empty. Qualification form opens with the confirmation unchecked and submit disabled. No scan, qualification, review or execution action was submitted.
- At390px, reset document/body/main are390px. The980px and760px tables scroll within358px overflow-auto containers, not the page. Reset/plans screenshots were viewed and show no overlapping controls. All three plans display cycle5 targets157/200/314 and boosts55/70/110 twice.
- Users entry was reached through Open carpool. Before any table screenshot, the list was filtered to the authorized synthetic ordinary127 email. Its More menu opens CarpoolTermModal. Desktop and390px modal screenshots were inspected; long synthetic identity wraps inside312px heading, document/dialog remain390px.
- Read-only opening preview returned five7/7/7/7/2day periods and correct four-seat550/550/550/550/157 quotas. After SOURCE verified exact synthetic group17/current plan8, director submitted exactly one new term with starts_at:null, no payment, no takeover or transfer. Initial post-success GET refresh timed out; a single read-only Retry recovered the view. The Open command was NOT retried. Diagnostic screenshot retained, request-log cause pending source inspection.
- Success view shows Four-seat active, cycle1 available550USD, boosts2/2, CNY0. Read-only renewal preview begins at the existing expiry and has all five cycles scheduled; no renewal submitted. Refreshed Users balance remains100USD.
- SOURCE read-only DB projection confirms exactly one term94, group17, plan8; start2026-09-05T21:07:38.418244Z, expiry2026-10-05T21:07:38.418244Z; active cycle466/base550, cycles467-470 scheduled with zero balances, cycle5 target157; one550USD ledger grant and zero payment rows. Key64/group17/status active/full-row hash15c8d3b5b03c17a5f5e90ee9c1cca47c and ordinary balance100 are unchanged. These numeric projections contain no key value or copied-user record.

Screenshots: director-v4-admin-empty.png, director-v4-admin-terms-desktop.png, director-v4-admin-cycles-desktop.png, director-v4-admin-ledger-desktop.png, director-v4-admin-reset-desktop.png, director-v4-admin-reset-390.png, director-v4-admin-plans-390.png, director-v4-admin-open-preview-desktop.png, director-v4-admin-open-preview-390.png, director-v4-admin-open-reload-error.png, director-v4-admin-open-success.png and director-v4-admin-renew-preview.png.

### V5 Follow-Through Required

Actual V4 ledger and preview screenshots expose raw fixed enums: cycle_initial/usage/boost, base/boost and grant/scheduled. The existing frontend owner is narrowly correcting their Chinese/English presentation and unknown-value fallback, without changing payloads, ledger reasons/references, stored plan/group names or business behavior. These diagnostic images do not accept that future correction. Fresh full frontend checks/build, immutable approval/rebuild and final-image screenshots are required.

V4 UI interactions are complete and the viewport override was reset. Browser is paused on /admin/carpool filtered127. SOURCE owns the post-UI conservation checks. Preserve batch1/announcement10 and original failed first-publication evidence; no runtime resets or rebaselines.

### V4 Post-UI Read-Only Review

Director inspected source-runtime-v4-post-ui-20260905T211421967Z.json itself: exact V4 image/approval/config/environment, isolation passed, hosthealth200, both copied conservation checks passed, copied-user terms0. It confirms one term94/grant550 with zero payment rows, ordinary100 unchanged and key64 full-row hash unchanged. Batch1 remains scheduled2026-09-06 22:00:00 Shanghai, revision0, targets0, reset ledger0, null effective/completed times; two qualification rows are preserved. Announcement10 remains active/sourcebatch1/qualification/revision0.

The source's structured application-log projection proves opening201 in12ms and immediate plans/terms GET200 in1ms/4ms. The initial groups/all request is absent from application access logs; the read-only retry's groups/all returns200 in4ms. No retained browser trace identifies the failing client dependency. A read-only bridge review found no demonstrated defect and no capacity saturation, but its connection failures are not traced. Do not assert a specific browser, bridge or server cause, and do not classify creation as failed. No speculative source/runtime fix was made.

## V5 Verification Started

Director inspected the six changed frontend files and source-derived mapping sets:11 ledger events,3 buckets and4 preview actions. Both actual Chinese and English dictionaries are exercised through exact rendered-cell assertions with unknown fallback and unchanged free-form audit text. The former raw bindings are replaced only at presentation helpers; request payloads and backend are unchanged. Frontend owner froze after focused20/20 tests and affected lint passed. Fresh director full typecheck/lint/tests/build started from sourceB696574939DB9A389525041B17FF5B466EAFF3BB87346938D1026BF20A292117; completion and rebuilt-image UI remain pending.

Director verification completed: full typecheck/lint/build exit0, full frontend265files/1907tests passed, whole backend build exit0. The first quiet backend build produced no Tee output file; a second successful build explicitly recorded the command and exit0 without changing source. Source hash remainedB696574939DB9A389525041B17FF5B466EAFF3BB87346938D1026BF20A292117 and dist isF8466F2E1B567CEE6A27ACCFA1EAD6087D131C9138CB69693152FDC9897D50AC. Immutable director-code-ready-final-image-v5.json passed Assert-DirectorCodeReady with SHA2562F8841CEB2659B384ABAF5642F56A725B26B35986AD556115ACFD4290FC8A422 and was submitted to SOURCE for local-only preservation-safe rebuild. It is not final acceptance. Existing build chunk-size/test-stub warnings remain, with no failed assertions.

## V5 UI Complete

Director viewed the actual V5 administrator ledger and renewal-preview screenshots at1440x1000 and390x844. Ledger122 displays localized event/bucket values, including boost claim, actual usage and initial cycle grant; audit reasons/references remain unchanged. The1120px ledger table scrolls inside358px, while document/main remain390px. Renewal preview for synthetic127/term94 starts at existing expiry and displays five scheduled periods with550/550/550/550/157 quotas and localized actions. Its352.625px table scrolls inside332px; the long heading fits312px. No renewal was submitted. Desktop preview captures the populated rows; mobile preview captures the upper dialog and bounded table container, not all below-fold rows.

Screenshots actually inspected: director-v5-admin-ledger-desktop.png, director-v5-admin-ledger-390.png, director-v5-admin-renew-preview-desktop.png and director-v5-admin-renew-preview-390.png.

After interruption the login page initially returned an empty DOM snapshot; a later current-viewport screenshot and snapshot showed the normal login form. No application defect was established. Using the existing synthetic122 identity, director completed read-only dashboard/details checks against /assets/index-BWKyQRnz.js. Ordinary balance0, usage2USD, boost remaining1/2 and reset count0 are unchanged. The period remains09/06 02:48 to10/06 02:48, with confirmed reset scheduled09/06 22:00:00 Shanghai.

Desktop and390px screenshots were actually viewed: director-v5-user-dashboard-desktop.png, director-v5-user-dashboard-390.png, director-v5-user-details-desktop.png and director-v5-user-details-390.png. At390, document/main width382; boost text wraps horizontally above its action without overlap. Time-rail segments measure approximately77.93px four times and22.28px, matching7:7:7:7:2; their text is only1..5, aria labels contain cycle/status, and no title/quota appears. Details full-page screenshot verifies all five dates and reset section without overlap.

V5 browser actions were login/logout, navigation and read-only previews only: no boost, opening, financial, reset or mark-read mutation. Viewport was reset and browser paused on /carpool. SOURCE received definitive UI complete for final conservation.

Four additive fake-clock cases in CarpoolDetailsView.spec.ts exercise actual Pinia/API behavior for reset-zero authoritative refetch without optimistic accounting, cycle expiry refetch, absent qualification and uncovered-term eligibility. Director read the actual assertions and full frontend log director-final-additive-frontend-full-tests.log:265files/1911tests passed. These are nonbundled test additions, not a new application image.

### V5 Post-UI Conservation

Director read source-runtime-v5-post-ui-20260906T001713694Z.json: exact V5 image/approval/config/environment/internal network, isolation passed, loopback-only bridge76160(parent30284), health200, both copied-record and copied-announcement conservation passed, copied-user terms0. User127 ordinary100 and key64 full-row hash15c8d3b5b03c17a5f5e90ee9c1cca47c remain unchanged. Term94 has five cycles, boost_used0, exactly one550USD grant, fifth target157 and zero payments. Its30day period is unchanged.

Batch1 remains scheduled2026-09-06 22:00:00 Shanghai/revision0, with null effective/completed times, targets0 and reset ledger0. The expected third qualification came from V5 pending_merge; the original two rows retain hash59f83135491c98b8ae0af5731cb349af. Announcement10 remains active/qualification/sourcebatch1/revision0 with entire-row hash27b5704aa81c78082531f4dc45aeeaea. The safety report correctly keeps final_feature_acceptance:false while separate lifecycle PG assertions are unfinished; it is not rewritten later.
