export default {
  concurrencyScopes: {
    legacy: 'GPT / legacy user concurrency', claude: 'Claude weekly-card concurrency', default: 'Independent default', override: 'User override', inherit: 'Use Claude default', waiting: 'Waiting', unavailable: 'Unavailable', notConfigured: 'Claude concurrency is not configured; new term requests are blocked.', shared: 'All Claude Keys and groups share one user pool. Renewing a card does not add slots. GPT keeps its own pool.', editOverride: 'Claude concurrency override (optional)', editHint: 'Leave blank to keep the current policy. Only a positive integer is allowed; changing GPT concurrency does not change Claude.', createHint: 'Leave blank to use the independent Claude default.', invalid: 'Claude concurrency must be a positive safe integer.'
  },
  subscriptionQuota: {
    title: 'Claude weekly card', manage: 'Manage Claude card', shared: 'Shared by every Key in this group',
    unavailable: 'Card information unavailable', noCurrent: 'No current Claude card', noCards: 'No Claude weekly cards',
    quota: 'Card allowance', used: 'Settled usage', pending: 'Known pending charges', remaining: 'Available remainder',
    starts: 'Starts', expires: 'Expires', countdown: 'Time remaining', awaitingRefresh: 'Refreshing server state…',
    unsettled: '{count} requests are awaiting settlement or review. Unknown usage is not estimated as zero.',
    next: 'Scheduled cards', last: 'Most recent card', history: 'Card history', usage: 'Usage for this card',
    nextPage: 'Load more', refresh: 'Refresh', retry: 'Retry', emptyUsage: 'No settled usage for this card',
    emptyHistory: 'No cards', request: 'Request', key: 'Key', model: 'Model', cost: 'Settled cost', tokens: 'Input / output tokens',
    loadFailed: 'Unable to load Claude card information', keyLimit: 'This Key’s own usage limit',
    keyLimitHint: 'Key limits and rates are independent constraints. They do not add to the shared card allowance.',
    open: 'Open card', renew: 'Schedule renewal', cancel: 'Cancel card', fixedDuration: 'Fixed validity: 168 hours (7 days)',
    renewHint: 'Renewal starts after the existing schedule ends. It does not refill the current card.',
    user: 'User', group: 'Claude group', chooseGroup: 'Choose a Claude group', amount: 'Card allowance (USD)',
    amountHint: 'Positive USD amount, up to 8 decimal places. This is usage allowance, not a sale price.',
    invalidAmount: 'Enter a positive decimal amount with at most 8 decimal places.', notes: 'Notes',
    preview: 'Preview schedule', previewTitle: 'Server preview', effectiveAmount: 'Effective allowance',
    actual: 'Actual saved interval', submit: 'Confirm card', saving: 'Saving…', loading: 'Loading…',
    conflict: 'The schedule changed. Your input is preserved; preview again before submitting.',
    saved: 'Card saved. The interval below is the server result.', replayed: 'Original operation replayed. Current card state is refreshed separately.',
    closeHint: 'Closing this dialog does not cancel an operation already sent to the server.',
    cancelReason: 'Cancellation reason', cancelHint: 'Cancellation stops availability; it does not refund or restore used allowance.',
    cancelConfirm: 'Confirm cancellation', noGroups: 'No active Claude term groups are available.',
    requests: 'Claude request review', filterUser: 'User ID', filterGroup: 'Group ID', allStatuses: 'All statuses',
    status: 'Status', admitted: 'Admitted at', category: 'Diagnostic category', review: 'Review request',
    resolution: 'Resolution', noCharge: 'Verified no charge', verifiedUsage: 'Verified upstream usage',
    evidence: 'Evidence reference', reason: 'Audit reason', requestedModel: 'Requested model (optional)',
    upstreamModel: 'Upstream model (optional)', responseModel: 'Response model (optional)', serviceTier: 'Service tier (optional)',
    inputTokens: 'Input tokens', outputTokens: 'Output tokens', cache5m: 'Cache creation (5m)', cache1h: 'Cache creation (1h)', cacheRead: 'Cache read tokens',
    countersInvalid: 'Every token counter must be a nonnegative safe integer.',
    resolve: 'Submit audited resolution', accepted: 'Resolution accepted. Settlement status will be refreshed; acceptance is not proof of settlement.',
    unknownCost: 'Unknown cost', requestRevision: 'Request revision', noRequests: 'No matching requests',
    statuses: {
      scheduled: 'Scheduled', active: 'Active', exhausted: 'Allowance exhausted', reconciling: 'Usage under review',
      expired: 'Expired', cancelled: 'Cancelled', closing: 'Closed to new requests; settling', closed: 'Closed',
      admitted: 'Admitted', usage_known: 'Usage known', settled: 'Settled', no_charge: 'No charge', reconcile_required: 'Review required'
    }
  }
}
