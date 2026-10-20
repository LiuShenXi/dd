# I6 obligations retained after I4 freeze

These are pending, not passed. I4's local tests and historical I0–I3 evidence do
not waive I5 paid orders or this final deployment gate.

- Add controlled-scope operation counters for Claude acquire, queue entry and
  promotion, renew failure/lost lease, cleanup outcome and cancellation safety
  deadline. Reuse existing in-process snapshot/structured-log conventions; no
  user/request IDs as metric labels and no change to original GPT metric meaning.
  Financial subscription metrics and instantaneous active/wait stats are not
  substitutes. Final trace map must point to actual counter assertions and the
  exposed snapshot/log evidence.
- Implement and exercise the actual maintenance/preview/conversion management
  path. Conversion uses a stable, approved user/quota set, ordered scope/parent/
  term locks, final group recheck, durable operation and cache outbox atomically.
  All unfinished protected markers include queue waiting and asynchronous usage;
  no test-authored UPDATE may stand in for the real conversion method.
- T72 final image: old cached windows group with DBterm performs zero legacy
  user acquire/wait actions; pending pre-slot marker blocks real conversion;
  proven no-send cancellation permits drain; attempted unknown stays held.
  All replica capabilities, independent default3 and lease-protocol version must
  agree before activation; reject mixed configuration. Preserve GPT active/WS
  state during group conversion.
- Preserve the preexisting startup cleanup behavior. Synthetic two-instance
  tests start both instances before injecting GPT traffic. Production's actual
  single-instance replacement uses complete drain before the new app starts;
  starting another old-cleanup instance during active GPT is not proof of
  unchanged slots.
- Repeat final runtime HTTP/JSON/SSE/WS and real-browser gates on the agreed
  final image. Include >15minute SSE heartbeat/lease ownership, client/lease-loss
  actual upstream cancellation, account capacity limits and 5+2 independent
  synthetic pools. Test values5/2 are not the production default.
- High-cardinality performance must include cards with substantial ledger
  histories (Apply currently sums the card ledger) and a large settled request
  history (backlog currently uses full-table count filters). Measure scope lock
  time/throughput and GPT impact before claiming low overhead; change scanning
  only if evidence justifies it and rerun monetary invariants.
- Build and retain R0→R1→R2 and R2→compatibleR1 actual rollback evidence after
  I5. The final rollback must recognize the current receipt/scope and continue
  known usage plus already-paid fulfillment with new sales/admission disabled.
  No down-migration, ledger reset, database restore over new money, or term→wallet
  fallback. I3's R1 is historical and is not the final paid-order rollback image.
- The latest user instruction limits this session to accepted I4 commit/push;
  do not implement I5/I6 or deploy production in this session. Later production
  work must follow the completed gates and then-current user scope. No recipients
  or amounts were specified, so do not automatically convert groups, open real
  cards or consume an existing customer Key. Push only the dedicated branch,
  without force-pushing main.
