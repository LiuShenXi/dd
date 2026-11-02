"""Scan Git index blobs without printing any matching credential values."""

import hashlib
import importlib.util
import json
import re
import subprocess
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location("i4scan", HERE / "I4-credential-scan.py")
scan = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scan)


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, check=True, capture_output=True).stdout


def main():
    fixture_count, secrets = scan.private_values(scan.external_resources())
    names = git("diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z").decode("utf-8").split("\0")
    findings = []
    blobs = []
    text_count = 0
    for name in filter(None, names):
        data = git("show", ":" + name)
        entry = {"path": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}
        blobs.append(entry)
        suffix = Path(name).suffix.lower()
        if suffix in scan.BINARY:
            if suffix not in {".zip", ".png", ".jpg", ".jpeg", ".webp"}:
                findings.append({"path": name, "category": "unexpected_binary_in_index"})
            continue
        try:
            text = data.decode("utf-8-sig")
        except UnicodeError:
            findings.append({"path": name, "category": "unexpected_non_utf8_in_index"})
            continue
        text_count += 1
        if any(value in text for value in secrets):
            findings.append({"path": name, "category": "exact_private_fixture_credential"})
        for category, pattern in scan.PATTERNS.items():
            matches = list(pattern.finditer(text))
            if category == "literal_sensitive_assignment" and name == "deploy/config.example.yaml":
                matches = [m for m in matches if not re.search(r"(?i)your[-_]", m.group())]
            if matches:
                findings.append({"path": name, "category": category})
    report = {
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "scope": "actual staged Git index blobs, not working-tree approximations",
        "head_before_commit": git("rev-parse", "HEAD").decode().strip(),
        "staged_blob_count": len(blobs),
        "staged_bytes": sum(b["bytes"] for b in blobs),
        "scanned_text_blobs": text_count,
        "task_private_fixtures_read": fixture_count,
        "exact_private_values_compared": len(secrets),
        "matching_values_never_serialized": True,
        "binary_policy": "Only evidence ZIP and password-masked synthetic browser images allowed; ZIP originals independently scanned and every entry verified",
        "findings": findings,
        "no_findings": not findings,
        "largest_blobs": sorted(blobs, key=lambda b: b["bytes"], reverse=True)[:8],
    }
    (HERE / "I4-staged-credential-scan.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({key: report[key] for key in ["staged_blob_count", "staged_bytes", "scanned_text_blobs", "task_private_fixtures_read", "exact_private_values_compared", "findings", "no_findings"]}))
    return 0 if not findings else 1


if __name__ == "__main__":
    raise SystemExit(main())
