# Claude 周卡迭代实施计划

## 总体执行规则

依赖顺序为 I0 → I1 → I2 → I3 → I4 → I5 → I6。I1 包含可安全回退的兼容能力，I4 是手动开卡完整闭环，I5 完成在线售卖，I6 才进入生产启用。

每次只交付一个明确迭代，不把源代码升级、模型定价调整、代理变更和本需求混入一个提交。每个迭代交付变更文件、数据库影响、实际执行测试及结果、未解决问题和可回退版本；不能把计划测试写成通过。

v2 并发隔离是必做范围，详见 concurrency-isolation.md：I1 增加 users 独立并发字段、配置和鉴权快照；I2 增加管理设置及审计；I3 增加 Claude scope 槽、等待集合、续租、释放及三种 HTTP 路由接线；I4 展示两类并发；I6 完成新旧池切换排空。相关 Redis 和 Handler 测试 T63 至 T74 纳入各阶段，不能只完成周卡数据库计费后宣称双权益完全隔离。

## I0 固定基线与实现映射

**目标：** 保证开发起点和生产一致，确认准确的 Handler、Repository 和构造函数接点。

- 再次核验 main-server 的应用镜像、revision 和运行程序 SHA-256。
- 从匹配提交建立独立开发目录；不要在本包所在旧 HEAD 上直接实现，也不要覆盖其他聊天的工作区。
- 运行本包本地基线核验工具，确认所研究文件和迁移摘要。
- 读取适用的 carpool-contracts、carpool-state-contracts、release-safety 和当前实际发布资料。
- 从真实 Router 列举 Claude Messages、Chat Completions、Responses 和别名；记录到实现映射。
- 确认 pricing snapshot 能覆盖现有 Claude 模型、缓存计价、用户分组倍率、返回模型计价和当前 Kiro 路径。
- 把 testcontainers harness 指向隔离 PostgreSQL，匹配生产 PostgreSQL 18；不使用生产 DSN。
- 建立 R0 现有 GPT 回归基线，记录已有失败，不为本任务修改无关故障。

**退出条件：** 来源和工作区清楚；重要接点已确认；原 GPT 回归基线可以重复；技术未知不再靠猜测。

## I1 增量结构与兼容拒绝版本 R1

**目标：** 新结构可存在，旧功能不变；新模式即使尚无实现也不会落入旧余额路径。

预期文件：

- 新增 backend/migrations 的增量 SQL，按 schema.sql.example 拆分。
- backend/ent/schema/group.go、user_subscription.go、subscription_plan.go、payment_order.go、usage_log.go 及生成产物。
- backend/internal/service/group.go、user_subscription.go、api_key_auth_cache_impl.go 及快照 DTO。
- backend/internal/repository/api_key_repo.go 和 carpool_repository.go 的窄范围共存修复。
- backend/internal/server/middleware/api_key_auth.go、api_key_auth_google.go、billing_cache_service.go 的模式拒绝。
- 旧订阅 assign/extend/reset/revoke/restore、批量、兑换、退款和支付入口的 mode guard。
- wire.go 和默认关闭的新模块配置；部署候选文件中的显式开关。

增加 quota_mode 的反序列化默认值和缓存版本。R1 中 term 默认返回服务不可用，绝不能按原生 windows 或余额处理。仅配置为 term 的 Claude Key 可与 GPT 并存，普通分组绕过仍被拒绝。

对准备切换的 Claude 分组提供发布保护清单：在标准模式阶段也执行最终数据库状态校验并记录在途请求数，用于精确排空；生产启用前全部服务副本都必须具备这项能力。

**退出条件：** 空库升级、含旧账本升级、迁移重放测试通过；新模块关闭时 GPT 行为及金额相同；新模式拒绝测试和双 Key 共存测试通过；R1 有可构建镜像和回退收据。

## I2 卡期领域与后台操作

**目标：** 实现开卡、续卡、取消、摘要和幂等，暂不开放生成。

新增 domain/subscription_quota.go、service/subscription_quota_service.go、service/subscription_quota_port.go、repository/subscription_quota_repo.go、管理 Handler 和真实路由注册。命名可按目录习惯统一，但保持领域边界。

- 实现正额度字符串解析和八位规范化。
- 实现 scope 锁、父记录创建、固定 168 小时排期、不可变快照。
- 实现管理幂等、expected_revision、取消和父摘要更新。
- 实现当前卡、未来卡、历史查询与用户归属校验。
- 实现 outbox 缓存失效；不提供直接改当期额度接口。
- 继续拒绝旧 reset、extend 和 restore 对 term 的操作；旧窗口维护与到期提醒跳过 term。

**退出条件：** T01 至 T12、T24 通过；数据库证明并发开卡不重叠、重复操作不多开卡。T73 的独立设置、旧批量接口与缓存失效后端部分通过。

## I3 网关计费与恢复

**目标：** Claude 新模式在隔离环境完成准入、上游模拟、真实费用结算和恢复。

主要修改：

