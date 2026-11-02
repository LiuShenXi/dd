# 公司续接：Claude 独立周卡 I4

本轮范围已收敛为 **I4 验收、提交并推送专属分支**。I4 已由协调方接受，生产未改动、未迁移、未上线；没有自动转换生产分组或给真实用户开卡。I5/I6 尚未实施完成。本轮到提交推送为止，不继续在线售卖或生产发布。

## 从远程新 checkout 开始

```powershell
git clone --branch codex/claude-weekly-quota https://github.com/LiuShenXi/dd.git dd-claude-weekly
cd dd-claude-weekly
git status --short
git rev-parse HEAD
git ls-remote origin refs/heads/codex/claude-weekly-quota
```

核对本轮最终交付的提交 SHA 与远程分支；不要改用 main，不要覆盖公司原有工作目录。本分支基于已核验生产源码 `8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`，最后一个 I3 提交为 `9005acffad8f9b30975ea3dd27e82095eb9e5520`。I4 的具体提交以克隆后的 HEAD 和协调方最终 push 记录为准。

先按顺序阅读：

1. `.trellis/tasks/10-19-claude-weekly-quota-architecture/README.md` 和 `sol-handoff.md`：架构、基线及不变式。
2. `validation/claude-weekly-quota/coordinator-current-status.md`、`coordinator-resume-state.md`、`coordinator-I4-review.md`：实际实现状态和最新范围；历史架构材料不是当前实施状态。
3. 同目录 `I4-receipt.md`、`I4-F4-key-projection-receipt.md`、`I4-ui-receipt.md`、浏览器验收记录及证据索引。

整体任务保持 in_progress。下一实施阶段是 **I5**，无需从头重做 I4；按架构依赖随后完成 I6。新代码改动仍需相应回归，现有通过记录不能自动证明下一阶段或生产已可启用。

## 已验证边界

Claude 卡按用户、分组、卡期独立记账，多 Key 共享；GPT 原额度和指纹保留。Claude 用户并发跨 Key/组/卡使用独立池，独立默认3、正整数用户覆盖；GPT 配置、槽和队列保持原口径。

最终 I4 F4 测试源码为 `9e2654b342516eee35f4d66e78ebfc13a550f1acb5515f94f291fffe66887f9c`，本机隔离候选镜像为 `sha256:4bfa2945902b61e8386d8aadcbd59a2a42117efc1ea99d5bc63978f5be4a24b9`。Windows532主测试/1005含子例、原 GPT84集合、全仓 unit 编译及相关 vet 通过；Linux critical race512主测试通过，实际 native PG/Redis 缓存回归和真实 `/v1/usage` A覆盖2/B继承3通过。浏览器已实际覆盖用户/Key/管理投影、请求响应丢失重放、续卡、409草稿保护、切换用户与倒计时刷新，详见协调方验收。

较早 F2/F3 长流、双实例、WS 等证据各自保留原 source/image 身份及兼容映射；不要把历史记录重新标为某个新镜像通过。旧失败、修复和覆盖界限都在索引中。

## 证据恢复

I4 重复大 manifest、历史诊断和失败原始日志保存在仓库内 `I4-diagnostics-archive.zip`，逐项名称、字节数和 SHA256 见 `I4-diagnostics-archive-manifest.json`；I3 有独立归档。归档随 Git 推送，不依赖本机私密目录。

```powershell
python validation/claude-weekly-quota/I4-verify-evidence.py
python validation/claude-weekly-quota/I4-verify-evidence.py --extract
```

先校验压缩包及每个 entry，再恢复到同一 validation 目录。失败记录不会被删除或改成通过。首次只需阅读外部保留的最终收据、源码清单和索引；需要脚本引用的历史 raw 文件时再解压。

## 隔离重建和验证

仓库 Go 要求1.27.0；本轮 Windows Go1.27.0、Linux Go1.27.2。宿主实际 Node20.14.0/pnpm9.5.0；仓库 Dockerfile 使用 Node24、pnpm9 和 Go1.27.0。前端 lockfile、法律文档和自定义资源均须保留。

```powershell
cd frontend
pnpm install --frozen-lockfile
pnpm run typecheck
pnpm run lint:check
pnpm run test:run
pnpm run build
cd ../backend
go test -tags=unit -run '^$' ./...
go test -tags=unit -count=1 -run '^TestClaudeSubscriptionConcurrency|^TestSubscriptionQuota' ./internal/service ./internal/repository ./internal/handler/... ./internal/server/...
```

前端 build 会生成被 Git 忽略的嵌入资源；公司新构建应记录自己的 source/image hash，不能为凑旧 hash 手改产物。验证脚本中的本机工具绝对路径可以在新环境的验证副本中适配，业务源码保持按实际任务改动。

真实仓储集成测试使用原 TestMain 启动临时 PostgreSQL18.1-alpine3.23、Redis8.4-alpine，需要 Docker；设置 `CI=true`，Docker 不可用时必须失败，不能算跳过通过。使用合成数据和独立数据库，不配置生产 DSN。

```powershell
$env:CI='true'
$env:SUB2API_TEST_POSTGRES_IMAGE='postgres:18.1-alpine3.23'
go test -tags=integration -count=1 -run '^TestClaudeSubscriptionConcurrencyNativeAuthColdL1L2AndV27Reload$' ./internal/repository
```

完整实际 HTTP/双实例测试另需从源构建候选与内部 fake provider，配置 `CWQ_I4_*` 的本机隔离资源；入口见 `I4-pg-test-plan.md` 和 `subscription_quota_http_i4_integration_test.go`。现有 environment helper 的容器 ID/私有路径不是可迁移环境。普通 `docker build -t claude-weekly-i4-local .` 可重建源码镜像，但不代表复现本机 R0 基层镜像或最终发布验收。

## 不带走的文件与后续工作

不需要本机 `PrivateTests`：不迁移其中的密码、API Key、JWT、fixture JSON，不带数据库备份、Docker volume、运行容器、临时 exe 或生产镜像归档。新机器通过测试源码生成新的合成夹具。账号、SSH/Codex认证分别在公司环境按自己的配置使用，不复制本机认证资料。被排除资源的路径清单仅用于说明旧证据依赖，不是复制清单。

I5 待完成套餐默认额度与覆盖、订单不可变快照、订单租约锁内发权益和审计/outbox原子提交、已付恢复及退款边界；参考 `I5-readonly-interface-lock-map.md`。I6 待完成真实维护转换、混合副本/配置校验、scope 操作指标、高流水性能、最终兼容回退与发布演练；参考 `I6-pending-obligations.md`。当前 I4 通过不能替代这些义务，也不构成生产上线许可。
