# 跨模块集成契约

这份文档补充已有代码的接线和事务边界。I0 可以调整具体函数名，不能删除对应的并发、幂等或兼容要求。

## 计价与领域边界

保持现有 PricingResolver/BillingService 为唯一价格规则所有者。增加只读的 ResolveTermPricingSnapshot 和从快照计算费用的入口；不要在 SubscriptionQuotaService 内写一份模型价格表。

准入输入包含已选择账号和本次请求的原模型、映射模型、计费模型来源。最终数据库准入时钟决定 pricing_at 与高峰规则；快照由已有规则解析为版本化、可序列化的数值。准入前的模型选择或价格解析失败必须在发上游前返回错误。

快照必须覆盖当前 Claude 的输入、输出、缓存读取、5m/1h 缓存创建、长上下文阶梯、用户专属倍率、分组倍率、高峰时间和 response_model 采纳规则。支持的工具收费也应有冻结价格；不能只保存一个 multiplier，事后再查已变化的价目表。账号成本与用户消费分别保存，禁止拿 TotalCost 直接当周卡扣量。

现有 calculateRecordUsageCost 在部分失败分支返回零费用；新模式要返回结构化错误，进入核查而非免费。该调整限于新模式入口，旧 GPT 计算及指纹保持原行为。

## SQL 与 Ent 事务

新领域采用现有 sql.DB 的短事务。Repository 可以暴露内部 openTermTx，供本模块的手动开卡、支付履约和首次转换使用；不能在已经持有 Ent 事务时再开启独立 SQL 事务，然后声称两者原子提交。

当前 payment_fulfillment.go 的 ensurePaymentSubscriptionAssigned 使用 Ent 事务写原生订阅与 SUBSCRIPTION_ASSIGNED 审计，之后 doSub 才执行分销和 markCompleted。新模式在进入这个旧方法之前分派到专用 FulfillPaidTerm 方法：

1. 保持既有支付通知验签、付款状态判定、履约租约获取及重试机制。
2. 新 Repository 开 SQL 事务，先锁 payment_orders 行，验证订单仍允许履约、未退款，且 updated_at 仍等于当前 lease.version。
3. 按 billing-protocol 的 scope、幂等、parent、term 顺序执行。校验订单不可变快照、用户与分组身份，按 renew 规则追加卡；一个订单只能开一张。
4. 在同一事务写新卡、operation、SUBSCRIPTION_ASSIGNED 审计和失效 outbox。audit 继续使用原唯一订单动作约束；不可另造不关联订单的成功日志。
5. 提交后沿旧流程执行分销、markCompleted。若这一步失败，下次回调依据原卡及 audit 返回已发放结果，不重新开卡；租约失效的 worker 不越权标记完成。

这里的原子边界是“权益发放 + 发放审计”，订单 completed 状态沿现有可恢复状态机收敛。不能在支付成功回调中先标 completed 再异步发权益。也不能把分销失败误解释成用户没有拿到卡。

手动开卡不需要订单行锁。首次模式转换使用专用管理事务，复用同一个 openTermTx 不变式。转换期间组处于维护状态并禁止该组开卡、续卡、退款及履约写入；排序拿完目标 scope、parent 和 term 后，最后锁组复核并修改 mode，避免反转普通事务锁顺序。此事务失败全部回滚。

## 父订阅与旧维护任务

quota_revision 是排期版本，只在开卡、续卡、取消等权益变动时增加。term.revision 反映本卡用量及状态。请求 revision 反映计费事实状态；租约续期不改变它。三个版本不能混用。读摘要用单条 SQL/CTE 的一致快照计算 used、pending 和 remaining，避免跨语句恰遇结算造成重复或漏算。

父 starts_at 为最早卡期起点，expires_at 为未取消排期最晚到期时间；没有未取消卡时保留已结束历史终点，并将状态投影为 expired 或 cancelled。父字段仅服务兼容列表，真实准入始终查当前 term。父记录不软删除，不通过 restore 重发额度。

原 subscription_expiry_service、subscription_maintenance_queue 和 user_subscription_repo 的日周月重置必须排除 term；原生到期提醒也不能拿父记录最远到期时间充当当前卡的到期。首版 term 查询和卡状态由专用维护更新，原自动订阅邮件跳过 term。若后续增加周卡邮件，要按 term_id 去重并设计单独事件，不能顺手复用旧父记录提醒。

