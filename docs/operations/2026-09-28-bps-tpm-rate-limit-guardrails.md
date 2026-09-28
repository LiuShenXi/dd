# 2026-09-28 BasisPoints 共享组织 TPM 限流防护调整

## 变更概述

2026-09-28（北京时间）针对 Excel / BasisPoints 通道持续出现的上游 `429` 做了生产防护调整。上游错误显示多个 OAuth 账号共用同一个隐藏组织的 GPT-6 Astra TPM 桶，典型错误为：

```text
Rate limit reached ... on tokens per min (TPM): Limit 500000000, Used 500000000
```

这类错误可能以 HTTP 200 建立流、随后在 SSE 中返回 `event: error` 的形式出现，客户端因此表现为流提前关闭。它是上游共享组织额度耗尽，不是单个本地账号独立额度耗尽；本地限流只能降低继续放大问题的速度，不能立即清空上游滚动窗口。

生产部署位置：

```text
SSH 别名：bwh
部署目录：/home/linuxuser/apps/sub2api
活动槽：blue
```

## 最终生效配置

### 请求体与入口边界

以下三个应用层请求体上限均为 `33554432` 字节（32 MiB）：

```text
SERVER_MAX_REQUEST_BODY_SIZE=33554432
GATEWAY_MAX_BODY_SIZE=33554432
GATEWAY_TEXT_MAX_BODY_SIZE=33554432
```

容器路由 Nginx 同时设置：

```nginx
client_max_body_size 32m;
```

这样请求会在外层路由、服务端和 Gateway 三层受到相同的保守边界约束，避免此前 256 MiB 入口上限允许过大的上下文或工具请求进入共享组织桶。

当前镜像内置的 BasisPoints 内层限制仍保留：结构化输出 schema 约 1 MiB、工具或 structured value 约 16 MiB。当前镜像未暴露可通过环境变量修改这些编译期限制的配置项；不能把外层 32 MiB 误认为已经修改了 BPS 二进制内部限制。

### 账号池与请求预算

分组 `2 / codex 20x 拼车` 的最终值：

| 项目 | 最终值 | 作用 |
|---|---:|---|
| 5 个 OpenAI OAuth 账号并发 | 每账号 2 | 将共享组织可能同时占用的活动请求控制在较小范围 |
| 分组 RPM | 60 | 每个用户在该分组内的每分钟请求上限；不是跨所有账号的组织级 TPM |
| `max_reasoning_effort` | `xhigh` | 保留用户需要的高推理档位 |
| 超过推理上限 | `downgrade` | 请求超过 `xhigh` 时降级，不继续放大预算 |

受影响的生产账号 ID 为 `5、11、14、15、16`。文档不记录账号凭据或 OAuth 内容。

### 排队与熔断

```text
GATEWAY_SCHEDULING_STICKY_SESSION_MAX_WAITING=1
GATEWAY_SCHEDULING_STICKY_SESSION_WAIT_TIMEOUT=30s
GATEWAY_SCHEDULING_FALLBACK_MAX_WAITING=20
GATEWAY_SCHEDULING_FALLBACK_WAIT_TIMEOUT=10s

GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_FAILURE_THRESHOLD=2
GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_WINDOW_SECONDS=60
GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_TTL_SECONDS=60
```

流式上游熔断的含义是：60 秒窗口内连续 2 次流失败后，对相应代理暂时隔离 60 秒。它是代理维度的保护，不是按隐藏组织或模型维度的全局熔断；因此不能把它当成组织级 TPM 配额的替代品。

`GATEWAY_OPENAI_HTTP2_FALLBACK_TTL_SECONDS=600` 属于 HTTP/2 传输回退保护，与上游流式限流熔断分开，当前保持不变，避免网络传输在一分钟内反复抖动切换。

## 部署过程

本次采用蓝绿方式，避免直接重启活动槽：

