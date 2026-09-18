# 0.2.6 渐进兼容升级：功能保留与验证矩阵

审阅开始于 2026-09-18，验证更新于 2026-09-19；源码兼容提交为 `8c3275d597cdcf381a30dee87fe0e2c9fa2773ef`。本文记录本地候选；没有执行生产部署。运行验收尚未完成的项目不得由源码检查或 mock 测试替代。

## 版本来源与覆盖范围

| 基线 | 固定提交 / 来源 |
|---|---|
| 上游 0.2.1 共同基线 | `578785ee7fb35030b094b69624efe25670a36f5f` |
| v0.2.2 | `5485f368b29d05adb95a00f71801c7c23d8f48af` |
| v0.2.3 | `8fa67d477d6651a744754392a8982ea589c26ae6` |
| v0.2.4 | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` |
| v0.2.5 tag 解引用提交 | `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea` |
| 0.2.6 保存 fork | [geniusywb/sub2api](https://github.com/geniusywb/sub2api)，`8b69738d782ccaa7fd26511e1cca26ba8d1b58db` |
| 0.2.6 发布合并提交 | fork HEAD 的父提交 `49a39b6dc1abed30fd227611e8af1108bc427610`；fork HEAD 仅进一步同步 VERSION |
| 发布合并第一 / 第二父 | `efe9aab1e4ec89a42ba45e8dac20e882c5409a6a` / `3c2f05c957b4b93866318ec8695fc5a28fff70eb` |
| 二开来源 | BWH 运行镜像 `sub2api:carpool-imagefix-20260915-gpt55`，源码标签 `dfb612ae3676c45ed7f2b334d7844d751bc0c292+image-main-gpt55`，本地后续发布安全文档 HEAD `197422403` |

v0.2.5 tag 所指树的 VERSION 仍是 0.2.4，后续 `881f3202694c6bc932446931a30c27d9675178b9` 才同步 0.2.5。判断来源使用提交与文件内容，不能只读取 VERSION。

当日查询官方 Release API、tag API 均找不到 v0.2.6（404），`git ls-remote` 的官方 main 在 `efe9aab1...`，但 fork 保留完整合并。公开 [PR #7315](https://github.com/Wei-Shaw/sub2api/pull/7315) 仍显示 merged / closed，讨论被锁定。已取得的作者和维护者公开评论没有解释撤回原因，**原因未知**；旧 Copilot review 不能当作撤回原因。

已检查 fork 固定提交是集成 HEAD 的祖先。在 `backend`、`frontend`、`deploy`、`Dockerfile`、`.github` 范围内，0.2.1→0.2.6 共 804 个上游变更路径：`7ac4f2bc5` 加兼容工作树的 2026-09-18 23:55 路径审阅快照中，738 个最终文件状态与 fork 一致，66 个与二开交叠；没有删除 fork 中现存的产品路径。此计数早于最终图片/Ollama测试夹具修正。对交叠路径逐项检查了接线、计费、允许模型、身份状态、品牌、图片与站点模式。此结果证明合并覆盖与静态保留情况，不等价于所有真实服务端行为已验收。

完整机器可读路径集合、重叠路径和源码审计保存在本次 evidence 目录：`C:\WORK-SPACE\sub2api-upgrade-0.2.6-evidence-20260918`，文件为 `upstream-feature-retention.json`、`upstream-0.2.6-audit.md`、`frontend-compatibility-audit.md`、`backend-compatibility-review.md`。以下矩阵按功能归并；完整提交清单可复现：

```powershell
git log --no-merges --oneline 578785ee7fb35030b094b69624efe25670a36f5f..8b69738d782ccaa7fd26511e1cca26ba8d1b58db -- backend frontend deploy Dockerfile .github
git diff --name-status 8b69738d782ccaa7fd26511e1cca26ba8d1b58db -- backend frontend deploy Dockerfile .github
```

## 0.2.6 重点功能

“单项 PASS”指本次候选测试日志中有该测试通过结果；“含全量前端 PASS”指对应文件包含在 310 文件 / 2361 测试通过的结果中。后端首轮环境/测试夹具问题修正后，全量 unit 复跑已通过：10992 个顶层测试、20147 个含子测试结果 PASS，19 个条件 skip，59 个有测试的 package PASS。

从最终 JSONL 按顶层测试名称另行提取的功能证据：Codex ticket 35、兑换历史 1（含 11 子例）、group summary 2、images 68、WS 456、OpenCode 40、allowlist 43、carpool 72 个 PASS，以上选择均 0 fail / 0 skip。它们是按名称筛选且可能重叠的测试集合，不能相加为全量数量；完整名称与 package 存于 evidence 的 `upstream-feature-test-evidence.json`。

| 功能与入口 | 源码保留 / 二开兼容 | 本次自动化证据 | 实际运行验收 |
|---|---|---|---|
| Codex 292 位票据采集与 1 小时缓存 | `service/openai_codex_ticket.go` 与 fork 一致；按账号+出站模型隔离；OAuth / setup-token 支持，credential shadow 豁免；wire 的构造和 cleanup 完整保留 | `openai_codex_ticket_test.go`、`openai_codex_ticket_lifecycle_test.go` 在全量中执行；关闭模式、运行开关相关单项 PASS | 本地配置验收见下两行；未使用真实 OAuth 凭据采集或声称票据有效性已实测 |
| 默认关闭、管理端即时开关 | config 默认 `enabled=false`；setting runtime 缓存与保存失效保留；采集、调度、注入分别检查有效开关 | `TestApplyOpenAICodexTicket_DisabledNoop`、`TestRefreshOpenAICodexTickets_DisabledSkipsHarvest`、`TestCodexTicketEnabledRuntimeSettingOverridesYaml` 单项 PASS；SettingsView toggle 测试含全量前端 PASS | 本地候选 UI 实测 OFF→ON→保存→reload ON→OFF→保存→reload OFF；没有开启生产开关 |
| 独立采集代理及热更新 | `setting_gateway_runtime.go`、admin setting handler、setting update 与 fork 一致；采集使用专用代理，业务沿用账号代理 | `TestSettingsCodexTicketProxyWriteReadAndHotReload`、`TestSettingsCodexTicketRejectInvalidProxyWithoutLeakingPassword` 单项 PASS；SettingsView 掩码显示与替换提交含全量前端 PASS | 候选 UI 确认合成代理密码掩码、保存后仍掩码；关闭时输入可编辑以提前配置。未使用真实采集代理 |
| 票据保密与后台刷新兼容 | raw extra 不进入普通 DTO / 列表 / 导出；客户端写票据被拒绝；行锁下保留最新票据；票据写为 scheduler-neutral；状态仅返回 ready/blocked/length/时间；DTO 二开只增拼车字段 | `account_repo_codex_ticket_test.go`、`admin/account_codex_ticket_test.go`、`setting_handler_codex_ticket_test.go` 保留；全量最终状态见验证表 | 待运行时 DTO / 导出验收；不输出真实票据 |
| HTTP / Messages / WS / compact 注入 | 所有调用链与 fork 保留；compact 按实际出站模型判门控；二开计费及准入在原有外围继续执行 | 上游票据/compact 用例保留；新增 WS allowlist+carpool、取消后结算测试 PASS（mock） | 待隔离请求验收；真实模型身份未经证明 |
| 用户兑换历史分页 | handler/service/repo/frontend API/UI 与 fork 一致；无分页参数保留旧数组；有参数返回分页对象；默认 1/20，上限 100；used_at DESC、id DESC；按认证用户隔离 | `TestRedeemHistory` 全子例单项 PASS（legacy/default/size_only/other user/cap/empty/invalid/overflow/unauthenticated）；`RedeemView.spec.ts` 含全量前端 PASS | 候选 390px UI 显示 3 条合成记录及总数，页大小20→50成功，单页前后按钮正确禁用；HTTP多页由运行负责人单独验收。未在浏览器新增兑换 |
| 分组用量汇总索引扫描 | `repository/custom_group_usage_rollup_repo.go` 与 fork 一致；先读水位，再向尾段查询传 `created_at >= $7`；沿用现有索引；无效水位回退 epoch | `usage_log_repo_group_summary_test.go` 的 tail/fallback 单项 PASS | 金额与并发集成验证由隔离 PostgreSQL 套件覆盖；生产规模 EXPLAIN / 1721 万行性能未复测 |

### 票据关闭与 fail_closed 的准确含义

`applyOpenAICodexTicket`、`openAICodexTicketBlocksAccount` 和 `refreshOpenAICodexTicketsContext` 都先检查有效 enabled 状态；关闭时分别直接返回、不阻塞账号、不枚举和采集。因此 `fail_closed=true` **不会使默认关闭的功能阻塞普通请求**。开启后，配置的目标模型在没有有效票据时才进入 fail-closed；没有采集代理也就无法取得新票据，应在启用前验证。

其他上游默认值：长度 292、TTL 3600 秒、提前刷新 600 秒、探测间隔 6 秒、单次 25 秒、目标模型 `gpt-6-astra` / `gpt-5.6-sol`。保存设置使当前实例缓存失效，其余实例依靠短缓存刷新。关闭不删除已有票据；它们不再注入并自然过期。proxy 的未提交、空值和掩码值都保留旧配置，UI 留空不表示删除。

当前实现仍有待实际环境评估的限制：只有进程内账号/模型 singleflight，没有跨实例采集锁或实例总并发上限；原始票据存于现有 accounts.extra JSONB；采集响应头验证不等于验证最终 `response.completed.model`。本文不将这些限制推断为已发生的故障，也不推断为撤回原因。

## 中间版本新增功能与跨层兼容

以下按 0.2.1→0.2.6 的最终树核对，避免依赖偶尔滞后的 VERSION 文本划分。

| 功能组 | 源码 / 合并决策 | 自动化与运行边界 |
|---|---|---|
| 分组模型 allowlist | 各网关入口、模型列表、分组管理、调度和 auth cache 均保留；二开 cache v25 同时保存每用户拼车投影和 allowlist，拒绝旧 v24 | 组合 allowlist+carpool HTTP/WS mock 用例 PASS；迁移真实验证见下文；实际 UI 待验收 |
| 推理强度映射拒绝、simple 模式基础分组 | 保留上游能力边界、非空分组删除保护、映射 reject；组类型标准/订阅/拼车并存 | 上游 tests 保留；前端全量 PASS；真实路由待验收 |
| OpenCode Zen / GO 平台 | 账号/组/模型、三种协议路由、mapped model、会话头、402 与计费处理保留；Key provider 筛选包含 OpenCode | `opencode_go*`、`openai_opencode_session*`、mapping tests 保留；KeysView 全量 PASS；无真实提供商调用 |
| MiniMax 平台 | 注册表、账号/组/监控/额度/阈值停调与前端平台选项保留 | 对应上游 tests 保留，前端全量 PASS；无真实 MiniMax 调用 |
| 站点类型三态 | 充值+订阅 / 仅充值 / 仅订阅的 settings、public config、featureFlags、billingMode、路由文案和页面开关保留；关闭普通订阅不清理或隐藏拼车业务 | billingMode / featureFlags / header/sidebar 等含前端全量 PASS；真实三态切换待验收 |
| API Key 批量编辑和 provider 筛选 | 上游批量操作完整保留；二开拼车组禁止用户自选，已指派 Key 的组只读；筛选计数以实际可选组计算 | KeysView / admin 用户组件含前端全量 PASS；候选 UI 确认已指派拼车组只读，新建只提供普通组，窄屏转为卡片；实际批量修改未执行 |
| 订阅批量操作、使用明细链接、并发延期 | API/UI、用户筛选、行锁串行延期保留；不把普通订阅规则套到拼车 | 相关前端套件 PASS；真实事务由独立 DB 套件和运行验收判定 |
| 批量删除用户、账号到期月/年预设 | handler/admin UI 保留，同时保留拼车审计/管理字段 | 前端全量 PASS；实际删除未执行 |
| 原生 Codex Images / Image 2.5 | `openai_images_direct.go`、payload、native generations/edits 与 Responses fallback 保留；二开 fallback 主控模型固定为线上已验证 `gpt-5.5`，图片模型不随之改写 | 第一轮 3 个 Luna fallback 旧断言按实际二开契约修正后，全量 unit PASS；无真实图片消费 |
| 图片计费与用量精度 | 新增 image cache price、ImageCacheReadTokens、实际尺寸/部分结果/流式取消处理保留；拼车 known-usage 判定补齐 image cache 正值和负值边界；八位小数展示保留 | 图片缓存成本与拼车结算 focused mock PASS：客户成本 0.00517000，倍率 0.8；负数拒绝且不回退普通余额；真实账本见独立 DB 验证 |
| 大图片请求内存与失败处置 | 保留上游减少大 Responses 图片分配、媒体准入/上游冷却/部分输出处理；自定义媒体准入与同步结算保留 | 上游 image tests 和二开 focused tests；真实大图负载未测 |
| WS 池、上下文和多轮次 | factor 默认 5.0；等待者按账号池变化重选；常驻读循环 ping；request_kind 执行作用域；HTTP bridge 隔离；预检/抢占/配额恢复/turn slot 保留 | dedicated/passthrough 两轮拼车测试、第二轮 allowlist 拒绝、取消后结算 focused PASS（mock）；真实 WS 待验收 |
| HTTP/2 PING、上游取消 | long stream profile、先取消再关闭 body 保留 | 上游 tests 保留；真实长连接压力未测 |
| 模型目录、manifest、最大上下文 | 可见目录、model_routing、固定账号发现、混合默认模型、display name、maximum context 保留；二开组/Key allowlist 一并投影 | 对应上游 tests 保留；前端完整性通过；真实各平台目录待验收 |
| Anthropic / Claude / Fable | Claude 版本下限与指纹、thinking beta、max_tokens=1 探针、mid-conversation output beta、Fable credits 按模型隔离保留 | 上游测试保留；无真实 Anthropic 请求 |
| Gemini / Antigravity | 3.7/3.8 Flash、价格、toolConfig、混合工具、plan 保留、账号级 token cache、带内错误与 finishReason 分类、SSE 空行修复保留 | 上游测试保留；无真实 Gemini 请求 |
| DeepSeek / Ollama / 协议桥 | output cap、Bearer auth、base URL 正规化、async reset、模型校验、system/developer 兼容、Responses done 参数/文本与 namespace 工具保留 | Windows 同 tick 测试夹具令旧 reset+1ms 构成真正新代际，仅改测试；全量 unit 复跑 PASS |
| Grok 媒体与工具 | 媒体资格、槽位释放、视频 ownership、映射、sequence_number、external_web_access 处理保留 | 上游 tests 保留；无真实 Grok 调用 |
| 价格、配额、调度 | DeepSeek peak / V4.1、GLM、Gemini、cache price、平台用量展示；Codex 规范窗口；持久 cooldown；model-not-found failover、粘性计数保留 | 对应上游 tests 保留；真实提供商价格/额度未重新核对 |
| 跨平台运维和监控 | token/request stats、TTFT、UTC bucket、排名隐藏、base URL path、自动刷新间隔、数据库日志限额保留 | 前端 ops / monitor tests 全量 PASS；实际长期负载未验收 |
| 支付、兑换和价格显示 | sanitized Markdown 帮助、可滚动续费、EasyPay 类型、履约与兑换限流隔离、10 分钟失败窗、折扣/峰价展示保留 | 对应前端测试 PASS；真实支付未发起 |
| 注册和认证 | 密码确认、验证码 loading、OAuth promo、临时服务故障保留会话、注册入口可见性、通知邮箱身份语义保留；品牌仍为二开默认 | 前端 auth/identity suites PASS；真实第三方登录未验收 |
| 自定义页面与管理交互 | 可拖动/可隐藏打开按钮、菜单视窗、代理 expiry/fallback/凭据清空/分页、部分批量结果、用量导出筛选保留 | 前端全量 PASS；真实外部页面与代理连通性未测 |
| 部署、备份和依赖 | Apple container subnet/web update、备份/迁移 advisory lock、Windows zip close、go-redis 9.22 和 grpc 1.83.2 等更新保留 | 初次备份 3 项测试因 Windows PATH 缺 sh 失败；本轮加入 Git/bin 后全量 unit 复跑 PASS；未进行生产部署 |

## 0.2.6 其余变化核对

| 上游增量 | 保留情况与证据 |
|---|---|
| 暂停 OAuth 账号仍刷新令牌 | repo 候选过滤变更与 fork 一致；仍受 active、平台、冷却条件限制 |
| Gemini native list 可发现 Antigravity 映射 | 列表与 allowlist / opt-in 同时保留 |
| Antigravity 归属 metadata 剥离 | 只针对 Antigravity system 开头，原生 Anthropic 未改 |
| 严格 Chat 上游 developer→system | 精确 provider / domain 行为、顺序和原请求隔离保留 |
| DeepSeek 工具输出图片与并行工具连续性 | 新媒体 user message 与延迟中间 system/developer 保留 |
| 取消后的 response affinity 持久化 | WithoutCancel + 有界超时保留，二开账单仍使用有界独立结算上下文 |
| Manifest 去重解析与 key 验证 | 两项均保留，没有仅去重而丢掉输入校验 |
| 公告部分成功 | Promise.allSettled 保留成功项；每个 callback 额外检查用户 generation，防止跨身份污染；同会话刷新只更新原提交 ID；新增竞态测试 PASS |
| 订单筛选页码、代理批测去重、数字跳页 | 原有上游实现与单测保留，含前端全量 PASS |
| TOTP 错误归一与数字同步、clipboard fallback 错误 | 原有上游实现与单测保留，含前端全量 PASS |
| 非法支付金额恢复、退款提示按实际申请金额、配置请求复用 | 原有上游实现与单测保留，含前端全量 PASS |
| 平台负配额拒绝、空模型标签允许 Tab、注册优惠码避免闪烁 | 原有上游实现与单测保留，含前端全量 PASS |
| 弹窗 title ID 唯一、订阅清理 loading | 原有上游实现与单测保留，含前端全量 PASS |

## 二开保留与迁移

- 拼车与普通余额继续分账，immutable term / admission / receipt / ledger / reconciliation 保留；管理端普通计费不套用用户拼车额度。前端未知、过期、真实零值与普通余额独立展示。
- 以最新已部署滚动补充机制为准：固定 28 天 term 到期、`next_natural_reset_at` 滚动周期、boost / manual carryover 和旧 fixed-cycle term 共存。没有按旧文档恢复固定七天逻辑。
- App 生命周期保留身份切换、退出清理、延迟登录检查、详情/公告轮询；公告新 allSettled 不绕过已有 race guard。KeysView、dashboard、header/sidebar、admin/user carpool、KeyUsage、登录/注册品牌已复核。
- 原生图片新路径与二开 `gpt-5.5` fallback 并存；新增图片缓存 token 纳入拼车已知用量判定，不能落入普通余额兜底。
- .5→.6 没有 schema/migration 变化；.1→.5 仍有 group allowlist、MiniMax、OpenCode 等迁移。二开相同数字前缀的迁移按完整文件名保留，已应用文件不可重写。新增 `234z_group_model_allowlist_compatibility.sql` 是前向兼容修复，不修改历史 checksum。
- 迁移负责人已报告隔离 PostgreSQL 100 个顶层 / 180 个含子测试用例全部通过，0 skip / 0 fail，真实迁移记录 291→297，并覆盖旧/新写入、并发、重放和拼车账本/汇总/DST；最终证据入口与命令以总验收记录为准。此项不代表生产数据库已经升级。

## 验证快照与范围限制

| 检查层次 | 当前证据 |
|---|---|
| 来源 / 合并 | 固定 fork、提交祖先、804 路径集合、66 个交叠路径复核完成 |
| 前端依赖 / 静态检查 | `pnpm install --frozen-lockfile`、typecheck、lint:check PASS；随后修改测试文件单独 lint PASS |
| 前端全部测试 | `pnpm test:run`：310 files / 2361 tests PASS，`frontend-tests-final.log` |
| 前端构建 | `pnpm build` PASS，包含 i18n 完整性 3 tests、vue-tsc、Vite；185 个 dist 文件；index.html SHA256 `10c2582dcef9b46c32a994fed2b353035ee827a81783a694b5854f0b876cd726` |
| 后端专项测试 | billing/auth/WS/取消上下文 focused PASS；票据设置/关闭、兑换分页、summary 单项 PASS（见 backend-unit-all.jsonl） |
| 后端全量 | 最终 PASS：10992 顶层 / 20147 含子测试 PASS，19 条件 skip，59 个有测试的 package PASS，`backend-unit-all-final.jsonl`。首轮备份 shell PATH、图片 fallback 契约断言和 Ollama 同 tick 夹具问题已修正后复跑 |
| 后端 vet | 协调者报告全套 PASS，`backend-vet.log` |
| 生产已有发行 CLI 打包 | `d3737ab99` 在标准 Dockerfile 编译并复制 `/app/carpool-release`；Linux/amd64 编译与无网络容器 `-h` PASS。未执行整个标准多阶段 Docker 构建，专用验收镜像不冒充正式发布镜像 |
| PostgreSQL 迁移 / 账本 | 协调者报告隔离套件 100 顶层 / 180 含子测试 PASS，0 skip / 0 fail；最终输出入口见总验收记录 |
| 新 embed 候选实际 UI | 真实本地浏览器行为通过：票据保存/重载、代理掩码、admin合约展开、active/ordinary身份及额度隔离、Key分组限制、兑换页大小；1280与390宽度DOM稳定后无页面横向溢出。截图CDP超时，像素级视觉验收未完成 |
| 候选 HTTP / WS 与旧 binary 回滚 | 协调者报告本地运行验收和真实旧版回滚 PASS、candidate 已恢复；具体请求/计费边界和证据以运行报告为准，不等同真实提供商验收 |
| 真实模型、票据采集、支付和生产 | 未执行，不属于已通过范围 |

浏览器验收详情及页面结构证据在 `C:\WORK-SPACE\sub2api-upgrade-0.2.6-evidence-20260918\ui\browser-acceptance.md`。active用户显示550拼车额度且隐藏其123普通余额；切到ordinary后显示77且没有残留550。稳定时窄屏Keys为390/390，details/dashboard/redeem为382/382（scrollWidth/clientWidth，含垂直滚动条）；desktop为1272/1272。捕获的4条console error均来自既有Chrome扩展的postMessage，未观察到候选应用error。截图API反复超时后停止重试，没有声称截图/像素视觉验收通过。

最后票据恢复OFF、站点模式未改、viewport已reset，预览停在合成ordinary用户仪表盘。站点三态逐一切换、未来/已过期用户分别登录、全部厂商管理表单、真实票据/模型/支付、生产规模性能和生产发布均不在本次浏览器通过范围。浏览器行为、单元测试、独立SQL和HTTP/WS报告应各自按实测范围阅读。
