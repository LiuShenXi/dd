# Sub2API 当前生产环境运维手册

核验时间：**2026-09-10 17:39–17:42，北京时间（Asia/Shanghai）**。本手册面向后续接手运维的人和自动化任务。动态配置、额度、余额、活动槽位必须在操作前重新读取。

**生产账务与应用已迁到 `155.138.234.216`；旧机 `137.220.56.176` 仍保留兼容转发及其他业务。只允许新机承担 Sub2API 生产写入。**

本手册记录已部署状态，优先于项目中仍指向旧机的部署示例和 9 月 8 日发布设计。项目当前有未提交的开发改动，不能把本地源码或设计方案视为已经上线。本文不含密码、代理认证信息、API Key 或 OAuth Token。

## 1. 服务拓扑与资产

```text
用户 / API 客户端
  └─ sub2api.monasapi.com（Cloudflare 仅 DNS，A → 155.138.234.216）
      └─ 新机宿主 Nginx :443（TLS）
          └─ 127.0.0.1:8080 / sub2api-router
              └─ sub2api-blue:8080（当前活动槽）
                  ├─ sub2api-postgres（唯一生产账本）
                  ├─ sub2api-redis（缓存 / 调度 / 运行状态）
                  ├─ 账号 1 pro 20x → HTTP 代理 178.94.233.85:45614 → 上游
                  └─ 账号 7 七号车 → HTTP 代理 178.94.222.225:47536 → 上游

旧 DNS 缓存客户端 / 旧机 MonasAPI 内部 http://sub2api:8080
  └─ 137.220.56.176 上的 sub2api-router
      └─ HTTPS 155.138.234.216（Host / SNI = sub2api.monasapi.com，校验证书）
          └─ 同一套新机应用和账本
```

| 项目 | 当前值 / 用途 |
|---|---|
| 生产域名 | `https://sub2api.monasapi.com` |
| 管理后台 | `https://sub2api.monasapi.com/admin/accounts` |
| API | `/v1/responses`、`/v1/chat/completions`，客户端 Base URL 通常为 `https://sub2api.monasapi.com/v1` |
| 新服务器 | Vultr，`155.138.234.216`，SSH 用户 `root` |
| 系统 | Ubuntu 22.04.5 LTS，x86-64，内核 5.15.0-190-generic |
| 资源快照 | 2 vCPU，约 7.7 GiB 内存，8 GiB swap，根分区 469 GiB、已用约 17 GiB |
| 宿主时区 | UTC；应用 `TZ=Asia/Shanghai`，数据库当前显示 CST |
| 部署目录 | `/home/linuxuser/apps/sub2api` |
| 旧服务器 | `137.220.56.176`，SSH 用户 `linuxuser`，本机已有别名 `newapi-vultr` |
| 本地项目 | `/Users/shenxi/Desktop/WORK-SPACE/dd` |

### 登录方式

```bash
# 在运维电脑执行，使用受控渠道保存的认证材料
ssh root@155.138.234.216
ssh newapi-vultr
```

迁移期间新机使用已有认证建立了临时 SSH 复用连接，**未在本次工作中安装持久 SSH 公钥或新的永久别名**。`/tmp/sub2api-resume-mux`、`/tmp/sub2api-source-mux` 是当时会话的临时 socket，不是未来登录凭据。旧机既有私钥路径为 `~/.ssh/newapi_vultr_ed25519`，不要复制到项目或文档。

## 2. DNS、TLS 与网络入口

| 配置 | 当前值 |
|---|---|
| DNS 提供商 | Cloudflare，区域 `monasapi.com` |
| 主机记录 | A `sub2api` → `155.138.234.216` |
| 代理状态 | 仅 DNS（灰云），客户端直接连接新服务器；Cloudflare 不承担本域名 HTTP 反向代理 |
| TTL | 自动；权威与公共解析查询为 300 秒 |
| AAAA | 无记录，不应凭空添加 IPv6 地址 |
| 权威 NS | `anderson.ns.cloudflare.com`、`zara.ns.cloudflare.com` |
| 证书 | Let's Encrypt，ECDSA，当前到期 `2026-11-29 05:55:20 UTC` |
| 续期 | `certbot.timer`，webroot `/var/www/html`；迁移当天 dry-run 成功 |