- 新增 service/subscription_quota_gateway_billing.go 和版本化 receipt。
- service/usage_billing.go 增加新来源验证及单独指纹版本。
- repository/usage_billing_repo.go 增加事务内 term 分支；旧三来源分支保持原指纹和账务效果。
- service/gateway_usage_billing.go、handler/gateway_handler.go、gateway_handler_chat_completions.go、gateway_handler_responses.go 接入不可变快照。
- 新增 handler/subscription_quota_gateway.go；同一 helper 在最终上游发送点调用。
- 新增恢复 worker、统计补写、日志清理兼容、指标和 wire 生命周期管理，具体见 integration-contracts.md。

完成所有新模式生成路径覆盖，阻断未支持端点及跨组 fallback。没有 repository 不能启用 legacy 多写兜底。已知费用先落盘，未知用量不计零。同一卡多个 Key 共享用量。

**退出条件：** T13 至 T44、T63 至 T71、T74 通过；断连、崩溃、重放和到期竞争能恢复；GPT HTTP/SSE/WS 原回归仍通过。并发用真实隔离 Redis 验证原子性，不能只测 helper mock。

## I4 后台与用户完整闭环

**目标：** 管理员逐用户填写额度，用户看到与实际账本一致的独立周卡。

- 新增开卡及续卡弹窗，预览、幂等键、revision 冲突和异步身份保护。
- 添加 quota_mode 判别类型及统一金额显示工具，不以浮点做金额结算。
- 扩展用户订阅卡、进度组件、用量明细与 /v1/usage。
- 保留 GPT 拼车卡、四周期展示、加量和重置行为。
- 适配中英文、窄屏、键盘和错误反馈。
- 管理端提供异常请求查询和受审计的核查界面，不能让用户任改账单。

**退出条件：** T45 至 T49 及 T73 的真实前端部分通过；最终候选镜像真实浏览器操作通过；A 100 USD、B 300 USD、同 A 多 Key 和双订阅交错用量的账本证据完整。

## I5 套餐和在线订单

**目标：** 默认额度进入商品配置，付款后按订单快照开卡。

- 扩展 payment_config_plans.go、订单创建、履约、DTO 和套餐 UI。
- term 商品固定七天且默认额度大于零；无效商品不能上架。
- 订单创建时冻结 schema_version、mode、group、platform、duration_seconds、quota_usd 及展示信息。
- 履约调用同一 OpenTerm 领域操作；payment_order_id 唯一，原订单租约和审核逻辑保留。
- 回调重复、进程崩溃、套餐变价、下架和删除均不能改变已付权益。
- 无快照旧订单走原 windows 路径；若目标已变 term，进入显式人工处理，禁止猜额度。
- 明确阻断未适配的兑换码及按旧天数模型撤权的自动全额/部分退款。现金退款不能被改成普通余额充值。

**退出条件：** T50 至 T55 通过；支付沙箱或项目既有可控 fake provider 的全链路通过，不能用真实用户付款测试。

## I6 发布演练和生产交接

**目标：** 完成结构、程序、配置和账本的可验证升级及回退。

- 隔离环境演练 R0 → R1 → R2、启用和 R2 → R1 的降级拒绝。
- 与真实部署方式核对，不照搬历史 BWH 蓝绿命令。
- 准备切换预览，列明现有 Claude Key 所属用户及计划额度；没有指定额度的用户不自动获得额度。
- 按迁移发布文档排空旧 Claude 请求，原子切换分组和明确选定权益，失效缓存并验证。
- 新收费请求验证必须取得用户明确指定的测试身份及消费授权；不能直接拿真实用户 Key 探测。
- 保存构建 revision、源文件摘要、前端资源摘要、迁移摘要和最终镜像证据。

**退出条件：** T56 至 T62、T72 通过，全部 74 项验收证据汇总；回退演练可保留最新账本。生产启用按实施会话最新用户授权执行，本架构包本身不构成发布授权。

## 验证命令

下面命令以独立开发目录为工作目录，先确认本机工具可用。后端集成 harness 使用 testcontainers，不填写生产地址。

```powershell
# backend 目录
go test -tags=unit -run '^$' ./...
go test -tags=unit ./internal/service ./internal/handler/... ./internal/server/...
go test ./internal/domain/...
go vet ./...
$env:SUB2API_TEST_POSTGRES_IMAGE = 'postgres:18-alpine'
go test -tags=integration ./internal/repository
# frontend 目录
pnpm typecheck
pnpm lint:check
pnpm test:run
pnpm build
```

先运行与当前迭代相关的测试，再在集成节点运行完整相关包。Race 检测在支持 CGO 的 Linux 构建环境运行关键并发测试。不要因 unit 命令通过就跳过真实 PostgreSQL、路由和最终镜像验收。

## 完成定义

每个 PR 描述具体触发条件、前后行为、验证和风险。契约变更必须同时更新本包和测试，不允许仅在代码注释里改变金额、续卡或失败回退语义。设计不要求新微服务、消息队列、数据库拆库或大范围文件重排。

