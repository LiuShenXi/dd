# 0.2.7 数据迁移与二开保留审查

审查时间：2026-09-19。范围仅为本地源码、Git 对象与既有脱敏验收记录；没有连接或修改生产，也没有独立运行数据库/业务测试。测试结果由本次任务协调者统一记录。

## 正确基线

- 权威来源：`C:/WORK-SPACE/sub2api-sentinel-integration-20260919`，分支 `codex/codex-ticket-sentinel`。
- 基线 HEAD：`2fefcf51f0e00618686ab825d81e65444ee1ccfd`；应用发布源码为其父提交 `f8f099c1d2851d1cfe7416e7f41a5108d529079c`，HEAD 仅补充部署记录。
- 此历史包含已完成的 `a8370c5529406e5333d818d95b95cde6bcc9d460` 兼容 0.2.6 发布，以及双列迁移、缓存版本 25、图片缓存计费、carpool-release 打包和 sentinel r2 修复。
- 本次合并区：`C:/WORK-SPACE/sub2api-compatible-0.2.7-20260919`。检查时 HEAD 为上述基线，MERGE_HEAD 为 `7484192016807acf55c6ef4f2827d371eb61ec1c`。最初来自旧 `dd` HEAD `197422403` 的比较不代表实际最新二开基线，其缺失项结论已被本审查取代。

## 已确认保留

1. **数据库迁移零增删改。** 当前全部 297 个 SQL 与权威 0.2.6 工作区逐文件 SHA-256 相同；包括 `234z_group_model_allowlist_compatibility.sql`、上游 235/236 和二开 235–244。`migrations_runner.go` 未变。不能删除、重命名或重写 234z：它保留旧/新分组列并用触发器同步，是昨天已经上线的兼容契约。
2. **账务原子性实现未变。** `usage_billing_repo.go`、`usage_billing.go`、`gateway_usage_billing.go`、`openai_gateway_usage.go`、拼车 repository/service/contract/reconciliation/recovery/reset 文件均保持基线内容。已知用量先持久化，dedup、原周期借记与 settled 状态在同一 SQL 事务提交；拼车没有普通余额降级分支。
3. **昨天的跨层修复已包含。** `apiKeyAuthSnapshotVersion=25` 保留；`openAIForwardResultHasKnownBillableUsage` 已包含 `ImageCacheReadTokens` 的负数检查及正数识别。旧基线审查指出的这两项不是当前新基线的遗漏。
4. **Sentinel r2 完整保留。** controller、control API、原生票据采集续期/缺票放行、诊断 CLI、HTTP/WS 观察与重连、plugin transport 和 embedded frontend 路由排除均无 Git 内容变化。`/internal/codex-sentinel/status` 与 `/refresh` 仍注册；新 Seedance 路由没有替换这些入口。
5. **发行工具与保护不变。** Dockerfile 的 `carpool-release` 构建/复制、`deploy/blue-green` 业务就绪与发布保护均无变化。
6. 上述保留检查所选 456 个已跟踪文件中，相对 `2fefcf51f` 的变更列表为空；完整上游增量为 59 个文件，主要新增 Seedance、插件宿主服务及供应商兼容修正。

`sentinel.py` 新工作区原始字节使用 Windows 换行，而原权威工作区为 LF；Git 内容相同。这不属于逻辑改动，但旧验收记录中的原始源码哈希不能直接充当新工作区构建哈希。最终发行仍应从固定 Git 归档构建并重新记录来源。

## 唯一已识别的新增二开边界

上游 Seedance 异步任务复用 Grok 任务账单，但没有持久化创建时的拼车准入/周期。直接开放给拼车会破坏现有结算契约。负责实现的 agent 已在 `SeedanceTasks` 中加入 `key.Group.IsCarpoolType()` 检查，以 `403 / CARPOOL_SEEDANCE_UNSUPPORTED` 在上游调用前拒绝 POST/GET/DELETE。审查时已看到该代码；其回归执行结果仍以协调者测试记录为准。

其余变更中：Grok 默认平台仍为 Grok，只有 Seedance 分支选 OpenAI；原视频查找选择器保留包装函数。新插件 KV 使用独立 `plugin:kv:v1:` Redis 前缀，不修改拼车账务表；plugin host services 的账号目录绑定不替换现有业务 transport/sentinel 调用链。没有发现另一个需要阻断本次合并的二开保留缺陷。

## 需要本次验证确认的证据

- 真实 PostgreSQL：`TestUpgrade026ProductionMigrations_ExactBaselineUpgradeReplayAndRollback`、`TestGroupModelAllowlistCompatibility_*` 和拼车原子结算/恢复/周期/重置回归；现有测试仍验证旧 291 条到完整 297 条，而本次 0.2.7 本身不新增 SQL。
- 真实运行回归：拼车 HTTP 与两轮 WS、未知 usage 不扣费、图片缓存费用与取消后已知用量结算。
- Sentinel：续期/过期/缺票放行、旧异常不覆盖新异常、嵌入前端控制路由和插件 transport 回归。
- Seedance：所有入口别名及方法的拼车拒绝，确认无上游调用；普通计费任务原有 owner/dedup 行为继续通过。

静态审查证明的是源码和迁移保留，不代表本次运行测试或生产部署已完成。原 0.2.6 的部署与通过记录是历史证据，不能冒充 0.2.7 的新验收。
