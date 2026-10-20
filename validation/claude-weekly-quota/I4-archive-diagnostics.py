"""Archive immutable diagnostic originals; keep readable final evidence in Git."""

import hashlib
import json
import zipfile
from datetime import datetime, timezone
from pathlib import Path

BASE = Path(__file__).resolve().parent
ARCHIVE = BASE / "I4-diagnostics-archive.zip"
MANIFEST = BASE / "I4-diagnostics-archive-manifest.json"
KEEP = {
    "I4-main-F4-key-projection-source.json",
    "I4-ui-20261019T174120Z-tests.json",
    "I4-windows-F4-20261019T233536Z-unit.jsonl",
    "I4-windows-F4-20261019T233536Z-list.log",
    "I4-windows-F4-20261019T233536Z-compile.log",
    "I4-windows-F4-20261019T233536Z-vet.log",
    "I4-unit-race-F4-20261019T234019Z.jsonl",
    "I4-unit-race-F4-20261019T234019Z-list.log",
    "I4-F4-native-projection-20261019T233536Z.jsonl",
    "I4-pg-F4-20261019T234019Z.jsonl",
    "I4-v2-MetadataAndUnsupportedRoutes-F4-20261019T234155Z.jsonl",
    "I4-v2-T68-F3-20261019T181135Z.jsonl",
}


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def select(path):
    if not path.is_file() or not path.name.startswith(("I4-", "environment-I4-")) or path.name in KEEP:
        return False
    if path.suffix == ".json":
        return path.stat().st_size > 500_000 or path.name.startswith(("I4-backend-business-freeze-before-", "I4-backend-business-freeze-v1"))
    return path.suffix in {".jsonl", ".log"}


def main():
    if ARCHIVE.exists() or MANIFEST.exists():
        raise RuntimeError("Immutable I4 diagnostic archive already exists; do not overwrite it")
    scan = json.loads((BASE / "I4-credential-scan-before-archive.json").read_text(encoding="utf-8-sig"))
    if not scan["no_findings"] or not scan["task_private_fixtures_read"]:
        raise RuntimeError("Credential scan must pass, including actual task-private fixture comparisons")
    files = sorted((path for path in BASE.iterdir() if select(path)), key=lambda path: path.name)
    entries = [{"name": path.name, "bytes": path.stat().st_size, "sha256": digest(path)} for path in files]
    with zipfile.ZipFile(ARCHIVE, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as target:
        for path in files:
            target.write(path, path.name)
    with zipfile.ZipFile(ARCHIVE) as source:
        if len(source.infolist()) != len(entries):
            raise RuntimeError("Archive entry count mismatch")
        for expected in entries:
            content = source.read(expected["name"])
            if len(content) != expected["bytes"] or hashlib.sha256(content).hexdigest() != expected["sha256"]:
                raise RuntimeError("Archive entry verification failed")
    report = {
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "purpose": "Duplicate manifests and superseded/debug plaintext logs, including failures; all originals retained locally and recoverable from this committed archive",
        "archive": ARCHIVE.name,
        "archive_sha256": digest(ARCHIVE),
        "archive_bytes": ARCHIVE.stat().st_size,
        "entry_count": len(entries),
        "uncompressed_bytes": sum(entry["bytes"] for entry in entries),
        "verified_every_entry": True,
        "secrets_or_fixtures_included": False,
        "database_backups_or_runtime_binaries_included": False,
        "entries": entries,
    }
    MANIFEST.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    with (BASE / ".gitignore").open("a", encoding="utf-8") as target:
        target.write("\n# I4 raw originals are preserved in the committed, verified diagnostic archive.\n")
        for entry in entries:
            target.write("/" + entry["name"] + "\n")
        target.write("/__pycache__/\n")
    print(json.dumps({key: report[key] for key in ("archive", "archive_sha256", "archive_bytes", "entry_count", "uncompressed_bytes", "verified_every_entry")}))


if __name__ == "__main__":
    main()
