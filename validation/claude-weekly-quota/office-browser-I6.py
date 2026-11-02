"""Prepare a separate browser database and Redis from the final image.

Default/--help is inert. `prepare --serve` clones the synthetic database with a
transaction-consistent pg_dump and starts independent Redis/provider/application
containers. Main apps, main database and main Redis are never changed.
Only run after the coordinator reviews this script and explicitly requests it.
Secrets and raw responses stay under PRIVATE; --serve keeps the bridge alive.
"""
import argparse
import hashlib
import json
import os
import re
import secrets
import shlex
import signal
import socket
import subprocess
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
VALIDATION = ROOT / "validation/claude-weekly-quota"
PRIVATE_ROOT = Path.home() / ".codex/private/claude-weekly-quota"
MAIN_FIXTURE = PRIVATE_ROOT / "office-runtime-20261020-v2/synthetic-fixture.json"
DEFAULT_PRIVATE = PRIVATE_ROOT / "office-browser-I6-20261020-v1"
MAIN_MANIFEST = VALIDATION / "office-I6-environment.json"
PUBLIC_MANIFEST = VALIDATION / "office-I6-browser-environment.json"
BRIDGE = PRIVATE_ROOT / "office/loopback-bridge"
EXPECTED_IMAGE = "sha256:feee80eaef62bbb64848acd7253e9b0e00ba0fba7923120494c4d2e93031a483"
TASK_LABEL = "com.codex.local-task=claude-weekly-quota"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def now():
    return datetime.now(timezone.utc).isoformat()


def write_private(path, value):
    raw = json.dumps(value, indent=2) + "\n" if not isinstance(value, str) else value
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as handle:
        handle.write(raw)
    path.chmod(0o600)


def private_directory(path):
    path = path.expanduser().resolve()
    require(path.is_relative_to(PRIVATE_ROOT.resolve()) and path != PRIVATE_ROOT.resolve(),
            "private directory must be below the task private root")
    return path


def sql_literal(value):
    return "'" + str(value).replace("'", "''") + "'"


def parse_env(values):
    return dict(value.split("=", 1) for value in values if "=" in value)


def group_ids(value, *extra):
    ids = {int(part) for part in value.split(",") if part.strip()}
    ids.update(extra)
    require(all(value > 0 for value in ids), "invalid synthetic group identity")
    return ",".join(str(value) for value in sorted(ids))


class Driver:
    def __init__(self, private):
        self.private = private
        self.counter = 0
        self.url = ""

    def run(self, name, command, *, input_text=None, timeout=60):
        # Do not print commands, subprocess output or exception text: inherited
        # container environment and HTTP/provider responses can contain secrets.
        self.counter += 1
        result = subprocess.run(command, input=input_text, text=True, capture_output=True,
                                timeout=timeout, cwd=ROOT)
        write_private(self.private / f"{self.counter:03d}-{name}.log", result.stdout + result.stderr)
        require(result.returncode == 0, f"{name} failed; inspect private log")
        return result.stdout

    def docker(self, name, *args, **kwargs):
        return self.run(name, ["docker", *args], **kwargs)

    def inspect(self, identity):
        return json.loads(self.docker("inspect", "inspect", identity))[0]

    def sql(self, fixture, query):
        require(re.fullmatch(r"cwq_browser_i6_[0-9a-f]{12}", fixture["Database"]), "SQL target must be this script's separate browser database")
        return self.docker("synthetic-sql", "exec", "--interactive", fixture["Postgres"],
                           "psql", "--no-psqlrc", "--quiet", "--tuples-only", "--no-align",
                           "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", fixture["Database"],
                           input_text=query, timeout=30).strip()

    def request(self, method, path, payload=None, token=None, *, form=False, raw=False, idempotency_key=None):
        require(path.startswith("/") and not path.startswith("//"), "request path must be local")
        encoded = None
        headers = {"Accept": "application/json", "User-Agent": "CWQ-Synthetic-Browser-Fixture"}
        if token:
            headers["Authorization"] = "Bearer " + token
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        if payload is not None:
            headers["Content-Type"] = "application/x-www-form-urlencoded" if form else "application/json"
            encoded = (urllib.parse.urlencode(payload) if form else json.dumps(payload)).encode()
        request = urllib.request.Request(self.url + path, data=encoded, headers=headers, method=method)
        self.counter += 1
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                body = response.read().decode()
        except urllib.error.HTTPError as error:
            write_private(self.private / f"{self.counter:03d}-http-error.log", error.read().decode(errors="replace"))
            raise RuntimeError(f"HTTP {method} {path} returned {error.code}; inspect private log") from None
        write_private(self.private / f"{self.counter:03d}-http-response.log", body)
        if raw:
            return body
        envelope = json.loads(body)
        require(envelope.get("code") == 0 and "data" in envelope, "application response failed; inspect private log")
        return envelope["data"]

    def login(self, email, password):
        result = self.request("POST", "/api/v1/auth/login", {"email": email, "password": password})
        require(not result.get("requires_2fa") and result.get("access_token"), "synthetic login did not return a session")
        return result["access_token"]

    def wait_health(self):
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(self.url + "/health", timeout=2) as response:
                    if response.status == 200:
                        return
            except (urllib.error.URLError, TimeoutError, OSError):
                pass
            time.sleep(1)
        raise RuntimeError("browser application did not become healthy")


