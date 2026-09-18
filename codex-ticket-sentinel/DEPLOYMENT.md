# 生产部署记录

2026-09-19 修正版 r2 已部署到搬瓦工，与 Sub2API 同机运行。

- 本地子目录：`C:\WORK-SPACE\dd\codex-ticket-sentinel`
- 服务器服务目录：`/home/linuxuser/apps/sub2api/codex-ticket-sentinel/`
- 发布与回滚资料：`/home/linuxuser/apps/sub2api/releases/codex-ticket-sentinel-20260919-r2-retry1/`
- Sub2API：`sub2api:carpool-sentinel-0.2.6-f8f099c1d`，当前 **blue**；上一版 green 已停止保留。
- 哨兵：`codex-ticket-sentinel:20260919-v2`，独立 Compose 项目 `sub2api-ticket-sentinel`。
- 监护范围：账号 **2（OAuth）**，`gpt-6-astra`、`gpt-5.6-sol`；采集使用 **US-static-car8**。
- 票据总开关已恢复开启；`GATEWAY_OPENAI_CODEX_TICKET_FAIL_CLOSED=false` 已生效。

## 已实现

哨兵接收 312 响应头和上游原始 `response.completed` 模型不匹配的脱敏事件，同时检查票据缺失和提前十分钟到期，提交有界刷新任务。控制器通过专用鉴权接口调用 Sub2API 原生采集器，不持有 OAuth、数据库或 Redis 凭据。

有可用 292 时继续注入；缺票或票据过期时正常转发，不因这个机制阻断调度或请求。新票供后续请求和新建的上游 WebSocket 连接使用，不重放业务请求，不强制断开在途连接。相同有效票据再次采集到时，沿用原生续期语义，并核对数据库与缓存中的新采集时间；新异常不会被旧任务覆盖。SQLite 保留任务、冷却和退避，单目标每小时最多六次哨兵尝试；原生采集器另有自己的周期。

## 验证与实际票据结果

Python Windows 和 Linux 各 34 项测试通过，Linux 使用实际 v2 镜像代码；Go 专项测试覆盖续期、过期、缺票放行和异常事件。正式 Sub2API 镜像在独立新数据库/Redis 中通过启动、鉴权和主开关关闭验收，测试资源已清理。

生产已先排空请求和待落账任务，再备份、串行切换。297 条迁移与受保护的拼车、账户、计费数据指纹保持不变；健康检查 200、未认证模型接口 401，公网版本与三个前端资源通过核对。管理接口实测两个目标的 `blocked=false`，并非只核对环境变量。

2 号 Astra 的哨兵自动刷新已尝试但失败，随后人工验证被冷却机制拒绝（429 / cooldown），未发起第二次上游采集。这不影响缺票时正常转发。

此前一次原生有界探针确认 US-static-car8 返回 **HTTP 200、312 位票据**，没有返回 292。已核查服务器代理清单：另两条为 TW-static-pro20x、TW-static-car7，没有其他可确认的已配置美国出口，因此额外代理探测为零，没有更换出口。本次发布首次因关闭总开关时的状态验收条件错误触发自动回滚，校验修正后在 retry1 成功切换，数据库未回退。第一版曾因同票据拒绝续期和缺票拦截存在兼容问题；r2 修复续期并启用缺票放行。临时关闭的票据总开关已经恢复。

**292/312 是操作信号，不能据此保证模型质量、证明官方撤销含义或保证上游一定发新票。** 当前成果是哨兵和兼容策略上线，不能据此宣称已经解决所有降智、重连问题。验证记录在 `integration/*-r2.json`、`integration/revision2-validation.json`。

## 查看与停用

```sh
cd /home/linuxuser/apps/sub2api/codex-ticket-sentinel
docker compose --env-file deploy/runtime.env -f deploy/compose.example.yml ps
docker compose --env-file deploy/runtime.env -f deploy/compose.example.yml logs --tail 30 sentinel
docker compose --env-file deploy/runtime.env -f deploy/compose.example.yml stop sentinel
```

停哨兵只停止它的事件处理；关闭 Sub2API 管理端票据总开关才会同时停止原生采集和注入。回滚至上一版时先关闭票据总开关，防止旧版缺票拦截策略重新生效，再按本次发布资料中的排空、串行停止/启动、核验流程恢复；不恢复数据库覆盖上线后的计费数据。

兼容源码位于 `C:\WORK-SPACE\sub2api-sentinel-integration-20260919`，分支 `codex/codex-ticket-sentinel`；生产后端源码提交 `f8f099c1d2851d1cfe7416e7f41a5108d529079c`。当前项目保留控制器、完整后端集成补丁和脱敏验收记录。正式镜像由该提交的干净归档构建，未使用工作树中忽略的前端构建产物。