GetAvailableGroups 和 Key 创建/换组的权限需支持有效 term 订阅；只有未来卡时可以展示排期，但生成仍拒绝。不得通过 user_allowed_groups 发余额权限。已绑定 GPT 的用户不能用 standard Claude 组绕过；只有正式 term 组适用并存例外。

## 未完成消费与准入

后扣费仍允许已准入请求造成超额。为避免结算故障扩大已知欠记金额，准入使用：effective_used = term.used_usd + 同卡 usage_known 的 actual_cost_usd 合计。该合计在同 scope 锁下读取；settled 已在 used 中，不重复计算。

effective_used 达到额度时拒绝新请求，已知未结算费用不另造 debit。未完成的 admitted 尚无金额，不猜费用。超过“该入口最大请求处理时间 + 可配置宽限”的 admitted 转为 reconcile_required；当前卡存在未解决核查请求时返回 TERM_RECONCILIATION_PENDING，暂停该卡新准入。旧卡的核查不阻止时间上已生效的新卡，更不影响 GPT。

迟到的可信 usage 可以把尚未存在已知凭据、也未被人工终结的 reconcile_required 收敛为 usage_known；已经有首份可信凭据、no_charge 或 settled 却收到矛盾事实只能告警和核查，不能覆盖原结果。自动恢复与人工 resolve 都须锁同 request，并校验 revision。

## 统计补写和清理

新增 requests.usage_log_recorded_at，只有成功写入或核对已有同身份同金额的 usage_logs 后才设置。补写只扫描 settled 且此字段为空的请求。账本提交与日志之间崩溃可重试，统计唯一键仍使用现有 request_id 加 api_key_id。

usage_cleanup_repo 删除新模式日志前必须确认其请求 usage_log_recorded_at 已设置；未确认写入的日志暂不清理，避免“写日志后、标完成前”崩溃与清理竞态。已确认的日志按原清理策略删除后不再自动补回；请求凭据、ledger 和写入完成标记保留。旧 GPT 日志的清理条件保持原行为。

用户的卡期用量明细以专用请求和 ledger 的脱敏投影为依据，不把可能被清理的全站统计表作为余额真相。对账中应区分“尚未写统计”与“历史统计按策略已删除”，不能把后者每次都报警为漏账。

## 退款和兑换范围

首版对 term 禁用所有会直接增减原生订阅天数的自动退款路径，**包括全额和部分退款**；旧 windows 商品退款保持原行为。界面应明确新模式尚不支持自动权益退款，不能让接口悄悄按天撤回。

需要现金退款时由运营明确处理退款和卡期取消，使用已审计的领域取消操作及支付侧凭据；不把现金退款转成 users.balance 充值，不自动清除已用量。未来若要求自动退款，另设订单—卡期撤销事务和在途结算契约。兑换码同理，未适配额度快照前拒绝。

## 配置、缓存和启动

建议新配置项采用 subscription_quota 命名空间：issue_enabled、admission_enabled、recovery_enabled、enabled_group_ids、protected_group_ids、recovery_batch_size、recovery_interval、request_stale_grace。具体环境变量映射遵从现有 config loader，不能只加一个无人读取的 env。

issue_enabled 控制新手动发卡和新订单创建；已经收款的订单仍须可恢复履约。admission_enabled 只控制生成；recovery_enabled 控制后台 worker。故障或回退期间必须保留已付订单和已知用量恢复路径，原生 GPT 后台不读取这些开关。

Redis 失效任务随权益变更同事务进入已有 outbox。新模式不得调用 QueueUpdateSubscriptionUsage、QueueDeductBalance 或 standard user_platform_quota 的增量写入；它们不是新卡余额。API Key 和账号使用计数仍沿现有规则并在同一账务事务执行。API Key 的 auth snapshot 新增 mode 并提升版本，L1/L2 都测试陈旧数据被拒绝。

启动时配置要求开启 admission，而新 Repository、receipt codec 或必需迁移不完整，应拒绝启用该能力并报告 503；不得静默转 legacy。恢复启动不受新发卡开关控制。正式回退镜像应验证能够读写当前 receipt_version，再冻结镜像摘要。
