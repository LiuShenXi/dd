# Claude 未知用量不再暂停整卡（2026-10-22）

部署状态：已正式发布并完成生产验收，2026-10-22 10:09 CST。

用户已明确授权解除本次状态并永久移除同类拦截。原生产用户16、
Key98、term3 的 request441 在 10月21日23:26:02 因 context canceled
缺失完整用量。已通过正常管理员 resolve API 做免扣核销，revision2、
no_charge、费用0；原请求和审计原因保留。该操作是管理员免扣决定，
不声称上游确认零消费。

代码提交：8982c3f6132603d2a424e25a74a95c200b297f1a。未知用量仍保留 reconcile_required / NULL费用，
但不再进入 BlockingReconciliationCount，因此允许当前卡继续准入并显示
active。已有费用凭据冲突和 ledger_mismatch 仍阻止当前卡；已知待结算
额度、到期及其他并发条件不变。迟到的可信用量继续幂等补结算。

已通过相关 domain/dto/repository/service 单元测试、go vet、完整
TestSubscriptionQuotaBilling_ 集成测试，以及最终同一镜像的真实接口
wallet matrix 和 Redis故障取消回归。取消请求保持待核查，下一请求
结算0.00060000，仅使用隔离合成数据。测试容器、卷和网络已清理。

候选版本：2.8.13-monas-claude-weekly.3。不可变镜像：sha256:5fa04a58c0caabcedbc4c9ff153d1d7f7fe37f00039a2ce7e44061696643cf10。
嵌入式前端哈希与当前 .2 相同；无数据库迁移。
生产备份及单次免扣回执：
/root/sub2api-ops/claude-reconciliation-20261022/。

## 生产结果

- 两域由 blue / sub2api2 / 29080 提供服务，容器9d41c4992890，
  旧green cb20cb0f9c56自然退役、restart=no。
- PostgreSQL/Redis以及其余27个原运行容器身份、启动时间和重启次数相同。
  新实例HTTP open、后台leader active；reconcile timer已恢复active。
- 原request441免扣审计保留。用户16/Key98实测原用Opus5.5返回200、
  message_stop；新记录449正常结算0.005432 USD，term3显示active，
  总额度165 USD、到期时间2026-10-27 13:24:41 CST保持不变。
- 两个正式域名各368次健康采样全部200，合计736次；采样不代替逐条请求审计。
- 初次最小验收选择了该组未开放的旧Haiku名，返回404；随后读取该Key
  的当前模型目录（12个Opus名称），改用原模型Opus5.5验收成功。
- 本次开发提交8982c3f61，仍在codex/claude-weekly-quota分支，未push或合并main。
  本地Trellis没有scripts/current-task/journal，直接记录本任务及发布回执。
