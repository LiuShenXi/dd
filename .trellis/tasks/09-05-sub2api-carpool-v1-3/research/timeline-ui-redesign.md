# Timeline UI Revision, 2026-09-06

## Latest User Scope

The user rejected the red numbered reset markers and detached timestamp list. The red annotation in their screenshot was only an attention marker, not visual authority. Their newest clarification limits the change strictly to the membership timeline widget: progress rail, reset nodes, timestamps and refill copy. Do not redesign the whole page, statistic cards, header/sidebar, cycle detail list, reset-window section or other routes.

The immediately preceding instruction moves all reset timestamps ABOVE the rail. This supersedes all prior below-rail visual requirements, but not the historical evidence of what previously shipped. No business/API/database behavior changes.

## Design Direction

Subject: a member inspecting the passage of their four-week API allowance term and real successful refills. Job: understand when a refill happened and its target without confusing time progress with quota consumption.

Palette: existing teal accent #0D9488, quiet success #0F766E, ink #1F2937, secondary #64748B, line #DCE5E5, near-white #F8FAFC. Retain existing dark-mode tokens. No red/pink, gradients, numbered badges, decorative framed labels or new page backgrounds.

Type: inherit the existing Chinese/system sans for labels, 12-14px with restrained semibold; tabular figures for date/time and quota. No extra fonts, viewport-scaled type, negative tracking or oversized heading.

Layout: replace the heavy 40px colored blocks with a refined slim segmented time rail and discrete meaningful cycle labels. Actual reset instants remain precisely located on that rail using subtle teal success dots/checks. Above the rail, keep each reset timestamp and `Filled to $700` copy together as one unframed annotation. Associate it visually with the actual node using fine neutral/teal leaders. Nearby events must be collision-managed, not assigned to arbitrary 50%-width timestamp columns. Both fixture resets should be readable simultaneously on desktop and390px without hover. Dense history can group nearby annotations coherently; never drop events or invent dates, and keep the entire annotation region above the rail.

Signature: restrained data-annotation leaders joining real time instants to readable paired date/refill labels. The precision is the design, not decorative UI. Prefer grouped annotations if independent connectors would cross text; no unbounded row height or page overflow.

Self-critique: a generic three-stat-card redesign would exceed the request and is rejected. A red badge cleanup alone would leave the detached information problem and is also insufficient. Rebuild this widget only, keep surrounding markup/functionality unchanged, and judge the actual fixture screenshot rather than claiming aesthetics from passing tests.

## Ownership And Gates

- timeline_ui implementer: `frontend/src/views/user/CarpoolDetailsView.vue` timeline-only markup/presentation; one dedicated timeline component/helper only if meaningful for layout complexity; focused tests. Reuse existing localization keys/icons; no locale files unless explicitly necessary and coordinated. Preserve store/API/clock/boundary behavior.
- Director: this design/PRD/architecture synchronization, source/image gates, preserved-runtime rebuild and browser visual review. Reviewer: narrow diff/functionality check after implementation.
- Workers use gpt-5.6-sol/high. Preserve all existing changes. No production, database writes, fixture import, boosts, clock changes, unrelated ports, commit or push.
- Validate two close reset events, zero/no events, first/last edge, dense histories, long amounts, English/Chinese, dark mode,390px and desktop, and legacy five-cycle width. All dates above; no intersecting labels or unwanted overflow. Run typecheck, affected lint/tests, then build/full frontend gate before local image refresh. Preserve current image/data until replacement is ready.
