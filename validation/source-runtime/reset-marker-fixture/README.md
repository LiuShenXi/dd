# Historical reset-marker fixture

This importer creates one isolated, synthetic read-only UI fixture in the already sanitized local PostgreSQL copy. It never targets production, scope 1, copied users, or previously accepted synthetic rows.

The fixture uses test-only scope `2147480914`, which is inside the signed 32-bit advisory-lock domain. It creates a new synthetic user and carpool group, one active 28-day three-seat term, four seven-day cycles, two completed historical reset batches, two successful targets, matching qualifications, and an append-only ledger. The first reset restores a deliberately depleted `$163` to the immutable `$700` base target. The second target succeeds with a zero grant, so both UI markers are visible while the final spendable quota remains `$700`.

This is historical fixture rendering only. It does not prove scheduler execution, qualification detection, announcement publication, or due-time behavior. Those remain covered by real PostgreSQL execution tests. Because product boost APIs intentionally resolve business scope 1 and this fixture uses a non-1 test scope, this identity is unsuitable for boost acceptance.

`import-reset-marker-fixture.ps1` is approval-gated and create-only. Without `-Apply` it builds/tests the importer and leaves the database unchanged. Before `-Apply`, the director must capture the preservation baseline exactly once with `check-preserved-synthetic.ps1 -CaptureBaseline` after the reviewed runtime rebuild. The apply path verifies copied-record, copied-announcement, and accepted synthetic-state conservation before and after the transaction. Any existing attempt/result manifest, database marker, email, group, scope, operation, or ledger key refuses replay.

The result manifest contains the synthetic login password and remains only under the private runtime directory. Do not copy it into Git, evidence, logs, screenshots, prompts, or reports.
