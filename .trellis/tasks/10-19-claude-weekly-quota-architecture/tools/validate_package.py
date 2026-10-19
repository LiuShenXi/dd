"""Architecture artifact checks only. Does not execute SQL or application tests."""
import hashlib
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import unquote

import jsonschema

ROOT = Path(__file__).resolve().parents[1]
errors = []
counts = {}

def require(condition, message):
    if not condition:
        errors.append(message)

def load(relative):
    return json.loads((ROOT / relative).read_text(encoding="utf-8-sig"))

required = ["README.md", "prd.md", "design.md", "decisions.md", "data-model.md",
            "billing-protocol.md", "api-contracts.md", "integration-contracts.md", "ui-contracts.md",
            "implement.md", "test-plan.md", "traceability.md", "migration-rollout.md", "operations.md",
            "sol-handoff.md", "task.json", "implement.jsonl", "check.jsonl", "contracts/openapi.json",
            "contracts/schema.sql.example", "evidence/production-baseline.json", "evidence/production-recheck.json",
            "evidence/local-baseline.json", "evidence/code-index.md", "evidence/architecture-review.md", "tools/README.md"]
for relative in required:
    require((ROOT / relative).is_file(), "required file missing: " + relative)

json_count = 0
for file in ROOT.rglob("*.json"):
    json.loads(file.read_text(encoding="utf-8-sig"))
    json_count += 1
jsonl_count = 0
for file in ROOT.glob("*.jsonl"):
    for n, line in enumerate(file.read_text(encoding="utf-8").splitlines(), 1):
        if line.strip():
            entry = json.loads(line)
            require("file" in entry and "reason" in entry, f"bad context record: {file.name}:{n}")
            jsonl_count += 1

link_count = 0
for file in ROOT.rglob("*.md"):
    content = file.read_text(encoding="utf-8")
    require("openapi.yaml" not in content, "stale OpenAPI extension: " + file.name)
    for target in re.findall(r"\[[^\]]+\]\(([^)]+)\)", content):
        target = target.strip("<>").split("#", 1)[0]
        if not target or re.match(r"^[a-zA-Z]+://", target):
            continue
        link_count += 1
        require((file.parent / unquote(target)).exists(), f"broken link: {file.name} -> {target}")

doc = load("contracts/openapi.json")
require(doc.get("openapi") == "3.1.0", "unexpected OpenAPI version")
require(doc.get("servers") == [{"url": "/api/v1"}], "API prefix mismatch")
schemas = doc["components"]["schemas"]
def walk(value):
    if isinstance(value, dict):
        if "$ref" in value:
            pointer = value["$ref"]
            require(pointer.startswith("#/"), "external schema ref not permitted")
            current = doc
            try:
                for part in pointer[2:].split("/"):
                    current = current[part.replace("~1", "/").replace("~0", "~")]
            except (KeyError, TypeError):
                errors.append("unresolved ref: " + pointer)
        for child in value.values():
            walk(child)
    elif isinstance(value, list):
        for child in value:
            walk(child)
walk(doc)
for name, schema in schemas.items():
    try:
        jsonschema.Draft202012Validator.check_schema(schema)
    except jsonschema.SchemaError as exc:
        errors.append("invalid JSON Schema " + name + ": " + exc.message)

opids = []
for path, path_item in doc["paths"].items():
    for method, operation in path_item.items():
        opids.append(operation["operationId"])
        require(bool(operation.get("security")), "missing auth: " + path)
        params = operation["parameters"]
        for name in re.findall(r"\{([^}]+)\}", path):
            require(any(p["in"] == "path" and p["name"] == name and p["required"] for p in params), "missing path param: " + path)
        if method == "post" and not path.endswith("/preview"):
            require(any(p["in"] == "header" and p["name"] == "Idempotency-Key" and p["required"] for p in params), "missing idempotency: " + path)
require(len(opids) == len(set(opids)), "duplicate operation id")

base = {"group_id": 6, "operation": "open", "expected_revision": 0}
cases = [
    ("A100", "OpenTermInput", {**base, "quota_usd": "100.00000000"}, True),
    ("B300", "OpenTermInput", {**base, "quota_usd": "300"}, True),
    ("plan default", "OpenTermInput", {**base, "plan_id": 1}, True),
    ("no quota", "OpenTermInput", base, False),
    ("smallest unit", "OpenTermInput", {**base, "quota_usd": "0.00000001"}, True),
]
for value in ["0", "0.00000000", "-1", "NaN", "Infinity", "1e3", "1.000000001", "1000000000000", "", None, 100]:
    cases.append(("reject " + repr(value), "OpenTermInput", {**base, "quota_usd": value}, False))
