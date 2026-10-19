# Claude 周卡接口契约

## 协议约定

沿用 /api/v1 的现有 Response：成功 code 等于 0、message 等于 success、data 为结果；错误 code 为 HTTP 状态整数，reason 为稳定业务错误标识，metadata 为字符串映射。Gateway 的 Anthropic/OpenAI 错误外壳分别沿现有适配器，不能把后台响应格式直接塞进流式协议。

金额统一为最多八位小数的字符串；输出固定八位。额度必须大于零，拒绝空串、负数、非有限值、科学计数法和超出 NUMERIC(20,8) 的值。省略额度只表示从选定套餐复制默认值，绝不表示不限额。

管理和查询接口使用现有登录身份认证。普通用户不能提交 user_id 来查询他人；管理接口先检查管理员角色再加载目标记录。路径 ID 不能被 payload 内的身份覆盖。

所有写操作要求 Idempotency-Key，长度 16 至 128，使用 UUID 即可；只读 preview 虽使用 POST，也不写入幂等记录。重放返回原结果；同 scope 同键不同规范化载荷返回 409。返回的 replayed 只表示本操作是否重放。列表采用稳定游标分页，limit 为 1 至 100，默认 20；items 为空时返回 []。

机器接口清单见 [OpenAPI](contracts/openapi.json)。它描述新增接口；下述原接口兼容义务同样属于开发范围。

## 管理接口

| 方法及路径 | 行为 |
| --- | --- |
| GET /admin/users/:user_id/subscription-quota?group_id=6 | 当前卡、未来卡、汇总、父 revision；无记录也返回空集合 |
| GET /admin/users/:user_id/subscription-quota/terms?group_id=6 | 该用户该组的历史卡分页 |
| POST /admin/users/:user_id/subscription-quota/terms/preview | 只计算额度和排期，不创建订阅父记录或占用卡期 |
| POST /admin/users/:user_id/subscription-quota/terms | 幂等开卡或续卡；创建成功 201，重放 200 |
| POST /admin/subscription-quota/terms/:term_id/cancel | 取消权益，返回原始到期时间和实际取消时间；不退款、不转余额 |
| GET /admin/subscription-quota/requests?status=... | 分页查询异常或待结算请求，只返回脱敏计费信息 |
| POST /admin/subscription-quota/requests/:request_id/resolve | 受审计的未知用量核查；只允许无已结算流水的请求 |

开卡输入：

```json
{
  "group_id": 6,
  "operation": "open",
  "quota_usd": "100.00000000",
  "expected_revision": 0,
  "notes": "手动开通 Claude 周卡"
}
```

operation 为 open 或 renew。open 在存在尚未结束的未取消卡时返回冲突，避免误开下一期；renew 明确按队尾追加，过期后从当前时间重新开卡。若选择 plan_id，可以省略 quota_usd；明确传值优先于默认额度并记录来源为管理员覆盖。

validity_days 和 platform 由服务器固定，不接受客户端改变。允许省略 plan_id 做完全手动开卡。没有套餐且没填额度返回 QUOTA_REQUIRED。

预览返回 effective_quota_usd、proposed_starts_at、proposed_expires_at、server_time、current_revision、warnings 和已有当前/未来卡摘要。预览不是预留，提交时 expected_revision 不匹配返回 409 并要求重新预览。首次无父记录时版本为 0。

提交以数据库重新计算时间为准。若预览后当前卡自然到期导致开始时间变化，返回新的实际起止时间，不能创建倒签卡。未来排期因并发续卡变化必须由 revision 冲突拦截。

取消输入为 expected_revision（父订阅排期版本）和非空 reason。默认取消单张，不批量取消未来卡；当前卡已有在途请求时返回 closing 状态，并说明仍有待结算消费。已结束卡再次取消同操作键返回原结果；新操作键请求取消已结束卡返回不可操作。正常用量只递增 term.revision，不改变父排期版本。

核查 resolve 输入为 expected_request_revision、resolution、evidence_reference、reason 及可选的 verified_usage；以 requests.revision 做条件更新。仅 reconcile_required、actual_cost_usd 为空且尚无已知凭据或已结算流水的请求允许人工提交；可信用量进入正常 PersistKnownUsage 再 Apply，确定无消费进入 no_charge。已有已知凭据的矛盾不开放通用改写，返回 409 留待专门账务审查。禁止由前端直接提交“扣哪个钱包”或负金额。verified_usage 使用明确的模型及 token 字段，由后台按准入时已冻结定价计算，不接受任意 JSON 结算命令。接受核查返回 202 和请求状态；账务随后由正常幂等结算完成，不能提前显示已结算。

## 用户接口

| 方法及路径 | 返回 |
| --- | --- |
| GET /subscription-quota | 本人各 Claude term 分组的摘要列表 |
| GET /subscription-quota/groups/:group_id | 当前卡、未来卡、时间和额度 |
| GET /subscription-quota/groups/:group_id/terms | 本人卡期历史分页 |
| GET /subscription-quota/terms/:term_id/usage | 本人该卡已结算用量分页，不暴露内部账务桶 |

