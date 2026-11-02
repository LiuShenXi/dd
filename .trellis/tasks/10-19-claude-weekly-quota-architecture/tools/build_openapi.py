"""Generate the design-only OpenAPI contract. No network or application writes."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
def ref(name):
    return {"$ref": "#/components/schemas/" + name}
def obj(properties, required=None):
    return {"type": "object", "additionalProperties": False, "properties": properties,
            "required": list(properties) if required is None else required}
def array(schema):
    return {"type": "array", "items": schema}
def nullable(schema):
    return {"anyOf": [schema, {"type": "null"}]}
def enum(*values):
    return {"type": "string", "enum": list(values)}
ID = {"type": "integer", "format": "int64", "minimum": 1}
REV = {"type": "integer", "format": "int64", "minimum": 0}
DT = {"type": "string", "format": "date-time"}
TEXT = {"type": "string"}
NOTE = {"type": "string", "minLength": 1, "maxLength": 500}
BOOL = {"type": "boolean"}
money_input = {"type": "string", "pattern": r"^(0|[1-9][0-9]{0,11})(\.[0-9]{1,8})?$",
               "not": {"pattern": r"^0(\.0+)?$"},
               "maxLength": 21, "description": "十进制字符串；服务端另外校验大于零；拒绝科学计数和多余小数位。"}
money = {"type": "string", "pattern": r"^(0|[1-9][0-9]{0,11})\.[0-9]{8}$"}
schemas = {
    "MoneyInput": money_input, "Money": money,
    "Error": obj({"code": {"type": "integer", "minimum": 400, "maximum": 599},
                  "message": TEXT, "reason": TEXT,
                  "metadata": {"type": "object", "additionalProperties": {"type": "string"}}},
                 ["code", "message", "reason"]),
}
term_props = {"id": ID, "group_id": ID, "subscription_id": ID, "term_no": ID,
              "quota_usd": ref("Money"), "used_usd": ref("Money"), "pending_usd": ref("Money"), "remaining_usd": ref("Money"),
              "status": enum("scheduled", "active", "exhausted", "expired", "cancelled", "reconciling"),
              "lifecycle_state": enum("scheduled", "active", "closing", "closed"),
              "starts_at": DT, "expires_at": DT, "cancelled_at": nullable(DT),
              "revision": REV, "unsettled_request_count": REV}
schemas["Term"] = obj(term_props)
schemas["Summary"] = obj({"group_id": ID, "quota_mode": {"const": "term"}, "revision": REV,
                           "server_time": DT, "current_term": nullable(ref("Term")),
                           "last_term": nullable(ref("Term")), "next_terms": array(ref("Term"))})
open_fields = {"group_id": ID, "operation": enum("open", "renew"), "quota_usd": ref("MoneyInput"),
               "plan_id": ID, "expected_revision": REV, "notes": {"type": "string", "maxLength": 500}}
schemas["OpenTermInput"] = obj(open_fields, ["group_id", "operation", "expected_revision"])
schemas["OpenTermInput"]["anyOf"] = [{"required": ["quota_usd"]}, {"required": ["plan_id"]}]
schemas["PreviewInput"] = obj({k: v for k, v in open_fields.items() if k != "expected_revision"},
                               ["group_id", "operation"])
schemas["PreviewInput"]["anyOf"] = [{"required": ["quota_usd"]}, {"required": ["plan_id"]}]
schemas["Preview"] = obj({"group_id": ID, "effective_quota_usd": ref("Money"),
                           "proposed_starts_at": DT, "proposed_expires_at": DT, "server_time": DT,
                           "current_revision": REV, "warnings": array(TEXT),
                           "current_term": nullable(ref("Term")), "next_terms": array(ref("Term"))})
schemas["TermMutationResult"] = obj({"term": ref("Term"), "summary": ref("Summary"), "replayed": BOOL})
schemas["CancelInput"] = obj({"expected_revision": REV, "reason": NOTE})
schemas["VerifiedUsage"] = obj({
    "requested_model": TEXT, "upstream_model": TEXT, "response_model": TEXT,
    "input_tokens": REV, "output_tokens": REV, "cache_creation_5m_tokens": REV,
    "cache_creation_1h_tokens": REV, "cache_read_tokens": REV,
    "service_tier": TEXT, "search_call_count": REV,
}, ["input_tokens", "output_tokens", "cache_creation_5m_tokens", "cache_creation_1h_tokens", "cache_read_tokens"])
schemas["VerifiedUsage"]["description"] = "仅用于管理员凭证核查；身份和定价从原请求取；模型必须属于原冻结候选，非负整数，不接受费用或原始结算命令。"
resolve_common = {"expected_request_revision": REV, "evidence_reference": {"type": "string", "minLength": 1, "maxLength": 500}, "reason": NOTE}
schemas["ResolveInput"] = {"oneOf": [
    obj({**resolve_common, "resolution": {"const": "no_charge"}}),
    obj({**resolve_common, "resolution": {"const": "verified_usage"}, "verified_usage": ref("VerifiedUsage")})
]}
schemas["Request"] = obj({"id": ID, "request_id": TEXT, "user_id": ID, "group_id": ID,
                           "term_id": ID, "api_key_id": ID,
                           "status": enum("admitted", "usage_known", "settled", "no_charge", "reconcile_required"),
                           "revision": REV, "actual_cost_usd": nullable(ref("Money")),
                           "admitted_at": DT, "updated_at": DT, "settled_at": nullable(DT),
                           "last_error_category": nullable(TEXT)})
schemas["ResolveResult"] = obj({"request": ref("Request"), "replayed": BOOL})
schemas["UsageItem"] = obj({"request_id": TEXT, "api_key_id": ID, "term_id": ID,
                             "model": TEXT, "actual_cost_usd": ref("Money"), "settled_at": DT,
                             "input_tokens": REV, "output_tokens": REV})
for item in ["Term", "Request", "UsageItem"]:
    schemas[item + "Page"] = obj({"items": array(ref(item)), "next_cursor": nullable(TEXT), "has_more": BOOL})
schemas["SummaryList"] = array(ref("Summary"))
def envelope(name):
    result = name + "Response"
    if result not in schemas:
        schemas[result] = obj({"code": {"const": 0}, "message": {"const": "success"}, "data": ref(name)})
    return ref(result)
def param(name, where, schema, required=False):
    return {"name": name, "in": where, "required": required or where == "path", "schema": schema}
idem = param("Idempotency-Key", "header", {"type": "string", "minLength": 16, "maxLength": 128}, True)
page_params = [param("cursor", "query", TEXT), param("limit", "query", {"type": "integer", "minimum": 1, "maximum": 100, "default": 20})]
paths = {}
def add(path, method, opid, summary, output, body=None, code="200", parameters=None, replay=False):
    p = list(parameters or [])
    for part in path.split("/"):
        if part.startswith("{"):
            p.append(param(part[1:-1], "path", ID))
    if method == "post" and body != "PreviewInput":
        p.append(idem)
    response = {"description": "成功", "content": {"application/json": {"schema": envelope(output)}}}
    op = {"operationId": opid, "summary": summary,
          "tags": ["admin" if path.startswith("/admin/") else "user"],
          "security": [{"bearerAuth": []}], "parameters": p,
          "responses": {code: response,
                        "default": {"description": "错误：reason 见 api-contracts.md；管理接口校验管理员角色。",
                                    "content": {"application/json": {"schema": ref("Error")}}}}}
    if replay:
        op["responses"]["200"] = {**response, "description": "原结果幂等重放"}
    if body:
        op["requestBody"] = {"required": True, "content": {"application/json": {"schema": ref(body)}}}
    paths.setdefault(path, {})[method] = op

admin = "/admin/users/{user_id}/subscription-quota"
group_query = [param("group_id", "query", ID, True)]
add(admin, "get", "adminGetQuota", "查询目标用户指定 Claude 组额度", "Summary", parameters=group_query)
add(admin + "/terms", "get", "adminListQuotaTerms", "查询卡期历史", "TermPage", parameters=group_query + page_params)
add(admin + "/terms/preview", "post", "adminPreviewQuotaTerm", "只读开卡预览，无占位写入", "Preview", "PreviewInput")
add(admin + "/terms", "post", "adminOpenQuotaTerm", "逐用户开卡或续卡", "TermMutationResult", "OpenTermInput", "201", replay=True)
add("/admin/subscription-quota/terms/{term_id}/cancel", "post", "adminCancelQuotaTerm", "取消指定卡，保留已准入用量", "TermMutationResult", "CancelInput")
add("/admin/subscription-quota/requests", "get", "adminListQuotaRequests", "异常及待结算请求查询", "RequestPage",
    parameters=page_params + [param("status", "query", schemas["Request"]["properties"]["status"]), param("user_id", "query", ID), param("group_id", "query", ID)])
add("/admin/subscription-quota/requests/{request_id}/resolve", "post", "adminResolveQuotaRequest", "核查请求；路径 ID 为数据库行 ID", "ResolveResult", "ResolveInput", "202", replay=True)
add("/subscription-quota", "get", "listOwnQuota", "本人各 Claude term 分组摘要", "SummaryList")
add("/subscription-quota/groups/{group_id}", "get", "getOwnQuota", "本人指定组摘要", "Summary")
add("/subscription-quota/groups/{group_id}/terms", "get", "listOwnQuotaTerms", "本人卡期历史", "TermPage", parameters=page_params)
add("/subscription-quota/terms/{term_id}/usage", "get", "listOwnQuotaUsage", "本人指定卡的已结算用量", "UsageItemPage", parameters=page_params)

doc = {"openapi": "3.1.0", "info": {"title": "Claude weekly quota design contract", "version": "1.0.0-design",
        "description": "待实现设计，不表示线上已存在；与 api-contracts.md 联合使用。"},
       "servers": [{"url": "/api/v1"}], "paths": paths,
       "components": {"securitySchemes": {"bearerAuth": {"type": "http", "scheme": "bearer"}}, "schemas": schemas}}
(ROOT / "contracts" / "openapi.json").write_text(json.dumps(doc, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps({"operations": sum(len(v) for v in paths.values()), "schemas": len(schemas)}, ensure_ascii=False))
