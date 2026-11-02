# Claude 周卡运维与对账

## 运行控制

建议配置分为三个独立开关：新手动开卡和新商品销售、网关新准入、恢复与对账。缺省前两个关闭，第三个在有新领域数据时保持可运行；已付订单履约不受新销售开关阻断。现有 CARPOOL_BACKGROUND_ENABLED 不控制 Claude；停用 Claude 不停 GPT 后台。

worker 沿现有应用生命周期启动和关闭，退出时停止领取新任务并等待有界处理完成。多实例使用数据库认领，不依赖“只开一个实例”的部署假设。

v2 增加 Claude 用户并发租约申请、等待、续租失败与清理指标，按 scope 分类；用户级详情显示两个独立池。原平台并发聚合仍是上游账号统计，不能被当成用户额度策略。Redis 读失败显示未知；不能用 0 伪装空闲。只操作新 scope 的清理，不清 GPT 活跃键。

## 指标

| 指标 | 用途 |
| --- | --- |
| subscription_quota_admission_total 按 result | 区分正常放行、过期、耗尽、依赖失败 |
| subscription_quota_settlement_total 按 result | 成功、幂等重放、冲突、失败 |
| subscription_quota_pending_receipts 按 status | 已知待结算与未知用量分开 |
| subscription_quota_oldest_pending_seconds | 发现恢复积压 |
| subscription_quota_ledger_mismatch_total | 非零必须处理 |
| subscription_quota_usage_log_missing_total | 账本已扣但统计缺失 |
| subscription_quota_overage_usd | 后扣产生的本卡超额 |
| subscription_quota_recovery_retries_total | 恢复故障及重试 |
| subscription_quota_scope_lock_wait_seconds | 发现卡期事务拥堵 |
| protected_group_inflight_requests | 模式转换排空证据 |

标签使用平台、入口、状态或少量受控组 ID；不要把 user_id、request_id、原始异常消息作为高基数监控标签。排障日志可使用内部请求 ID，但不记录 Key、Authorization 或提示词。

## 核心对账

第一层逐卡检查：

```sql
SELECT t.id, t.used_usd, COALESCE(SUM(l.delta_used_usd), 0) AS ledger_used
FROM subscription_quota_terms t
LEFT JOIN subscription_quota_ledger l ON l.term_id = t.id
GROUP BY t.id, t.used_usd
HAVING t.used_usd <> COALESCE(SUM(l.delta_used_usd), 0);
```

第二层逐请求检查 settled 与唯一 debit、金额、term_id、api_key_id 相符；usage_known 尚未完成不能误算为零费用成功。第三层对尚未确认统计写入的 settled 请求检查 usage_logs，金额和新关联 ID 是否一致；已确认后按清理策略删除的历史统计不当作漏账，也不补回。

第四层检查同窗口内 Claude 请求没有 ordinary balance 或 carpool 来源；GPT 请求没有 subscription_term_id。不要把账号成本 account_stats_cost 与用户实际消费 actual_cost 相加。

上述 SQL 是未来新表上的只读诊断示例；架构阶段不能在当前生产运行。运行前应确认新迁移和版本，按卡期或时间范围限制扫描。

## 异常处理

| 情况 | 处理 |
| --- | --- |
| usage_known 积压 | 恢复冻结凭据，检查 DB、锁和 worker；禁止重新定价 |
| admitted 长期无凭据 | 标记核查，查上游请求证据；禁止猜零或扣 GPT |
| 幂等指纹冲突 | 冻结该请求自动处理，保留原载荷摘要和差异分类 |
| 卡已用与流水不符 | 停止相关卡新准入，查重复或漏记；审计修复，不能重置整卡 |
| 统计日志缺失 | 从已保存投影补写日志及既有聚合，不重扣 |
| 到期卡保持 closing | 查看未终结请求，不能直接关卡丢消费 |
| 过额 | 保留真实已用并告警，不把差额转移其他权益 |
| 套餐默认金额误改 | 修正未来默认值；已售卡需独立经过授权的业务处理 |

人工 resolve 必须记录处理人、原因、证据引用和原状态版本。只有可信无收费证据才可标记 no_charge。纠错流水必须引用原行；首版不提供任意改数据库金额的操作按钮。

## 发布后的核验记录

保留检查时间、镜像和代码摘要、迁移摘要、目标组配置、测试请求身份的内部 ID、生成结果状态、具体 ledger/request/log ID 和逐表金额差额。只记录脱敏证据。

上线成功意味着最终候选程序实际接流、账务正确、GPT 回归与指标正常。仅 health 返回 200、前端出现卡片或数据库插入成功都不能单独证明完成。

