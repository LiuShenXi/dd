# Claude 拼车正式发布与蓝绿切换 — 2026-10-20

北京时间 11:58:50 发布完成。主服务器 `main-server` / `23.19.230.146`
的 `sub2api.monasapi.com`、`teamsub2.monasapi.com` 均由 green 服务。
版本 `2.8.13-monas-claude-weekly.1`；应用提交 `cace0104c`，后台恢复工具提交 `a7e6b6de6`。
分支 `codex/claude-weekly-quota`，本次未 push 或合并 main。

## 实际运行状态

| 项目 | 状态 |
| --- | --- |
| green | `sub2api2-green`，`127.0.0.1:29081`，healthy，restart unless-stopped，零重启/OOM |
| blue / 原 R0 | `sub2api2`，原端口 29080，已停止，exit 0，restart no，容器保留 |
| 镜像 | `sub2api:2.8.13-monas-claude-weekly.1-cace0104c6bb` |
| 镜像 ID | `sha256:1ef6ac26fd2405baa50ca36d6b6629bd2d19c736e8e85702902220c6aab152c0` |
| 程序 SHA256 | `68f7a0b6414e6b8586f8e3b4631cb67b998cf2a581b22b6e3a0388ed7391f958` |
| 主机 Nginx | 1.30.5，master 未重启，平滑重载共享 upstream |
| 数据服务 | 原 postgres/redis 容器 ID、启动时间与重启次数保持不变 |
| 数据库 | PostgreSQL 18.6，297→307 项迁移，307 项 checksum 全部匹配代码 |
| 后台任务 | coordinated / active / leader=true；安全恢复 timer 已启用 |

镜像保留已核验的 R0 基础层及资源。继承的 OCI version label 仍为 `.13`；
以不可变 ID、程序 `--version`、构建提交和实际界面版本识别本次版本。

## 切换与验证

原服务器没有蓝绿运行配置。本次新槽启动保留 Redis 并发状态，后台保持 held，
旧容器继续服务；共享 upstream 同时切换两个域名，原 TLS、HTTP/2、WebSocket
及超时/缓冲设置保留。首次等待 90 秒仍有旧连接，控制器返回 deferred，
保留旧 HTTP gate 和容器运行。刷新证据后继续等待自然排空，没有强制结束连接。

HTTP、账务、队列、后台任务及外部异步图片/批量图片/备份/恢复全部归零后，
关闭原容器自动重启并停止。R0 于 UTC 03:58:49.778 正常退出，03:58:50.326 新后台激活。
UTC 03:55:12–04:02:45 两域共 450 次健康采样全部 200；这只是该窗口健康采样，
不代表逐条审计所有客户请求。两域未认证模型接口 401、管理实例 ID 与 green 一致，
每域 6 个主页面 JS/CSS 字节哈希与 green 一致；实际浏览器管理页显示新版本。

同镜像隔离演练通过切流、故障回退、HTTP/SSE/WS/TLS H2、晚到 keepalive/H2 请求
及 pending_usage 排空。7 次 GPT、7 次 Claude 精确结算，无重复扣费或上游取消。
追加验证新槽重启后的 held→精确激活、真实令牌轮换及重复执行无副作用。
隔离 Nginx 是 arm64 1.27.1，生产是 1.30.5，不混称同一二进制环境。
失败尝试、修正原因及不同源码/镜像的历史验收范围均保留在收据中。

数据库 dump 及配置/数据归档已验证，服务器私有备份：
`/root/sub2api-release-20261020/backup/`。上线后不得覆盖恢复旧账本快照。

## 后续操作

- 应用配置 `/opt/sub2api2/blue-green/compose.json`：两个槽都有显式 profile，
  始终按槽 `up -d --no-deps <slot>`，不要同时启动两个槽。
- 控制器 `/opt/sub2api2/blue-green/scripts/host-release.py`；
  私有配置/状态 `/opt/sub2api2/blue-green/private/{config,state}.json`。
- 共享 upstream `/etc/nginx/conf.d/00-sub2api-release-upstream.conf`；
  原站点仍为 `sub2api-migrated.conf`、`teamsub2.conf`。
- 原 `/opt/sub2api2/docker-compose.migrated.yml` 已只保留 postgres/redis，
  数据服务定义逐字段一致，原文件私有归档为 `.retired`。不要重启原 `sub2api2`。
- 下次使用 blue 前保留并改名原停止容器，准备支持 preserve/coordinated 的候选，
  更新 blue 不可变镜像 ID 和 `preserve_mode_required=true`，再定向启动。
  使用同一控制器切流/排空；详细契约见 `deploy/blue-green/HOST_RELEASE.md`。
- 原进程仍运行时可控制器 rollback；原 R0 退休后不可重启回退，必须使用支持共享状态
  保护和后台交接的兼容版本。不要使用旧 `deploy.sh` 或历史 BWH 流程。

`sub2api-background-reconcile.timer` 已 enabled/active，每分钟一次，开机 90 秒后开始。
它与发布控制器共用锁，只在已完成发布、旧实例精确身份停止且 restart=no、当前 HTTP open、
两域匹配时恢复后台。不会重启容器、切换 Nginx 或解除迁移锁。生产手动运行返回 already_active，
正常提前续期真实通过。token pair/access token 仅在 root 私有目录，0600；
凭据撤销、刷新不确定或未知状态会拒绝恢复，需正常登录补充 pair，不能伪造令牌或清标记重试。

## 业务边界

本次发布代码和部署设施，没有转换真实分组或发放额度。issue/admission=false、recovery=true；
真实 term 分组、term 卡和 rollout 行均为 0。启用仍需目标分组、明确用户/额度清单和销售配置。
没有替用户发起付费上游验收。用户提供的现有管理员密码正常登录成功，没有重置密码；
项目、知识库和常驻记忆不保存密码、API Key 或令牌。

## 证据入口

- `production-20261020-release.json`：真实退休/激活、脚本哈希、450 次健康采样。
- `production-20261020-final-verification.json`：最终容器、两域、迁移和业务状态。
- `production-20261020-build.json`、`production-20261020-image-transfer.json`：构建/传输。
- `production-20261020-backup.json`、`production-20261020-token-refresh.json`：备份/续期。
- `production-blue-green-runtime-receipt.md`、`blue-green-background-reconcile-runtime-receipt.md`：
  真实协议、账务、重启恢复及隔离环境清理。

Trellis CLI/scripts 和开发者身份缺失，仅维护当前任务元数据与收尾记录，不伪造他人 journal。
