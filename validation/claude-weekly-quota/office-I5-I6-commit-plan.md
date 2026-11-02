# I5/I6 batched commit plan — pending one-shot confirmation

Baseline: `203047edaf09f17fc9e73bf4a3541b5fc190b282` on
`codex/claude-weekly-quota`, tracking `origin/codex/claude-weekly-quota`.
The initial checkout was clean; every path below was created or modified by
this session's coordinator/workers. No unrecognized dirty files are included.
Nothing has been staged, committed, pushed or deployed in this continuation.

## 1. feat(subscription): finish paid Claude cards and controlled rollout

Backend paid rights, exact frozen snapshots, recovery/lease/refund guards,
controlled conversion and metrics/history indexes; actual frontend plans,
purchase/orders/result/admin flows and regression tests; executable contracts.

- `.trellis/spec/backend/claude-quota-payment-rollout.md`
- `.trellis/spec/backend/index.md`
- `.trellis/spec/frontend/carpool-state-contracts.md`
- `.trellis/spec/frontend/index.md`
- `.trellis/spec/frontend/quality-guidelines.md`
- `backend/cmd/server/wire.go`
- `backend/cmd/server/wire_gen.go`
- `backend/cmd/server/wire_gen_test.go`
- `backend/internal/config/config.go`
- `backend/internal/config/subscription_quota_rollout_test.go`
- `backend/internal/domain/subscription_quota.go`
- `backend/internal/domain/subscription_quota_payment.go`
- `backend/internal/domain/subscription_quota_payment_test.go`
- `backend/internal/domain/subscription_quota_rollout.go`
- `backend/internal/handler/admin/payment_handler.go`
- `backend/internal/handler/admin/payment_handler_test.go`
- `backend/internal/handler/admin/subscription_quota_handler.go`
- `backend/internal/handler/admin/subscription_quota_rollout.go`
- `backend/internal/handler/payment_handler.go`
- `backend/internal/handler/payment_handler_resume_test.go`
- `backend/internal/handler/payment_subscription_quota_i5_test.go`
- `backend/internal/handler/wire.go`
- `backend/internal/repository/migrations_runner.go`
- `backend/internal/repository/subscription_quota_history_indexes_i6_integration_test.go`
- `backend/internal/repository/subscription_quota_http_i4_integration_test.go`
- `backend/internal/repository/subscription_quota_http_v2_i4_integration_test.go`
- `backend/internal/repository/subscription_quota_local_runtime_integration_test.go`
- `backend/internal/repository/subscription_quota_migration.go`
- `backend/internal/repository/subscription_quota_observability.go`
- `backend/internal/repository/subscription_quota_payment.go`
- `backend/internal/repository/subscription_quota_payment_i5_integration_test.go`
- `backend/internal/repository/subscription_quota_performance_i6_integration_test.go`
- `backend/internal/repository/subscription_quota_plan_units_i5_integration_test.go`
- `backend/internal/repository/subscription_quota_promotion_http_i6_integration_test.go`
- `backend/internal/repository/subscription_quota_r1_image_integration_test.go`
- `backend/internal/repository/subscription_quota_readiness.go`
- `backend/internal/repository/subscription_quota_repo.go`
- `backend/internal/repository/subscription_quota_rollout_http_i6_integration_test.go`
- `backend/internal/repository/subscription_quota_rollout_i6_integration_test.go`
- `backend/internal/repository/subscription_quota_rollout_repo.go`
- `backend/internal/repository/subscription_quota_rollout_unit_test.go`
- `backend/internal/repository/subscription_quota_upgrade_rollback_i6_integration_test.go`
- `backend/internal/repository/wire.go`
- `backend/internal/server/routes/admin.go`
- `backend/internal/service/payment_config_plans.go`
- `backend/internal/service/payment_config_providers.go`
- `backend/internal/service/payment_config_service.go`
- `backend/internal/service/payment_fulfillment.go`
- `backend/internal/service/payment_order.go`
- `backend/internal/service/payment_service.go`
- `backend/internal/service/payment_subscription_quota_guard.go`
- `backend/internal/service/subscription_quota_mode.go`
- `backend/internal/service/subscription_quota_observability.go`
- `backend/internal/service/subscription_quota_payment.go`
- `backend/internal/service/subscription_quota_payment_test.go`
- `backend/internal/service/subscription_quota_rollout.go`
- `backend/internal/service/subscription_quota_rollout_test.go`
- `backend/internal/service/subscription_service.go`
- `backend/internal/service/user_concurrency_scope.go`
- `backend/internal/service/user_concurrency_scope_metrics.go`
- `backend/internal/service/user_concurrency_scope_metrics_test.go`
- `backend/internal/service/wire.go`
- `backend/migrations/252_subscription_quota_order_snapshot_immutable.sql`
- `backend/migrations/253_subscription_quota_rollout.sql`
- `backend/migrations/254_subscription_quota_history_indexes_notx.sql`
- `frontend/src/__tests__/subscriptionQuotaFixtures.ts`
- `frontend/src/__tests__/subscriptionQuotaRolloutFixtures.ts`
- `frontend/src/api/__tests__/payment.spec.ts`
- `frontend/src/api/__tests__/subscriptionQuotaRollout.spec.ts`
- `frontend/src/api/admin/subscriptionQuotaRollout.ts`
- `frontend/src/api/payment.ts`
- `frontend/src/components/admin/group/SubscriptionQuotaRolloutModal.vue`
- `frontend/src/components/admin/group/__tests__/SubscriptionQuotaRolloutModal.spec.ts`
- `frontend/src/components/admin/payment/AdminOrderDetail.vue`
- `frontend/src/components/admin/payment/AdminOrderTable.vue`
- `frontend/src/components/admin/payment/__tests__/orderCurrencyDisplay.spec.ts`
- `frontend/src/components/admin/user/SubscriptionQuotaTermModal.vue`
- `frontend/src/components/admin/user/__tests__/SubscriptionQuotaTermModal.spec.ts`
- `frontend/src/components/payment/OrderTable.vue`
- `frontend/src/components/payment/PaymentQRDialog.vue`
- `frontend/src/components/payment/PaymentStatusPanel.vue`
- `frontend/src/components/payment/SubscriptionPlanCard.vue`
- `frontend/src/components/payment/SubscriptionQuotaOrderSummary.vue`
- `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts`
- `frontend/src/components/payment/__tests__/SubscriptionQuotaOrderSummary.spec.ts`
- `frontend/src/components/payment/validity.ts`
- `frontend/src/i18n/locales/en/subscriptionQuota.ts`
- `frontend/src/i18n/locales/zh/subscriptionQuota.ts`
- `frontend/src/types/payment.ts`
- `frontend/src/types/subscriptionQuotaRollout.ts`
- `frontend/src/views/admin/GroupsView.vue`
- `frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts`
- `frontend/src/views/admin/orders/AdminOrdersView.vue`
- `frontend/src/views/admin/orders/AdminPaymentPlansView.vue`
- `frontend/src/views/admin/orders/PlanEditDialog.vue`
- `frontend/src/views/admin/orders/__tests__/AdminOrdersView.spec.ts`
- `frontend/src/views/admin/orders/__tests__/AdminPaymentPlansView.spec.ts`
- `frontend/src/views/admin/orders/__tests__/PlanEditDialog.spec.ts`
- `frontend/src/views/user/PaymentResultView.vue`
- `frontend/src/views/user/PaymentView.vue`
- `frontend/src/views/user/UserOrdersView.vue`
- `frontend/src/views/user/__tests__/PaymentResultView.spec.ts`
- `frontend/src/views/user/__tests__/PaymentView.spec.ts`

