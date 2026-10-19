# Claude 独立周卡迭代架构包

本包交给 Sol 实施。目标是在生产现有 Monas 二开上，增加管理员逐用户填写额度的 Claude 七日卡，并让同一用户的 Claude 订阅和 GPT 拼车分别鉴权、分别计费。保留现有 GPT 拼车周期、加量卡、重置、账本和恢复机制。

**交付状态：架构完成，业务实现尚未开始。** 本包没有执行生产配置变更、数据库迁移、模型调用或部署。用户授权本轮输出详尽架构；后续开发由用户另行安排 Sol。

## 基线

| 项目 | 固定值 |
| --- | --- |
| 核验日期 | 2026 年 10 月 19 日，Asia/Shanghai |
| 生产目标 | SSH 别名 main-server，应用 sub2api2，数据库 sub2api2-postgres |
| 生产镜像 | sub2api:2.8.13-monas-bps.13 |
| 源码提交 | 8d869d41f40d6eedbe2ee05aaf8973145cccbe4d |
| 镜像 ID | sha256:fb5ecafe42c3346916e901879ba5b3064c16c91ab3e0d76e748e643e377e8677 |
| 运行程序 SHA-256 | 85d991720e15993813fdfdddc408852fefe90c54314e760d68765cf5ba0767cd |
| 对应源码目录 | C:/Users/Administrator/.codex/worktrees/web-search-history-compat/dd |
| 本包所在目录 | C:/WORK-SPACE/dd/.trellis/tasks/10-19-claude-weekly-quota-architecture |

C:/WORK-SPACE/dd 的当前 HEAD 是旧版本，不能作为开发起点。对应生产的源码目录仅用于取证，不覆盖其已有文件。Sol 应先复核生产是否有新版本，再从匹配提交建立自己的隔离开发目录，复制本包。生产若变化，更新基线和受影响契约后再开发。

[生产快照](evidence/production-baseline.json)记录镜像、程序摘要、脱敏分组、表结构和迁移校验和。[代码索引](evidence/code-index.md)提供源码路径、函数锚点和文件摘要。[本地基线核验](evidence/local-baseline.json)记录源码与生产迁移的比对。

## 阅读顺序

1. [Sol 接手指令](sol-handoff.md)：开发边界、起点、每次迭代的交付规则。
2. [需求与验收](prd.md)：用户已确认的产品行为和不变式。
3. [总体设计](design.md)：模块边界、请求流、兼容策略。
4. [数据模型](data-model.md)与[结构草案](contracts/schema.sql.example)：字段、约束、生命周期。
5. [计费协议](billing-protocol.md)：锁顺序、准入、结算、恢复、跨期和幂等。
6. [接口契约](api-contracts.md)与[接口描述](contracts/openapi.json)：管理、用户、现有接口兼容。
7. [跨模块集成](integration-contracts.md)与[前端契约](ui-contracts.md)：支付事务、维护与清理、后台逐用户开卡、用户额度显示。
8. [迭代实施计划](implement.md)：I0 至 I6 的依赖、文件和退出条件。
9. [测试矩阵](test-plan.md)、[需求追踪](traceability.md)与[迁移及发布](migration-rollout.md)：工程验收和回退边界。
10. [架构决策](decisions.md)、[运维与对账](operations.md)、[设计审查](evidence/architecture-review.md)。

## 核心决策

- 复用现有用户、分组、Key、价格计算、订阅父记录和订单系统；新增独立的订阅周期、请求凭据、用量流水和管理操作记录。
- 通过分组 subscription_quota_mode 区分原生 windows 订阅和新 term 订阅。新模式只支持 anthropic 订阅分组；不复用拼车表存 Claude 数据。
- 一个新请求只选择一个计费来源。即使费用为零，来源仍然明确。到期、额度耗尽、配置缺失和恢复失败均不能回退到 GPT 或普通余额。
- 继续使用现有后扣费语义。耗尽后拒绝新请求，已准入请求的实际费用可能超过卡面额度；超出部分只记录原卡，不抵扣下一张卡。严格零超额属于另一个需要上界估价或预占协议的迭代。
- 七日卡从开通或排队续卡的起点开始连续 168 小时；续卡排在既有有效排期之后，剩余额度不结转。
- 已开卡额度不可被套餐修改或日周月重置重写。首版不提供中途改额度、余额互转、拼车加量卡兑换 Claude 额度。
- 先发布能识别新模式且能安全拒绝新流量的兼容版本，再启用周卡。激活后的常规回退目标是兼容版本，不能直接退回不认识新模式的 .13。

## 交付和实施状态

所有生产快照均为时间点证据。结构草案和 API 是拟实现的契约，不是已运行代码；不能把 SQL 草案放入迁移目录直接执行。测试矩阵是开发验收要求，不代表这些新测试已通过。

本仓库缺少 .trellis/scripts/task.py 和 get_context.py。本包按已有任务文件结构直接落盘，不安装或修复 Trellis，不设置当前开发任务，也不修改其他任务状态。task.json 的规划状态与业务代码实施状态分开记录。

无需为本方案引入新微服务、消息队列或缓存基础设施；继续使用当前 Go、PostgreSQL、Redis 和 Vue。完整功能按 I0 至 I6 分阶段交付，每阶段都要有可审查的代码、测试和兼容证据。

## 本包验证

交付前再次只读核验生产镜像及运行程序，见 [复核记录](evidence/production-recheck.json)。47 个源码锚点均已定位，297 条生产迁移与参考提交的校验和全部匹配。

[静态验证结果](evidence/validation-receipt.json)覆盖文档链接、JSON/JSONL、11 个 API 操作、27 个 JSON Schema、21 个契约示例及 18 项需求到 62 项业务验收的映射。[文件清单](evidence/package-manifest.json)提供 SHA-256；[工具说明](tools/README.md)提供重新核验方法。上述结果不包含 SQL 执行、业务测试或部署验证。

