# 生产对应代码索引

提交：`8d869d41f40d6eedbe2ee05aaf8973145cccbe4d`。下列行号来自对应生产的实际源码文件；完整摘要见 [本地基线](local-baseline.json)。

此表描述现有实现和改动接点，不表示已实现周卡。文件以生产源码根目录为基准。

## S01 · 用户拼车绑定投影及其他分组拒绝；需要窄范围 Claude term 例外

- 路径：`backend/internal/repository/api_key_repo.go`
- 行号：245
- 锚点：`func (r *apiKeyRepository) projectCarpoolBilling(ctx context.Context, key *service.APIKey) (*service.APIKey, error) {`
- Git 内容 SHA-256：`64aecd7211a5bd66215cbd3ae53a379b01db1413fcb756512f82fd22977a9e45`

## S02 · 开卡及续卡的全部 Key 同组约束；保留余额与绑定要求

- 路径：`backend/internal/repository/carpool_repository.go`
- 行号：372
- 锚点：`func prepareCarpoolUserGroupTx(ctx context.Context, tx *sql.Tx, params domain.CreateCarpoolTermParams) error {`
- Git 内容 SHA-256：`e08832be1ca0c50b1a5ee1520edcefef216ddd31f13d3adfa3be5fa4aec71466`

## S03 · 逐用户拼车绑定、现有金额保存协议

- 路径：`backend/migrations/244_carpool_balance_preserving_release.sql`
- 行号：3
- 锚点：`CREATE TABLE IF NOT EXISTS carpool_billing_bindings (`
- Git 内容 SHA-256：`c81e7a10d8cee22637ae9bb9e423f69f1d031e05e95dc3baf2e09160c53323fc`

## S04 · 可选分组与有效订阅权限

- 路径：`backend/internal/service/api_key_service.go`
- 行号：1034
- 锚点：`func (s *APIKeyService) GetAvailableGroups(ctx context.Context, userID int64) ([]Group, error) {`
- Git 内容 SHA-256：`ee3ca1910c565d0afecf2345ebe5dd70446833e756bf1603e6ce8d759397e96c`

## S05 · 原订阅存在性、窗口检查及缺失依赖的余额路径

- 路径：`backend/internal/server/middleware/api_key_auth.go`
- 行号：202
- 锚点：`if isSubscriptionType && subscriptionService != nil && !billingInfoRequest {`
- Git 内容 SHA-256：`7f91ea8fe3ad0593364faa05a97277603a142d9c1a7eea86979c99a99885e4d6`

## S06 · nil subscription 可能变成普通计费；最终 term 分派必须明确

- 路径：`backend/internal/service/billing_cache_service.go`
- 行号：736
- 锚点：`func (s *BillingCacheService) CheckBillingEligibility(ctx context.Context, user *User, apiKey *APIKey, group *Group, subscription *UserSubscription, platform string) error {`
- Git 内容 SHA-256：`0b543992a37f1a89fcfc21fb06b4cca479852f0be9419a66e07f829e03f0246a`

## S07 · 原每周窗口重置语义不是七日卡快照

- 路径：`backend/internal/service/user_subscription.go`
- 行号：86
- 锚点：`func (s *UserSubscription) NeedsWeeklyReset() bool {`
- Git 内容 SHA-256：`faba748f7f767ca107f45558185e18dee8b39591e42884cfa1b57cfe60607a31`

## S08 · 原续费修改父记录；新 term 应先分派

- 路径：`backend/internal/service/subscription_service.go`
- 行号：290
- 锚点：`func (s *SubscriptionService) updateExistingSubscriptionTerm(`
- Git 内容 SHA-256：`864c89110341897e19c9869fe51e5fab0bd9d5dbfc4a7040276a1c9662b88a7b`

## S09 · 父记录计数与有效期模型

- 路径：`backend/ent/schema/user_subscription.go`
- 行号：36
- 锚点：`func (UserSubscription) Fields() []ent.Field {`
- Git 内容 SHA-256：`58ab438b5f33b127bdf4c4ba5f6f906113d99f289726c51cdee79d018f10c3b1`

## S10 · 套餐缺逐期额度字段

- 路径：`backend/ent/schema/subscription_plan.go`
- 行号：31
- 锚点：`func (SubscriptionPlan) Fields() []ent.Field {`
- Git 内容 SHA-256：`a131adc9a1b37d5dcda960f5f5c9ceac75e437a455af479da5d76a418e04acd4`

## S11 · 平台、订阅模式、共享日周月限额

- 路径：`backend/ent/schema/group.go`
- 行号：37
- 锚点：`func (Group) Fields() []ent.Field {`
- Git 内容 SHA-256：`15dc476630732e4e5f62b35564653431ae2c7418a961470b5899501648940c36`

