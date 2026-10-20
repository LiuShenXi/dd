"""Read-only precommit scan. Never serialize matching text or fixture values."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = Path(__file__).resolve().parent
BINARY = {".zip", ".exe", ".dll", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".ico", ".pyc", ".woff", ".woff2", ".pdf", ".tar", ".gz"}
PATTERNS = {
    "private_key_pem": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"),
    "provider_credential_shape": re.compile(r"\b(?:sk-[A-Za-z0-9_-]{24,}|gh[pousr]_[A-Za-z0-9]{30,}|AKIA[A-Z0-9]{16})\b"),
    "literal_sensitive_assignment": re.compile(r"(?i)(?:api[_-]?key|access[_-]?token|secret[_-]?key|private[_-]?key|authorization|password)\s*[\"']?\s*[:=]\s*[\"'](?:Bearer\s+)?[A-Za-z0-9/+_!=.-]{20,}[\"']"),
    "postgres_database_dump": re.compile(r"(?m)^-- PostgreSQL database dump\b"),
}


def leaves(value, prefix=""):
    if isinstance(value, str):
        yield prefix, value
    elif isinstance(value, dict):
        for key, child in value.items():
            yield from leaves(child, prefix + "." + key)
    elif isinstance(value, list):
        for child in value:
            yield from leaves(child, prefix)


def external_resources():
    paths = set()
    for path in EVIDENCE.glob("*.json"):
        if not re.match(r"(?:environment-I4-|coordinator-)", path.name):
            continue
        try:
            data = json.loads(path.read_text(encoding="utf-8-sig"))
        except (ValueError, UnicodeError):
            continue
        for _, value in leaves(data):
            if re.match(r"^[A-Za-z]:[\\/]", value) and not value.lower().startswith(str(ROOT).lower()):
                paths.add(value)
    return sorted(paths)


def private_values(paths):
    values = set()
    loaded = 0
    for name in paths:
        path = Path(name)
        # Only task-owned synthetic fixtures are read; no SSH/Codex/auth profile
        # or other external resource is opened by this scanner.
        if "privatetests" not in name.lower() or "fixture" not in path.name.lower() or path.suffix != ".json" or not path.is_file():
            continue
        try:
            data = json.loads(path.read_text(encoding="utf-8-sig"))
        except (ValueError, UnicodeError):
            continue
        loaded += 1
        for key, value in leaves(data):
            sensitive = re.search(r"(?i)password|token|secret|api.?key|(?:^|\.)(?:[ABCD]Key\d*|GPTKey|Keys?)(?:\.|$)", key)
            if sensitive and len(value) >= 12:
                values.add(value)
    return loaded, values


def git_names(*args):
    result = subprocess.run(["git", *args], cwd=ROOT, check=True, capture_output=True, text=True, encoding="utf-8")
    return [ROOT / name for name in result.stdout.splitlines() if name]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default="I4-credential-scan.json")
    args = parser.parse_args()
    resources = external_resources()
    fixture_count, secrets = private_values(resources)
    paths = set(git_names("diff", "--name-only", "HEAD"))
    paths.update(git_names("ls-files", "--others", "--exclude-standard"))
    # Include old ignored plaintext diagnostics, not only newly staged files.
    paths.update(path for path in EVIDENCE.rglob("*") if path.is_file())
    paths.update(path for path in ROOT.iterdir() if path.is_file() and re.search(r"(?i)debug|\.log$|fixture|backup|dump", path.name))
    findings = defaultdict(set)
    reviewed_placeholders = []
    scanned = 0
    for path in sorted(paths):
        if not path.is_file() or path.suffix.lower() in BINARY:
            continue
        try:
            text = path.read_text(encoding="utf-8-sig")
        except UnicodeError:
            continue
        scanned += 1
        relative = path.relative_to(ROOT).as_posix()
        if any(value in text for value in secrets):
            findings[relative].add("exact_private_fixture_credential")
        for category, pattern in PATTERNS.items():
            matches = list(pattern.finditer(text))
            if category == "literal_sensitive_assignment" and relative == "deploy/config.example.yaml":
                placeholders = [match for match in matches if re.search(r"(?i)your[-_]", match.group())]
                if placeholders:
                    reviewed_placeholders.append({"path": relative, "category": category, "classification": "documented your-* example placeholder, not a runtime credential", "count": len(placeholders)})
                    matches = [match for match in matches if match not in placeholders]
            if matches:
                findings[relative].add(category)
    report = {
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "scope": "changed/nonignored files plus every validation plaintext original and root plaintext debug files",
        "scanned_text_files": scanned,
        "task_private_fixtures_read": fixture_count,
        "exact_private_values_compared": len(secrets),
        "matching_values_never_serialized": True,
        "binary_pixels_not_scanned": "browser owner attests password inputs stayed masked; screenshots contain synthetic accounts only",
        "findings": [{"path": name, "categories": sorted(categories)} for name, categories in sorted(findings.items())],
        "reviewed_non_credentials": reviewed_placeholders,
        "no_findings": not findings,
    }
    output = EVIDENCE / args.output
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    exclusions = [{"path": name, "exists_locally": Path(name).exists(), "migration_policy": "not copied to Git or portable evidence; rebuild isolated resources on the next machine"} for name in resources]
    (EVIDENCE / "I4-local-external-resources-excluded.json").write_text(json.dumps(exclusions, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps({"scanned_text_files": scanned, "private_fixture_count": fixture_count, "private_values_compared": len(secrets), "finding_count": len(findings), "findings": report["findings"]}, ensure_ascii=False))
    return 0 if not findings else 1


if __name__ == "__main__":
    raise SystemExit(main())
