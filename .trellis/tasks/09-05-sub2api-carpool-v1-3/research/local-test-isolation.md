# Sub2API 真实数据副本的本地隔离启动研究

结论：源码没有一个“测试模式”总开关。安全启动必须同时满足：先离线还原并脱敏、应用只连接本地 PostgreSQL/全新 Redis、容器网络 `internal: true`、只绑定 `127.0.0.1`。仅把账号设为 disabled 或仅清空代理变量都不够。

## 1. 启动时会外联的后台任务与精确关闭点

- OAuth 自动刷新：默认开启，且启动后立即扫描（`backend/internal/config/config.go:2538`；`backend/internal/service/token_refresh_service.go:202-247`）。环境变量 `TOKEN_REFRESH_ENABLED=false`。
- OpenAI 自动用卡/重置：Wire 无总开关，启动 2 秒后扫描；逐账号开关在 `accounts.extra.auto_reset_credit_enabled`（`backend/internal/service/openai_quota_auto_reset.go:128-207`；`openai_quota_auto_reset_config.go:14-41`）。副本中把该键置 `false`；更稳妥是所有 `accounts.status='disabled' AND schedulable=false`。
- 上游计费探测：DB 设置缺失时反而默认开启；还要求账号 `extra.upstream_billing_probe_enabled=true`（`upstream_billing_probe.go:98-102,177-227,367-445`）。写 `settings.upstream_billing_probe_settings={"enabled":false,"interval_minutes":5}`，并清除账号两个键 `upstream_billing_probe_enabled/upstream_billing_rate_sync_enabled`。
- Ollama Cloud 用量刷新：全局 `settings.ollama_cloud_usage_settings.enabled` 与账号 `extra.ollama_cloud_usage_auto_refresh` 双开关（`ollama_cloud_usage.go:32,175-241,437-476,716-740`）。显式写全局 disabled 并清账号键。
- 国产渠道余额查询：默认开启（`config.go:2455-2457`），环境变量 `GATEWAY_CN_PROVIDERS_BALANCE_CHECK_ENABLED=false`（运行门控见 `cn_provider_balance_check_service.go:60-81`）。
- 渠道监控：runner 启动即加载 `channel_monitors.enabled=true`（`channel_monitor_runner.go:115-154`）；运行时软开关是 `settings.channel_monitor_enabled`（`domain_constants.go:460-466`）。两处都关：设置 `false`，并 `UPDATE channel_monitors SET enabled=false`；V2 聚合另用 `CHANNEL_MONITOR_V2_DISABLE_AGGREGATOR=1`（`wire.go:1030-1038`）。
- 定时账号测试：无全局开关，runner 每分钟取 `scheduled_test_plans.enabled=true` 并真实测账号（`scheduled_test_runner_service.go:45-66,90-145`；表字段/FK 在 `migrations/066_add_scheduled_test_tables.sql:4-21`）。副本中全部设 `enabled=false`。
- 支付订单过期器：无配置开关，每 60 秒处理过期 PENDING，先 `QueryOrder`，可能再取消并履约（`wire.go:978-982`；`payment_order_expiry_service.go:57-116`；`payment_order_lifecycle.go:152-207,379-409`）。脱敏前绝不能启动；副本需把 PENDING 订单隔离/改为非 PENDING，并清支付凭证。
- 邮件：队列本身启动，但只在入队后发；订阅到期任务可周期发提醒（`email_queue_service.go:50-93`；`subscription_expiry_service.go:74-199`）。写 `subscription_expiry_notify_enabled=false`，清空 `smtp_*`，并关闭 `ops_monitoring_enabled`/`ops_email_notification_config`，后者驱动告警和报表邮件（`domain_constants.go:432-451`）。
- Prompt Audit：后台 worker 会领取待处理任务并请求 Guard（`securityaudit/prompt_worker.go:40-101,136-171`）。写 `risk_control_enabled=false` 且 `prompt_audit_config.enabled=false`（键见 `securityaudit/prompt_types.go:9-10`），并将 `prompt_audit_jobs` 非终态任务隔离。
- 定时备份/S3 清理：DB 键 `backup_schedule`、`backup_s3_config`（`backup_service.go:28-49,96-118,229-246`）。写 `backup_schedule={"enabled":false,"cron_expr":"","retain_days":0,"retain_count":0}`，删除/置空 `backup_s3_config`；不要挂载生产对象存储目录。
- 定价初始化会在启动阶段访问 GitHub raw；默认远程 URL 非空（`config.go:2285-2291`；`pricing_service.go:211-227,286-317`）。在本地 `config.yaml` 明写 `pricing.remote_url: ""` 与 `pricing.hash_url: ""`，使用镜像内 fallback 文件；不要只传空 env，因为本项目没有调用 Viper `AllowEmptyEnv`，空值可能被当成未设置而回落到默认 GitHub URL。
- Codex 版本同步会请求 GitHub，DB 开关缺失视为开启（`openai_codex_version_sync_service.go:59-98,121-182`）。写 `openai_codex_version_auto_sync_enabled=false`。
- 插件管理器启动即 reconcile 并可拉起本地出站插件（`cmd/server/main.go:151-162`；`plugin_manager.go:79-103,260-335`）。把 `sub2api_plugin_bindings.enabled=false`、installation `state='disabled'` 且 `config_encrypted=''`（表/FK见 `migrations/229_plugins.sql:5-42`）。
- 纯本地但会改副本的清理/过期服务也建议关：`OPS_ENABLED=false`、`USAGE_CLEANUP_ENABLED=false`、`DASHBOARD_AGGREGATION_ENABLED=false`、`BATCH_IMAGE_ENABLED=false`、`BATCH_IMAGE_QUEUE_ENABLED=false`、`DATABASE_USER_PLATFORM_QUOTA_FLUSHER_ENABLED=false`。Viper 的环境映射规则是点转下划线（`config.go:1790-1791`）。账号、代理、订阅过期器没有总开关；如要求副本逐字不变，应增加 test-only 启动门控，不能靠现有配置保证。

