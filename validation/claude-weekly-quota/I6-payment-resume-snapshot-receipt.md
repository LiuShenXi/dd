# I6 signed payment-result entitlement correction

Backend correction verified 2026-10-20 02:30 UTC. Actual F2 browser checkout
redirected to `/payment/result?resume_token=...`; the page showed payment/balance
amounts without frozen rights. Authenticated order DTOs had the entitlement,
but the signed resume-token `PublicOrderResult` did not. This was a missing
cross-layer response contract, not missing order data.

## Product scope

Only `backend/internal/handler/payment_handler.go` changes in this worker's
product patch. `PublicOrderResult` gains
`SubscriptionQuotaSnapshot *domain.SubscriptionQuotaOrderSnapshot` with JSON
name `subscription_quota_snapshot`; its builder calls the existing strict
`service.PaymentOrderQuotaSnapshot(order)`. Formatting aligns the existing
fields; the underlying additional behavior is one field and one assignment.

The signed token still passes the existing signature and order/identity checks.
Valid frozen data is projected as the typed allowlist; legacy, malformed,
future-version or unknown-field snapshots yield null. No raw provider/order map
is forwarded. The anonymous `PublicOrderVerifyResult` and its builder remain
unchanged: public order numbers do not grant access to entitlements, amounts,
plan/user IDs or provider data. No billing, payment provider, migration, lease,
quota amount or token-validation behavior was changed.

Frozen production-file SHA-256:
`607a8da93be72c4070ec2f13114ef8ea1d4d76b3090a71cc2469b65c7454bdeb`.
The frontend owner separately owns result-page type and label corrections.

## Actual verification

`payment_handler_resume_test.go` calls the real handlers against Ent/SQLite
with real signed resume tokens. A completed subscription order retains
price12.34, payment86.38 CNY and frozen quota125.12345678/604800s as distinct
values. Four new cases cover valid, malformed numeric quota, future version and
an unknown secret field. They assert exact typed valid data or null, no provider
secret/user/private-order fields, and the unchanged anonymous status/timestamp
allowlist on those same orders. Existing legacy balance result, wrong-user
token rejection, anonymous lookup and WeChat claim tests also ran.

```sh
cd backend
GOMAXPROCS=3 go test -p 1 -ldflags='-s -w' -tags=unit ./internal/handler -run '^(TestResolveOrderPublicByResumeToken|TestVerifyOrderPublic|TestPaymentOrderResponseFrozenQuotaI5|TestApplyWeChatPaymentResumeClaims)' -count=1 -json
```

Result: **8 top-level / 12 including subtests PASS, 0 FAIL, 0 SKIP**.
`gofmt` and scoped `git diff --check` passed. Private log:
`/Users/shenxi/.codex/private/claude-weekly-quota/office/payment-resume-quota-unit.jsonl`,
SHA-256 `c8139de5b2c5edd3ee224dc2923809bca31a03ddbdf7d30017af1dd5908a9772`.

The first new test fixture failed before its cases because it omitted SQLite
`PRAGMA foreign_keys=ON`; it was corrected to match the existing fixtures and
the complete selection reran successfully. Its separate `-attempt1.jsonl` log
is retained. No product assertion or parser restriction was removed.

This is handler/unit evidence, not a final F3 image, browser or merchant test.
The coordinator must build F3 and follow actual checkout through its signed
result URL, plus the affected compatible-image recovery gate. Earlier image
receipts keep their own hashes. No production resources, external payment,
commit or push were changed by this worker.