## 2. test(claude-quota): record I5/I6 acceptance and rollout handoff

Portable isolated build/runtime/browser test tooling, success/failure receipts,
per-image source audit, public synthetic screenshots, resource cleanup records,
74-case functional evidence consolidation, current task state and release handoff.
Private credentials, raw logs, synthetic DB backups, binaries and Docker archives
are outside Git and excluded. Older iteration receipts remain attributed to their
original image; failures are preserved. Production conditions remain explicit.

- `.trellis/tasks/10-19-claude-weekly-quota-architecture/task.json`
- `validation/claude-weekly-quota/I5-backend-receipt.md`
- `validation/claude-weekly-quota/I5-frontend-receipt.md`
- `validation/claude-weekly-quota/I5-integration-receipt.md`
- `validation/claude-weekly-quota/I6-F2-image-proof-receipt.md`
- `validation/claude-weekly-quota/I6-F3-image-proof-receipt.md`
- `validation/claude-weekly-quota/I6-PG18.6-rehearsal-receipt.md`
- `validation/claude-weekly-quota/I6-acceptance-delta-draft.md`
- `validation/claude-weekly-quota/I6-controlled-rollout-receipt.md`
- `validation/claude-weekly-quota/I6-conversion-runtime-receipt.md`
- `validation/claude-weekly-quota/I6-final-metrics-unit-receipt.json`
- `validation/claude-weekly-quota/I6-final-pg-receipt.json`
- `validation/claude-weekly-quota/I6-final-unit-receipt.json`
- `validation/claude-weekly-quota/I6-frontend-receipt.md`
- `validation/claude-weekly-quota/I6-image-proof-receipt.md`
- `validation/claude-weekly-quota/I6-independent-review.md`
- `validation/claude-weekly-quota/I6-payment-resume-snapshot-receipt.md`
- `validation/claude-weekly-quota/I6-pending-obligations.md`
- `validation/claude-weekly-quota/I6-performance-before-results.json`
- `validation/claude-weekly-quota/I6-performance-indexed-results.json`
- `validation/claude-weekly-quota/I6-performance-receipt.md`
- `validation/claude-weekly-quota/I6-plan-duration-and-promotion-receipt.md`
- `validation/claude-weekly-quota/I6-production-handoff.md`
- `validation/claude-weekly-quota/I6-root-change-review.md`
- `validation/claude-weekly-quota/coordinator-I5-I6-review.md`
- `validation/claude-weekly-quota/coordinator-acceptance.json`
- `validation/claude-weekly-quota/coordinator-current-status.md`
- `validation/claude-weekly-quota/coordinator-resume-state.md`
- `validation/claude-weekly-quota/coordinator_verify_r0.py`
- `validation/claude-weekly-quota/environment-I4-loopback-bridge/main.go`
- `validation/claude-weekly-quota/environment-I4-provider/main.go`
- `validation/claude-weekly-quota/environment-I4-provider/main_test.go`
- `validation/claude-weekly-quota/environment-I4-ws-proof/main.go`
- `validation/claude-weekly-quota/environment-I6-runtime.Dockerfile`
- `validation/claude-weekly-quota/office-F2-rollback-20261020T022359Z.json`
- `validation/claude-weekly-quota/office-F2-rollback-cleanup-20261020T022359Z.json`
- `validation/claude-weekly-quota/office-F3-rollback-20261020T023709Z.json`
- `validation/claude-weekly-quota/office-F3-rollback-cleanup-20261020T023709Z.json`
- `validation/claude-weekly-quota/office-I5-I6-commit-plan.md`
- `validation/claude-weekly-quota/office-I5-I6-final-verification.json`
- `validation/claude-weekly-quota/office-I6-F1-F2-source-audit.json`
- `validation/claude-weekly-quota/office-I6-boundary-receipt.json`
- `validation/claude-weekly-quota/office-I6-browser-environment.json`
- `validation/claude-weekly-quota/office-I6-browser-receipt.json`
- `validation/claude-weekly-quota/office-I6-build-F2.json`
- `validation/claude-weekly-quota/office-I6-build-F3.json`
- `validation/claude-weekly-quota/office-I6-build-F4.json`
- `validation/claude-weekly-quota/office-I6-build-attempt1-failed.json`
- `validation/claude-weekly-quota/office-I6-build-before-portable-fix.json`
- `validation/claude-weekly-quota/office-I6-build-receipt.json`
- `validation/claude-weekly-quota/office-I6-cache-cleanup-for-pg186.json`
- `validation/claude-weekly-quota/office-I6-conversion-receipt.json`
- `validation/claude-weekly-quota/office-I6-conversion-v3-cleanup.json`
- `validation/claude-weekly-quota/office-I6-conversion-v3-compliance.json`
- `validation/claude-weekly-quota/office-I6-conversion-v3-conversion-20261020T021317Z.json`
- `validation/claude-weekly-quota/office-I6-conversion-v3-environment.json`
- `validation/claude-weekly-quota/office-I6-conversion-v3-prepare-20261020T021159Z.json`
- `validation/claude-weekly-quota/office-I6-conversion-v3-second-20261020T021238Z.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-cleanup.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-compliance.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-conversion-20261020T021721Z.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-environment.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-observed.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-prepare-20261020T021616Z.json`
- `validation/claude-weekly-quota/office-I6-conversion-v4-second-20261020T021653Z.json`
- `validation/claude-weekly-quota/office-I6-environment.json`
- `validation/claude-weekly-quota/office-I6-final-source-audit.json`
- `validation/claude-weekly-quota/office-I6-interim-F1-source-audit.json`
- `validation/claude-weekly-quota/office-I6-long-sse-receipt.json`
- `validation/claude-weekly-quota/office-I6-lost-lease-receipt.json`
- `validation/claude-weekly-quota/office-I6-main-runtime-cleanup.json`
- `validation/claude-weekly-quota/office-I6-metadata-receipt.json`
- `validation/claude-weekly-quota/office-I6-metrics-before-promotion-failed.json`
- `validation/claude-weekly-quota/office-I6-metrics-receipt.json`
- `validation/claude-weekly-quota/office-I6-metrics.json`
- `validation/claude-weekly-quota/office-I6-mixed-bootstrap-failed.json`
- `validation/claude-weekly-quota/office-I6-mixed-bridge-failed.json`
- `validation/claude-weekly-quota/office-I6-mixed-receipt.json`
- `validation/claude-weekly-quota/office-I6-pools-receipt.json`
- `validation/claude-weekly-quota/office-I6-prepare-attempt1-failed.json`
- `validation/claude-weekly-quota/office-I6-prepare-receipt.json`
- `validation/claude-weekly-quota/office-I6-promotion-background-timestamp-failed.json`
- `validation/claude-weekly-quota/office-I6-promotion-nonempty-account-failed.json`
- `validation/claude-weekly-quota/office-I6-promotion-receipt.json`
- `validation/claude-weekly-quota/office-I6-rebuildable-cleanup.json`
- `validation/claude-weekly-quota/office-I6-redis-fault-receipt.json`
- `validation/claude-weekly-quota/office-I6-rollback-attempt1-failed.json`
- `validation/claude-weekly-quota/office-I6-rollback-corrected-20261020T020609Z.json`
- `validation/claude-weekly-quota/office-I6-rollback-corrected-20261020T020715Z.json`
- `validation/claude-weekly-quota/office-I6-rollback-corrected-20261020T020737Z.json`
- `validation/claude-weekly-quota/office-I6-rollback-corrected-20261020T020851Z.json`
- `validation/claude-weekly-quota/office-I6-rollback-receipt.json`
- `validation/claude-weekly-quota/office-I6-runner-change-audit.json`
- `validation/claude-weekly-quota/office-I6-second-receipt.json`
- `validation/claude-weekly-quota/office-I6-superseded-binary-cleanup.json`
- `validation/claude-weekly-quota/office-I6-wallets-receipt.json`
- `validation/claude-weekly-quota/office-I6-ws-proof.json`
- `validation/claude-weekly-quota/office-I6-ws-receipt.json`
- `validation/claude-weekly-quota/office-PG18.6-F4-small-task-cache-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-F4-task-cache-and-copy-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-build-cache-candidates.json`
- `validation/claude-weekly-quota/office-PG18.6-compiled-dependency-cache-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-exact-build-cache-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-performance-20261020T023957Z.json`
- `validation/claude-weekly-quota/office-PG18.6-performance-20261020T025846Z.json`
- `validation/claude-weekly-quota/office-PG18.6-rehearsal-20261020T023957Z.json`
- `validation/claude-weekly-quota/office-PG18.6-rehearsal-20261020T025846Z.json`
- `validation/claude-weekly-quota/office-PG18.6-retained-fixture-backup-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-retired-F1-F2-cache-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-retired-F1-F2-image-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-retired-F3-image-cache-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-retired-driver-F3-copy-cleanup.json`
- `validation/claude-weekly-quota/office-PG18.6-retry-preflight-20261020T025846Z.json`
- `validation/claude-weekly-quota/office-PG18.6-runtime-provenance.json`
- `validation/claude-weekly-quota/office-PG18.6-stopped-container-volume-candidates.json`
- `validation/claude-weekly-quota/office-PG18.6-task-source-context-cleanup.json`
- `validation/claude-weekly-quota/office-acceptance-before-I5-I6.json`
- `validation/claude-weekly-quota/office-browser-I6-instructions.md`
- `validation/claude-weekly-quota/office-browser-I6.py`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F2-result-missing-snapshot.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F3-admin-detail-missing-summary.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F3-admin-plans.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F3-paid-frozen-en-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F3-paid-frozen-zh-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F4-admin-detail-en-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F4-admin-detail-zh-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F4-admin-detail-zh-viewport.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F4-admin-detail-zh.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-F4-admin-orders-zh.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-frozen-pending-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-manual-default-override.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i5-orders-en-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i6-converted-released-390.png`
- `validation/claude-weekly-quota/office-browser-screenshots/i6-stale-preview-390.png`
- `validation/claude-weekly-quota/office-build-I6.py`
- `validation/claude-weekly-quota/office-continuation-20261020.md`
- `validation/claude-weekly-quota/office-previous-I4-coordinator-current-status.md`
- `validation/claude-weekly-quota/office-previous-I4-coordinator-resume-state.md`
- `validation/claude-weekly-quota/office-r0-provenance.json`
- `validation/claude-weekly-quota/office-runtime-I6.py`
- `validation/claude-weekly-quota/office-source-snapshot.py`

## Unrecognized files

None at this snapshot. Reinspect immediately before execution and stop inclusion
of any newly introduced/unrecognized work; use explicit path lists, never an
unreviewed `git add -A`.

## Execution boundary

`.trellis/workflow.md` Phase3.4 requires: **“Present the plan once, ask for
one-shot confirmation”**; after confirmation it says **“Do not amend. Do not
push.”** Reply “行”/“ok” to execute these two commits, or specify edits/manual
handling. This plan does not ask for or grant production rollout approval.

The code is frozen at the F4 source hash in `office-I6-build-F4.json`.
Final PostgreSQL18.6 validation passed12top/0fail/0skip through300k; browser
and source/diff/evidence reviews are complete. Functional acceptance consolidates
74 cases across their recorded I1–I6 scopes. No tests are silently promoted to a
new image; see `coordinator-I5-I6-review.md` for overlaps, existing skips and
performance limits. The final18.6 mixed-burst GPT throughput fell19.3%; no
production SLO or real rollout is claimed.
