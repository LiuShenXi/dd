"""Read-only local/source comparison; only writes this architecture package evidence.

Does not connect to production, fetch Git, migrate a database or modify source.
Use a separately refreshed production snapshot for future verification.
"""
import argparse
import hashlib
import json
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ANCHORS = [
    ("S01", "backend/internal/repository/api_key_repo.go", r"^func .*projectCarpoolBilling", "用户拼车绑定投影及其他分组拒绝；需要窄范围 Claude term 例外"),
    ("S02", "backend/internal/repository/carpool_repository.go", r"^func prepareCarpoolUserGroupTx", "开卡及续卡的全部 Key 同组约束；保留余额与绑定要求"),
    ("S03", "backend/migrations/244_carpool_balance_preserving_release.sql", r"CREATE TABLE", "逐用户拼车绑定、现有金额保存协议"),
    ("S04", "backend/internal/service/api_key_service.go", r"^func .*GetAvailableGroups", "可选分组与有效订阅权限"),
    ("S05", "backend/internal/server/middleware/api_key_auth.go", r"subscriptionService != nil", "原订阅存在性、窗口检查及缺失依赖的余额路径"),
    ("S06", "backend/internal/service/billing_cache_service.go", r"^func .*CheckBillingEligibility", "nil subscription 可能变成普通计费；最终 term 分派必须明确"),
    ("S07", "backend/internal/service/user_subscription.go", r"^func .*NeedsWeeklyReset", "原每周窗口重置语义不是七日卡快照"),
    ("S08", "backend/internal/service/subscription_service.go", r"^func .*updateExistingSubscriptionTerm", "原续费修改父记录；新 term 应先分派"),
    ("S09", "backend/ent/schema/user_subscription.go", r"^func .*Fields", "父记录计数与有效期模型"),
    ("S10", "backend/ent/schema/subscription_plan.go", r"^func .*Fields", "套餐缺逐期额度字段"),
    ("S11", "backend/ent/schema/group.go", r"^func .*Fields", "平台、订阅模式、共享日周月限额"),
    ("S12", "backend/internal/service/subscription_bulk_action.go", r"^func .*BulkSubscriptionAction", "旧批量操作必须按模式保护"),
    ("S13", "backend/internal/service/usage_billing.go", r"const UsageBillingMonetaryScale", "八位量化、旧指纹顺序、来源互斥与 receipt v1"),
    ("S14", "backend/internal/repository/usage_billing_repo.go", r"^func .* Apply\(", "统一去重和事务内账务效果"),
    ("S15", "backend/internal/service/gateway_usage_billing.go", r"^func .* RecordUsage\(", "ActualCost、模型计价、旧失败日志和 legacy 兜底"),
    ("S16", "backend/internal/service/carpool_gateway_billing.go", r"^type CarpoolGatewayBilling", "现有准入和耐久凭据接口；参考模式但不修改旧协议"),
    ("S17", "backend/internal/handler/gateway_handler.go", r"^func .* Messages\(", "Messages 实际分支、跨组 fallback 和用量入口"),
    ("S18", "backend/internal/handler/gateway_handler_chat_completions.go", r"^func .* ChatCompletions\(", "Claude OpenAI Chat 适配入口"),
    ("S19", "backend/internal/handler/gateway_handler_responses.go", r"^func .* Responses\(", "Claude Responses 适配入口"),
    ("S20", "backend/internal/server/routes/gateway.go", r"^func ", "真实端点、别名和强制平台路由"),
    ("S21", "backend/internal/service/payment_order.go", r"^func .* CreateOrder\(", "订单创建快照"),
    ("S22", "backend/internal/service/payment_fulfillment.go", r"^func .*ensurePaymentSubscriptionAssigned", "履约订阅事务、幂等和 outbox"),
    ("S23", "backend/internal/service/payment_config_plans.go", r"^func .* GetGroupInfoMap", "商品信息当前使用分组限额"),
    ("S24", "backend/internal/service/payment_refund.go", r"ValidityDays|ReduceSubscription|reduceSubscription|subscription", "退款按天撤回路径需 mode guard"),
    ("S25", "backend/internal/service/redeem_service.go", r"AssignOrExtendSubscription", "旧订阅兑换不能绕过卡期领域"),
    ("S26", "backend/internal/service/api_key_auth_cache_impl.go", r"apiKeyAuthSnapshotVersion", "鉴权快照版本和新字段兼容"),
    ("S27", "backend/internal/repository/usage_log_repo_insert.go", r"^func ", "成功统计日志写入与既有幂等"),
    ("S28", "backend/internal/server/routes/admin.go", r"subscriptions :=", "管理订阅路由清单"),
    ("S29", "backend/internal/server/routes/user.go", r"subscriptions :=", "用户订阅路由清单"),
    ("S30", "backend/internal/pkg/response/response.go", r"^type Response", "后台实际响应 envelope"),
    ("S31", "backend/internal/service/billing_service.go", r"^func .* CalculateCostUnified", "复用定价规则，扩展可冻结的只读价格解析"),
    ("S32", "backend/internal/repository/migrations_runner.go", r"content := strings.TrimSpace", "迁移哈希算法、notx 与历史兼容白名单"),
    ("S33", "backend/internal/repository/integration_harness_test.go", r"SUB2API_TEST_POSTGRES_IMAGE", "隔离 PostgreSQL 集成 harness"),
    ("S34", "frontend/src/views/admin/SubscriptionsView.vue", r"weekly_limit_usd", "管理端共享组限额展示需要模式分派"),
    ("S35", "frontend/src/views/user/SubscriptionsView.vue", r"weekly_limit_usd", "用户订阅额度展示"),
    ("S36", "frontend/src/components/common/SubscriptionProgressMini.vue", r"weekly_limit_usd", "迷你进度的模式契约"),
    ("S37", "frontend/src/components/payment/SubscriptionPlanCard.vue", r"weekly_limit_usd", "商品卡默认额度展示"),
    ("S38", "frontend/src/utils/subscriptionQuota.ts", r"^export function isOneTimeDailyQuota", "现有期限解释工具，不能按期限猜新模式"),
    ("S39", "backend/internal/repository/api_key_carpool_binding_test.go", r"^func Test", "原其他组拒绝测试需拆成窄例外与防绕过"),
    ("S40", "backend/internal/repository/carpool_billing_binding_integration_test.go", r"^func Test", "GPT 绑定跨到期及余额保护回归"),
    ("S41", "backend/internal/repository/carpool_standard_open_integration_test.go", r"^func Test", "GPT 开通和续费真数据库测试"),
    ("S42", "backend/internal/service/api_key_auth_cache_carpool_binding_test.go", r"^func Test", "两种 Key 的缓存投影隔离"),
    ("S43", "backend/internal/service/gateway_service_subscription_billing_test.go", r"^func Test", "用户实际消费与上游 TotalCost 区分"),
    ("S44", "backend/internal/service/subscription_expiry_service.go", r"^func .*runOnce", "父订阅到期更新与邮件提醒，term 需隔离"),
    ("S45", "backend/internal/service/subscription_maintenance_queue.go", r"^func ", "原生窗口维护队列的模式保护"),
    ("S46", "backend/internal/repository/user_subscription_repo.go", r"^func .*BatchUpdateExpiredStatus", "父订阅批量状态更新不能主导 term 权益"),
    ("S47", "backend/internal/repository/usage_cleanup_repo.go", r"^func .*DeleteUsageLogsBatch", "统计补写需尊重已完成的日志清理"),
]

