# 架构包核验工具

工具仅生成或检查本包文档与契约，不修改业务源码，不执行 SQL，不连接生产。真实生产复核通过受控只读 SSH 单独完成，初版和 v2 结果分别保存在 evidence/production-recheck.json、evidence/concurrency-production-recheck.json。

## 运行方式

在本包目录运行，路径按实际复制后的工作区调整：

```powershell
python tools/build_openapi.py
python tools/verify_baseline.py --source C:/Users/Administrator/.codex/worktrees/web-search-history-compat/dd
python tools/validate_package.py
```

verify_baseline 默认输出新的 local-baseline.recheck.json，不覆盖原交付证据。它比较的是保存的生产快照，不会自行判断今天线上是否变了。开发前先由执行者重新只读取得生产版本，再与对应源码核验。

build_openapi 会重写本包的 contracts/openapi.json。修改接口时更新生成脚本并重建，避免两个来源漂移。validate_package 使用 Python jsonschema；当前本机已具备，其他机器先检查环境，不要把缺依赖误报为业务错误。

validate_package 核对链接、JSON/JSONL、内部 API 引用、JSON Schema 示例、需求及测试编号映射、代码锚点、生产证据一致性与 SQL 草案关键字段。它不是完整 OpenAPI 标准验证器，也不执行数据库约束测试。运行成功生成 validation-receipt.json 和带 SHA-256 的 package-manifest.json。

SQL 和 API 描述都是拟实现设计。真正的 Go、PostgreSQL、前端、路由及发布验证命令在 implement.md；这些业务检查本轮尚未执行。
