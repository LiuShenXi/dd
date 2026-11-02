"""Verify the portable diagnostic archive, optionally restore its raw originals."""

import argparse
import hashlib
import json
import zipfile
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--extract", action="store_true")
    args = parser.parse_args()
    base = Path(__file__).resolve().parent
    manifest = json.loads((base / "I4-diagnostics-archive-manifest.json").read_text(encoding="utf-8-sig"))
    archive = base / manifest["archive"]
    actual = hashlib.sha256(archive.read_bytes()).hexdigest()
    if actual != manifest["archive_sha256"]:
        raise RuntimeError("Archive SHA256 mismatch")
    with zipfile.ZipFile(archive) as source:
        if len(source.infolist()) != manifest["entry_count"]:
            raise RuntimeError("Archive entry count mismatch")
        for expected in manifest["entries"]:
            name = expected["name"]
            destination = (base / name).resolve()
            if destination.parent != base or Path(name).name != name:
                raise RuntimeError("Archive entry escapes evidence directory")
            content = source.read(name)
            if len(content) != expected["bytes"] or hashlib.sha256(content).hexdigest() != expected["sha256"]:
                raise RuntimeError("Archive entry SHA256 mismatch")
            if args.extract:
                if destination.exists() and hashlib.sha256(destination.read_bytes()).hexdigest() != expected["sha256"]:
                    raise RuntimeError("Refusing to overwrite a changed local raw original")
                destination.write_bytes(content)
    print(json.dumps({"archive": archive.name, "sha256": actual, "verified_entries": manifest["entry_count"], "extracted": args.extract}))


if __name__ == "__main__":
    main()
