# Excel / BPS 协议（OpenAI OAuth）

在账号管理 → 编辑现有 OpenAI OAuth 账号 → 打开“Excel / BPS 协议”并保存。使用该账号已有的 ChatGPT access token/account ID，不需要 GitHub 登录、sidecar 或新建 API Key 上游账号。原有凭据刷新逻辑继续生效。默认关闭；切换后新开 Codex 会话。

本入口面向 HTTP `/v1/responses` 和 `/v1/responses/compact`。强制上游 HTTP/SSE，优先于账号的自动透传、WS mode 和 Codex ticket 注入。保持原始模型名或显式账号映射，不因模型权限不足偷偷切换模型。现有调度、分组授权和并发额度继续生效；开关不会重新启用已停用的账号。

## 工具调用与并发

- 客户端 tools 转成 developer 消息中的目录. function 使用 `run_officejs.code` 内的 JSON 信封; custom 可使用显式 `codex2api.custom/<完整工具名>` 标记传递原始文本. 服务器只做协议转换, 不执行 OfficeJS 或客户端命令.
- 支持 function、custom、namespace、Lite additional_tools，保留 custom 原始文本与 JSON 大整数。
- 文本增量实时转发；工具等完整 response.completed 到齐后统一验证，失败、不完整或未知工具不会被提前发送执行。
- 完整原生 item 按账号、API Key、线程作用域缓存。回放保留上游 ID、summary、references 等内容；多个完整工具和乱序结果按 call_id 配对。
- 子线程优先使用 thread-id / x-codex-turn-metadata，不因共用父会话 session_id 混用缓存；没有设置全账号串行锁。并行子代理仍受账号并发数、调度及上游能力约束。
- 提示词仍要求每个 transport 内只放一个工具对象，响应声明 parallel_tool_calls=false。多工具转换与子线程隔离已有离线回归，不等于真实 Codex 多代理工作流已完整验收。
- 缓存位于当前进程，有条数和内存上限。重启、跨实例、换账号或淘汰后的缺失原始调用会报错，不能恢复任意旧线程。

### 下游转发与错误定位

- 模型将单个工具写成 `functions.shell({"command":"pwd"})` 时, 适配器按完整调用解析工具名和一个 JSON 字面量参数. 允许可选的 `await` / `return` 和末尾分号, 工具必须已在客户端目录中声明. function 参数必须为 JSON 对象; custom 参数必须为 JSON 字符串, 解码后原样交给客户端. 此处不运行 JavaScript.
- 不从脚本, 多调用, 数组, 未闭合调用或带尾随内容的字符串中截取工具对象. 这类返回会产生 `basispoints_protocol_error`, 不会先发送其中一个工具执行. 未带明确工具身份的原始代码仍会被拒绝.
- `basispoints_request_invalid` 的内容错误附带分叉接收到的字段位置, 例如 `path=input[2].output[1]; type=input_file`. 位置同时覆盖消息内容和 function/custom 工具结果. `input_image` URL 校验失败也附带位置.
- 错误仅显示固定的已知协议类型名称; 任意未知类型归类为 `unknown`, 缺失类型或非对象内容分别标记. 不回显正文, 图片字节, URL 或任意自定义 type 值. 不支持的文件, 音频或其他内容仍返回明确错误, 不会静默丢弃或伪装为文本.
- `supports text and HTTPS input_image content only` 表示内容分块类型不被支持, 单凭这条错误不能判断为 base64 转换失败. 下游应保留完整错误消息及请求 ID, 以便定位字段而不需要保存客户正文.

## 能力限制

`max`/`ultra` 映射 `xhigh`，`none`/`minimal` 映射 `low`，实际 effort 出现在响应/用量中。拒绝强制指定工具、托管工具、结构化输出与仅 previous_response_id 的增量历史。不要把 HTTP 200 当作模型能力证明。

仅转发 HTTPS 图片 URL，不上传本地图片，不接受 base64/data URL 或 file_id。本地请求校验通过不等于上游视觉已验收。CPA 参考项目也记录了实际图片 422。

原始协议来自 hloolx/codex2api：9d02d3f5 → c125e560 → 20ff3e86 → d39f7e36 → 4dea83ec（含中间依赖修复）。另外对照 JaxsonWang/cpa-plugin-oai-basispoints 05b2d97 的工具目录、信封和回放实现。出处见 `backend/internal/service/basispoints/NOTICE.md`。

验证区分：账号 300 的 gpt-5.6-sol 直连 BPS 糖果题返回 21；这只是文本上游验证。PR 中完整 Sub2API 转发、工具回放、多个子线程使用 mock 回归，尚未将该改动部署到生产。

## 本次兼容更新边界

本候选以本地 `0.2.7` 二开线为基线，回移上游 `v2.8.11` 的 Excel / Basis Points 协议、账号测试路径、稳定调用 ID、错误处理和相关前端配置；版本标识为 `2.8.11-monas-bps.1`。保留本地 carpool、billing、Sentinel、品牌和现有迁移，不执行数据库迁移。

上游与本地分支存在大量结构差异，本次没有整 tag 覆盖，也没有零散带入 harvest、Mihomo、Pelican/quality、分组计费限制、Responses admission 或其他依赖后续迁移的功能。候选通过本地编译、服务端回归、前端检查后再等待人工决定是否上线。
