# Claude 与 GPT 用户并发隔离

2026 年 10 月 20 日补充，属于本周卡功能的必做范围。v1 只隔离额度，仍调用原用户并发池；本契约取代该接线方式。

## 当前版本结论

当天只读复核 main-server，仍为 .13 和提交 8d869d41f40d6eedbe2ee05aaf8973145cccbe4d。现有用户并发上限来自 users.concurrency，Claude Messages、Chat、Responses 与 GPT 都传同一个 user_id。Redis 活跃槽为 concurrency:user:用户ID，等待计数为 concurrency:wait:用户ID，因此同用户两类请求会争用并发和排队容量。

现有账号并发以 account_id 计数，可分别保护不同上游账号；按平台或组显示的运维并发是账号统计聚合。它们都不能替代用户的 Claude/GPT 独立限额。API Key 并发追踪当前也只是统计，不是独立的用户权益限制。

## 必须实现的行为

以合成测试参数 GPT 上限 5、Claude 上限 2 为例：允许同用户同时执行 5 个 GPT 和 2 个 Claude 请求；第 6 个 GPT 只进入 GPT 等待规则，第 3 个 Claude 只进入 Claude 等待规则。一边满额或排队满不能占用另一边用户容量。5 和 2 只是测试数值，不是要写入生产的配置。

同一用户所有 Claude term Key、分组和卡期共用一个 Claude 并发池，不能通过新增 Key、换 Claude 分组或续卡增加并发。并发是用户使用策略，不是卡的可累加额度；换卡不重置活跃槽。首版仍拒绝 Claude term 的 WebSocket 和其他未支持收费端点，GPT WS 保持原接入与回合槽规则。

## 范围与配置

采用两个明确 scope：legacy_user 与 claude_subscription。GPT 及其他原有未迁移模式使用 legacy_user 原有键和上限；只有可信 anthropic subscription term 分组使用 claude_subscription。scope 由鉴权后的真实分组模式决定，不能由客户端 model、请求路径或 Header 选择。

- users.concurrency 原字段及所有旧用户管理入口继续原语义，不修改现存数值。
- 新增 users.claude_subscription_concurrency 可空正整数，表示该用户 Claude 专用覆盖值；不接受 0 或负数作为不限额。
- 新增独立配置 default.claude_subscription_concurrency，必须是明确的正整数。实际值在启用时确定，不动态继承 users.concurrency。
- 有用户覆盖取覆盖，否则取 Claude 默认值；两者都未配置或非法时拒绝新周卡开通/下单及生成，返回 TERM_CONCURRENCY_NOT_CONFIGURED，不能降级旧用户池。
- 已付订单恢复不因运营暂时修改并发设置而丢失已购权益；恢复卡期仍执行，生成待配置恢复后放行。
- 管理用户编辑接口增加可选字段 claude_subscription_concurrency，缺省表示不改，不影响原 concurrency；单独审计变更。原批量 concurrency 修改不能顺手覆盖 Claude。
- 管理页面分别显示原用户/GPT 并发与 Claude 周卡并发；用户和 Key 页面分别显示两类当前使用及有效上限。汇总数只作统计，不作为两池共同的准入门槛。

限额变更后失效用户所有 Key 的 L1/L2 快照；等待中的请求重新解析所属 scope 的有效上限。下调上限不强杀正在处理的请求，后续只在活跃数低于新上限时准入。策略变更与正在发生的准入竞态需测试，不能仅更新表单。

## Redis 接口和键

保留旧 AcquireUserSlot/ReleaseUserSlot/IncrementWaitCount API 的 legacy_user 适配，新增带显式 scope 的统一入口。GPT 调用保持旧键，不能为了这次功能把所有用户并发键整体迁移。

建议新 Claude 键：

```text
concurrency:scope:{claude_subscription:user:123}:active
concurrency:scope:{claude_subscription:user:123}:waiting
```

花括号是 Redis hash tag，使该用户同 scope 的多键 Lua 原子操作可以处于同 slot。slot leaseID 与 waiting ticketID 均由服务端生成；释放闭包捕获原 scope、user_id 和 leaseID，不能结束时从可能变化的 Key 组重新计算。

新 Claude active 与 waiting 都用有 TTL 的 ZSET 成员；Lua 先按 Redis TIME 清理过期成员，再判上限并申请。等待成员需续租，成功、超时、取消、错误全部按本 ticket 幂等删除，不能使用可能重复减为负数的裸计数器。旧 GPT 结构和队列算法不在本次重写。

这里的成员 TTL 用 ZSET score 保存到期毫秒，并由脚本清理实现，不依赖 Redis 原生单成员过期能力。key 的 EXPIRE 只回收空闲集合，必须覆盖最新成员截止时间，不能因新短租约提前删除其他仍有效的租约。使用无用户输入的服务端 UUID，重试同一个租约 ID 不能多占容量。

仓储端口至少覆盖 TryAcquire、TryEnqueue、Promote、Renew、Release 和 GetScopeStats；返回明确的 acquired/full/missing/unavailable 状态，不用一个 bool 混淆满额与 Redis 故障。Promote 在同一 Lua 中验证等待 ticket 仍有效、检查 active 容量、删除原 ticket 并插入 active lease；不能先删等待再申请。Release 必须幂等，仅删除本租约；关闭心跳和释放使用 once 保护，清理使用有超时的独立 context，避免请求已取消导致清理直接跳过。

