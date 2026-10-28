#!/usr/bin/env bash
# check-docs.sh — documentation checks for Cairn (CONTRIBUTING.md).
#
# Phase markers: README.md and docs/PLAN.md must each carry exactly one
# <!-- cairn-phase: N --> marker, N an integer from 0 to 10, and the two N
# must match.
#
# Changelog (pull requests only): CHANGELOG.md must be among the files the
# pull request changes, unless the pull request has the skip-changelog label.
#
# Usage:
#   scripts/check-docs.sh [--root DIR] [--base SHA] [--labels LIST]
#
# --base and --labels default to $PR_BASE_SHA and $PR_LABELS, which the docs
# workflow sets. Without a base the changelog check is skipped, so the script
# also runs locally with the phase check alone. --labels is comma-separated.
# Exits 0 when every check passes, 1 when any fails.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/check-docs.sh [options]

  --root DIR      Repository root to check (default: the one this script is in).
  --base SHA      Base commit of the pull request. Enables the changelog check.
  --labels LIST   Comma-separated labels on the pull request.
  --help          Show this help.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BASE="${PR_BASE_SHA:-}"
LABELS="${PR_LABELS:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --root)
      [ "$#" -ge 2 ] || { echo "check-docs: --root needs a directory" >&2; exit 1; }
      ROOT="$2"
      shift 2
      ;;
    --base)
      [ "$#" -ge 2 ] || { echo "check-docs: --base needs a commit" >&2; exit 1; }
      BASE="$2"
      shift 2
      ;;
    --labels)
      [ "$#" -ge 2 ] || { echo "check-docs: --labels needs a list" >&2; exit 1; }
      LABELS="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "check-docs: unknown argument: $1 (see --help)" >&2
      exit 1
      ;;
  esac
done

# marker_phase FILE: print the phase N in FILE's single cairn-phase marker.
# Returns 1 with a reason on stderr when the marker is missing, repeated,
# malformed or out of range.
marker_phase() {
  local file="$1" opens value
  if [ ! -f "$file" ]; then
    echo "$file: not found" >&2
    return 1
  fi

  # Count occurrences, not lines, so two markers on one line still fail.
  opens="$(grep -o -E '<!-- *cairn-phase:' "$file" | wc -l | tr -d ' ' || true)"
  if [ "$opens" -eq 0 ]; then
    echo "$file: no <!-- cairn-phase: N --> marker" >&2
    return 1
  fi
  if [ "$opens" -gt 1 ]; then
    echo "$file: $opens cairn-phase markers, expected exactly one" >&2
    return 1
  fi

  value="$(grep -o -E '<!-- *cairn-phase: *[^ >]* *-->' "$file" | head -n 1 \
    | sed -E 's/.*cairn-phase: *([^ >]*) *-->.*/\1/' || true)"
  case "$value" in
    ''|*[!0-9]*)
      echo "$file: cairn-phase marker is malformed (value: '${value}')" >&2
      return 1
      ;;
  esac
  if [ "$value" -gt 10 ]; then
    echo "$file: cairn-phase $value is out of range (0 to 10)" >&2
    return 1
  fi
  printf '%s\n' "$value"
}

FAILED=0

readme_phase=""
plan_phase=""
if readme_phase="$(marker_phase "$ROOT/README.md")"; then
  echo "phase marker: README.md says $readme_phase"
else
  FAILED=1
fi
if plan_phase="$(marker_phase "$ROOT/docs/PLAN.md")"; then
  echo "phase marker: docs/PLAN.md says $plan_phase"
else
  FAILED=1
fi
if [ -n "$readme_phase" ] && [ -n "$plan_phase" ] && [ "$readme_phase" != "$plan_phase" ]; then
  echo "phase marker: README.md ($readme_phase) and docs/PLAN.md ($plan_phase) disagree" >&2
  FAILED=1
fi

# Changelog. Skipped without a base commit (local run) or with the label.
if [ -z "$BASE" ]; then
  echo "changelog: skipped (no pull request base commit)"
elif grep -qx 'skip-changelog' <<< "$(printf '%s' "$LABELS" | tr ',' '\n')"; then
  echo "changelog: skipped (skip-changelog label)"
else
  changed="$(git -C "$ROOT" diff --name-only "$BASE...HEAD")" \
    || { echo "changelog: cannot diff against $BASE; is the history fetched?" >&2; exit 1; }
  if grep -qx 'CHANGELOG.md' <<< "$changed"; then
    echo "changelog: CHANGELOG.md is changed"
  else
    echo "changelog: CHANGELOG.md is not changed. Add an entry, or set the skip-changelog label." >&2
    FAILED=1
  fi
fi

if [ "$FAILED" -ne 0 ]; then
  echo "docs check: FAILED" >&2
  exit 1
fi
echo "docs check: passed"