## 2. 脱敏清单、保留项与外键

- `users`：保留 `id,balance,frozen_balance,total_recharged,concurrency,status` 和时间字段；将 `email` 唯一伪名化、`password_hash='!disabled!'`、`username/notes` 清空、`totp_secret_encrypted=NULL,totp_enabled=false,balance_notify_extra_emails='[]'`（`ent/schema/user.go:36-123`）。另删 `passkey_credentials/passkey_user_handles`，二者以 `user_id` CASCADE（`migrations/191_passkey_credentials.sql:1-18`）。
- 第三方登录态：删除 `pending_auth_sessions`（先删会级联 decision），删除 `auth_identities`（级联 channels），清 `auth_identity_migration_reports`；其 user FK/级联关系见 `migrations/108_auth_identity_foundation_core.sql:23-140`。`user_attribute_values.value` 可能含微信/公司身份，按 `user_id` 保留行可伪名化，或直接删值表（FK见 `migrations/018_user_attributes.sql:33-43`）。
- `api_keys`：必须原地随机化 `key` 并 `status='disabled'`，保留行 ID、group/user FK、quota/usage/window 账务列；`usage_logs.api_key_id` 和 carpool 表依赖这些 ID（`ent/schema/api_key.go:34-147`；`migrations/235_carpool_core.sql:71-95,116-141`）。不要删整表。
- `accounts`：保留 `id,platform,type,rate_multiplier` 及 usage 关联，原地置 `credentials={}`, `extra={}`, `status='disabled',schedulable=false`；credentials 明含 API key/access token/refresh token/cookie（`ent/schema/account.go:63-87,116-204`）。不要删账号，否则 `usage_logs/account_groups` 依赖断裂（`migrations/001_init.sql:105-169`）。
- `proxies`：保留 ID 供 `accounts.proxy_id`，但改为无效本地 host、清 username/password、status disabled、fallback none；字段与反向依赖见 `ent/schema/proxy.go:32-79`。
- `channel_monitors`：`api_key_encrypted`、`endpoint`、`extra_headers/body_override` 都可能带凭据；禁用并清值，保留 ID/历史 FK（`ent/schema/channel_monitor.go:32-124`）。模板的 headers/body 也清（`channel_monitor_request_template.go:36-69`）。
- `settings.value` 是无类型秘密仓：至少删除/替换 `smtp_*`、各 OAuth/OIDC/微信/钉钉/GitHub/Google client id/secret、验证码 secret、`admin_api_key`、`backup_s3_config`、Prompt Audit token/config；完整键名集中于 `domain_constants.go:196-348,412-487`。`security_secrets.value` 中的 `jwt_secret` 必须旋转，才能使复制来的 JWT 全失效（`repository/security_secret_bootstrap.go:19-55`）。本地不要复用生产 `TOTP_ENCRYPTION_KEY`。
- 支付：`payment_provider_instances.config` 是加密凭据，设 `enabled=false,refund_enabled=false,allow_user_refund=false,config=''`（`ent/schema/payment_provider_instance.go:31-66`）。`payment_orders.provider_snapshot` 也可能保留可用配置，清空它及 pay URL/二维码；已结算/退款的金额、状态、时间保留，PENDING 单独隔离（`payment_order.go:32-172`）。支付审计 detail 可能含上游响应，按需脱敏但保留 order_id（`payment_audit_log.go:31-53`）。
- Carpool 账务不要删：`carpool_terms -> cycles -> ledger/payments/billing_requests`，同时依赖 users/groups/api_keys；精确 FK 在 `migrations/235_carpool_core.sql:19-141`。只伪名化 `external_order_no/request_id/request_fingerprint/notes/billing_payload/last_error` 中的外部标识，保留金额、周期、delta、状态和所有 ID。
- 其他数据泄漏面：`usage_logs.ip_address/user_agent/request_id`、`audit_logs.request_body`、`prompt_audit_events.full_prompt`（明确是未脱敏全文，`migrations/182_prompt_audit_full_prompt.sql:1-5`）。若不影响 v1.3 账务测试，应清除这些文本/网络标识。

