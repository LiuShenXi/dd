# Production UI acceptance correction — 2026-10-20

The user accepts the existing management entry and requires a working UI flow,
personally exercised on the administrator account, before delivery. No agents.

Observed: Users → More → Manage Claude card has an empty group selector. There
is no term group and production issue/admission flags are disabled. A fresh
subscription group with the existing Claude upstream is being prepared through
the actual Groups UI; existing customer groups and balances are not converted.

Change boundary: enable only that new group through the existing deployment
configuration, complete its existing guarded UI conversion, then issue a small
administrator test card through the UI and verify subscriptions/key usage.
Document the actual clicks and results in the project and local knowledge base.
No new management page or financial-model refactor.

The next blue-green switch exposed a controller defect: switch() rejects an old
completed retirement record even when the slot now contains a new verified
process. Fix only host-release.py and its regression tests: permit a recorded,
stopped predecessor with a different container ID in a preserve-mode slot;
keep foreign/incomplete/identical-process retirement guards. Application image
and database schema are unchanged. Test both the second full release and denial
of incomplete/unrecorded/same-container replacements before host installation.
