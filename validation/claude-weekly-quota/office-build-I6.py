"""Build immutable local I6/R1 images on macOS/Linux without production writes."""
import argparse
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VALIDATION = ROOT / "validation/claude-weekly-quota"
BASE = "sub2api:2.8.13-monas-bps.13"
spec = importlib.util.spec_from_file_location("source_snapshot", VALIDATION / "office-source-snapshot.py")
snapshot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(snapshot)


def sha(path):
    with path.open("rb") as handle:
        return hashlib.file_digest(handle, "sha256").hexdigest()


def inspect(tag):
    return json.loads(subprocess.check_output(["docker", "image", "inspect", tag], text=True))[0]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--private-dir", required=True, type=Path)
    parser.add_argument("--receipt", required=True, type=Path)
    args = parser.parse_args()
    private = args.private_dir.resolve()
    allowed = Path.home() / ".codex/private/claude-weekly-quota"
    if not private.is_relative_to(allowed) or private.exists():
        raise ValueError("use a fresh directory under the task's private root")
    private.mkdir(parents=True, mode=0o700)
    before = snapshot.capture(ROOT)
    (private / "source-before.json").write_text(json.dumps(before, indent=2) + "\n")
    source = before["source_sha256"]
    created = datetime.now(timezone.utc).isoformat()
    revision = before["head"] + "-I6-" + source
    receipt = {
        "started_utc": created, "source_sha256": source, "head": before["head"],
        "frontend_dist_sha256": before["frontend_dist_sha256"], "revision": revision,
        "target": "linux/amd64", "cgo_enabled": False, "production_modified": False,
        "builder_sha256": sha(Path(__file__)),
        "private_build_directory": str(private), "status": "building", "operations": [],
    }

    def run(name, command, cwd=ROOT, env=None):
        log = private / (name + ".log")
        with log.open("w") as handle:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=handle, stderr=subprocess.STDOUT, timeout=2400)
        receipt["operations"].append({"name": name, "exit_code": result.returncode, "log_sha256": sha(log)})
        print(json.dumps({"operation": name, "exit_code": result.returncode}), flush=True)
        result.check_returncode()

    try:
        provenance = json.loads((VALIDATION / "office-r0-provenance.json").read_text())
        base = inspect(BASE)
        assert provenance["provenance_verified"] and base["Id"] == provenance["local_image_id"]
        assert len(base["RootFS"]["Layers"]) == provenance["rootfs_layer_count"]
        receipt["runtime_base_image_id"] = base["Id"]
        receipt["go_version"] = subprocess.check_output(["go", "version"], cwd=ROOT / "backend", text=True).strip()
        candidate, provider = private / "candidate", private / "provider"
        candidate.mkdir()
        provider.mkdir()
        env = os.environ | {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0", "GOMAXPROCS": "3"}
        ldflags = f"-s -w -X main.Version=2.8.13-monas-claude-weekly-I6-local -X main.Commit={revision} -X main.Date={created} -X main.BuildType=release"
        run("server", ["go", "build", "-p=1", "-tags=embed", "-trimpath", "-buildvcs=false", "-ldflags=" + ldflags, "-o", str(candidate / "sub2api"), "./cmd/server"], ROOT / "backend", env)
        run("release", ["go", "build", "-p=1", "-trimpath", "-buildvcs=false", "-o", str(candidate / "carpool-release"), "./cmd/carpool-release"], ROOT / "backend", env)
        provider_source = VALIDATION / "environment-I4-provider/main.go"
        run("provider", ["go", "build", "-p=1", "-trimpath", "-o", str(provider / "provider"), str(provider_source)], ROOT / "backend", env)
        shutil.copytree(ROOT / "backend/resources", candidate / "resources")
        shutil.copy2(VALIDATION / "environment-I6-runtime.Dockerfile", candidate / "Dockerfile")
        shutil.copy2(VALIDATION / "environment-I4-provider.Dockerfile", provider / "Dockerfile")
        receipt["provider_source_sha256"] = sha(provider_source)
        receipt["provider_tag"] = "claude-weekly-quota-provider:" + sha(provider_source)[:12]
        run("provider-image", ["docker", "build", "--platform=linux/amd64", "--network=none", "-t", receipt["provider_tag"], str(provider)])
        for role, stage in (("r2", "I6-isolated-candidate"), ("r1", "I6-compatible-R1")):
            tag = "sub2api:claude-weekly-i6-" + role + "-" + source[:12]
            run(role + "-image", ["docker", "build", "--platform=linux/amd64", "--pull=false", "--network=none", "-t", tag, "--build-arg", "SOURCE_SHA256=" + source, "--build-arg", "REVISION=" + revision, "--build-arg", "BUILD_DATE=" + created, "--build-arg", "QUOTA_STAGE=" + stage, str(candidate)])
            built = inspect(tag)
            assert built["RootFS"]["Layers"][:len(base["RootFS"]["Layers"])] == base["RootFS"]["Layers"]
            receipt[role + "_image"] = tag
            receipt[role + "_image_id"] = built["Id"]
            run(role + "-version", ["docker", "run", "--rm", "--platform=linux/amd64", "--network=none", "--read-only", "--user=1000:1000", "--entrypoint=/app/sub2api", tag, "--version"])
        assert inspect(BASE)["Id"] == base["Id"]
        after = snapshot.capture(ROOT)
        (private / "source-after.json").write_text(json.dumps(after, indent=2) + "\n")
        receipt["source_after_sha256"] = after["source_sha256"]
        assert before["source_sha256"] == after["source_sha256"], "source changed while building"
        receipt["binaries"] = {p.name: sha(p) for p in (candidate / "sub2api", candidate / "carpool-release", provider / "provider")}
        receipt["migration_sha256"] = {p.name: sha(p) for p in sorted((ROOT / "backend/migrations").glob("*.sql"))}
        receipt["runtime_recipe_sha256"] = sha(candidate / "Dockerfile")
        receipt["runtime_base_layers_retained"] = True
        receipt["status"] = "built_source_stable_pending_runtime_acceptance"
    except Exception as error:
        receipt["status"] = "failed"
        receipt["error"] = str(error)
        raise
    finally:
        receipt["finished_utc"] = datetime.now(timezone.utc).isoformat()
        args.receipt.write_text(json.dumps(receipt, indent=2) + "\n")
        print(json.dumps({"receipt": str(args.receipt), "status": receipt["status"]}), flush=True)


if __name__ == "__main__":
    main()
