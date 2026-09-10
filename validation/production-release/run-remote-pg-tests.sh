#!/bin/sh
set -eu
umask 077
export LC_ALL=C

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 '^(TestExactOne|TestExactTwo)$' [--compile-only]" >&2
  exit 64
fi
compile_only=false
if [ "$#" -eq 2 ]; then
  if [ "$2" != --compile-only ]; then exit 64; fi
  compile_only=true
fi
test_pattern=$1
case "$test_pattern" in
  ^*\$) ;;
  *) echo 'test pattern must be anchored with ^ and $' >&2; exit 64 ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
go_bin=${CARPOOL_RELEASE_GO_BIN:-/Users/shenxi/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.darwin-arm64/bin/go}
run_id=$(date -u '+%Y%m%dT%H%M%SZ')-$$-$(openssl rand -hex 4)
run_dir=$(mktemp -d "${TMPDIR:-/tmp}/carpool-release-pg.XXXXXX")
remote_dir="/tmp/carpool-release-pg-$run_id"
remote_host=newapi-vultr
export GOTOOLCHAIN=local GOMAXPROCS=2

printf '%s\n' "$test_pattern" >"$run_dir/test-pattern"
printf 'run_id=%s\nlocal_dir=%s\nremote_dir=%s\n' "$run_id" "$run_dir" "$remote_dir" >"$run_dir/manifest"
echo "RELEASE_PG_PREPARING run=$run_id logs=$run_dir"
fingerprint_sources() {
  find "$repository_root/backend" "$repository_root/validation/source-runtime/repository-tests" \
    -type f \( -name '*.go' -o -name '*.sql' -o -name go.mod -o -name go.sum \) -print0 \
    >"$run_dir/source-files.private"
  xargs -0 shasum -a 256 <"$run_dir/source-files.private" >"$run_dir/source-hashes.private"
  LC_ALL=C sort "$run_dir/source-hashes.private" | shasum -a 256 | awk '{print $1}'
}
source_hash_before=$(fingerprint_sources)
printf '%s\n' "$source_hash_before" >"$run_dir/source.sha256"
cd "$repository_root/backend"
if [ -n "${CARPOOL_RELEASE_REUSE_DIR:-}" ]; then
  if [ ! -f "$CARPOOL_RELEASE_REUSE_DIR/source.sha256" ] || \
     [ "$(cat "$CARPOOL_RELEASE_REUSE_DIR/source.sha256")" != "$source_hash_before" ] || \
     [ ! -f "$CARPOOL_RELEASE_REUSE_DIR/repository-tests" ] || \
     [ "$(shasum -a 256 "$CARPOOL_RELEASE_REUSE_DIR/repository-tests" | awk '{print $1}')" != "$(awk '{print $1}' "$CARPOOL_RELEASE_REUSE_DIR/binary.sha256")" ]; then
    echo 'compiled artifact does not match current source or binary fingerprint' >&2
    exit 1
  fi
  ln -s "$CARPOOL_RELEASE_REUSE_DIR/repository-tests" "$run_dir/repository-tests"
else
"$go_bin" test -p 2 \
  ../validation/source-runtime/repository-tests/prepare-overlay.go \
  ../validation/source-runtime/repository-tests/prepare_overlay_test.go \
  >"$run_dir/overlay.private.log" 2>&1
"$go_bin" run -p 2 ../validation/source-runtime/repository-tests/prepare-overlay.go \
  -source "$repository_root/backend/internal/repository/integration_harness_test.go" \
  -output "$run_dir/integration_harness_test.go" \
  -overlay "$run_dir/overlay.json" \
  -helper "$repository_root/validation/source-runtime/repository-tests/source_runtime_testmain_test.go" \
  -virtual "$repository_root/backend/internal/repository/source_runtime_isolated_testmain_test.go" \
  >>"$run_dir/overlay.private.log" 2>&1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$go_bin" test -p 2 -c -tags=integration \
  -overlay="$run_dir/overlay.json" -o "$run_dir/repository-tests" \
  ./internal/repository >"$run_dir/compile.private.log" 2>&1
fi
if [ "$(fingerprint_sources)" != "$source_hash_before" ]; then
  echo 'source changed during test compilation; refusing stale acceptance artifact' >&2
  exit 1
fi
chmod 555 "$run_dir/repository-tests"
shasum -a 256 "$run_dir/repository-tests" >"$run_dir/binary.sha256"
if [ "$compile_only" = true ]; then
  echo "RELEASE_PG_COMPILED run=$run_id logs=$run_dir"
  exit 0
fi

ssh -o BatchMode=yes -o ConnectTimeout=10 "$remote_host" "mkdir -m 700 '$remote_dir'"
scp -q -o BatchMode=yes -o ConnectTimeout=10 \
  "$run_dir/repository-tests" "$run_dir/test-pattern" \
  "$script_dir/remote-pg-test-worker.sh" "$remote_host:$remote_dir/"

set +e
ssh -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 \
  "$remote_host" "timeout -s TERM -k 30s 1500s sh '$remote_dir/remote-pg-test-worker.sh' '$remote_dir' '$run_id'" \
  >"$run_dir/remote.private.log" 2>&1
test_exit=$?
scp -q -o BatchMode=yes -o ConnectTimeout=10 \
  "$remote_host:$remote_dir/test.private.log" "$run_dir/test.private.log"
fetch_exit=$?
set -e
if [ "$test_exit" -ne 0 ] || [ "$fetch_exit" -ne 0 ]; then
  echo "RELEASE_PG_FAILED run=$run_id exit=$test_exit fetch=$fetch_exit logs=$run_dir remote=$remote_dir" >&2
  exit 1
fi
run_count=$(awk '/^=== RUN / { n++ } END { print n+0 }' "$run_dir/test.private.log")
pass_count=$(awk '/^--- PASS: / { n++ } END { print n+0 }' "$run_dir/test.private.log")
skip_count=$(awk '/^[[:space:]]*--- SKIP: / { n++ } END { print n+0 }' "$run_dir/test.private.log")
if [ "$run_count" -eq 0 ] || [ "$skip_count" -ne 0 ]; then
  echo "RELEASE_PG_FAILED run=$run_id tests=$run_count skips=$skip_count logs=$run_dir" >&2
  exit 1
fi
echo "RELEASE_PG_PASSED run=$run_id tests=$run_count passed=$pass_count skips=$skip_count logs=$run_dir"
