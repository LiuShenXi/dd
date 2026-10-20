# 2026-10-20 用户要求精简界面：已正式发布

- 删除“我的订阅”、密钥管理、Key 用量查询的并发说明整条。
- 删除订阅卡中的逐请求用量表和对应明细加载；保留卡期历史。
- 直接复用现有 `/usage` 分组 / Key / 日期筛选、汇总与分页；正式界面已查到 group7 的真实调用：4105/1 Token，费用 0.000822。没有新增统计接口。
- 修改提交 `bbb4903c488e56736bd2cbee08498af6ac41b5ab`；镜像 `sub2api:2.8.13-monas-claude-weekly.2-bbb4903c488e`，不可变 ID `sha256:3691b498ce6a5ac1979dec0266d014fad9acfa387078a32188c5b59e93ddaac1`。
- Green `sub2api2-green` / 29081 / `cb20cb0f9c564fc18f94f3c1c1d24e0321d6dc9625bc09331bbe0d11be845d67` 健康，两个正式域名确认同一实例，后台 leader active。Blue 自然排空后停止、restart=no；PG/Redis 身份、启动时间、重启次数不变。enabled/protected仍仅[7]。
- 43 项相关前端测试、3 项语言键测试、受影响文件 ESLint、Vue 类型检查、Vite 嵌入式构建、34 项控制器测试通过。两个域名250次健康采样全200（采样不等于逐条请求审计）。
- 发布后实际浏览器验证：版本 .2、订阅页并发整条消失、展开历史没有用量明细表、Key页并发整条消失；测试卡剩余0.999178。
- 备份在 `/root/sub2api-claude-cleanup-20261020/backup/`，回执见本目录 cleanup-*.json；不保存认证信息。后台恢复 timer 已恢复 active。
- 本地初次打包因缓存/链接阶段磁盘不足失败；仅清理本任务旧可再生成二进制后重试成功，构建后释放 Go 缓存。没有改动线上数据或为构建停止服务。
- 使用教程：`docs/CLAUDE_WEEKLY_CARD_GUIDE.md` 与本地知识库副本保持一致；未 push 或合并 main。

![正式新版订阅页](/Users/shenxi/Desktop/WORK-SPACE/dd/validation/claude-weekly-quota/ui-activation-20261020/user-card-clean.png)