Claude 等待容量可复用现有纯计算函数 max(1, CalculateMaxWait(Claude有效上限) - Claude有效上限)，等待时限沿用现有用户等待超时配置，但计数和 ticket 完全独立。首版沿用有界重试等待，不额外承诺 FIFO；重试加抖动并受请求取消及总超时控制。排队期间已有 streaming ping 时，429/503 必须经现有流式错误封装终止，不能尝试再次写普通 HTTP 状态。

初始工程参数可采用租约 60 秒、心跳 15 秒，必须测试 Redis 延迟和长流；这不是容量结论。续租只延长仍存在的 lease，不得在丢租后偷偷插回计数集合。新 Claude Redis 申请失败返回 TERM_CONCURRENCY_UNAVAILABLE；续租持续失败或丢失时取消本次转发并走原用量核查/结算规则，不能当免费，也不能换 GPT 池。

每次 Redis 操作必须有短于心跳间隔的超时。客户端以最后一次确认续租为依据设置本地安全截止时间，留出取消与清理余量；不能无限等待 Redis 返回，直到服务端租约已过期仍继续发送新内容。由于进程暂停和网络分区，租约不能保证供应商立即停算；隔离测试要验证可控故障下及时取消、用量保全与恢复，不宣称故障期间物理执行绝对零重叠。

原普通槽默认 TTL 为 15 分钟，不能直接复制后不续租，使长 Claude SSE 尚未结束就被当空闲。worker 只清理新 scope 的过期成员，旧启动清理与 active index 不应误处理新命名空间。Redis 故障下保持新 Claude 拒绝策略，不改变 GPT 原故障语义。

## Handler 接线与释放

1. 鉴权完成后确定 scope 和上限，先获取所属用户槽，必要时只进入该 scope 的等待集合；不持 PostgreSQL 事务等队列。
2. 获得 Claude 用户槽后继续原账号选择和账号槽申请，不再申请 legacy_user 槽。两者需同时满足：Claude 用户上限和所选上游账号容量。
3. 账号排队完成、真正发送前，执行既定数据库周卡准入。卡过期或取消则释放所有已经申请的槽，不发上游。
4. 同组确定无消费的账号切换保持原 Claude 用户 lease；释放旧账号槽再取新账号槽。不得重复占用户槽，也不得换 scope。
5. SSE 与非 SSE 都在实际本地转发任务结束后释放一次。客户端取消触发上游取消及终结清理；不能仅收到取消信号便允许后台继续转发却提前释放用户槽。
6. 流结束后账务持久化与恢复按原协议继续；长期待结算 receipt 不永久占并发。并发统计指本网关活跃转发任务，不保证供应商在断连后已经停止内部计算。

Messages、Anthropic Chat Completions、Anthropic Responses 及全部 HTTP 别名均接同一 resolver/helper；错误出口、panic 清理、超时和熔断必须覆盖。不要只改主 Messages Handler。

## 源码接点

| 路径 | 现状或改动 |
| --- | --- |
| backend/ent/schema/user.go 与 service/user.go | 当前只有共享 concurrency；补 Claude 策略字段和映射 |
| server/middleware/api_key_auth.go 与 api_key_auth_cache_impl.go | AuthSubject 与快照传递 scope/上限；提升快照版本 |
| service/concurrency_service.go | 原 AcquireUserSlot 按 user_id；增加 scope-aware 入口和租约生命周期 |
| repository/concurrency_cache.go | 原用户键、等待键；增独立 Lua 与新键，不重写 GPT 键 |
| handler/gateway_helper.go | 原即时获取、等待、Key 追踪、释放封装；新分支禁止回落旧 scope |
| handler/gateway_handler.go、gateway_handler_chat_completions.go、gateway_handler_responses.go | 全部 Claude 入口接线 |
| handler/openai_gateway_handler.go | GPT HTTP/WS 原行为回归，不迁移其槽 |
| handler/admin/user_handler.go 与前端 admin/user/UserEditModal.vue | 独立设置、审计、两类当前数值 |
| service/ops_concurrency.go | 现有平台聚合保持原口径，另补用户 scope 视图 |

对应生产函数和摘要见 [源码索引 S48 至 S55](evidence/code-index.md)。实现阶段必须补齐 user repository、Ent 生成 DTO、管理员 create/update/list/detail、用户 self 与 Key 展示投影；只加数据库列或只改 AuthSubject 不构成接线完成。

## 迭代安排与启用门槛

I1 增量字段、配置校验、鉴权字段和缺依赖拒绝；I2 管理用户策略；I3 独立 Redis 槽、队列、续租、释放及所有 Claude 入口；I4 设置和双并发显示；I6 排空与最终镜像验收。

上线时已经按旧池运行的 Claude 请求要先排空，不能直接切换计数造成同一类新旧池各放一份容量。所有实例均支持新 scope 后才启用。回退镜像要识别独立 scope，或拒绝 Claude 新请求并保留旧请求清理；不能把 Claude 重新接回 GPT 用户池。保持原 GPT 活跃槽、WS ingress 和 live user 计数不动。

用户并发独立不等于物理资源无限独立。上游账号仍按 account_id 限制，服务器负载和上游供应商容量仍可能造成等待；不要为了达到用户并发示例而提高真实账号容量。