普通用户没有开卡、改额度、resolve、重置或互转接口。不存在或属于他人的 term 返回统一 404，避免泄露身份。

摘要 data 示例：

```json
{
  "group_id": 6,
  "quota_mode": "term",
  "revision": 12,
  "server_time": "2026-10-19T12:00:00Z",
  "current_term": {
    "id": 101,
    "quota_usd": "100.00000000",
    "used_usd": "20.00000000",
    "pending_usd": "0.00000000",
    "remaining_usd": "80.00000000",
    "status": "active",
    "starts_at": "2026-10-19T12:00:00Z",
    "expires_at": "2026-10-26T12:00:00Z",
    "unsettled_request_count": 0
  },
  "next_terms": []
}
```

current_term 可以为 null，last_term 提供最近结束卡的状态；历史卡另行分页呈现。不能把已经到期的卡伪装为 active。unsettled_request_count 不等于已用金额，不能拿未知请求数量估算费用。内部 raw_overage_usd 只供管理员，用户剩余显示下限为零。

## 现有接口兼容表

| 入口 | windows 模式 | term 模式 |
| --- | --- | --- |
| GET /subscriptions 及 active progress summary | 原行为 | 添加 quota_mode 和 quota_summary，读新卡期 |
| 管理 assign 或 bulk-assign | 原行为 | 要求新参数并委托 OpenTerm；无参数返回明确错误 |
| extend 及 bulk extend | 原行为 | 拒绝按天修改，提示使用续卡 |
| reset-quota | 原行为 | 拒绝，不得清除本期已用 |
| revoke delete restore | 原行为 | 首版拒绝旧动作，使用明确 cancel；不复活历史卡 |
| 分组管理 | 原行为 | 校验平台、计费模式、无跨组 fallback；有卡后禁止降级普通模式 |
| 套餐创建更新 | 原行为 | 七天、正默认额度；改默认不改已开卡 |
| 下单及回调 | 原行为 | 保存并使用 term 额度快照 |
| 兑换码及旧式自动全额/部分退款 | 原行为 | 首版拒绝并给出原因，不能只增减父记录天数 |
| GET /v1/usage | 原行为 | 返回当前卡剩余和到期；不能返回 ordinary balance |

新模式 /v1/usage 的结果增加 billing_scope 等于 claude_subscription、quota_mode 等于 term；保留该入口现有 Key 限额投影，前端明确哪一项是 Key 限制。统计展示不得把两个剩余相加。

## Gateway 接线矩阵

| 入口 | term 支持要求 |
| --- | --- |
| POST /v1/messages | SSE 与非 SSE、Kiro/API Key/OAuth 已有 Anthropic 账号分支都经过最终准入 |
| POST /v1/chat/completions | 经 GatewayHandler 的 Anthropic 转换路径接入 |
| POST /v1/responses | 经 GatewayHandler 的 Anthropic 转换路径接入 |
| /responses 和现有转发子路径 | 只有确实生成并已接线的路径允许；输入计数元数据不扣额度 |
| /v1/models /v1/usage 等元数据 | 保持原鉴权规则，可读取权益状态，不授予生成权 |
| WebSocket 及其他供应商收费端点 | term 模式明确拒绝，原 GPT 能力保持原有行为 |

必须从真实 Router 枚举别名，不靠文件名推测覆盖。新收费端点若缺 term adapter，应 fail closed；不能只在主 Messages Handler 加一条 if。

## 错误表

| HTTP | reason | 含义 |
| --- | --- | --- |
| 400 | QUOTA_REQUIRED / INVALID_QUOTA | 未填写可解析的正额度 |
| 400 | TERM_GROUP_INVALID | 不是允许的 Claude term 分组 |
| 403 | TERM_NOT_ACTIVE / TERM_EXPIRED / TERM_CANCELLED | 无当前可用权益 |
| 403 | TERM_ENDPOINT_UNSUPPORTED / TERM_PLATFORM_MISMATCH | 请求入口或目标平台不在卡的范围 |
| 409 | IDEMPOTENCY_CONFLICT / QUOTA_REVISION_CONFLICT | 载荷重放冲突或预览过时 |
| 409 | LEGACY_SUBSCRIPTION_OPERATION_UNSUPPORTED | 旧接口不能处理新模式 |
| 429 | TERM_QUOTA_EXHAUSTED | 本卡额度耗尽 |
| 503 | TERM_BILLING_UNAVAILABLE / TERM_ADMISSION_RETRY | 依赖失效或边界竞争，未发上游 |
| 503 | TERM_RECONCILIATION_PENDING | 当前卡有待核查消费，暂不接受新请求 |

429 只在已知存在下一卡时给准确的 next_available_at；不能暗示到每周一会自动重置。错误日志使用分类值，保留用户可理解的信息而不泄露密钥、原始上游响应或他人 ID。

