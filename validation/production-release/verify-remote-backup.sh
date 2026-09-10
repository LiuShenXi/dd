#!/bin/sh
set -eu
umask 077
export LC_ALL=C
if [ "$#" -ne 2 ]; then echo "usage: $0 /remote/production-before.dump sha256" >&2; exit 64; fi
dump_path=$1
expected_sha=$2
case "$dump_path" in /home/linuxuser/apps/sub2api/production-release-[0-9-]*/production-before.dump) ;; *) exit 64 ;; esac
case "$dump_path" in *[!A-Za-z0-9_./-]*) exit 64 ;; esac
case "$expected_sha" in *[!0-9a-f]*|'') exit 64 ;; esac
[ "${#expected_sha}" -eq 64 ] || exit 64
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
run_id=$(date -u '+%Y%m%dT%H%M%SZ')-$$-$(openssl rand -hex 4)
run_dir=$(mktemp -d "${TMPDIR:-/tmp}/carpool-backup-restore.XXXXXX")
remote_dir="/tmp/carpool-backup-restore-$run_id"
printf 'run=%s\nremote=%s\ndump=%s\nsha256=%s\n' "$run_id" "$remote_dir" "$dump_path" "$expected_sha" >"$run_dir/manifest"
echo "RELEASE_BACKUP_RESTORE_PREPARING run=$run_id logs=$run_dir"
ssh -o BatchMode=yes -o ConnectTimeout=10 newapi-vultr "mkdir -m 700 '$remote_dir'"
scp -q -o BatchMode=yes -o ConnectTimeout=10 "$script_dir/remote-backup-restore-worker.sh" "newapi-vultr:$remote_dir/"
set +e
ssh -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 newapi-vultr \
  "timeout -s TERM -k 30s 900s sh '$remote_dir/remote-backup-restore-worker.sh' '$remote_dir' '$run_id' '$dump_path' '$expected_sha'" \
  >"$run_dir/remote.private.log" 2>&1
restore_exit=$?
set -e
if [ "$restore_exit" -ne 0 ]; then
  echo "RELEASE_BACKUP_RESTORE_FAILED run=$run_id exit=$restore_exit logs=$run_dir remote=$remote_dir" >&2
  exit 1
fi
echo "RELEASE_BACKUP_RESTORE_PASSED run=$run_id users=24 keys=29 migration=234 sha256=$expected_sha logs=$run_dir remote=$remote_dir"