def validate_main(d, manifest, fixture):
    require(manifest["synthetic_only"] and not manifest["external_egress"], "not an isolated synthetic manifest")
    require(manifest["image_id"] == fixture["ImageID"] == EXPECTED_IMAGE, "final image differs from reviewed candidate")
    require(manifest["source_sha256"] == fixture["Source"], "source provenance differs")
    require(manifest["application"] == fixture["Application"], "main application identity differs")
    require(manifest["postgres"] == fixture["Postgres"] and manifest["redis"] == fixture["Redis"], "data service identity differs")
    require(manifest["network"] == fixture["Network"], "network identity differs")
    network = json.loads(d.docker("network-inspect", "network", "inspect", fixture["Network"]))[0]
    require(network["Internal"], "browser fixture network must have no external egress")
    apps = [fixture["Application"], fixture.get("Application2")]
    require(all(apps) and len(set(apps)) == 2, "expected exact two main applications")
    for role, container in (("app1", apps[0]), ("app2", apps[1]),
                            ("postgres", fixture["Postgres"]), ("redis", fixture["Redis"])):
        inspected = d.inspect(container)
        require(inspected["Config"].get("Labels", {}).get("com.codex.local-task") == "claude-weekly-quota", "container task label differs")
        require(set(inspected["NetworkSettings"]["Networks"]) == {fixture["Network"]}, "container network differs")
        require(not inspected["HostConfig"].get("PortBindings"), "container publishes unexpected ports")
        if role.startswith("app"):
            require(inspected["Image"] == EXPECTED_IMAGE, "main application image differs")
        else:
            require(inspected["State"]["Running"], "isolated data service is not running")
    provider = json.loads(d.docker("provider-image-inspect", "image", "inspect", fixture["ProviderImage"]))[0]
    require(provider["Id"] == fixture["ProviderID"] == manifest["provider_id"], "provider image differs")
    require(BRIDGE.is_file() and os.access(BRIDGE, os.X_OK), "existing native loopback bridge is unavailable")
    return apps


def validate_build(manifest):
    build = json.loads((VALIDATION / "office-I6-build-receipt.json").read_text())
    require(build["status"] == "built_source_stable_pending_runtime_acceptance"
            and build["r2_image_id"] == manifest["image_id"]
            and build["source_sha256"] == manifest["source_sha256"], "final build provenance differs")


def write_env(path, environment):
    require(all("\n" not in str(value) and "\r" not in str(value) for value in environment.values()), "multiline environment value")
    write_private(path, "".join(f"{key}={value}\n" for key, value in sorted(environment.items())))


def complete_order(d, state, order_id):
    require(order_id > 0, "order ID must be positive")
    token = d.login(state["admin_email"], state["admin_password"])
    detail = d.request("GET", f"/api/v1/admin/payment/orders/{order_id}", token=token)["order"]
    require(detail["user_id"] in state["user_ids"] and detail.get("plan_id") == state["sale_plan_id"],
            "only this browser fixture's own users and sale plan may receive a synthetic callback")
    require(str(detail.get("provider_instance_id")) == str(state["provider_instance_id"]), "order has another provider")
    require(detail.get("subscription_quota_snapshot", {}).get("group_id") == state["term_group_id"], "order snapshot group differs")
    if detail["status"] == "COMPLETED":
        return detail
    require(detail["status"] in ("PENDING", "PAID", "FAILED", "RECHARGING"), "order is not eligible for synthetic completion")
    form = {"pid": state["merchant_id"], "out_trade_no": detail["out_trade_no"],
            "trade_no": "fake-" + detail["out_trade_no"], "trade_status": "TRADE_SUCCESS",
            "money": format(Decimal(str(detail["pay_amount"])), ".2f"), "sign_type": "MD5"}
    canonical = "&".join(f"{key}={form[key]}" for key in sorted(form) if key not in ("sign", "sign_type") and form[key])
    form["sign"] = hashlib.md5((canonical + state["payment_key"]).encode()).hexdigest()
    reply = d.request("POST", "/api/v1/payment/webhook/easypay", form, form=True, raw=True)
    require(reply.strip().lower() == "success", "synthetic callback was not acknowledged")
    final = d.request("GET", f"/api/v1/admin/payment/orders/{order_id}", token=token)["order"]
    require(final["status"] == "COMPLETED", "synthetic payment did not complete")
    require(final["subscription_quota_snapshot"] == detail["subscription_quota_snapshot"], "frozen order snapshot changed")
    return final


