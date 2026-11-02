# 海上列车 JPEG 截图修复上线记录

状态：**已上线并验证**。版本：**2.8.13-monas-bps.5**。

- 切换时间：2026-09-26 17:24:52（北京时间）。
- 当前生产槽：blue；服务 healthy，无重启、无 OOM。
- 旧 green 实例已自然排空，并以退出码 0 停止；旧镜像和容器保留用于应用版本回退。
- 相关会话：查找3D冒险岛技术方案，ID 01a0dcd8-1eb8-7f03-b715-95b622356d14。

## 修复内容

JPEG 附件上传固定使用 image.jpg；PNG、GIF、WebP 也使用上游接受的固定后缀，避免受到 Go 或操作系统 MIME 扩展名排序影响。原先 Go 1.27 把 .jfif 排在首位，导致附件上传成功但模型推理拒绝该文件。图片内容与 MIME 类型保持一致，未通过改写图片内容解决问题。

新实例使用新的进程内附件缓存，不会沿用旧进程中由错误文件名生成的 file_id。用户可在原会话发送“继续”；本次未代替用户启动原会话的软件安装任务。

## 验证结果

- 回归测试先在旧逻辑上失败，明确捕获 image.jfif 与期望 image.jpg 的差异；修复后通过。
- 覆盖 JPEG 用户图片、工具截图及历史重放，以及流式和非流式转发；固定文件名测试覆盖 JPEG/PNG/GIF/WebP。
- 完整 basispoints 包测试通过，相关 Excel BPS、图片、附件与工具转发测试通过，git diff --check 通过。
- 候选实例 6 次实测全部完成；生产公网 5 次实测全部完成并返回正确答案。
- 通过访问日志中的网关请求 ID 与 client_request_id 关联用量表，候选 6 条、生产 5 条记录全部存在，并确认走 /basispoints/api/responses。
- 2026-09-26 17:24:52 至 2026-09-26 17:26:19 的观察窗口内，JPEG 格式错误 0 条，总错误 0 条。此结果仅代表已核查窗口。
- 健康检查返回 200，未认证模型列表返回预期 401；旧实例停止后再次检查通过。

| 生产测试 | 网关请求 ID | 结果 |
| --- | --- | --- |
| JPEG 工具截图 | 99ce39ca-b6af-4919-bf87-64430634bda6 | HTTP 200 / response.completed / 正确识别 |
| JPEG 历史截图重放 | 7c587cae-d9a6-4a9d-8471-2127eb7dceac | HTTP 200 / response.completed / 正确识别 |
| JPEG 非流式传图 | bc4d6346-f355-4e5f-9f02-73f398a8dabf | HTTP 200 / response.completed / 正确识别 |
| PNG 工具截图 | fa57b090-b157-46c3-92ea-1dbb9892840c | HTTP 200 / response.completed / 正确识别 |
| 纯文本 | 435f000a-ff49-430a-9288-6ee96e111bee | HTTP 200 / response.completed / 正确识别 |

## 工件与回退

- 修复提交：fd43b3478b1f4775e6ae4a4f04c19a52eab951cc
- 镜像：sub2api:2.8.13-monas-bps.5
- 镜像 ID：sha256:404f8e80e6c97d333140293bd0899dc0e5305a8a3195274f8021d31cbdab0efd
- 二进制 SHA256：c7073096df248d939747eeca2f314d074c5d95201bf01aa1d12b9eaf970ac068
- 构建时间：2026-09-26T09:18:19Z
- 发布前备份：/home/linuxuser/apps/sub2api/backups/automated/20260926T091707Z，完整性校验通过。
- 服务器发布记录及源补丁：/root/bps-jpeg-fix-20260926。
- 本地修复分支：codex/bps-jpeg-extension-20260926。
- 回退版本：2.8.13-monas-bps.4（保留的 green 容器）。必要时先启动 green 并检查健康，再在发布锁保护下通过现有 switch-slot.sh green 切换路由。该旧版本仍有本次 JPEG 后缀问题。
- 本次没有数据库 schema 迁移；应用回退不得覆盖已继续产生业务数据的生产数据库。
