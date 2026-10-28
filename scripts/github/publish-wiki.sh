#!/usr/bin/env bash
# publish-wiki.sh — publish the repository's wiki/ folder to the GitHub wiki.
#
# Clones <repo>.wiki.git (branch master), copies wiki/ over it, and commits
# only when something changed. The commit message names the source commit.
#
# wiki/ in this repository is the single source of truth. The wiki is a
# mirror of it: --apply makes the wiki match wiki/ exactly, so a page in the
# wiki that is not in wiki/ is deleted, and an edit made on the wiki's own
# pages is overwritten by the next publish. Make wiki changes in wiki/.
#
# Usage:
#   scripts/github/publish-wiki.sh [--apply]
#   scripts/github/publish-wiki.sh --help
#
# Dry run by default: clones the wiki and lists what would change, including
# every page that would be deleted from the wiki. Nothing is committed or
# pushed. --apply commits and pushes.
#
# Where the wiki comes from, first match wins:
#   WIKI_REMOTE       explicit URL (used by tests).
#   GITHUB_ACTIONS    CI (GITHUB_ACTIONS=true): https://github.com/$GITHUB_REPOSITORY.wiki.git
#                     with GITHUB_TOKEN passed as an HTTP header, never in the URL.
#                     Dies if GITHUB_TOKEN is not set.
#   origin remote     the user's own git or ssh credentials; the origin URL
#                     with .wiki appended before .git.
#
# Any remote printed in a message has user:password@ redacted to ***@.
#
# Refuses to run if wiki/ is missing or has no Home.md. Bash 3.2 compatible.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/github/publish-wiki.sh [options]

  --apply   Commit and push. Without it, only the planned changes are shown,
            including the pages that would be deleted from the wiki.
  --help    Show this help.
EOF
}

APPLY=0

# Replaces user:password@ in any http(s) URL with ***@. The credential may hold
# an @ itself: the match runs to the last @ before the first /, so the whole
# credential goes, not only the part before its first @.
REDACT='s#(https?://)[^/[:space:]]*@#\1***@#g'

die() {
  printf 'publish-wiki: %s\n' "$*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --apply)
      APPLY=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1 (see --help)"
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WIKI_SRC="$ROOT/wiki"

[ -d "$WIKI_SRC" ] || die "wiki/ is missing from $ROOT"
[ -f "$WIKI_SRC/Home.md" ] || die "wiki/Home.md is missing; refusing to publish an empty wiki"

SHA="$(git -C "$ROOT" rev-parse HEAD)" || die "$ROOT is not a git checkout"

if [ -n "${WIKI_REMOTE:-}" ]; then
  URL="$WIKI_REMOTE"
elif [ "${GITHUB_ACTIONS:-}" = "true" ]; then
  [ -n "${GITHUB_TOKEN:-}" ] || die "running in CI but GITHUB_TOKEN is not set"
  [ -n "${GITHUB_REPOSITORY:-}" ] || die "GITHUB_REPOSITORY is not set"
  URL="https://github.com/${GITHUB_REPOSITORY}.wiki.git"
  # The token goes in a header so it never appears in the URL or in messages.
  AUTH_HEADER="AUTHORIZATION: basic $(printf 'x-access-token:%s' "$GITHUB_TOKEN" | base64 | tr -d '\n')"
  export GIT_CONFIG_COUNT=1
  export GIT_CONFIG_KEY_0="http.https://github.com/.extraheader"
  export GIT_CONFIG_VALUE_0="$AUTH_HEADER"
else
  ORIGIN="$(git -C "$ROOT" remote get-url origin)" || die "no origin remote to derive the wiki from"
  URL="$(printf '%s' "$ORIGIN" | sed -e 's#\.git$##').wiki.git"
fi

# The remote as it may be printed: credentials redacted.
SHOWN_URL="$(printf '%s' "$URL" | sed -E "$REDACT")"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/cairn-wiki.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
CLONE="$WORK/wiki"

# git's own error text can echo the URL with credentials, so it is redacted too.
if ! git clone --quiet -- "$URL" "$CLONE" 2>"$WORK/clone.err"; then
  sed -E "$REDACT" "$WORK/clone.err" >&2
  die "cannot clone the wiki at $SHOWN_URL. Has the wiki been created on GitHub?"
fi
# A wiki that has never been written clones as an empty repository: git warns,
# exits 0, and there is no commit or branch. checkout -B creates master there,
# so the first publish lands on master.
git -C "$CLONE" checkout -q -B master

if [ "$APPLY" = 1 ]; then
  # --checksum: a page is copied only when its content differs. The clone's
  # timestamps are its checkout time, so timestamps alone would rewrite every page.
  rsync -a --checksum --delete --exclude .git "$WIKI_SRC/" "$CLONE/"
  git -C "$CLONE" add -A
  if git -C "$CLONE" diff --cached --quiet; then
    echo "wiki is already up to date; nothing to push"
    exit 0
  fi
  MSG="Sync wiki from commit $SHA"
  if [ -z "$(git -C "$CLONE" config user.email || true)" ]; then
    git -C "$CLONE" -c user.name="cairn wiki publisher" \
      -c user.email="wiki-publisher@users.noreply.github.com" commit -q -m "$MSG"
  else
    git -C "$CLONE" commit -q -m "$MSG"
  fi
  git -C "$CLONE" push -q origin master
  echo "published wiki from commit $SHA"
else
  # rsync failures stop the script here; only the itemized lines are kept.
  # --checksum, as in the apply branch, so both list the same pages.
  RAW="$(rsync -an --checksum --delete --itemize-changes --exclude .git "$WIKI_SRC/" "$CLONE/")"
  # Additions and updates (> < c). The directory's own timestamp line (.d...)
  # is not a content change and is dropped.
  CHANGES="$(printf '%s\n' "$RAW" | grep -E '^[<>c]' || true)"
  # Deletions: pages in the wiki that are not in wiki/. --apply removes them.
  DELETES="$(printf '%s\n' "$RAW" | grep -E '^\*deleting' | sed -e 's/^\*deleting *//' || true)"
  if [ -z "$CHANGES" ] && [ -z "$DELETES" ]; then
    echo "dry run: the wiki is already up to date with wiki/ at $SHA"
  else
    echo "dry run: would publish wiki/ from commit $SHA to $SHOWN_URL (branch master):"
    if [ -n "$CHANGES" ]; then
      echo "  add or update:"
      printf '%s\n' "$CHANGES" | sed 's/^/    /'
    fi
    if [ -n "$DELETES" ]; then
      echo "  would DELETE from the wiki (not in wiki/):"
      printf '%s\n' "$DELETES" | sed 's/^/    /'
    fi
    echo "dry run: nothing committed or pushed. Pass --apply to publish."
  fi
fi