cases += [
    ("unknown field", "OpenTermInput", {**base, "quota_usd": "1", "platform": "openai"}, False),
    ("read-only preview", "PreviewInput", {"group_id": 6, "operation": "renew", "quota_usd": "100"}, True),
    ("cancel", "CancelInput", {"expected_revision": 2, "reason": "operator cancellation"}, True),
    ("no-charge evidence", "ResolveInput", {"expected_request_revision": 2, "resolution": "no_charge", "reason": "verified", "evidence_reference": "audit:synthetic-1"}, True),
    ("no-charge cannot inject amount", "ResolveInput", {"expected_request_revision": 2, "resolution": "no_charge", "reason": "verified", "evidence_reference": "audit:synthetic-1", "actual_cost_usd": "0"}, False),
]
case_results = []
for label, name, value, expected in cases:
    schema = {"$ref": "#/components/schemas/" + name, "components": doc["components"]}
    valid = jsonschema.Draft202012Validator(schema, format_checker=jsonschema.FormatChecker()).is_valid(value)
    require(valid == expected, "schema example mismatch: " + label)
    case_results.append({"case": label, "schema": name, "expected_valid": expected, "actual_valid": valid})

prd = (ROOT / "prd.md").read_text(encoding="utf-8")
test = (ROOT / "test-plan.md").read_text(encoding="utf-8")
trace = (ROOT / "traceability.md").read_text(encoding="utf-8")
rids = re.findall(r"^\| (R\d{2}) \|", prd, re.M)
tids = re.findall(r"^\| (T\d{2}) \|", test, re.M)
require(rids == [f"R{x:02d}" for x in range(1, 19)], "requirement sequence mismatch")
require(tids == [f"T{x:02d}" for x in range(1, 63)], "test sequence mismatch")
require(set(tids).issubset(set(re.findall(r"\bT\d{2}\b", trace))), "unmapped acceptance case")
require(set(rids).issubset(set(re.findall(r"\bR\d{2}\b", trace))), "unmapped requirement")

prod = load("evidence/production-baseline.json")
recheck = load("evidence/production-recheck.json")
baseline = load("evidence/local-baseline.json")
for key in ["image_id", "revision", "binary_sha256"]:
    require(prod[key] == recheck[key], "live snapshot drift: " + key)
require(baseline["head_matches_snapshot"], "source HEAD mismatch")
require(not baseline["findings"], "source comparison has unresolved findings")
require(not baseline["tracked_status"], "source tracked working tree not clean")
require(all(f["line"] is not None and f["anchor"] for f in baseline["files"]), "empty code anchor")
require(baseline["migration_counts"]["exact"] == len(prod["migrations"]), "migration comparison not all exact")

sql = (ROOT / "contracts/schema.sql.example").read_text(encoding="utf-8")
tables = re.findall(r"CREATE TABLE (subscription_quota_\w+)", sql)
require(set(tables) == {"subscription_quota_terms", "subscription_quota_requests", "subscription_quota_ledger", "subscription_quota_operations"}, "SQL table set mismatch")
for marker in ["pricing_snapshot JSONB", "usage_log_recorded_at TIMESTAMPTZ", "revision BIGINT", "INTERVAL '168 hours'", "actual_cost_usd IS NOT NULL AND actual_cost_usd = 0"]:
    require(marker in sql, "SQL design field/guard missing: " + marker)

counts.update(json_files=json_count, jsonl_records=jsonl_count, markdown_links=link_count,
              openapi_operations=len(opids), json_schemas=len(schemas), schema_examples=len(cases),
              requirements=len(rids), acceptance_cases=len(tids), source_files=len(baseline["files"]),
              exact_migrations=baseline["migration_counts"]["exact"])
receipt = {"checked_at_utc": datetime.now(timezone.utc).isoformat(), "result": "pass" if not errors else "fail",
           "counts": counts, "errors": errors, "schema_examples": case_results,
           "limitations": ["Architecture static checks only", "No complete OpenAPI spec validator was installed; references, operation shape and JSON Schema examples checked", "SQL draft not executed", "No application implementation, business tests, browser tests or production writes"]}
(ROOT / "evidence/validation-receipt.json").write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
manifest = []
for path in sorted(ROOT.rglob("*")):
    if path.is_file() and path.name != "package-manifest.json" and "__pycache__" not in path.parts:
        manifest.append({"path": path.relative_to(ROOT).as_posix(), "bytes": path.stat().st_size,
                         "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
(ROOT / "evidence/package-manifest.json").write_text(json.dumps({"created_at_utc": receipt["checked_at_utc"], "files": manifest}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps({"result": receipt["result"], "counts": counts, "errors": errors}, ensure_ascii=False))
sys.exit(1 if errors else 0)
