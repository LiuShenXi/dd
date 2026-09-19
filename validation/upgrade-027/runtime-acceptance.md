# 0.2.7 Linux 合成运行时验收

2026-09-19，当前合并代码及最终前端已完成 Linux/amd64 实际启动、专用路由和拼车账务验收。仅使用全新合成数据库和 internal Docker 网络；没有读取生产数据、连接生产环境或调用真实模型上游。

## 结果

| 检查 | 实测结果 |
| --- | --- |
| 主程序版本 / 启动 | 0.2.7；完整 DI 初始化成功 |
| 基础及业务就绪 | `/health` 200；未认证 `/v1/models` 401 |
| 数据库 | 297 条迁移；使用全新 PostgreSQL 18、Redis 8 |
| Sentinel 专用接口 | 无凭据及错误凭据均 401；正确凭据 metadata 200 JSON；嵌入前端未截获路由 |
| 票据关闭状态 | 默认关闭；refresh 返回 `disabled`；OAuth 账号数 0，无真实采集 |
| 票据设置 | 不重启即时启停、代理密码掩码、掩码回传保留配置、非法 scheme 400 均通过；最终关闭 |
| 拼车 HTTP | 1 次 Responses 调用对应 1 张已结算回执、1 条账本借记、1 条用量 |
| 拼车 WebSocket | 同一客户端连接连续两轮，各自建立独立 durable admission 和结算回执 |
| 最终账务 | 3 张独立已结算回执、3 条借记、费用 3 USD；普通余额前后均为 47.25 |
| 上游 mock | 测试模型原生 `/v1/responses` 推理增量 3，Chat Completions 增量 0；后台探测及连接预热单独记数 |
| 进程 | 0 次重启、无 OOM；应用仅连接本次 internal 网络 |
| 发行 CLI | `carpool-release` Linux/amd64 构建通过；未执行数据库操作 |
| 清理 | 本次 label 对应容器、网络、卷均已移除，复核计数全部为 0 |

核心结果见 [runtime-summary.json](runtime-evidence/runtime-summary.json)、[HTTP/WS 账务证据](runtime-evidence/http-ws-probe-report.json)、[Sentinel 运行时证据](runtime-evidence/sentinel-runtime.json)、[实时票据设置](runtime-evidence/ticket-settings-http.json) 和 [清理回执](runtime-evidence/cleanup.json)。本报告没有把模拟上游等同于真实 OAuth 票据有效性或模型质量验证，也没有把此候选验收镜像等同于生产部署。

## 构建来源与边界

合并提交已完成：`a52841d46670d6c040a18d278b13457e3319eb0c`。提交后重新验证全部构建输入逐文件哈希一致，且 backend/frontend 已跟踪源码无未提交差异；映射回执见 [source-commit-binding.json](runtime-evidence/source-commit-binding.json)。它绑定实际测试输入，不改写旧二进制的构建元数据。

- 候选镜像：`sub2api-upgrade027-app:candidate`，ID `sha256:63e822c268bc4adb0f4e99416dc94c83fe7ea8ede0960cd66c61cb6fea6646ce`。
- 应用二进制 SHA-256：`76e8c8a12f59c39259c5325f78b96e55095526e7ea40e70b542300ee9202c543`。
- 当前生产源码及嵌入前端输入集合 SHA-256：`ed8abebad9280551ef8d65f3c4c934512a540f9042d978cea3e4e40470ab9f1a`。
- 嵌入前端 `index.html` SHA-256：`193c2d73df5360a4ec7999138074b594309af0c62c21c1045b7959352b0a55e6`；前端文件 185 个。

构建发生于三方合并尚未提交时，HEAD 仍为 `2fefcf51f0e00618686ab825d81e65444ee1ccfd`，MERGE_HEAD 为 `7484192016807acf55c6ef4f2827d371eb61ec1c`。二进制 Commit 字段使用旧 HEAD 加 `upgrade027-local` 后缀；它不能单独代表验收源码。完整 [build-manifest.json](runtime-evidence/build-manifest.json) 对逐文件内容及嵌入前端取哈希，并在编译前后、启动前及验收清理后验证一致。后续 merge commit 应映射到这一相同输入集合，不能把旧 HEAD 当作最终 0.2.7 源码提交。

本轮在已有 026 合成 harness 上进行限定名称/路径/并发参数替换；副本只生成在私有目录。公共入口为 [run-runtime.py](run-runtime.py)，原框架及历史证据没有修改，来源哈希见 [harness-provenance.json](runtime-evidence/harness-provenance.json)。实际编译限制 `GOMAXPROCS=4`、`go build -p 4`。

私有目录是 `%LOCALAPPDATA%/Codex/PrivateTests/sub2api-upgrade-027-20260919/runtime`；随机凭据、token、可执行文件、原始进程日志仅保留在那里。应用入口为临时 `127.0.0.1:38627`，清理后不再提供服务。构建镜像和私有证据保留，不进行全局 Docker prune。

首次启动因本轮 token 初始化器先创建卷导致卷根目录为 root，应用 UID 1000 无法创建配置；已修正本次初始化器的属主后，通过受控 cleanup 删除首次全部合成资源并重新创建空数据库。此过程及边界保留在 [harness-start-retry.json](runtime-evidence/harness-start-retry.json)。另外修正了验收脚本 refresh 请求中的字段名，最终使用真实契约 `expected_ticket_version`；均未修改产品实现。

复跑顺序：`python validation/upgrade-027/run-runtime.py` 依次传入 `prepare`、`build`、`start`、`sentinel`、`settings`、`probe`、`check`、`cleanup`。必须先完成前端构建并冻结待测业务源码。创建流程拒绝复用已有同名资源，清理流程验证 task label、记录 ID、网络关联和卷引用。