推荐用两个产物：原始 dump 仅离线加密存放且永不挂给应用；应用只挂“脱敏副本”。所有 UPDATE/DELETE 在单一事务执行，完成后以只读查询断言：无 active account/key/monitor/payment provider/plugin、无 PENDING 支付、所有外联设置为 false、所有 secret 列为空；通过后才创建应用容器。

## 3. Docker 最稳妥方案

1. 先离线构建/拉取镜像。只启动专用 PostgreSQL，还原 dump 后在容器内执行脱敏；此阶段绝不启动 app。PostgreSQL 使用与生产相同 major，Redis 使用全新空实例，绝不复制生产 Redis（其中可能有 refresh token、队列和待执行任务）。
2. 使用独立 compose project，例如 `-p sub2api-carpool-sandbox`；不要复用现有固定 `container_name`、volume、network。项目现有 dev compose 会默认注入 `host.docker.internal:7897` 代理（`deploy/docker-compose.dev.yml:52-55`），不能直接用；local compose还默认绑定 `0.0.0.0:8080`（`docker-compose.local.yml:36-37`）。
3. app/postgres/redis 仅接一个 `internal: true` 的 user-defined bridge；仅 app 发布 `127.0.0.1:18080:8080`，Postgres/Redis 不发布宿主端口。不要配置 `extra_hosts: host.docker.internal`，显式清空 `HTTP_PROXY/HTTPS_PROXY/ALL_PROXY`。不要加入默认或第二个非 internal 网络，否则 app 会重新获得外网出口。
4. app 只用 `DATABASE_HOST=postgres,DATABASE_PORT=5432`、`REDIS_HOST=redis,REDIS_PORT=6379`；使用全新的 `/app/data` volume 和本地生成 JWT/TOTP key，禁止挂载/复制生产 `config.yaml`。现有 compose 的服务发现依赖即是 PostgreSQL+Redis（`docker-compose.local.yml:63-77,210-293`）。`AUTO_SETUP=true` 只在缺少本地配置时从这些 env 写配置（`cmd/server/main.go:77-94`；`setup/setup.go:541-609`）；之后再向这份本地配置加入上面的空 pricing URL。
5. 启动前检查渲染后的 `docker compose config`：只能出现 sandbox 名称、`postgres`/`redis` 主机、18080、空代理与上述禁用变量。启动后用无凭据 canary 验证容器能访问 postgres/redis/自身 health，但访问 `https://example.com`、GitHub、OAuth、SMTP 和支付域名均失败；同时监看日志不得出现 refresh/probe/reset/payment/email/backup/monitor 外联尝试。

`network_mode: none` 虽最强但会同时切断 PostgreSQL/Redis，不能运行完整应用；`internal: true` 单网桥是可用性与硬隔离的正确组合。若 Docker Desktop/宿主防火墙策略不可信，再叠加宿主出站拒绝规则，但不能以 DB 软开关替代网络硬隔离。
