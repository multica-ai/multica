# shellcheck shell=bash
# Source from a launchd-managed run-*.sh script BEFORE `exec` of the service
# binary. Redirects fd 1/2 through the in-repo log rotator so the files in
# .deploy/logs/ rotate at 100 MB / 7 generations instead of growing forever.
#
# Required vars:
#   REPO      — absolute path to repo root
#   LOG_NAME  — basename (e.g. "backend" → backend.out.log / backend.err.log)
#
# See .deploy/README.md for the rotation policy and JEH-1881 for context.

_rot() {
  exec python3 "$REPO/.deploy/log-rotator.py" \
    --max-bytes $((100 * 1024 * 1024)) --keep 7 "$1"
}

mkdir -p "$REPO/.deploy/logs"
exec > >(_rot "$REPO/.deploy/logs/${LOG_NAME}.out.log") \
     2> >(_rot "$REPO/.deploy/logs/${LOG_NAME}.err.log")