1. 执行生产备份。变更前自动备份目录为 `/home/linuxuser/apps/sub2api/backups/automated/20260928T062108Z/`。
2. 更新远端 `.env`、`docker-compose.local.yml` 和路由生成脚本。
3. 使用 Docker Compose `config -q` 校验合并配置。
4. 重建备用槽 `green`，通过 `/health` 和未授权 `/v1/models` 健康检查后切流。
5. 重建原活动槽 `blue`，再次健康检查后切回 `blue`。
6. 停止备用槽 `green`，避免低内存主机长期运行两个应用副本。
7. 通过数据库查询和容器环境变量复核账号并发、分组推理上限、请求体上限和熔断 TTL。

未发送新的模型生成请求；切换验证使用健康端点和配置检查。

## 当前验收状态

截至最终切换后：

- `sub2api-blue`：`healthy`
- `sub2api-router`：`healthy`
- `sub2api-postgres`：`healthy`
- `sub2api-redis`：`healthy`
- `sub2api-green`：已停止
- `run/active-slot`：`blue`
- 路由配置：`client_max_body_size 32m`
- `/health`：HTTP 200
- 活动容器环境中的流式熔断 TTL：`60`
- 最终复核窗口内未看到新的上游 429、panic 或 fatal 日志

健康检查通过不等于上游共享组织桶已经恢复。TPM 通常按滚动窗口自然回落；降载后一般需要一个到数个窗口，持续重试、排队请求或其他共享账号仍在使用时会延长恢复时间。

## 日常核查命令

以下命令只输出配置和健康状态，不读取凭据：

```bash
ssh bwh
cd /home/linuxuser/apps/sub2api

cat run/active-slot

docker ps -a --format '{{.Names}} {{.Status}}' | grep sub2api | sort

grep -n 'client_max_body_size' blue-green/router/conf.d/default.conf

docker inspect sub2api-blue --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(SERVER_MAX_REQUEST_BODY_SIZE|GATEWAY_MAX_BODY_SIZE|GATEWAY_TEXT_MAX_BODY_SIZE|GATEWAY_SCHEDULING_|GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_)=' \
  | sort

curl -fsS -i --max-time 5 http://127.0.0.1:8080/health | head

docker logs --since 10m sub2api-blue 2>&1 \
  | grep -E 'upstream.*429|Rate limit reached|status.?429|panic|fatal' \
  | tail -50 || true

docker exec sub2api-postgres psql -U sub2api -d sub2api -P pager=off -c \
  "SELECT id, concurrency FROM accounts WHERE id IN (5,11,14,15,16) ORDER BY id;"
docker exec sub2api-postgres psql -U sub2api -d sub2api -P pager=off -c \
  "SELECT id, rpm_limit, max_reasoning_effort, max_reasoning_effort_over_limit FROM groups WHERE id=2;"
```

不要在聊天、工单或提交记录中输出 `.env`、`config.yaml`、`accounts.credentials`、代理认证字段或完整 `docker inspect` 环境变量。

## 回滚原则

需要回滚时先确认目标是“只回滚本次保护参数”还是“恢复完整生产快照”。不要为了处理 429 直接覆盖当前数据库，因为这会丢失变更后的用量和业务数据。

只回滚应用配置时：

1. 先执行一次新的 `blue-green/scripts/backup.sh`。
2. 修改 `.env` 和 Compose 模板后执行 `docker compose ... config -q`。
3. 仍按“启动备用槽 → 健康检查 → 切流 → 重建另一槽 → 切回 → 停止备用槽”的顺序操作。
4. 回滚后重新核验活动槽、路由上限、账号并发、分组推理上限和熔断 TTL。

如果必须恢复数据库中的账号/分组限制，使用事务并先记录当前值；不要依赖记忆直接执行批量更新。数据库恢复只应使用经过校验的 PostgreSQL 备份，并在恢复后重新检查 Redis 调度快照和应用缓存。

## 相关项目文件

- [`deploy/docker-compose.local.yml`](../../deploy/docker-compose.local.yml)：应用环境变量默认值。
- [`deploy/.env.example`](../../deploy/.env.example)：新部署示例值。
- 生产部署目录中的 `blue-green/scripts/common.sh`：路由 `client_max_body_size` 生成逻辑；该蓝绿脚本不在本仓库当前跟踪文件中。
- [`docs/OPERATIONS.md`](../OPERATIONS.md)：通用生产运维基线；其中旧的入口上限描述需要以本文最终值为准。