## S12 · 旧批量操作必须按模式保护

- 路径：`backend/internal/service/subscription_bulk_action.go`
- 行号：25
- 锚点：`func (input *BulkSubscriptionActionInput) Validate() error {`
- Git 内容 SHA-256：`8b8a7d86ba5d5c25665c260e6793f8339f77a32354af641a49e03ce3fc8df601`

## S13 · 八位量化、旧指纹顺序、来源互斥与 receipt v1

- 路径：`backend/internal/service/usage_billing.go`
- 行号：195
- 锚点：`const UsageBillingMonetaryScale = 8`
- Git 内容 SHA-256：`7a55d080f8974fe2db1684189a872fd096a6dc24b221bddf074c669567044bdf`

## S14 · 统一去重和事务内账务效果

- 路径：`backend/internal/repository/usage_billing_repo.go`
- 行号：24
- 锚点：`func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {`
- Git 内容 SHA-256：`ee12a96f9abb5c2d1f9f5016a049efadc40773209da7fe872a8480b07cb49f30`

## S15 · ActualCost、模型计价、旧失败日志和 legacy 兜底

- 路径：`backend/internal/service/gateway_usage_billing.go`
- 行号：652
- 锚点：`func (s *GatewayService) RecordUsage(ctx context.Context, input *RecordUsageInput) error {`
- Git 内容 SHA-256：`33ddc6d80a749a2c44338cda4b9eb493d91c4533680f53949395299c53d3ba9f`

## S16 · 现有准入和耐久凭据接口；参考模式但不修改旧协议

- 路径：`backend/internal/service/carpool_gateway_billing.go`
- 行号：21
- 锚点：`type CarpoolGatewayBilling interface {`
- Git 内容 SHA-256：`f76ec9c0497c048584e0915a6cf73ce2a08a5727b5eed48b298e9ff5bd307afa`

## S17 · Messages 实际分支、跨组 fallback 和用量入口

- 路径：`backend/internal/handler/gateway_handler.go`
- 行号：123
- 锚点：`func (h *GatewayHandler) Messages(c *gin.Context) {`
- Git 内容 SHA-256：`55d5bd6d4115a7c91fbd4a0b1be02473d3bec7559e5fea5983f5b9cd51d78d2a`

## S18 · Claude OpenAI Chat 适配入口

- 路径：`backend/internal/handler/gateway_handler_chat_completions.go`
- 行号：23
- 锚点：`func (h *GatewayHandler) ChatCompletions(c *gin.Context) {`
- Git 内容 SHA-256：`9f31abc63ba0a2adc9883c3180f52e0e568246ec183c25a51878f851c9e37927`

## S19 · Claude Responses 适配入口

- 路径：`backend/internal/handler/gateway_handler_responses.go`
- 行号：23
- 锚点：`func (h *GatewayHandler) Responses(c *gin.Context) {`
- Git 内容 SHA-256：`b87e493c41ce1c1ec5e21d854e6ca8e9addf132399c04665fe85ed047ce49076`

## S20 · 真实端点、别名和强制平台路由

- 路径：`backend/internal/server/routes/gateway.go`
- 行号：21
- 锚点：`func RegisterGatewayRoutes(`
- Git 内容 SHA-256：`293172515f0b19cd278e1d515a783d8057e38bdd47bfa1601a284e6725f8e650`

## S21 · 订单创建快照

- 路径：`backend/internal/service/payment_order.go`
- 行号：25
- 锚点：`func (s *PaymentService) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResponse, error) {`
- Git 内容 SHA-256：`f42d12e6d11278aca8d1cbb602709cf1d98231bc14cc6dc5eb9c3ef75dde7820`

## S22 · 履约订阅事务、幂等和 outbox

- 路径：`backend/internal/service/payment_fulfillment.go`
- 行号：556
- 锚点：`func (s *PaymentService) ensurePaymentSubscriptionAssigned(ctx context.Context, o *dbent.PaymentOrder, groupID int64, days int) error {`
- Git 内容 SHA-256：`4e9d8058c08f94dceadb18c146d3d0d723b516a1d98503ffc5a542cb32f15bf0`

## S23 · 商品信息当前使用分组限额

- 路径：`backend/internal/service/payment_config_plans.go`
- 行号：92
- 锚点：`func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {`
- Git 内容 SHA-256：`5f38847a29e08eb881e5998f41903aee71a825a7c10f04889d93282f26cc1a35`

## S24 · 退款按天撤回路径需 mode guard

- 路径：`backend/internal/service/payment_refund.go`
- 行号：262
- 锚点：`sub, err := s.subscriptionSvc.GetActiveSubscription(ctx, o.UserID, *o.SubscriptionGroupID)`
- Git 内容 SHA-256：`cf03b04f1415ca847be4dfdda74cf9f4cf1b59d4363988e7022b85d4676eb010`