[Cloudflare DNS 管理页](https://dash.cloudflare.com/483de0967124efc12cf69dfc4314c0b1/monasapi.com/dns/records)。此次只迁移 `sub2api`，不要连带修改 `api`、`origin-api` 或根域等其他业务记录。

新机宿主配置：`/etc/nginx/sites-enabled/sub2api.monasapi.com`；证书目录：`/etc/letsencrypt/live/sub2api.monasapi.com/`；续期配置：`/etc/letsencrypt/renewal/sub2api.monasapi.com.conf`；成功续期 hook：`/etc/letsencrypt/renewal-hooks/deploy/sub2api-nginx-reload`，执行 `nginx -t` 后 reload。

宿主和容器 router 都设置了流式响应相关参数：`proxy_buffering off`、`proxy_request_buffering off`、HTTP/1.1、Upgrade 透传、读写超时 1200 秒、请求体上限 256 MiB。80 端口保留 ACME challenge，其余请求跳转 HTTPS。

**当前入口边界：** UFW 已启用，规则允许 22/80/443；但是 Docker 把 router 发布为 `0.0.0.0:8080`，本次从旧机访问 `http://155.138.234.216:8080/health` 实际返回 200。因此不能声称公网只开放了 22/80/443。后续应单独评估将新机 router 发布地址收敛到 loopback；旧机兼容转发使用新机 443。本文编写期间未修改端口或防火墙。

### DNS 与 TLS 快速检查

在新机或不受本地 Fake-IP 干扰的网络执行：

```bash
dig @anderson.ns.cloudflare.com sub2api.monasapi.com A +norecurse +noall +answer
dig @zara.ns.cloudflare.com sub2api.monasapi.com AAAA +norecurse +noall +answer
curl -fsS 'https://dns.google/resolve?name=sub2api.monasapi.com&type=A'
curl --noproxy '*' --resolve sub2api.monasapi.com:443:155.138.234.216 \
  --connect-timeout 5 --max-time 15 -sS -o /dev/null \
  -w 'HTTP=%{http_code} DNS=%{time_namelookup} connect=%{time_connect} TLS=%{time_appconnect} first=%{time_starttransfer}\n' \
  https://sub2api.monasapi.com/health
openssl x509 -in /etc/letsencrypt/live/sub2api.monasapi.com/fullchain.pem -noout -subject -dates
systemctl status certbot.timer --no-pager
```

17:40 复查：两台权威 NS 和 Cloudflare、Google、AliDNS 公共解析都返回新 IP，AAAA 均为空。DNS 正确不代表客户端代理节点可用，也不代表上游生成成功。

## 3. 容器、文件与运行配置

| 容器 | 当前状态 / 端口 |
|---|---|
| `sub2api-blue` | healthy，活动槽；宿主 `127.0.0.1:18080` → 容器 8080 |
| `sub2api-router` | healthy；宿主 `0.0.0.0:8080` → 容器 8080 |
| `sub2api-postgres` | healthy，PostgreSQL 18.6（`postgres:18-alpine`）；5432 未发布到宿主 |
| `sub2api-redis` | healthy，`redis:8-alpine`；6379 未发布到宿主 |
| green 槽 | 新机当前未运行；预留本地端口 28080 |

上述四容器 restart policy 为 `unless-stopped`，本次核验重启计数均为 0。应用镜像标签为 `sub2api:carpool-production-85936e617284`，**镜像 ID** 为：

```text
sha256:f51c7f038dbfdcc1bac4e70363abfc188a0d1bd9b19bcb86da45903993e2ed27
```

这是运行镜像 ID，不等同于可从镜像仓库拉取的 RepoDigest，也不能凭标签推断本地工作区与线上源码完全相同。

| 路径（新机部署目录下） | 含义 |
|---|---|
| `.env` | 基础运行凭据，敏感文件；不要 `cat` 到聊天或提交 Git |
| `data/` | 挂载到应用 `/app/data`，包含配置及业务文件 |
| `postgres_data/` | 实际数据库目录；`SHOW data_directory` 核验为 `/var/lib/postgresql/data` |
| `redis_data/` | Redis 持久化目录，容器 `/data` |
| `blue-green/docker-compose.yml` | 应用槽位及 router 的部署定义 |
| `blue-green/slot-images.env` | 槽位镜像引用 |
| `blue-green/router/conf.d/default.conf` | 当前路由目标 `sub2api-blue:8080` |
| `run/active-slot` | 当前内容 `blue`；排查时应与实际 Nginx upstream 交叉检查 |
| `blue-green/scripts/` | backup、deploy、switch-slot、common、release-guard 等运维脚本 |
| `backups/automated/` | 自动备份集合 |

PostgreSQL 还存在一个挂载到 `/var/lib/postgresql` 的 Docker 匿名卷。实际数据目录已以上述 SQL 确认，不能仅凭卷名猜测数据位置；清理卷前必须重新检查，禁止 `docker volume prune` 式盲目清理。

当前应用关键环境：

```text
CARPOOL_BACKGROUND_ENABLED=false
BATCH_IMAGE_QUEUE_ENABLED=false
RELEASE_DRAIN_START_HELD=false
TZ=Asia/Shanghai
CARPOOL_ORDINARY_BALANCE_READ_ONLY 未显式设置
```

不要照搬发布设计把 start-held 或后台任务开关带入生产。普通余额已清零是一次业务操作，不等于配置了永久“普通余额不可使用”。

## 4. 台湾代理与账号调度

| 上游账号 | 代理记录 ID / 名称 | 协议与入口 | 已测公网出口 |
|---|---|---|---|
| 1 `pro 20x` | 1 `TW-static-pro20x` | HTTP `178.94.233.85:45614` | `178.94.233.85` |
| 7 `七号车` | 2 `TW-static-car7` | HTTP `178.94.222.225:47536` | `178.94.222.225` |

两条代理 `status=active`、`fallback_mode=none`、`backup_proxy_id=NULL`。认证信息保存在新机数据库 `proxies` 记录中，账号通过 `accounts.proxy_id` 引用。本文不记录认证用户名或密码。

**17:39 的最新状态：只有账号 1、7 为 `schedulable=true`；账号 2、3、4、5、6 均为 false。** 两个可调度账号优先级均为 1。17:10 的历史快照中 2、5 曾可调度，后续排查不要继续套用那个旧状态。

新机已验证 Redis `sched:acc:1`、`sched:acc:7` 与数据库绑定一致，outbox 水位已越过变更事件，应用进程对两条代理都有实际 TCP 连接。这是账号级应用代理配置，没有设置代理失败回退直连；**并未设置整个服务器的防火墙强制代理出口**。若以后重新启用其他账号，也要核查其代理；目前其 `proxy_id` 为空。

OpenAI 上游通过这些连接看到的是代理公网出口 IP。ipify 返回与上表相同的 IP，IPinfo 标注 Taichung / Taiwan / TW，Cloudflare trace 为 `loc=TW`。供应商标称“台湾静态家宽”；查到 ASN 为 `AS215294 Lumina broadband UAB`。测试确认了当时的出口和地理标注，**未独立认证原生住宅属性、独享性、长期静态性，也不保证免风控**。

### 先测试再更换出口

1. 从新服务器测试候选代理的认证、HTTPS CONNECT、实际出口 IP、TLS 连通性；不能只在运维电脑上测试。
2. 只向账号原有上游发送一次最小生成请求，检查完整终止事件及输出。先避免改动正在接流的账号。
3. 通过管理端正常接口修改对应账号代理，保留原绑定。若经审查必须直接改库，应事务提交并投递 `scheduler_outbox` 的 `account_changed` 事件，让调度缓存更新；不要只更新一个字段就宣布生效。
4. 核对数据库、Redis 调度快照和应用实际连接；再用公开域名做一次完整请求并关联日志。观察新连接，允许既有请求正常结束。
5. 代理失败时先停用受影响账号或恢复已验证的原代理。不要为了恢复流量随意解除代理，否则可能改成服务器直连。

绑定前两次上游最小 `gpt-6-astra` 测试均成功：账号 1 约 4.647 秒，账号 7 约 3.683 秒，均出现 `response.completed` 并输出 `OK`（low reasoning，小上下文）。这些不是长期 SLA。

绑定后公开 `/v1/responses` 测试完整成功，约 24.54 秒；请求 ID `87082471-8a5c-446c-aca5-7c749c3d4228`。日志显示 **账号 1 返回 502 → failover → 账号 7 完成**。因此代理绑定完成与上游过载问题是两件事；较慢请求可能包含失败重试。成功用量集中在 7 号也不代表调度器从未尝试 1 号。

### 只读核查

在新机执行（SQL 只输出不含密钥的字段）：

```bash
docker exec -i sub2api-postgres psql -X -U sub2api -d sub2api -v ON_ERROR_STOP=1 <<'SQL'
SELECT a.id, a.name, a.schedulable, a.priority, a.proxy_id,
       p.host, p.port, p.protocol, p.status AS proxy_status,
       p.fallback_mode, p.backup_proxy_id
FROM accounts a LEFT JOIN proxies p ON p.id=a.proxy_id
ORDER BY a.id;
SQL

app_pid=$(docker inspect -f '{{.State.Pid}}' sub2api-blue)
nsenter -t "$app_pid" -n ss -tnp | grep -E '178\.94\.(233\.85:45614|222\.225:47536)'
```

无活动请求时没有 ESTABLISHED 连接不一定是故障。不要输出完整 `sched:acc:*`、`accounts.credentials`、`proxies` 或 `docker inspect` 环境变量，里面可能有敏感认证资料。

## 5. 账务、迁移与不可越过的边界

2026-09-10 迁移采用用户批准的口径：以旧机最后生产数据为准，新机原来的 5 条管理员测试用量约 `$1.572858504` 留存备份、不并入正式账本。

开放前核验：27 用户、22 拼车周期、132011 条用量；逐用户普通余额及逐周期拼车金额哈希一致，转储传输校验一致。普通余额合计 `18947.91442401`，拼车基础余额合计 `6827.14538701`，加量余额合计 `49.84107153`。这是**迁移开放前快照**，不是当前余额。

恢复接流后已做过拼车对账：22 个周期的“旧机结余 + 新机后续账本变动”全部匹配。之后正常消费、加量等继续记账，不能直接拿两台服务器当前余额相等作为验证标准。

16:58:32 按用户另行授权，将 **20 个余额非零的非管理员用户普通余额清零，合计 `9082.78071220`**。管理员普通余额保留；拼车基础、加量、人工余额在该事务中均未改变。已写入 20 条 `admin_balance` 调整记录，批次 `OBZ-20260910T165832`，并处理普通余额及认证缓存失效。17:39 复查非管理员普通余额非零数仍为 0。

运维约束：

- 普通余额 `users.balance` 和拼车周期/账本是不同余额体系。不能因“普通余额清零”去清理拼车周期、额度或账本。
- 管理端用户退款功能处理普通余额；已完成的批量清零不是每次部署都要重复的初始化步骤。
- 保留用户 ID、现有 API Key、分组及会话的连续性；余额修改应有明确业务授权、备份、事务和审计记录。
- 不要用旧备份覆盖当前生产账务来解决上游超时；这会丢失迁移后的真实扣费和业务变化。

```bash
docker exec -i sub2api-postgres psql -X -U sub2api -d sub2api <<'SQL'
SELECT count(*) AS nonadmin_nonzero, coalesce(sum(balance),0) AS balance_sum
FROM users WHERE role <> 'admin' AND balance <> 0;
SELECT count(*) AS usage_count, max(created_at) AS latest_usage FROM usage_logs;
SQL
```

## 6. 备份与恢复

### 自动备份

`sub2api-backup.timer` 已启用，UTC 每 6 小时的 `:20` 执行、额外随机延迟最多 5 分钟，`Persistent=true`。对应北京时间约 **02:20、08:20、14:20、20:20**，再加随机延迟。默认保留 14 天。

脚本：`/home/linuxuser/apps/sub2api/blue-green/scripts/backup.sh`。备份目录采用 UTC 时间命名，含：

- `postgres.dump`：`pg_dump -Fc`；脚本验证 `pg_restore -l` 可读。
- `redis-dump.rdb`：执行 Redis SAVE 后复制。
- `app-data.tar.gz`：应用 data，**排除 `data/logs`**。
- `runtime.env`、`docker-compose.local.yml`、`slot-images.env`、`blue-green-deployment.tar.gz`。
- `SHA256SUMS`：脚本生成并校验；备份文件权限 0600、目录 0700。

本次核验最新集合为 `20260910T080732Z`（北京时间 16:07:32，约 53 MiB）。这是迁移开放后的首份备份，**早于 16:58 普通余额清理及约 17:15 代理绑定**；不能用它恢复出之后的全部状态。后续定时备份是否成功需继续核查。

当前备份保存在同一台服务器；未核验异地副本、自动告警或定时异机恢复演练。脚本依次备份数据库、Redis、文件，不能声称它们是生产写入冻结后的同一原子时点。迁移或跨机回退需要另做停写、排空和对账。

```bash
# 检查，不修改业务配置
systemctl list-timers --all --no-pager | grep -E 'sub2api|certbot'
journalctl -u sub2api-backup.service -n 50 --no-pager
ls -lh /home/linuxuser/apps/sub2api/backups/automated/

# 需要立即保存最新状态时执行，会运行备份并产生磁盘 I/O
systemctl start sub2api-backup.service
journalctl -u sub2api-backup.service -n 30 --no-pager

# 对指定集合做完整性检查；替换为实际目录
cd /home/linuxuser/apps/sub2api/backups/automated/20260910T080732Z
sha256sum -c SHA256SUMS
```

备份包含密钥及用户数据，不能直接提交 Git。恢复时还需要目标服务器的宿主 Nginx、TLS、systemd、防火墙等环境；常规脚本没有打包整个 `/etc` 或应用 Docker 镜像，应另行保存可恢复的镜像和系统配置。

### 迁移现场与审计资料

| 位置 | 内容 |
|---|---|
| 旧机 `/home/linuxuser/apps/sub2api/migration-20260910-1545/` | 最终源库/文件迁移包、旧路由 `router-before.conf` |
| 新机 `/root/sub2api-migration-20260910-1545/` | 原新机备份、最终迁移快照及后续运维审计资料 |
| 新机数据库 `sub2api_pre_migration_20260910` | 迁移前新机测试数据，禁止混入正式账本 |
| 新机部署目录 `data.pre-migration-20260910`、`redis_data.pre-migration-20260910` | 迁移前文件与 Redis 资料 |
| 新机迁移目录 `ordinary-balance-before-zero-20260910-165216.private.jsonl` | 清零前逐用户余额备份 |
| 同目录 `ordinary-zero-20260910T165832.private.log` | 清零事务精确变更值与结果 |
| 同目录 `taiwan-proxy-bindings-before.jsonl` | 代理绑定前字段备份 |
| 同目录 `rollback-taiwan-proxy-bindings.sql` | 本次代理解绑回退 SQL，**未执行；执行会解除固定代理绑定** |

### 两类回退必须区分

**应用版本回退：** 在新机上使用同一份最新生产账本切换兼容版本；先确认数据库迁移向后兼容及目标镜像/槽健康。不能因为切回旧镜像就恢复旧数据库。

**整机迁回旧服务器：** 先在所有入口暂停新请求，明确在途流排空或取消策略，捕获新机最新数据库、Redis、文件和账务水位；验证备份后恢复到目标，逐用户/周期对账并处理缓存；确保仅一台具有写入资格，再切路由并验证真实生成及扣费。**不可只把 DNS 改回旧 IP，也不可直接启动旧应用。** 本手册不提供可被误当作日常操作的一键覆盖数据库命令。

## 7. 发布流程及现有脚本限制

这是蓝绿应用部署，共享一套 PostgreSQL、Redis 和 data；并非两套可同时独立写入的业务集群。正常方向是：可追溯镜像 → 备份 → 候选槽启动 → 健康和业务验证 → router 切换 → 排空旧槽 → 停止旧槽。

**当前迁移后脚本仍有一处适配缺口，修复并验证前不能直接运行 `deploy.sh` 或 `switch-slot.sh`：**

`blue-green/scripts/common.sh` 的 `verify_stable_route()` 仍执行 `docker exec new-api ... http://sub2api:8080/health`。新机没有 `new-api` 容器，它属于旧机业务拓扑，因此该检查会失败，可能使切换脚本恢复旧路由。应把新机本地健康检查与旧机 MonasAPI 兼容链路检查分开实现，并先验证失败恢复路径。本次只记录缺口，没有修改脚本或部署服务。

另外，`deploy.sh` 会 `docker pull` 并解析 RepoDigest；当前本地构建标签不能当然视为可拉取。后续发布应使用已发布、可追溯且经过验证的镜像，不要直接使用脚本默认的第三方 `latest` 覆盖此定制拼车版本。

发布前后必须核对：

1. 执行目标为新机；当前槽位、镜像、运行环境与准备发布的工件一致。
2. 备份新鲜且可读取；审查本次 schema 迁移、账务变更、后台任务开关和兼容回退方式。
3. 候选实例启动不能意外暂停生产接流或重复执行业务后台任务；只读 guard 检查不能替代人工审查。
4. 首页、健康、认证接口和至少一次完整流式生成正常，关联 request ID、实际账号和用量入账。
5. 观察错误率、完整成功率、首 token、端到端耗时、在途请求及拼车账本，再停止旧槽。

禁止照旧执行 `docker compose -f docker-compose.local.yml up -d` 或 `--remove-orphans`；旧单应用模板可能重建错误拓扑或影响共享依赖。不要在旧机运行发布、bootstrap 或槽切换脚本。

## 8. 日常巡检与故障定位

### 最小只读巡检（新机 root）

```bash
date -Is
uptime
free -h
df -h /
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
cat /home/linuxuser/apps/sub2api/run/active-slot
python3 /home/linuxuser/apps/sub2api/blue-green/scripts/release-guard.py monitor
curl -fsS --max-time 10 https://sub2api.monasapi.com/health
systemctl --failed --no-pager
systemctl list-timers --all --no-pager | grep -E 'sub2api|certbot'
```

`sub2api-release-guard.timer` 每 30 秒运行；本次状态 success。它只读检查活动容器、镜像、start-held 配置，以及应用直连/router 的 `/health=200`、未认证 `/v1/models=401`。**它不测试上游完整生成、不核对拼车余额、不自动修复服务，也没有证明配置了外部通知。**

### 日志入口

```bash
# 日志可能带用户信息，只按时间和请求 ID 取必要片段
docker logs --since 15m --tail 500 sub2api-blue
docker logs --since 15m --tail 200 sub2api-router
journalctl -u sub2api-release-guard.service -n 30 --no-pager
journalctl -u nginx -n 50 --no-pager
tail -n 100 /var/log/nginx/error.log
```

容器当前使用 `json-file` 日志驱动，容器级 `LogConfig.Config={}`；本次未发现 `/etc/docker/daemon.json` 配置。尚未确认有效轮转上限，应后续核查磁盘增长并配置合适的日志保留。不要为了排障输出完整请求正文、Token 或全库快照。

### 常见现象

| 现象 | 应检查的层次 |
|---|---|
| 手机能开、电脑 `ERR_CONNECTION_CLOSED` | 电脑实际代理端口、节点、系统代理与 TUN 规则；对比物理网卡直连、`--resolve`、域名访问 |
| DNS 返回 `198.18.*` | 本地代理 Fake-IP 可能接管了解析；从权威 NS/远程可信网络验证，不直接宣布 DNS 污染 |
| `503 Service migration in progress` | 服务端迁移门禁或遗留路由；9 月 10 日约 15:53:50–16:00:39 的此类错误属于实际迁移窗口 |
| 首页正常但生成 502/503、overloaded | 按 request ID 检查选中账号、代理连接、上游返回、failover 与最终终止事件 |
| SSE HTTP 200 或 `response.created` 后断流 | 尚未生成成功；Responses 必须检查 `response.completed` 和输出，Chat Completions 检查内容与协议结束标记/错误事件 |
| 网关总耗时远大于最后一条用量耗时 | 可能包含前序账号失败、排队、重试和切换；不能只用最终账号用量耗时判断端到端延迟 |
| 流量集中 7 号 | 先比较失败尝试日志和成功用量；核查 1 号错误、调度开关、额度、优先级，不凭面板推断未调度 |
| `no available accounts` | 核对所请求模型、套餐门槛、账号额度/限流/可调度状态；不等同于 DNS 或代理故障 |
| 普通余额 0 仍能正常使用 | 可能正在走拼车计费，属已验证场景；检查拼车绑定和账本，不要擅自补普通余额 |

关联用户报错至少记录：北京时间到分钟、完整域名/接口、模型、用户或 API Key **名称/ID**、request ID/trace ID；不索要密钥内容。宿主 UTC 日志要换算，使用带时区的时间区间，避免把不同时段请求对错。

本机曾同时使用 Clash Verge（7897/TUN）和 ClashX Pro（系统代理 7890）。最终通过 ClashX 活动配置顶部的 `DOMAIN,sub2api.monasapi.com,DIRECT` 恢复 Chrome；Verge 也设置了该域名直连、Fake-IP 排除和新机 IP /32 TUN 排除。ClashX 订阅更新可能覆盖手工规则。它是特定运维电脑的修复，不能推断所有用户已修复，更不应改动 Codex 登录配置来解决服务端故障。

## 9. 旧机保留范围与后续事项

旧机 production `sub2api-blue` 已停止、退出码 0、restart policy `no`；green 也停止。旧生产 PostgreSQL/Redis 保留在线，但不再用于生产写入。旧机 Sub2API 自动备份、release guard 两个 timer 均 disabled。router 持续负责旧入口及 MonasAPI 的跨机转发。

旧机仍有独立的 `sub2api-carpool-blue-test`、测试 PostgreSQL/Redis、loopback `127.0.0.1:38080` 测试 ingress 在运行；这些不是当前生产。不能用测试环境成功替代生产验证，也不要因生产迁移而删除其他业务或测试实例。

| 后续事项 | 当前边界 / 下一步 |
|---|---|
| 发布脚本迁移适配 | 修正新机不存在 `new-api` 的检查，并验证可控失败和回退；之后才恢复日常脚本发布 |
| router 公网 8080 | 已从旧机实测可达，评估改为 loopback 发布，验证 443 及旧机兼容链路 |
| 备份覆盖最新状态 | 核查下次备份包含普通余额清理和代理绑定；必要时手动备份 |
| 灾难恢复 | 增加异机加密备份、可恢复镜像/系统配置清单，并执行隔离恢复对账演练 |
| 监控 | 为备份失败、磁盘、证书到期、上游完整成功率/延迟配置实际告警接收方；当前未核验已有通知 |
| 日志与凭据 | 明确日志轮转；审查凭据文件权限和持久 SSH 访问。新机根目录 `.env` 核验权限为 0644，应单独安排权限收敛 |
| 代理交付 | 留存供应商、订单、到期/续费、静态独享约定和安全凭据位置；这些信息未提供，不在本文猜测 |
| 旧机退役 | 先迁走 MonasAPI 兼容依赖及其他业务，确认无实际流量和恢复依赖后再评估，不能立即关机 |

## 10. 证据与维护方式

本次只读核验来自两台服务器的 Docker / systemd / Nginx / UFW、定向 SQL、调度缓存、应用网络命名空间和权威/公共 DNS 查询；没有改动生产配置或产生新的模型探测消费。先前完整生成与迁移核验详见以下归档（均为历史快照）：

- [迁移与普通余额调整记录](operations/2026-09-10-migration.md)
- [台湾代理绑定及完整生成测试](operations/2026-09-10-taiwan-egress.md)
- [DNS 与用户请求失败核查](operations/2026-09-10-dns-incidents.md)

原始私有证据及脚本保存在本次任务 `/Users/shenxi/Documents/Codex/2026-09-10/ni-2/work/`，其中包含敏感文件，未复制到项目。服务器侧审计路径见第 6 节。

每次变更服务器、DNS、槽位、镜像、代理、账号调度、计费配置或备份策略后，更新本手册的核验时间和当前值，并追加变更原因、验证结果及回退边界。历史附件保留原时间，避免将历史金额和状态重写成当前事实。
