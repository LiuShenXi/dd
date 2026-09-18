#!/bin/sh
# Run on the Linux deployment host as root. Never recursively changes ownership.
set -eu
root=/home/linuxuser/apps/sub2api/codex-ticket-sentinel
target=${1:-$root/state}
[ "$(id -u)" = 0 ] || { echo 'Run as root to assign UID 10001.' >&2; exit 1; }
case "$target" in "$root"/*) ;; *) echo 'State path is outside the dedicated sentinel root.' >&2; exit 1 ;; esac
[ ! -L "$target" ] || { echo 'Refusing a symlink state directory.' >&2; exit 1; }
resolved=$(realpath -m -- "$target")
case "$resolved" in "$root"/*) ;; *) echo 'Resolved state path escapes sentinel root.' >&2; exit 1 ;; esac
[ ! -e "$target" ] || [ -d "$target" ] || { echo 'State path is not a directory.' >&2; exit 1; }
install -d -o 10001 -g 10001 -m 0700 -- "$target"
echo 'State directory ready for UID/GID 10001; existing files were not changed.'