## S25 · 旧订阅兑换不能绕过卡期领域

- 路径：`backend/internal/service/redeem_service.go`
- 行号：528
- 锚点：`_, _, err := s.subscriptionService.AssignOrExtendSubscription(txCtx, &AssignSubscriptionInput{`
- Git 内容 SHA-256：`e8478905fa88d88d9dc2725fefc22bb2074a3fcd3c4ff15c0a86ee8642e9c971`

## S26 · 鉴权快照版本和新字段兼容

- 路径：`backend/internal/service/api_key_auth_cache_impl.go`
- 行号：17
- 锚点：`const apiKeyAuthSnapshotVersion = 25 // v25: carpool billing projection and enforcing model_allowlist; invalidate both v24 formats`
- Git 内容 SHA-256：`0c4d41943fe58394115131e2d6a957a425e7673a7a2e78e2c4966d53782fd911`

## S27 · 成功统计日志写入与既有幂等

- 路径：`backend/internal/repository/usage_log_repo_insert.go`
- 行号：156
- 锚点：`func (r *usageLogRepository) Create(ctx context.Context, log *service.UsageLog) (bool, error) {`
- Git 内容 SHA-256：`e81ff7e9f32dd9944f9c6fcc5871262a2e80fbf4d04d1706c78870b5361b595f`

## S28 · 管理订阅路由清单

- 路径：`backend/internal/server/routes/admin.go`
- 行号：699
- 锚点：`subscriptions := admin.Group("/subscriptions")`
- Git 内容 SHA-256：`3160d8fd2528b94913c0323de0d3377b4b74219ee985d3c391fd38bb61b4ff50`

## S29 · 用户订阅路由清单

- 路径：`backend/internal/server/routes/user.go`
- 行号：138
- 锚点：`subscriptions := authenticated.Group("/subscriptions")`
- Git 内容 SHA-256：`79e462e58917a5d734c8a0867b7bee9f2bdd6e0a9d3feda9481eef4c6c2e2842`

## S30 · 后台实际响应 envelope

- 路径：`backend/internal/pkg/response/response.go`
- 行号：15
- 锚点：`type Response struct {`
- Git 内容 SHA-256：`38b5dc41d4b75111f950346f19531a95d34458756d53bc90235c81fb03de2d50`

## S31 · 复用定价规则，扩展可冻结的只读价格解析

- 路径：`backend/internal/service/billing_service.go`
- 行号：1397
- 锚点：`func (s *BillingService) CalculateCostUnified(input CostInput) (*CostBreakdown, error) {`
- Git 内容 SHA-256：`ef6efb30f2cd06e747ff6b965bfcd86902266a7da262ccbd3fc6c34e660f8d85`

## S32 · 迁移哈希算法、notx 与历史兼容白名单

- 路径：`backend/internal/repository/migrations_runner.go`
- 行号：189
- 锚点：`content := strings.TrimSpace(string(contentBytes))`
- Git 内容 SHA-256：`d18d93efe65bba309bc1184037cb2a24ddfcb3c954c2100de834a67ad6ed40d1`

## S33 · 隔离 PostgreSQL 集成 harness

- 路径：`backend/internal/repository/integration_harness_test.go`
- 行号：148
- 锚点：`// SUB2API_TEST_POSTGRES_IMAGE overrides the PostgreSQL image so the suite can be`
- Git 内容 SHA-256：`011d77089a29945b0e2ac1950c3764f453b30d0e62fd632d9bb770d8f1ac033f`

## S34 · 管理端共享组限额展示需要模式分派

- 路径：`frontend/src/views/admin/SubscriptionsView.vue`
- 行号：288
- 锚点：`<div v-if="row.group?.weekly_limit_usd" class="usage-row">`
- Git 内容 SHA-256：`041c788c0467df2085f2af5e694a2df006bdf1450ebb82e825b18934a6d20b91`

## S35 · 用户订阅额度展示

- 路径：`frontend/src/views/user/SubscriptionsView.vue`
- 行号：141
- 锚点：`<div v-if="subscription.group?.weekly_limit_usd" class="space-y-2">`
- Git 内容 SHA-256：`f9f16261d39cc4d2f5a6a452206b3717f97dac712389b286c88ffbc1bdb1f048`

## S36 · 迷你进度的模式契约

- 路径：`frontend/src/components/common/SubscriptionProgressMini.vue`
- 行号：103
- 锚点：`<div v-if="subscription.group?.weekly_limit_usd" class="flex items-center gap-2">`
- Git 内容 SHA-256：`984f06b2a3624eda133161b0be528af1138abab02fb3985fc5b22c1c3a505ff3`

