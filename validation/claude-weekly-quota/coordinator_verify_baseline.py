"""Read-only acceptance checks for the verified production baseline."""
import hashlib
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = Path(__file__).resolve().parent
BASE = "8d869d41f40d6eedbe2ee05aaf8973145cccbe4d"

def git(*args):
    return subprocess.check_output(["git", "-C", str(ROOT), *args], text=True, encoding="utf-8").strip()

baseline = json.loads((EVIDENCE / "production-baseline-20261019.json").read_text(encoding="utf-8"))
failures = []
for item in baseline["migrations"]:
    path = ROOT / "backend/migrations" / item["filename"]
    if not path.is_file():
        failures.append({"file": item["filename"], "reason": "missing"})
        continue
    actual = hashlib.sha256(path.read_text(encoding="utf-8").strip().encode("utf-8")).hexdigest()
    if actual != item["checksum"]:
        failures.append({"file": item["filename"], "reason": "production checksum changed"})
ancestor = subprocess.run(["git", "-C", str(ROOT), "merge-base", "--is-ancestor", BASE, "HEAD"], capture_output=True).returncode == 0
result = {"checked_at_utc": datetime.now(timezone.utc).isoformat(), "base_revision": BASE, "head": git("rev-parse", "HEAD"), "branch": git("branch", "--show-current"), "baseline_is_ancestor": ancestor, "production_migrations_checked": len(baseline["migrations"]), "migration_failures": failures, "scope": "source baseline integrity only; does not establish functional test success"}
(EVIDENCE / "coordinator-baseline-integrity.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
print(json.dumps(result, indent=2))
raise SystemExit(0 if ancestor and not failures else 1)