def prepare(args):
    manifest = json.loads(MAIN_MANIFEST.read_text())
    main_fixture = json.loads(MAIN_FIXTURE.read_text())
    fixture = dict(main_fixture)
    validate_build(manifest)
    private = private_directory(args.private_dir)
    require(not private.exists(), "use a fresh private browser directory; never automatically repeat fixture preparation")
    private.mkdir(mode=0o700, parents=True)
    d = Driver(private)
    state = {"status": "preparing", "started_utc": now(), "image_id": manifest["image_id"],
             "source_sha256": manifest["source_sha256"], "main_fixture": str(MAIN_FIXTURE),
             "admin_email": fixture["AdminEmail"], "admin_password": fixture["Password"]}
    write_private(private / "browser-fixture.json", state)
    try:
        apps = validate_main(d, manifest, fixture)
        environment = parse_env(d.inspect(apps[0])["Config"]["Env"])
        require(environment.get("DATABASE_DBNAME") == fixture["Database"], "application database differs")
        suffix = secrets.token_hex(6)
        run_id = "cwq-browser-i6-" + suffix
        fixture["Database"] = "cwq_browser_i6_" + suffix
        state.update({"run_id": run_id, "database": fixture["Database"], "redis_db": 0,
                      "main_applications_untouched": apps, "main_database_untouched": main_fixture["Database"],
                      "main_redis_untouched": main_fixture["Redis"],
                      "totp_key": secrets.token_hex(32), "payment_key": secrets.token_hex(32),
                      "merchant_id": "synthetic-browser-" + suffix})
        # pg_dump owns a read-only consistent snapshot of the actively running
        # synthetic source. Restore only into a freshly named separate database.
        dump = d.docker("main-synthetic-consistent-dump", "exec", main_fixture["Postgres"], "pg_dump",
                        "--no-owner", "--no-privileges", "--serializable-deferrable", "-U", "postgres",
                        "-d", main_fixture["Database"], timeout=120)
        d.docker("browser-database-create", "exec", fixture["Postgres"], "createdb", "-U", "postgres",
                 "--owner=postgres", fixture["Database"])
        d.docker("browser-database-restore", "exec", "--interactive", fixture["Postgres"], "psql",
                 "--no-psqlrc", "--quiet", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", fixture["Database"],
                 input_text=dump, timeout=120)
        require(fixture["Database"] != main_fixture["Database"], "browser database must be independent")
        # Only the copied registry is cleared. No original row or heartbeat is
        # changed, and no stale-advertisement wait is needed in this new DB.
        d.sql(fixture, "DELETE FROM subscription_quota_runtime_replicas;")
        # Never replace/disable an existing payment provider, even in the copy.
        enabled = int(d.sql(fixture, "SELECT count(*) FROM payment_provider_instances WHERE enabled;"))
        require(enabled == 0, "an enabled provider already exists; inspect isolated fixture before proceeding")
        # Immutable startup allowlists require group IDs before the browser
        # process starts. These are the only preboot SQL-created entities;
        # users, keys, plans, provider, maintenance and orders use real APIs.
        term_name = "Synthetic Browser Claude Cards " + suffix
        rollout_name = "Synthetic Browser Claude Conversion " + suffix
        groups = json.loads(d.sql(fixture, f"""
          WITH term_group AS (
            INSERT INTO groups(name,description,platform,subscription_type,subscription_quota_mode,status,rate_multiplier)
            VALUES({sql_literal(term_name)},'Synthetic browser I5 only','anthropic','subscription','term','active',1)
            RETURNING id
          ), rollout_group AS (
            INSERT INTO groups(name,description,platform,subscription_type,subscription_quota_mode,status,rate_multiplier)
            VALUES({sql_literal(rollout_name)},'Synthetic browser I6 only','anthropic','standard','windows','active',1)
            RETURNING id
          ) SELECT json_build_object('term',term_group.id,'rollout',rollout_group.id) FROM term_group,rollout_group;
        """))
        state.update({"term_group_id": groups["term"], "rollout_group_id": groups["rollout"],
                      "term_group_name": term_name, "rollout_group_name": rollout_name})
        write_private(private / "browser-fixture.json", state)
        # Redis databases share CLIENT PAUSE and Pub/Sub. Reuse only the local
        # native image, never the main Redis process or its data/commands.
        redis_image = d.inspect(main_fixture["Redis"])["Image"]
        redis_alias = run_id + "-redis"
        redis = d.docker("browser-redis-start", "run", "--detach", "--pull", "never",
                         "--name", redis_alias, "--label", TASK_LABEL, "--network", fixture["Network"],
                         "--network-alias", redis_alias, "--memory", "128m", "--cpus", "0.5",
                         "--pids-limit", "128", redis_image, "redis-server", "--save", "", "--appendonly", "no").strip()
        state.update({"redis_container": redis, "redis_image_id": redis_image})
        require(d.inspect(redis)["Image"] == redis_image, "browser Redis image differs")
        redis_deadline = time.monotonic() + 30
        while True:
            try:
                if d.docker("browser-redis-ping", "exec", redis, "redis-cli", "PING").strip() == "PONG":
                    break
            except RuntimeError:
                pass
            require(time.monotonic() < redis_deadline, "independent browser Redis is not ready")
            time.sleep(1)
        provider_alias = run_id + "-payment"
        write_env(private / "provider.env", {"CWQ_SYNTHETIC_PAYMENT_KEY": state["payment_key"]})
        provider = d.docker("browser-provider-start", "run", "--detach", "--pull", "never", "--platform", "linux/amd64",
                            "--name", provider_alias, "--label", TASK_LABEL, "--network", fixture["Network"],
                            "--network-alias", provider_alias, "--read-only", "--cap-drop", "ALL",
                            "--security-opt", "no-new-privileges", "--memory", "128m", "--cpus", "0.5",
                            "--env-file", str(private / "provider.env"), fixture["ProviderID"]).strip()
        state["provider_container"] = provider
        environment.update({"TOTP_ENCRYPTION_KEY": state["totp_key"], "RUN_MODE": "standard", "SKIP_SETUP": "true",
                            "DATABASE_DBNAME": fixture["Database"], "REDIS_HOST": redis_alias, "REDIS_PORT": "6379",
                            "REDIS_PASSWORD": "", "REDIS_DB": "0",
                            "SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID": "browser-1",
                            "SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS": "browser-1",
                            "SUBSCRIPTION_QUOTA_PROTECTED_GROUP_IDS": group_ids(environment.get("SUBSCRIPTION_QUOTA_PROTECTED_GROUP_IDS", ""), groups["rollout"]),
                            "SUBSCRIPTION_QUOTA_ENABLED_GROUP_IDS": group_ids(environment.get("SUBSCRIPTION_QUOTA_ENABLED_GROUP_IDS", ""), groups["term"], groups["rollout"]),
                            "SUBSCRIPTION_QUOTA_ISSUE_ENABLED": "true", "SUBSCRIPTION_QUOTA_ADMISSION_ENABLED": "true",
                            "SUBSCRIPTION_QUOTA_RECOVERY_ENABLED": "true", "DEFAULT_CLAUDE_SUBSCRIPTION_CONCURRENCY": "3"})
        write_env(private / "application.env", environment)
        application = d.docker("browser-application-start", "run", "--detach", "--pull", "never", "--platform", "linux/amd64",
                               "--name", run_id + "-app", "--label", TASK_LABEL, "--network", fixture["Network"],
                               "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
                               "--memory", "1536m", "--cpus", "2", "--pids-limit", "512",
                               "--tmpfs", "/app/data:rw,size=128m,uid=1000,gid=1000", "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000",
                               "--env-file", str(private / "application.env"), EXPECTED_IMAGE).strip()
        state["application"] = application
        require(d.inspect(application)["Image"] == EXPECTED_IMAGE, "browser application image differs")
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        with (private / "bridge-process.log").open("w") as output:
            bridge = subprocess.Popen([str(BRIDGE), "--container", application, "--port", str(port),
                                       "--log", str(private / "bridge-metadata.log")],
                                      stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        d.url = f"http://127.0.0.1:{port}"
        state.update({"url": d.url, "bridge_pid": bridge.pid})
        write_private(private / "browser-fixture.json", state)
        d.wait_health()
        admin = d.login(state["admin_email"], state["admin_password"])
        users = []
        for purpose in ("buyer", "manual"):
            email, password = f"browser-{purpose}-{suffix}@example.invalid", secrets.token_urlsafe(24)
            user = d.request("POST", "/api/v1/admin/users", {"email": email, "password": password,
                             "username": "Synthetic Browser " + purpose, "role": "user", "balance": 0,
                             "concurrency": 5, "allowed_groups": [groups["term"], groups["rollout"]]}, admin)
            token = d.login(email, password)
            key = d.request("POST", "/api/v1/keys", {"name": "Synthetic conversion review key", "group_id": groups["rollout"]}, token)
            users.append({"purpose": purpose, "id": user["id"], "email": email, "password": password,
                          "access_token": token, "key": key})
        state.update({"users": users, "user_ids": [user["id"] for user in users]})
        config = {"enabled": True, "enabled_payment_types": ["alipay"], "balance_disabled": True,
                  "max_pending_orders": 100, "order_timeout_minutes": 60, "min_amount": 0, "max_amount": 0,
                  "daily_limit": 0, "subscription_usd_to_cny_rate": 7, "recharge_fee_rate": 0,
                  "payment_visible_method_alipay_enabled": True, "payment_visible_method_alipay_source": "easypay_alipay"}
        d.request("PUT", "/api/v1/admin/payment/config", config, admin)
        instance = d.request("POST", "/api/v1/admin/payment/providers", {"provider_key": "easypay",
                             "name": "Synthetic browser payment only", "supported_types": ["alipay"],
                             "enabled": True, "payment_mode": "qrcode", "refund_enabled": False,
                             "allow_user_refund": False, "config": {"pid": state["merchant_id"], "pkey": state["payment_key"],
                             "apiBase": f"http://{provider_alias}:8081", "currency": "CNY",
                             "notifyUrl": d.url + "/api/v1/payment/webhook/easypay", "returnUrl": d.url + "/payment/result"}}, admin)
        state["provider_instance_id"] = instance["id"]
        base_plan = {"group_id": groups["term"], "currency": "USD", "validity_days": 7, "validity_unit": "days",
                     "price": 12.34, "features": "Synthetic browser acceptance only", "product_name": "Claude 168-hour card",
                     "description": "Sale price and usage allowance are independent synthetic test values."}
        sale = d.request("POST", "/api/v1/admin/payment/plans", base_plan | {"name": "Browser sale $12.34 / quota $125.12345678",
                         "default_quota_usd": "125.12345678", "for_sale": True, "sort_order": 1}, admin)
        manual = d.request("POST", "/api/v1/admin/payment/plans", base_plan | {"name": "Browser manual default $300 (not for sale)",
                           "default_quota_usd": "300.00000000", "for_sale": False, "sort_order": 2}, admin)
        state.update({"sale_plan_id": sale["id"], "manual_plan_id": manual["id"]})
        # A harmless historical order makes the non-blocking late-payment warning
        # visible. It is synthetic and never sent to any provider.
        terminal_id = int(d.sql(fixture, f"""INSERT INTO payment_orders(user_id,amount,pay_amount,expires_at,out_trade_no,
          order_type,subscription_group_id,subscription_days,status)
          VALUES({users[0]['id']},1,1,clock_timestamp()-interval '1 day',{sql_literal('synthetic-terminal-'+suffix)},
          'subscription',{groups['rollout']},7,'CANCELLED') RETURNING id;"""))
        state["legacy_terminal_order_id"] = terminal_id
        rollout_path = f"/api/v1/admin/groups/{groups['rollout']}/subscription-quota/rollout"
        deadline = time.monotonic() + 30
        while True:
            rollout = d.request("GET", rollout_path, token=admin)
            if [replica["replica_id"] for replica in rollout["replicas"]] == ["browser-1"]:
                break
            require(time.monotonic() < deadline, "old/missing/extra replica evidence remains; inspect private responses")
            time.sleep(2)
        # The fixture starts maintained but grants no rights. The browser must
        # explicitly choose users/amounts and confirm its own new preview.
        maintained = mutation(d, rollout_path + "/maintenance", {"enabled": True,
                              "expected_revision": rollout["revision"], "reason": "Synthetic browser acceptance only"}, admin)
        preview = d.request("POST", rollout_path + "/preview", {"allocations": []}, admin)
        require(maintained["maintenance"] and not preview["blockers"] and preview["legacy_terminal_orders"] == 1,
                "synthetic conversion target is not ready for explicit browser review")
        # A real synthetic create + signed webhook supplies a completed frozen
        # snapshot for order/detail UI; additional purchases remain possible.
        created = d.request("POST", "/api/v1/payment/orders", {"plan_id": sale["id"], "order_type": "subscription",
                            "amount": 12.34, "payment_type": "alipay", "return_url": d.url + "/payment/result"}, users[0]["access_token"])
        state["sample_order_id"] = created["order_id"]
        write_private(private / "browser-fixture.json", state)
        completed = complete_order(d, state, created["order_id"])
        require(completed["subscription_quota_snapshot"]["quota_usd"] == "125.12345678", "purchased allowance differs")
        state.update({"status": "ready_for_browser", "finished_utc": now()})
        write_private(private / "browser-fixture.json", state)
        public = {"status": state["status"], "url": d.url, "image_id": EXPECTED_IMAGE,
                  "source_sha256": manifest["source_sha256"], "application": application,
                  "provider_container": provider, "provider_image_id": fixture["ProviderID"],
                  "postgres": fixture["Postgres"], "redis": redis, "redis_image_id": redis_image, "network": fixture["Network"],
                  "main_redis_untouched": main_fixture["Redis"],
                  "main_applications_untouched": apps, "database": fixture["Database"], "redis_db": 0,
                  "term_group_id": groups["term"], "rollout_group_id": groups["rollout"],
                  "sale_plan_id": sale["id"], "manual_plan_id": manual["id"], "sample_order_id": created["order_id"],
                  "user_ids": state["user_ids"], "bridge_pid": bridge.pid, "synthetic_only": True,
                  "production_modified": False, "private_fixture_path": str(private / "browser-fixture.json"),
                  "ui_paths": ["/login", "/purchase", "/orders", "/admin/orders/plans", "/admin/orders", "/admin/users", "/admin/groups"]}
        PUBLIC_MANIFEST.write_text(json.dumps(public, indent=2) + "\n")
        print(json.dumps(public), flush=True)
        if args.serve:
            # Codex may clean up children when a command exits. Keep this parent
            # in a persistent foreground exec session for the entire CUA visit.
            bridge.wait()
    except Exception:
        state["status"] = "failed_inspect_private_logs_no_automatic_retry"
        write_private(private / "browser-fixture.json", state)
        write_private(private / "failure.log", traceback.format_exc())
        print(json.dumps({"status": state["status"], "private_directory": str(private)}), flush=True)
        raise SystemExit(1) from None


def mutation(d, path, payload, token):
    return d.request("POST", path, payload, token, idempotency_key=secrets.token_hex(24))


def validate_browser(d, state):
    """Resolve only the already prepared browser application, never a main app."""
    public = json.loads(PUBLIC_MANIFEST.read_text())
    require(state["status"] == public["status"] == "ready_for_browser", "browser fixture is not ready")
    for key in ("application", "image_id", "source_sha256", "url", "database", "bridge_pid"):
        require(state[key] == public[key], "private/public browser identity differs")
    require(public["synthetic_only"] and not public["production_modified"], "not a synthetic browser fixture")
    main = json.loads(MAIN_FIXTURE.read_text())
    forbidden = set(state["main_applications_untouched"]) | {main["Application"], main.get("Application2")}
    require(state["application"] not in forbidden, "browser application is a main runtime application")
    require(re.fullmatch(r"cwq_browser_i6_[0-9a-f]{12}", state["database"]), "browser database name differs")
    require(state["database"] != main["Database"], "browser database is the main runtime database")
    require(state["redis_container"] != main["Redis"] and public["redis"] == state["redis_container"], "browser Redis is not separate")
    url = urllib.parse.urlsplit(state["url"])
    require(url.scheme == "http" and url.hostname == "127.0.0.1" and url.port and not url.path, "browser URL is not loopback")
    actual = d.inspect(state["application"])
    require(actual["Image"] == state["image_id"] and actual["State"]["Running"], "browser application identity/state differs")
    require(actual["Name"].startswith("/" + state["run_id"] + "-app"), "browser application name differs")
    require(actual["Config"].get("Labels", {}).get("com.codex.local-task") == "claude-weekly-quota", "browser application label differs")
    require(set(actual["NetworkSettings"]["Networks"]) == {public["network"]}, "browser network differs")
    require(not actual["HostConfig"].get("PortBindings") and actual["HostConfig"]["ReadonlyRootfs"], "browser application isolation differs")
    environment = parse_env(actual["Config"]["Env"])
    require(environment["DATABASE_DBNAME"] == state["database"], "browser application database differs")
    require(environment["REDIS_HOST"] == state["run_id"] + "-redis" and environment["REDIS_DB"] == "0", "browser application Redis differs")
    require(environment["SUBSCRIPTION_QUOTA_ROLLOUT_REPLICA_ID"] == "browser-1"
            and environment["SUBSCRIPTION_QUOTA_ROLLOUT_EXPECTED_REPLICA_IDS"] == "browser-1", "browser replica roster differs")
    return public, environment


def upgrade(args):
    """Upgrade only an existing browser app using an explicitly reviewed build."""
    private = private_directory(args.private_dir)
    state = json.loads((private / "browser-fixture.json").read_text())
    require(args.serve, "upgrade requires --serve to keep the replacement bridge alive")
    require(re.fullmatch(r"sha256:[0-9a-f]{64}", args.image_id), "supply an exact reviewed image ID")
    require(re.fullmatch(r"[0-9a-f]{64}", args.source_sha256), "supply the reviewed source digest")
    build = json.loads(args.build_receipt.read_text())
    require(build["status"] == "built_source_stable_pending_runtime_acceptance"
            and build["r2_image_id"] == args.image_id
            and build["source_sha256"] == build["source_after_sha256"] == args.source_sha256,
            "upgrade image/source differ from successful stable build receipt")
    upgrade_dir = private / ("upgrade-" + args.image_id[7:19])
    require(not upgrade_dir.exists(), "upgrade attempt already exists; inspect its private record before recovery")
    upgrade_dir.mkdir(mode=0o700)
    d = Driver(upgrade_dir)
    d.url = state["url"]
    record = {"status": "validating", "started_utc": now(), "previous_application": state["application"],
              "previous_image_id": state["image_id"], "previous_source_sha256": state["source_sha256"],
              "image_id": args.image_id, "source_sha256": args.source_sha256, "url": state["url"],
              "build_receipt": str(args.build_receipt.resolve())}
    write_private(upgrade_dir / "before-fixture.json", state)
    try:
        public, environment = validate_browser(d, state)
        require(state["image_id"] != args.image_id, "requested image is already active")
        built = json.loads(d.docker("upgrade-image-inspect", "image", "inspect", args.image_id))[0]
        labels = built["Config"].get("Labels", {})
        require(built["Id"] == args.image_id and labels.get("com.monas.source-sha256") == args.source_sha256,
                "image label provenance differs")
        require(labels.get("com.codex.local-task") == "claude-weekly-quota"
                and labels.get("com.monas.claude-quota-activation") == "local-synthetic-only"
                and labels.get("com.monas.claude-quota-stage") == "I6-isolated-candidate", "image is not the synthetic R2 candidate")
        port = urllib.parse.urlsplit(d.url).port
        bridge_command = shlex.split(d.run("previous-bridge-identity", ["ps", "-p", str(state["bridge_pid"]), "-o", "command="]).strip())
        require(bridge_command and bridge_command[0] == str(BRIDGE)
                and bridge_command[bridge_command.index("--container") + 1] == state["application"]
                and bridge_command[bridge_command.index("--port") + 1] == str(port), "previous bridge PID/target differs")
        token = d.login(state["admin_email"], state["admin_password"])
        order_path = f"/api/v1/admin/payment/orders/{state['sample_order_id']}"
        original_order = d.request("GET", order_path, token=token)["order"]
        write_env(upgrade_dir / "application.env", environment)
        record["status"] = "replacing_browser_application"
        state["status"] = "upgrading_browser_application"
        write_private(private / "browser-fixture.json", state)
        write_private(upgrade_dir / "upgrade.json", record)
        # The exact old browser app is stopped and retained for logs. Data
        # services/provider/main apps are neither stopped nor restarted.
        d.docker("previous-browser-stop", "stop", "--time", "10", state["application"], timeout=20)
        application = d.docker("upgraded-browser-start", "run", "--detach", "--pull", "never", "--platform", "linux/amd64",
                               "--name", state["run_id"] + "-app-" + args.image_id[7:19], "--label", TASK_LABEL,
                               "--network", public["network"], "--read-only", "--user", "1000:1000",
                               "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "1536m",
                               "--cpus", "2", "--pids-limit", "512", "--tmpfs", "/app/data:rw,size=128m,uid=1000,gid=1000",
                               "--tmpfs", "/tmp:rw,size=64m,uid=1000,gid=1000", "--env-file", str(upgrade_dir / "application.env"), args.image_id).strip()
        record["application"] = application
        write_private(upgrade_dir / "upgrade.json", record)
        require(d.inspect(application)["Image"] == args.image_id, "replacement browser image differs")
        deadline = time.monotonic() + 90
        while True:
            try:
                health = d.docker("replacement-internal-health", "exec", "--interactive", application,
                                  "nc", "-w", "2", "127.0.0.1", "8080",
                                  input_text="GET /health HTTP/1.0\r\nHost: localhost\r\n\r\n", timeout=5)
                if " 200 " in health.split("\r\n", 1)[0]:
                    break
            except (RuntimeError, subprocess.TimeoutExpired):
                pass
            require(time.monotonic() < deadline, "replacement application did not become healthy")
            time.sleep(1)
        # This PID was verified against the exact existing native executable,
        # browser container ID and stable port before any mutation.
        os.kill(state["bridge_pid"], signal.SIGTERM)
        deadline = time.monotonic() + 15
        while True:
            try:
                with socket.socket() as listener:
                    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                    listener.bind(("127.0.0.1", port))
                break
            except OSError:
                require(time.monotonic() < deadline, "previous bridge did not release its port")
                time.sleep(0.2)
        with (upgrade_dir / "bridge-process.log").open("w") as output:
            bridge = subprocess.Popen([str(BRIDGE), "--container", application, "--port", str(port),
                                       "--log", str(upgrade_dir / "bridge-metadata.log")],
                                      stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        record["bridge_pid"] = bridge.pid
        write_private(upgrade_dir / "upgrade.json", record)
        d.wait_health()
        token = d.login(state["admin_email"], state["admin_password"])
        final_order = d.request("GET", order_path, token=token)["order"]
        require(final_order["subscription_quota_snapshot"] == original_order["subscription_quota_snapshot"], "existing purchased snapshot changed")
        require(final_order["status"] == original_order["status"], "existing purchase status changed")
        # Preserve the copied database. Old instance heartbeats expire naturally;
        # there is deliberately no SQL deletion during an application upgrade.
        deadline = time.monotonic() + 75
        while True:
            rollout = d.request("GET", f"/api/v1/admin/groups/{state['rollout_group_id']}/subscription-quota/rollout", token=token)
            if [replica["replica_id"] for replica in rollout["replicas"]] == ["browser-1"]:
                break
            require(time.monotonic() < deadline, "replacement replica roster did not converge")
            time.sleep(2)
        record.update({"status": "ready_for_browser", "finished_utc": now(), "existing_snapshot_unchanged": True,
                       "database_preserved": True, "redis_preserved": True, "provider_preserved": True})
        state.update({"status": "ready_for_browser", "application": application, "image_id": args.image_id,
                      "source_sha256": args.source_sha256, "bridge_pid": bridge.pid})
        state.setdefault("upgrades", []).append(record)
        public.update({"status": "ready_for_browser", "application": application, "image_id": args.image_id,
                       "source_sha256": args.source_sha256, "bridge_pid": bridge.pid})
        public.setdefault("upgrades", []).append(record)
        write_private(upgrade_dir / "upgrade.json", record)
        write_private(private / "browser-fixture.json", state)
        PUBLIC_MANIFEST.write_text(json.dumps(public, indent=2) + "\n")
        print(json.dumps(record), flush=True)
        bridge.wait()
    except Exception:
        record["status"] = "upgrade_failed_inspect_private_logs_no_automatic_retry"
        write_private(upgrade_dir / "upgrade.json", record)
        write_private(upgrade_dir / "failure.log", traceback.format_exc())
        print(json.dumps({"status": record["status"], "private_directory": str(upgrade_dir)}), flush=True)
        raise SystemExit(1) from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--private-dir", type=Path, default=DEFAULT_PRIVATE)
    commands = parser.add_subparsers(dest="command", required=True)
    setup = commands.add_parser("prepare", help="clone a separate synthetic browser database; main runtime is untouched")
    setup.add_argument("--serve", action="store_true", help="keep bridge alive in this foreground exec session")
    complete = commands.add_parser("complete-order", help="send one signed synthetic callback for a browser-created order")
    complete.add_argument("order_id", type=int)
    replace = commands.add_parser("upgrade", help="replace only the existing browser app with an explicitly reviewed candidate")
    replace.add_argument("--image-id", required=True)
    replace.add_argument("--source-sha256", required=True)
    replace.add_argument("--build-receipt", required=True, type=Path)
    replace.add_argument("--serve", action="store_true", help="required: keep the stable-URL bridge alive")
    args = parser.parse_args()
    os.umask(0o077)
    if args.command == "prepare":
        prepare(args)
    elif args.command == "upgrade":
        upgrade(args)
    else:
        private = private_directory(args.private_dir)
        state = json.loads((private / "browser-fixture.json").read_text())
        d = Driver(private)
        d.counter = int(time.time())
        d.url = state["url"]
        try:
            validate_browser(d, state)
            order = complete_order(d, state, args.order_id)
            print(json.dumps({"order_id": order["id"], "status": order["status"], "synthetic_only": True}))
        except Exception:
            write_private(private / "callback-failure.log", traceback.format_exc())
            print(json.dumps({"status": "callback_failed_inspect_private_logs"}))
            raise SystemExit(1) from None


if __name__ == "__main__":
    main()
