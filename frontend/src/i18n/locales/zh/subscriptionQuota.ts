export default {
  concurrencyScopes: {
    legacy: 'GPT / 原用户并发', claude: 'Claude 周卡并发', default: '独立默认值', override: '用户覆盖值', inherit: '采用 Claude 默认值', waiting: '等待中', unavailable: '暂不可用', notConfigured: 'Claude 并发尚未配置，暂不接受新的周卡请求。', shared: '所有 Claude Key 和分组共用同一用户并发池，续卡不增加槽位；GPT 保持独立池。', editOverride: 'Claude 并发覆盖值（可选）', editHint: '留空表示不修改现有策略，仅接受正整数；修改 GPT 并发不会修改 Claude。', createHint: '留空采用独立 Claude 默认并发。', invalid: 'Claude 并发必须是正安全整数。'
  },
  subscriptionQuota: {
    title: 'Claude 周卡', manage: '管理 Claude 周卡', shared: '此分组内所有 Key 共用同一张卡',
    unavailable: '周卡信息暂不可用', noCurrent: '当前没有生效的 Claude 周卡', noCards: '尚未开通 Claude 周卡',
    quota: '本卡额度', used: '已结算用量', pending: '已知待结算费用', remaining: '可用剩余',
    starts: '生效时间', expires: '到期时间', countdown: '剩余时间', awaitingRefresh: '正在刷新服务器状态…',
    unsettled: '{count} 个请求待结算或核查，未知用量不会估算为零费用。',
    next: '已排期续卡', last: '最近一期', history: '周卡历史', usage: '本卡用量明细',
    nextPage: '加载更多', refresh: '刷新', retry: '重试', emptyUsage: '本卡暂无已结算用量',
    emptyHistory: '暂无周卡', request: '请求', key: 'Key', model: '模型', cost: '已结算费用', tokens: '输入 / 输出 Token',
    loadFailed: '无法加载 Claude 周卡信息', keyLimit: '此 Key 的使用上限',
    keyLimitHint: 'Key 上限和速率限制独立生效，不增加共用周卡额度。',
    open: '开通周卡', renew: '排期续卡', cancel: '取消周卡', fixedDuration: '有效期固定 168 小时（七天）',
    renewHint: '续卡在当前排期结束后生效，不会立即补满当前卡。',
    user: '用户', group: 'Claude 分组', chooseGroup: '请选择 Claude 分组', amount: '当期额度（USD）',
    amountHint: '正数 USD，最多八位小数。这是使用额度，不是售价。',
    invalidAmount: '请输入最多八位小数的正数额度。', notes: '备注',
    preview: '预览排期', previewTitle: '服务器预览', effectiveAmount: '实际采用额度',
    actual: '实际保存的生效区间', submit: '确认开卡', saving: '正在保存…', loading: '正在加载…',
    conflict: '排期已变化，已保留输入的额度，请重新预览后提交。',
    saved: '周卡已保存，以下为服务器实际生效区间。', replayed: '已重放原操作结果，当前周卡状态会另行刷新。',
    closeHint: '关闭弹窗不等于取消已经发送到服务器的操作。',
    cancelReason: '取消原因', cancelHint: '取消会停止可用，不会退款或恢复已用额度。',
    cancelConfirm: '确认取消', noGroups: '暂无可用的 Claude term 分组。',
    requests: 'Claude 请求核查', filterUser: '用户 ID', filterGroup: '分组 ID', allStatuses: '全部状态',
    status: '状态', admitted: '准入时间', category: '诊断类别', review: '核查请求',
    resolution: '处理方式', noCharge: '有证据确认无费用', verifiedUsage: '已核验上游用量',
    evidence: '证据引用', reason: '审计原因', requestedModel: '请求模型（可选）',
    upstreamModel: '上游模型（可选）', responseModel: '响应模型（可选）', serviceTier: '服务层级（可选）',
    inputTokens: '输入 Token', outputTokens: '输出 Token', cache5m: '缓存写入（5 分钟）', cache1h: '缓存写入（1 小时）', cacheRead: '缓存读取 Token',
    countersInvalid: '所有 Token 计数都必须是非负安全整数。',
    resolve: '提交审计处理', accepted: '处理已受理，将刷新结算状态；受理不代表已经结算完成。',
    unknownCost: '费用未知', requestRevision: '请求版本', noRequests: '没有匹配的请求',
    statuses: {
      scheduled: '待生效', active: '使用中', exhausted: '本卡额度已用完', reconciling: '用量待核查',
      expired: '已到期', cancelled: '已取消', closing: '已停止可用，正在结算', closed: '已结束',
      admitted: '已准入', usage_known: '用量已知', settled: '已结算', no_charge: '确认无费用', reconcile_required: '需要核查'
    }
  }
}
