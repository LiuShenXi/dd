# 研究基线

日期：2026-09-08。基线 HEAD：`35b42bb2b217f87059111bb5ef026c6beb93f192`。工作区已有拼车相关未提交变更；本任务不接管这些变更。以下为只读源码/官方文档研究，不是线上验收结果。

## 本地事实

路径相对仓库根目录，行号是本次读取时的定位，实施前应复查。

| 事实 | 源码位置 |
| --- | --- |
| 风控与审计总开关开启后，blocking 才同步；默认关闭 | `backend/internal/securityaudit/prompt_config.go:180`、`:347` |
| 同步两引擎并行，旧审核 Block 优先；Qwen 故障 503 | `backend/internal/securityaudit/coordinator.go:52`、`:94` |
| Qwen 包含九类，与 OpenAI 政策不同 | `backend/internal/securityaudit/prompt_qwen3guard.go:25` |
| Qwen 输出等级映成 0/0.5/1，Controversial 部分类别升为阻断 | `backend/internal/securityaudit/prompt_qwen3guard.go:158`、`:186` |
| 当前同步记录失败只计数和告警，不改变原决策 | `backend/internal/securityaudit/prompt_guard.go:153`；现有测试 `prompt_guard_test.go:249` |
| 旧 Moderation 请求错误放行，缺审核 Key 也跳过 | `backend/internal/service/content_moderation.go:1020`、`:1055` |
| 用户名和用户 ID 从鉴权上下文提取 | `backend/internal/handler/security_audit_helper.go:160` |
| 文本提取后重排；full_prompt 65536 字符封顶 | `backend/internal/securityaudit/prompt_snapshot.go:43`、`:78`、`:438` |
| latest_turn_only 会缩小扫描和保存范围 | `backend/internal/securityaudit/prompt_snapshot.go:460` |
| 同步/异步持久事件已有事务；队列有租约和版本控制 | `backend/internal/securityaudit/prompt_repository.go:143`、`:172`、`:221`、`:281` |
| 用户关联外键删用户后置空；需增加不可变 ID 快照 | `backend/migrations/181_prompt_audit.sql:7` |
| 当前事件原文字段为数据库 TEXT | `backend/migrations/182_prompt_audit_full_prompt.sql:1`、`backend/internal/securityaudit/prompt_repository.go:337` |
| 详情页直接取 full_prompt | `frontend/src/features/prompt-audit/components/EventDetailDialog.vue:97` |
| 原文 GET 不在敏感 GET 审计名单；一般管理审计是内存队列 | `backend/internal/server/middleware/audit_log.go:110`、`backend/internal/service/audit_log_service.go:70` |
| 已有同步审计仓储接口、AES-GCM 加密实现 | `backend/internal/service/audit_log.go:90`、`backend/internal/repository/aes_encryptor.go:22` |
| 密钥非固定时可能每次重启重新生成；没有现成 key ring | `backend/internal/config/config.go:1925`、`backend/internal/securityaudit/prompt_config_store.go:29` |
| 现有后台路由和接口可扩展 | `backend/internal/server/routes/admin.go:165`、`backend/internal/securityaudit/prompt_handler.go:14` |
| 已有路由与副作用顺序测试；结构断言不等于端到端验证 | `backend/internal/server/routes/prompt_audit_route_coverage_test.go:20`、`backend/internal/handler/security_audit_order_test.go:21` |

旧 `openspec/changes/add-openai-compatible-prompt-audit/design.md` 与 `verification.md` 仍包含“完整 Prompt 不入库”要求，与 migration 182 和当前源码不一致。保留为历史，不把其既有“通过”表格作为本次证据；实施时补版本化变更说明并同步相关规格。

## 外部资料

以下来源已在本任务前一轮读取，用于选型，不代表对本站数据的效果已验证。

- [OpenAI Moderation](https://developers.openai.com/api/docs/guides/moderation)：独立 `/v1/moderations`，`omni-moderation-latest` 支持文本和部分图像类别；官方接口免费。分类信号需由应用政策解释，不能覆盖全部使用政策。生成请求附带 moderation 的方式仍会生成，不能替代本任务的前置拒绝。
- [Qwen3Guard](https://github.com/QwenLM/Qwen3Guard)：Gen 提供 0.6B/4B/8B、119 种语言及方言、Safe/Controversial/Unsafe 与 Jailbreak 分类；可用 vLLM/SGLang 提供 OpenAI-compatible API。当前适配固定解析 Qwen 输出，其他模型不是只换 URL 即可。
- [Prompt Guard 2](https://github.com/meta-llama/PurpleLlama/blob/main/Llama-Prompt-Guard-2/86M/MODEL_CARD.md)：专门识别覆盖先前指令的意图，并不判断内容本身是否有害；窗口 512 token，中文效果需另测。
- [NeMo Guardrails](https://github.com/NVIDIA-NeMo/Guardrails)：输入/输出/工具规则编排框架，现有网关暂无引入必要。
- [LLM Guard](https://github.com/protectai/llm-guard)：官方 README 声明项目已归档，不作为新依赖。

政策映射实施时需要登记所采用的 OpenAI 使用政策版本和原文条目。本站规则、模型类别和政策条款分别维护；本次没有核验所有政策条款，不建立“任何模型 Unsafe 都等于 OpenAI 违规”的映射。

## 规划工具与验证边界

已读本仓库 Trellis workflow、backend/frontend 索引和跨层指南。`.trellis/scripts/get_context.py` 与整个 scripts 目录缺失，无法运行官方 task create/validate。为完成授权的设计工作，按现有 task.json 和三文档结构直接建立本任务；不重装 Trellis、不改已有任务指针。

后续使用隔离 PostgreSQL/Redis 和专用审核节点完成测试。任何历史测试端口、运行实例或线上开关均需重新核实；本轮没有连接生产，也没有执行业务测试。
