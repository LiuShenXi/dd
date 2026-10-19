# Claude 周卡前端与交互契约

## 后台开卡

在用户详情或订阅管理增加“开通 Claude 周卡”。字段为用户、Claude 分组、可选套餐、当期额度 USD、备注。有效期固定显示七天，售价在套餐或订单处显示，不能与 USD 使用额度混成一个输入框。

选套餐时填入默认额度，管理员可覆盖。变更用户、分组或套餐后重新生成预览；当前卡和已有续卡放在预览中，避免把 renew 当立即补额度。

提交必须显示实际生效和到期时间、当前卡额度、下一卡额度。续卡按钮明确“到当前排期结束后生效”，当前卡耗尽时不能宣称已经立即补满。

同批不同用户额度采用逐行输入或逐人操作，不复用统一分组限额。第一版不必新增大批量开卡 UI；后台逐用户操作满足 R01，bulk-assign 旧入口必须拒绝缺少额度的新模式请求。

## 异步与重复提交

沿用现有拼车 UI 的 modalGeneration 与目标身份防护：

1. 打开弹窗固定用户、分组、请求代次。
2. 创建预览请求时捕获完整载荷，旧响应不能覆盖新用户表单。
3. 提交时复制载荷并生成一次 Idempotency-Key；网络重试复用同一键。
4. 编辑载荷后生成新键并重做预览；不能用旧键提交新额度。
5. success、catch 和 finally 都检查弹窗代次及身份。
6. 关闭弹窗不等于取消服务器已提交操作；重新打开查询真实结果。

revision 冲突显示“排期已变化，请重新预览”，刷新摘要后保留管理员输入的额度，不能悄悄以新默认值替换。

## 用户额度页面

保持现有 GPT 拼车卡片与功能入口。新增 Claude 卡片，显示：

- 当前卡额度、已结算用量、已知待结算费用、可用剩余和进度。
- 生效时间、到期时间和服务器校准的倒计时。
- 当前状态与下一期摘要。
- 用量明细按本卡过滤。
- 有待结算请求时说明“部分请求正在结算”，不把它们估算为零费用。

不显示“总余额等于 GPT 加 Claude”。不提供互转、共用或自动补扣开关。到期倒计时到零只触发重新拉取，不能由前端自行清零已用或生成下一卡。

同一用户多个 Claude Key 显示同一份卡余额，不按 Key 分别复制卡面额度。API Key 自身限制另标“此 Key 的使用上限”，不作为另一份可用金额。

## 状态展示

| 状态 | 文案和动作 |
| --- | --- |
| 未开通 | 显示没有 Claude 周卡，仍保留 GPT 卡片 |
| scheduled | 显示生效时间，不能调用新卡 |
| active | 显示额度和用量 |
| exhausted | 显示本卡额度已用完；下一期按真实时间显示 |
| reconciling | 显示本卡有用量待核查，暂不可调用；不影响 GPT |
| expired | 显示已到期，历史用量可查看 |
| cancelled | 显示已取消，不能恢复旧额度 |
| closing / 待核查 | 用户看到已停止可用及待结算说明；管理员看到处理入口 |

卡片金额来源统一为服务端 quota_summary。旧 group.daily_limit_usd、weekly_limit_usd、monthly_limit_usd 仅用于 windows 模式，不在多个组件各自写“覆盖值优先”的逻辑。

## 文件边界

新增专用 API 类型与金额字符串类型，不让 UI 任意解析修改财务值。扩展现有 subscriptions store，通过 quota_mode 判别联合类型，windows 模式组件保持原行为。

预期涉及：

- frontend/src/api/admin/subscriptions.ts 与新增 subscriptionQuota API。
- frontend/src/api/subscriptions.ts、frontend/src/types/index.ts 或现有类型入口。
- frontend/src/views/admin/SubscriptionsView.vue。
- 新增后台 SubscriptionQuotaTermModal.vue 与其预览/请求组合逻辑。
- frontend/src/views/user/SubscriptionsView.vue。
- frontend/src/components/common/SubscriptionProgressMini.vue。
- 套餐默认值与 SubscriptionPlanCard.vue，订单确认页展示冻结额度。
- 中英文 i18n；不能把状态枚举当中文文案。
- GPT CarpoolView、CarpoolDetailsView 仅在必要并存导航处调整，不重排其周期和重置交互。

最终文件名以 I0 的实际目录确认结果为准；不要只为本功能进行大范围组件重命名或状态管理重构。

## 可用性验收

后台桌面和窄屏都能完整显示金额与时间；金额输入有 label、错误反馈和键盘操作；色彩之外还有状态文本。需真实浏览器验证用户切换、迟到响应、重复提交、到期刷新和多卡显示。Vitest 通过不能替代实际最终镜像的 UI 验收。

