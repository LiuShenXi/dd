# I4 F4 native API Key concurrency projection fix

Status: F4 local, actual candidate and browser checks accepted by the coordinator.

The real browser found that A's Key returned A's correct card but inherited
Claude default3 instead of A's override2. The native `GetByKeyForAuth` query
explicitly selected user fields without selecting
`user.FieldClaudeSubscriptionConcurrency`. The existing entity mapper and both
snapshot directions carried the field correctly, but could only copy nil from
that partial query. This was not cross-session B identity leakage. Final
generation policy resolves the limit from the database and was not changed.

Only two business files changed from F3:

- `api_key_repo.go` selects that one additional user column.
- `api_key_auth_cache_impl.go` uses version28 so existing faulty v27 snapshots
  cannot remain authoritative. Unsupported L2 snapshots are rejected before
  asynchronous L1 promotion; the existing L1/apply version check remains.

The new v27 materialized-cache test initially exposed an existing cache-order
detail: promoting an old L2 entry and immediately enqueuing its replacement
could leave the old entry inside Ristretto's asynchronous admission, even though
the returned request was correct. The failed unit JSONL is retained as
`I4-F4-key-projection-windows-debug-final.jsonl`; the narrow pre-promotion guard
fixes this without changing valid/negative-cache or GPT admission policies.

The limited APIKey.User construction audit found four native user loading sites:

- GetByID and ListByGroupID use full WithUser loading.
- GetByKey's WithUser callback only limits the AllowedGroups child edge; it does
  not restrict user columns.
- GetByKeyForAuth has the only restricted user-column Select and was fixed.

All these paths use `apiKeyEntityToService` and `userEntityToService`. Other Key
list/search operations do not materialize a User edge and are not alternative
authentication loaders. Snapshot creation, snapshot materialization and API Key
middleware AuthSubject copies already include the override. No additional
partial APIKey.User projection with this omission was found.

Regression coverage keeps the previous tests and adds:

- Real native SQLite GetByID/GetByKey/GetByKeyForAuth with override2 and NULL,
  preserving original GPT concurrency5.
- Rejection and authoritative reload of materialized v27 L1 and L2 entries.
- Real PostgreSQL native repository plus real Redis: cold native calls, proven
  L1 hits bypassing both native and Redis reads, a new service's L2 hits without
  native reads, and v27 L2 replacement with version28. A's two Keys remain2;
  B retains NULL and effective default3. Full user-row fingerprints stay equal.
- The environment owner's actual `/v1/usage` assertions for A/A2/GPT-Key ownerA
  and ownerB, preserving full prior wallet/card/window/provider guards.

Debug checks after the fix passed44top-level/71including subtests on Windows.
The first native PG/Redis diagnostic passed1top-level/5including subtests; those
debug runs are not final candidate evidence.

Stable F4 final checks completed on the same frozen source:

- `I4-windows-F4-20261019T233536Z-receipt.json`:532top-level/1005including
  subtests passed; inventory532 exactly matches, zero failed/individual-skipped
  tests, one no-test helper package skip. Original GPT84 identities are all
  present. All-repository unit compilation and selected package vet exited0.
- `I4-F4-native-projection-20261019T233536Z-receipt.json`:1top-level/5including
  subtests passed, zero failed/skipped tests, exit0. Original CI=true TestMain,
  actual PostgreSQL18.1 and real Redis are used. Before/after source is stable.

The environment owner's actual F4 server `/v1/usage` assertions and the
coordinator's real-browser rerun have also passed: A/A2/GPT-Key ownerA display2;
B remains NULL/effective3; original GPT5, card and wallet/provider guards stay
unchanged. See `I4-F4-environment-evidence-index.json` and
`coordinator-I4-review.md`. Unit/native tests are not presented as browser tests.

F4 complete-source hash:
`9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`.
The75business-file manifest aggregate is
`afcd0c9e634cc79db164b12104172b0cd059c59c904e870390f6db61b3cab33c`.
`I4-F3-to-F4-key-projection-diff.json` records exactly six changed source files,
including test changes. Frontend/embed/provider/schema are unchanged. Historical
F2/F3 results remain labeled with their original sources and do not count as
passing the new F4 image. The accepted I4 now proceeds to commit/push only;
I5/I6 and production deployment remain outside this session's scope.
