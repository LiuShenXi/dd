# I4 coordinator acceptance — 2026-10-20

I4 is accepted for a reviewable commit and remote feature-branch push. The latest
user direction stops this round after I4 so work can continue at the office.
I5, I6, production migration, deployment, real-user issuance and paid/model
consumption are not performed or accepted by this receipt.

## Accepted behavior

Manual per-user Claude cards, independent current/queued quotas, shared Keys,
full168h windows, immutable admission/billing ownership and independent Claude
concurrency are implemented. Claude defaults to3, supports positive per-user
overrides, and leaves GPT configuration/pool behavior intact. Actual browser
editing changed synthetic C from default3 to4 while original concurrency stayed5.
Actual open100 and queued renewal75 each created exactly one term and operation,
with contiguous168h windows and no consumption. A real lost successful response
caused Chrome transport replay; a separate real committed renewal followed by
proxy503 was manually retried with identical body/idempotency hashes and the
same term6 (201/replayedfalse to200/replayedtrue). The before-upstream test409
preserved draft25/notes, invalidated preview, disabled confirmation, performed
zero upstream writes and preserved all C card/operation/parent/user-money hashes.

Browser acceptance used the user's connected Chrome through CUA, not the prepared
standalone Playwright script. It covered late cross-user preview success/error,
two Claude Keys, GPT Key, CN/EN narrow layout, keyboard focus/Escape, default and
override displays, manual card operations and expiry. At real D expiry, amounts
13/0/0/13 plus queued17 remained unchanged during held refresh and30s timeout.
After releasing the gate, actual Retry obtained a new server projection17/0/0/17.
The original timed-out response is not claimed as successfully applied. D is an
explicitly seeded synthetic historical fixture: two full168h terms, no shortened
or mutated issued card. Local model-provider calls were not made by these UI steps;
the existing frontend may load its third-party static SDK assets.

## Final source and actual verification

Final F4 complete source: `9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`.
Image: `sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9`.
Browser found native Key authentication omitted the user's Claude override.
F4 selects that column and invalidates old auth snapshots using version28,
rejecting old L2 entries before asynchronous L1 promotion. Final real browser
A/A2/GPT-ownerA now display2; B inherits3, with GPT5 intact.

- F4 Windows532top/1005including subtests passed, preserving all84 GPT identities;
  compilation/vet passed. One empty helper package skip is not an individual test.
- F4 Linux critical unit race512top/968including subtests passed, zero failures/skips.
- F4 native PostgreSQL/Redis projection1top/5all and actual HTTP metadata1top/15all
  passed, including cold/L1/L2 and stalev27 replacement.
- Unchanged frontend typecheck/lint/build and2440component tests passed.
- Prior F2/F3 actual wallet matrix, six Claude aliases JSON/SSE, two-instance
  independent capacities and waiting queues, boundary/Redis-fault cancellation,
  original-card late debit, GPT WS bridge and973.0817914s SSE with62lease samples
  retain their original source/image labels. The F4 delta is two auth-projection
  business files plus tests; lease/billing/frontend/provider/schema paths are
  unchanged. These prior results are deliberately reused for those paths, not
  misrepresented as full F4 reruns. Live403/provider0 proves refusal only.

Primary evidence: `I4-cua-browser-20261019T2303Z/receipt.json`,
`I4-browser-independent-evidence-review.md`, `I4-browser-C-independent-verification.json`,
`I4-browser-C-final-observed-20261020T000024Z.json`, F4 receipts/index,
`coordinator-I4-T68-review.json` and the source-difference manifests.
Some screenshot filenames captured a neighboring asynchronous state. Their names
are not acceptance evidence. Reviewed reliable screenshots include the C409 draft
view and D expiry/refresh views; actual DOM, proxy and SQL observations are primary.
Failed/diagnostic runs remain retained and clearly labeled.

## Limits and next work

The74-case matrix remains scoped by iteration and evidence; it is not a full
release-pass claim. I5 immutable paid-order snapshots/fulfillment/refund recovery
and I6 controlled conversion, operational metrics, pool activation, scale/performance
and final state-preserving rollback/deploy remain pending. PostgreSQL18.6 archive
provenance is verified, but the required production-scale migration rehearsal has
not run. No production state was changed. Recheck live production before any later
release, preserve custom GPT behavior, and do not deploy merely because I4 is accepted.