def digest(data):
    return hashlib.sha256(data).hexdigest()
def run_git(source, *args):
    return subprocess.check_output(["git", "-C", str(source), *args])

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--output", default="evidence/local-baseline.recheck.json")
    parser.add_argument("--write-index", action="store_true")
    args = parser.parse_args()
    source = args.source.resolve()
    prod = json.loads((ROOT / "evidence/production-baseline.json").read_text(encoding="utf-8-sig"))
    head = run_git(source, "rev-parse", "HEAD").decode().strip()
    status = run_git(source, "status", "--short", "--untracked-files=no").decode().strip()
    findings = []
    if head != prod["revision"]:
        findings.append("source HEAD differs from production snapshot revision")
    if status:
        findings.append("source has tracked working changes; inspect before implementation")
    runner = run_git(source, "show", "HEAD:backend/internal/repository/migrations_runner.go").decode()
    rules = {}
    for name, body in re.findall(r'"([^"\n]+\.sql)":\s*newMigrationChecksumCompatibilityRule\(([^\n]+)\)', runner):
        rules[name] = re.findall(r'"([0-9a-f]{64})"', body)
    manifest = []
    for migration in prod["migrations"]:
        name = migration["filename"]
        path = "backend/migrations/" + name
        try:
            blob = run_git(source, "show", "HEAD:" + path)
            local_sha = digest(blob.decode().strip().encode())
            outcome = "exact" if local_sha == migration["checksum"] else "mismatch"
            allowed = rules.get(name, [])
            if outcome == "mismatch" and local_sha in allowed and migration["checksum"] in allowed:
                outcome = "declared_legacy_compatibility"
            manifest.append({"filename": name, "production_sha256": migration["checksum"],
                             "git_content_trimmed_sha256": local_sha, "comparison": outcome})
            if outcome == "mismatch":
                findings.append("migration requires review: " + name)
        except subprocess.CalledProcessError:
            manifest.append({"filename": name, "comparison": "missing"})
            findings.append("source migration missing: " + name)
    files = []
    for sid, path, pattern, purpose in ANCHORS:
        file = source / path
        if not file.is_file():
            findings.append("source file missing: " + path)
            continue
        content = file.read_text(encoding="utf-8")
        matched = [(n, line.strip()) for n, line in enumerate(content.splitlines(), 1) if re.search(pattern, line)]
        if not matched:
            findings.append("anchor not found: " + sid + " " + pattern)
        files.append({"id": sid, "path": path, "sha256_raw_worktree": digest(file.read_bytes()),
                      "sha256_git_blob": digest(run_git(source, "show", "HEAD:" + path)),
                      "anchor_pattern": pattern, "line": matched[0][0] if matched else None,
                      "anchor": matched[0][1] if matched else None, "purpose": purpose})
    receipt = {"checked_at_utc": datetime.now(timezone.utc).isoformat(), "source_root": source.as_posix(),
               "production_snapshot_time": prod["checked_at_utc"], "head": head, "tracked_status": status,
               "head_matches_snapshot": head == prod["revision"],
               "migration_hash_algorithm": "SHA256(TrimSpace(git HEAD file UTF-8 content)); see S32. Git blob avoids Windows checkout newline changes.",
               "migration_counts": {k: sum(x["comparison"] == k for x in manifest) for k in ["exact", "declared_legacy_compatibility", "mismatch", "missing"]},
               "migrations": manifest, "files": files, "findings": findings,
               "scope": "Local comparison to a saved read-only production snapshot. This is not a new live production check."}
    output = (ROOT / args.output).resolve()
    if ROOT not in output.parents:
        raise ValueError("output must remain in this package")
    output.write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    if args.write_index:
        lines = ["# 生产对应代码索引", "", "提交：`" + head + "`。下列行号来自对应生产的实际源码文件；完整摘要见 [本地基线](local-baseline.json)。", "",
                 "此表描述现有实现和改动接点，不表示已实现周卡。文件以生产源码根目录为基准。", ""]
        for f in files:
            lines += ["## " + f["id"] + " · " + f["purpose"], "",
                      "- 路径：`" + f["path"] + "`", "- 行号：" + str(f["line"]),
                      "- 锚点：`" + str(f["anchor"]).replace("`", "'") + "`",
                      "- Git 内容 SHA-256：`" + f["sha256_git_blob"] + "`", ""]
        (ROOT / "evidence/code-index.md").write_text("\n".join(lines), encoding="utf-8")
    print(json.dumps({"head_matches": head == prod["revision"], "source_files": len(files),
                      "migration_counts": receipt["migration_counts"], "findings": findings}, ensure_ascii=False))

if __name__ == "__main__":
    main()
