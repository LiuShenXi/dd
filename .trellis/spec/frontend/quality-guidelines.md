# Quality Guidelines

This document records the source-backed route verification contract from the
Claude quota payment work. It is not a complete frontend style guide.

## Verify the actual route owner

Do not treat a matching component filename or its isolated tests as proof that
a routed page changed. `/admin/orders` imports `AdminOrdersView.vue`, which uses
the shared `components/payment/OrderTable.vue` and owns its action slot and
detail dialog. `components/admin/payment/AdminOrderTable.vue` and
`AdminOrderDetail.vue` are not used by that route. Editing only those components
previously left the live admin refund actions and detail unchanged.

For a page change, trace `src/router/index.ts` → actual view import → rendered
component/slot → event handler → API response fields. Search component references
before assuming a reusable-looking component is mounted.

| Route | Actual owner | Relevant child or handler |
| --- | --- | --- |
| `/purchase` | `views/user/PaymentView.vue` | `SubscriptionPlanCard`, `PaymentStatusPanel`; create response and polling snapshots |
| `/orders` | `views/user/UserOrdersView.vue` | Shared `OrderTable`; `canRequestRefund` |
| `/payment/result` | `views/user/PaymentResultView.vue` | Signed resume result; `SubscriptionQuotaOrderSummary` |
| `/admin/orders/plans` | `views/admin/orders/AdminPaymentPlansView.vue` | Actual duration cell and `PlanEditDialog` |
| `/admin/orders` | `views/admin/orders/AdminOrdersView.vue` | Shared `OrderTable` action slot, detail dialog, refund handlers |

Treat this map as a source checkpoint, not a substitute for checking the current
router. The legacy `/payment/qrcode` has a router entry but no current purchase
flow reference; do not substitute its tests for `PaymentStatusPanel` acceptance.

## Monetary action targets and rendering guards

Keep a monetary action's selected order separate from read-only detail
selection. A detail response arriving after close or another selection must not
replace the order being refunded. Hiding a button is only the rendering guard;
the open, submit and query handlers also reject snapshot cards.

## Required tests at the routed view

Mount the actual view for behavior owned by its slots or handlers. Stub layout
and table primitives when useful, while rendering the real child responsible
for the changed data. The admin regression mounts the real `AdminOrdersView`,
shared `OrderTable` and `SubscriptionQuotaOrderSummary`:

```ts
// Wrong evidence for /admin/orders: only mounting the unused AdminOrderDetail.
// Correct: exercise the routed view's actual View action and detail output.
import AdminOrdersView from '../AdminOrdersView.vue'
const wrapper = mount(AdminOrdersView, { global: { stubs: layoutAndTableStubs } })
await flushPromises()
await wrapper.get('[data-testid="order-3"]').find('button').trigger('click')
await flushPromises()
expect(wrapper.get('[role="dialog"]').text()).toContain('125.12345678 USD')
```

| Case | Expected output or effect |
| --- | --- |
| Term `COMPLETED`, `PARTIALLY_REFUNDED`, `REFUND_REQUESTED`, `REFUND_FAILED`, `REFUND_PENDING` | Manual-refund explanation; no legacy refund/approve/retry-refund/query action |
| Same statuses without a quota snapshot | Existing windows refund/query operation uses the selected order ID |
| Term `PENDING` or `FAILED` | Preserve cancellation or issuance retry; issuance retry is not a refund |
| Snapshot arrives before stale action/submit | Reject locally; no refund/query API call |
| Residual confirm after close | No refund call; cleared target |
| Late detail response while another refund is selected | Refund still uses the independently selected order |
| Frozen detail after current plan changes | Original snapshot quota/duration; sale price separate from payment amount; no balance-credit label |

Executable reference:
`src/views/admin/orders/__tests__/AdminOrdersView.spec.ts` (18 route-view cases)
and `src/components/admin/payment/__tests__/AdminRefundDialog.balance.spec.ts`
(4 existing dialog cases). Run focused tests, targeted ESLint, typecheck and the
production build for changes to this view.

## Browser evidence scope

Record the immutable candidate, actual URL, observed state, language/viewport,
keyboard actions and screenshot hash. Component-test status/race coverage is not
browser coverage. If a late fix changes only one page, retain earlier observations
under their original candidate and state which page was rerun on the final image.

## Code review checklist

- Does the router mount the edited view/component?
- Does the view override child actions or render an independent detail dialog?
- Do the API result used by this route and the template both carry the snapshot?
- Are hidden monetary actions also guarded in their handlers?
- Do selected monetary targets survive unrelated late responses without retargeting?
- Do tests invoke the real route-owned event and assert visible results?
- Does the receipt distinguish automated assertions from browser observations?
