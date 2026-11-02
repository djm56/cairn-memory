#!/usr/bin/env bash
# m2-live-run-check.sh — the client-side helper for the M2 live run (issue #19, docs/M2-LIVE-RUN.md).
#
# Run it on the client, from the project directory, after the Claude Code session. It runs
# `cairn doctor` and `cairn export task <id>` and prints both in one block, ready to paste back.
#
# It is read-only: it creates, changes and deletes nothing. It never prints the API key. It
# does not read the key file either: cairn doctor reports where the key was found, not the key.
#
# Usage:
#   scripts/m2-live-run-check.sh <task-id>
#   CAIRN=/full/path/to/cairn scripts/m2-live-run-check.sh <task-id>
#
# CAIRN names the binary; it defaults to cairn on PATH. The task id has the form
# task-YYYYMMDD-NN, as the session reports it. Exits 0 when both commands exit 0, 1 when one
# does not, and 2 for a usage error.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/m2-live-run-check.sh <task-id>

  <task-id>   The task the Claude Code session created, such as task-20261031-01.
  CAIRN       The cairn binary to run (default: cairn on PATH).

Prints cairn doctor and cairn export task <task-id>, in one block to paste back.
EOF
}

if [ "$#" -eq 1 ] && { [ "$1" = "--help" ] || [ "$1" = "-h" ]; }; then
  usage
  exit 0
fi
if [ "$#" -ne 1 ]; then
  usage >&2
  exit 2
fi

task_id="$1"
# Accept only the id's own shape, so the argument cannot carry a flag or a space into cairn.
if ! [[ "$task_id" =~ ^task-[0-9]{8}-[0-9]+$ ]]; then
  echo "m2-live-run-check: the task id must look like task-YYYYMMDD-NN, got: $task_id" >&2
  exit 2
fi

cairn_bin="${CAIRN:-cairn}"
if ! command -v "$cairn_bin" >/dev/null 2>&1; then
  echo "m2-live-run-check: cannot find the cairn binary '$cairn_bin': put it on PATH, or set CAIRN" >&2
  exit 2
fi

doctor_status=0
export_status=0

echo "=== cairn doctor ==="
"$cairn_bin" doctor || doctor_status=$?
echo
echo "=== cairn export task $task_id ==="
"$cairn_bin" export task "$task_id" || export_status=$?
echo
echo "=== end: doctor exit $doctor_status, export exit $export_status ==="

if [ "$doctor_status" -ne 0 ] || [ "$export_status" -ne 0 ]; then
  exit 1
fi
exit 0