## S37 · 商品卡默认额度展示

- 路径：`frontend/src/components/payment/SubscriptionPlanCard.vue`
- 行号：60
- 锚点：`<div v-if="plan.weekly_limit_usd != null" class="flex items-center justify-between">`
- Git 内容 SHA-256：`ad54269a1ce6fe0ff17ce8dc61beb449ab99dafcd303cba07ec98f04075eb902`

## S38 · 现有期限解释工具，不能按期限猜新模式

- 路径：`frontend/src/utils/subscriptionQuota.ts`
- 行号：17
- 锚点：`export function isOneTimeDailyQuota(`
- Git 内容 SHA-256：`99bb120ef65e1494cf6db4c6c4013b88e70239adeb683a863e6f44caf2368ff6`

## S39 · 原其他组拒绝测试需拆成窄例外与防绕过

- 路径：`backend/internal/repository/api_key_carpool_binding_test.go`
- 行号：13
- 锚点：`func TestAPIKeyCarpoolBindingProjectionIsPrivateAndFailsClosed(t *testing.T) {`
- Git 内容 SHA-256：`c377e1805d378e376c4db38e9d30c037ce3cefe2500b6854559450e4d9e129c1`

## S40 · GPT 绑定跨到期及余额保护回归

- 路径：`backend/internal/repository/carpool_billing_binding_integration_test.go`
- 行号：22
- 锚点：`func TestCarpoolBillingBindingPreservesGroupAndAdminThroughAuth(t *testing.T) {`
- Git 内容 SHA-256：`e6cab81371e4840e65900208299449ab43ec512112e30fe11fcf82c357cc2092`

## S41 · GPT 开通和续费真数据库测试

- 路径：`backend/internal/repository/carpool_standard_open_integration_test.go`
- 行号：18
- 锚点：`func TestCarpoolStandardGroupOpenRenewAndExistingKeyAuth(t *testing.T) {`
- Git 内容 SHA-256：`2f5cc7d967c1fe843224f7f38ace60ff3bf312a8946a8c0ced931e0988f3d5f9`

## S42 · 两种 Key 的缓存投影隔离

- 路径：`backend/internal/service/api_key_auth_cache_carpool_binding_test.go`
- 行号：11
- 锚点：`func TestAPIKeyAuthSnapshotPreservesPerUserBillingIsolation(t *testing.T) {`
- Git 内容 SHA-256：`a8b2ef6ba2f0b535b530ef9ca2ad58e17b1031f0180406d82835b531e5e6172d`

## S43 · 用户实际消费与上游 TotalCost 区分

- 路径：`backend/internal/service/gateway_service_subscription_billing_test.go`
- 行号：13
- 锚点：`func TestBuildUsageBillingCommand_SubscriptionAppliesRateMultiplier(t *testing.T) {`
- Git 内容 SHA-256：`8c1fdb97af1e8d5fd2de0cf7e541413ed75dd57fceea1fe4a47e0d1f9d62888a`

## S44 · 父订阅到期更新与邮件提醒，term 需隔离

- 路径：`backend/internal/service/subscription_expiry_service.go`
- 行号：106
- 锚点：`func (s *SubscriptionExpiryService) runOnce() {`
- Git 内容 SHA-256：`0641161fbb95fc7092a0e69e09aeaccf0fa34a2cd8b45ef39725e67472220d85`

## S45 · 原生窗口维护队列的模式保护

- 路径：`backend/internal/service/subscription_maintenance_queue.go`
- 行号：19
- 锚点：`func NewSubscriptionMaintenanceQueue(workerCount, queueSize int) *SubscriptionMaintenanceQueue {`
- Git 内容 SHA-256：`b66e9ac47204a41d4df68df0f6d792f812378e164dbba0c359fb6ee03a0bd085`

## S46 · 父订阅批量状态更新不能主导 term 权益

- 路径：`backend/internal/repository/user_subscription_repo.go`
- 行号：505
- 锚点：`func (r *userSubscriptionRepository) BatchUpdateExpiredStatus(ctx context.Context) (int64, error) {`
- Git 内容 SHA-256：`12ce7309fa1733ecc718ee6e5622b132fd5a6b414d40b3ea2b9d575c077ac135`

## S47 · 统计补写需尊重已完成的日志清理

- 路径：`backend/internal/repository/usage_cleanup_repo.go`
- 行号：285
- 锚点：`func (r *usageCleanupRepository) DeleteUsageLogsBatch(ctx context.Context, filters service.UsageCleanupFilters, limit int) (int64, error) {`
- Git 内容 SHA-256：`886a5a7badf4e18ffde92da922486bd053b5effce07fdb3c2da3129e1124b059`
