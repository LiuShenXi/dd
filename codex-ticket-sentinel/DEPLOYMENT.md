# 生产部署记录

2026-09-19 已部署到搬瓦工，与 Sub2API 同机运行。

- 服务目录：`/home/linuxuser/apps/sub2api/codex-ticket-sentinel/`
- 发布与回滚资料：`/home/linuxuser/apps/sub2api/releases/codex-ticket-sentinel-20260919/`
- Sub2API：`sub2api:carpool-sentinel-0.2.6-6031d82f1`，当前 green；原 blue 已停止保留。
- 哨兵：`codex-ticket-sentinel:20260919-v1`，独立 Compose 项目 `sub2api-ticket-sentinel`。
- 监护范围：账号 2（OAuth），`gpt-6-astra`、`gpt-5.6-sol`；采集继续使用已配置的 `US-static-car8`。

哨兵检测 312 响应头、上游原始 `response.completed` 模型不匹配，以及票据缺失/临期，提交有界刷新任务。业务响应不重放、不改写；新票供后续请求和重新建立的上游 WebSocket 连接使用，不强制断开在途连接。

生产切换前已排空请求和待落账任务、备份数据库与配置。297 条迁移及受保护的拼车、账户、计费数据校验保持不变。切换后业务健康检查 200、未认证模型接口 401，公网版本及三个前端资源一致。Sub2API 与哨兵均 healthy，零重启、无 OOM；观察时哨兵约 33 MiB，限制 64 MiB。

验证包括：Python 30 项测试（Windows、Linux；Windows 另测优化模式）、Go 专项测试、实际 embedded 网页中间件与网关路由回归、正式镜像使用全新隔离数据库的启动和鉴权验收。默认 Go lint 通过。额外完整 embed 检查仍有原有的六处 Buffer.Write 告警，旧网页静态资源测试缺少 logo.png；相关限制在 integration/sub2api-validation.json 中记录。

## 实际续票尚未验证成功

部署后的两次 Astra 有界刷新均返回 `503 / collection_failed`，未替换原票。Sol 进入提前十分钟刷新窗口后，哨兵已自动建立并执行刷新任务，首次也失败并进入退避；紧邻该任务的人工检查被服务端 cooldown 拒绝。检查时两个模型的原票仍在本地有效期内。

因此，**已确认服务、监测与自动尝试链路运行，尚未观察到本次发布后成功获取并持久化新的 292 票据**。现有接口的 `collection_failed` 不能区分上游返回异常、未获得合格新票、重复票据等具体原因，不把它直接解释为代理失效。失败不会清空仍有效的旧票；任务继续按冷却、指数退避和每目标每小时六次的哨兵预算执行。Sub2API 原生采集器另有自己的周期，不计入该哨兵预算。

292/312 是操作信号，不能证明或保证模型质量。这个服务不能保证上游一定提供新票或一定返回所请求的模型。

## 查看与停用

```sh
cd /home/linuxuser/apps/sub2api/codex-ticket-sentinel
docker compose --env-file deploy/runtime.env -f deploy/compose.example.yml ps
docker compose --env-file deploy/runtime.env -f deploy/compose.example.yml logs --tail 30 sentinel
docker compose --env-file deploy/runtime.env -f deploy/compose.example.yml stop sentinel
```

停哨兵只停止它的事件处理与刷新任务。关闭 Sub2API 管理端的票据总开关，才会同时停止原生采集和注入。Sub2API 镜像回滚应使用本次发布资料中的排空、核验、串行恢复流程；不恢复数据库覆盖上线后的计费数据。

集成源代码位于独立兼容版本工作树 `C:\WORK-SPACE\sub2api-sentinel-integration-20260919`，分支 `codex/codex-ticket-sentinel`；生产后端源码提交 `6031d82f1d1081ac74ff58aba80b3cf2c2c0628b`。当前项目保留完整集成补丁、控制器和安全验收记录。
