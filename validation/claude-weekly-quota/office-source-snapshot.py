"""Portable equivalent of environment-I1-source-snapshot.ps1; no credentials."""
import argparse
import hashlib
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path


def capture(root):
    raw = subprocess.check_output(
        ["git", "-C", str(root), "ls-files", "-z", "--cached", "--others",
         "--exclude-standard", "--", "backend", "frontend", "docs/legal",
         "Dockerfile", ".dockerignore", "deploy/docker-entrypoint.sh"]
    )
    paths = set(raw.decode().strip("\0").split("\0")) - {""}
    dist = root / "backend/internal/web/dist"
    paths.update(p.relative_to(root).as_posix() for p in dist.rglob("*") if p.is_file())
    entries, aggregate, frontend = [], bytearray(), bytearray()
    for path in sorted(paths):
        file = root / path
        data = file.read_bytes() if file.is_file() else None
        digest = hashlib.sha256(data).hexdigest() if data is not None else "MISSING"
        entries.append({"path": path, "sha256": digest, "bytes": len(data) if data else 0})
        row = f"{path}\0{digest}\n".encode()
        aggregate.extend(row)
        if path.startswith("backend/internal/web/dist/"):
            frontend.extend(row)
    return {
        "captured_utc": datetime.now(timezone.utc).isoformat(), "root": str(root),
        "head": subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip(),
        "algorithm": "SHA256(sorted ordinal relative path + NUL + file SHA256 + LF)",
        "scope": "tracked and nonignored build inputs plus embedded frontend, including tests",
        "source_sha256": hashlib.sha256(aggregate).hexdigest(),
        "frontend_dist_sha256": hashlib.sha256(frontend).hexdigest(),
        "file_count": len(entries), "files": entries,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    result = capture(Path(__file__).resolve().parents[2])
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({k: result[k] for k in ("source_sha256", "frontend_dist_sha256", "file_count")}))
