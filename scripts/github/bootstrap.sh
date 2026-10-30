#!/usr/bin/env bash
# bootstrap.sh — set up GitHub project management for Cairn: labels,
# milestones, roadmap issues and the Projects v2 board.
#
# Usage:
#   scripts/github/bootstrap.sh [--repo owner/name] [--only SECTIONS] [--apply]
#   scripts/github/bootstrap.sh --help
#
# Dry run by default: prints every create and write it would make and writes
# nothing. --apply makes them.
#
# Re-running only fills what is missing. A value a human has set is never
# changed. Every function that writes to GitHub, and everything it may write:
#   sync_labels      creates a label that is missing. Names are compared without
#                    regard to case, as GitHub does. An existing label is never
#                    edited; colour or description drift is reported as info.
#   sync_milestones  creates a milestone that is missing. An existing milestone
#                    is never edited; description drift is reported as info.
#   sync_issues      creates a roadmap issue (title, body, labels, milestone)
#                    when no issue carries its marker and none has its title.
#                    An existing issue gets no label, milestone or body write
#                    from here. The epic children block is the one exception,
#                    handled by sync_children.
#   sync_children    PATCHes an epic's body. Only the lines between its
#                    children-start and children-end markers change, and only
#                    to fill what is missing: a line already there is kept as
#                    written (tick, added text, position) when it starts with a
#                    current child's #number or key; a child with no line is
#                    added at the end of the block. Everything outside the
#                    markers is sent back byte for byte, line endings included.
#                    Two separate checks stand before the write, each refusing it
#                    (warning, nothing written, counted as skipped):
#                    - cut check: the offsets are in range; the last line before
#                      the cut is the start marker, the first line after it is
#                      the end marker, and the old block holds no marker line.
#                      Catches a wrong cut point (offset fault). It cannot see a
#                      copy fault.
#                    - copy check: the first and last bytes of the new body are
#                      compared with the original. Catches a fault in building
#                      or copying the body. It cannot see a wrong cut, since both
#                      sides use the same offsets.
#                    The body is read once, before the PATCH; an edit made on
#                    GitHub between that read and the PATCH is overwritten (a
#                    race this script does not close).
#                    A body without exactly one start line and one end line, in
#                    that order, is warned about and left alone. A body that
#                    would not change is not written.
#   ensure_field     creates a single-select field (Status, Phase, Area) when the
#                    project lacks it, with its options. An existing field is
#                    never changed; missing options are reported with manual
#                    steps.
#   sync_project     creates the "Cairn Memory Roadmap" project when none exists
#                    and links it to the repository when it is not linked.
#   sync_items       adds a roadmap issue to the board when it is not there, and
#                    sets its Phase or Area only when that value is empty. Phase
#                    is history for work from before Plan v1. Milestone is
#                    GitHub's built-in board field: it follows the issue's own
#                    milestone, which sync_issues sets on create. This script
#                    creates no Milestone field and sets no Milestone value.
#
# Nothing is deleted, closed or reopened. Roadmap issues are matched by the
# exact marker line <!-- cairn-roadmap: KEY -->. A missing marker is never
# replaced by a new issue: if an issue with the same title exists, the script
# warns and skips it so the marker can be restored by hand. Issues opened from
# the issue forms are not read or changed by this script.
#
# Needs gh (logged in) and jq. The project section needs the project scope:
# gh auth refresh -s project,read:project
#
# Reads .github/labels.json and .github/roadmap.json. Bash 3.2 compatible.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/github/bootstrap.sh [options]

  --repo owner/name   Repository to act on (default: the one gh resolves here).
  --only SECTIONS     labels, milestones, issues, project. Repeat the flag or
                      separate with commas. Default: all four, in that order.
  --apply             Make the changes. Without it nothing is written.
  --help              Show this help.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LABELS_FILE="$ROOT/.github/labels.json"
ROADMAP_FILE="$ROOT/.github/roadmap.json"
PROJECT_TITLE="Cairn Memory Roadmap"

APPLY=0
REPO=""
SECTIONS=""
CREATED=0
UPDATED=0
SAME=0
SKIPPED=0
LOADED=0

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'bootstrap: %s\n' "$*" >&2
  exit 1
}

# want SECTION: true when the section was asked for.
want() {
  case " $SECTIONS " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --apply)
      APPLY=1
      shift
      ;;
    --repo)
      [ "$#" -ge 2 ] || die "--repo needs owner/name"
      REPO="$2"
      shift 2
      ;;
    --only)
      [ "$#" -ge 2 ] || die "--only needs a section name"
      while IFS= read -r item; do
        case "$item" in
          labels|milestones|issues|project) SECTIONS="$SECTIONS $item" ;;
          *) die "unknown section: $item (labels, milestones, issues, project)" ;;
        esac
      done < <(printf '%s\n' "$2" | tr ',' '\n')
      shift 2
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

[ -n "$SECTIONS" ] || SECTIONS="labels milestones issues project"

command -v gh >/dev/null 2>&1 || die "gh is not installed"
command -v jq >/dev/null 2>&1 || die "jq is not installed"
[ -f "$LABELS_FILE" ] || die "missing $LABELS_FILE"
[ -f "$ROADMAP_FILE" ] || die "missing $ROADMAP_FILE"
jq empty "$LABELS_FILE" "$ROADMAP_FILE" || die "labels or roadmap JSON does not parse"

if [ -z "$REPO" ]; then
  REPO="$(gh repo view --json nameWithOwner --jq .nameWithOwner)" \
    || die "cannot resolve the repository; pass --repo owner/name"
fi
OWNER="${REPO%%/*}"
NAME="${REPO#*/}"

# Scratch space for request bodies and intermediate JSON. Removed on exit.
WORK="$(mktemp -d "${TMPDIR:-/tmp}/cairn-bootstrap.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

if [ "$APPLY" = 1 ]; then
  log "apply: writing to $REPO"
else
  log "dry run: nothing is written to $REPO. Pass --apply to make these changes."
fi

# gh_list PATH: every page of a REST collection as one JSON array.
gh_list() {
  gh api --paginate "$1" | jq -s 'add // []'
}

# gql QUERY VARIABLES_JSON: one GraphQL call, response on stdout. Exits on
# transport failure and on any GraphQL error.
gql() {
  local out
  jq -n --arg q "$1" --argjson v "$2" '{query: $q, variables: $v}' > "$WORK/gql-req.json"
  out="$(gh api graphql --input "$WORK/gql-req.json")" || die "GraphQL call failed"
  if printf '%s' "$out" | jq -e '.errors' >/dev/null 2>&1; then
    printf '%s' "$out" | jq -r '.errors[].message' >&2
    die "GraphQL returned errors"
  fi
  printf '%s\n' "$out"
}

# marker_matches KEY: JSON array of existing issues (any state) that carry the
# roadmap marker for KEY on a line of their own, lowest number first. The line
# may have surrounding spaces or a CR (web edits save CRLF). A marker quoted
# inside another sentence does not match.
marker_matches() {
  jq -c --arg m "<!-- cairn-roadmap: $1 -->" \
    '[.[] | select((.body // "") | gsub("\r"; "") | split("\n")
        | map(gsub("^[ \t]+|[ \t]+$"; "")) | any(. == $m))] | sort_by(.number)' \
    "$WORK/issues-existing.json"
}

# title_matches TITLE: JSON array of existing issues (any state) whose title
# equals TITLE exactly, lowest number first. Used when the marker is missing.
title_matches() {
  jq -c --arg t "$1" '[.[] | select(.title == $t)] | sort_by(.number)' \
    "$WORK/issues-existing.json"
}

# load_existing: read the repository once. Builds the existing-issue list, the
# milestone list, and index.tsv (key, number, node id) for matched issues.
# Read-only; safe in dry run.
load_existing() {
  [ "$LOADED" = 0 ] || return 0
  LOADED=1
  gh_list "repos/$REPO/issues?state=all&per_page=100" \
    | jq '[.[] | select(has("pull_request") | not)]' > "$WORK/issues-existing.json"
  gh_list "repos/$REPO/milestones?state=all&per_page=100" > "$WORK/milestones-now.json"
  jq -c '.issues[]' "$ROADMAP_FILE" > "$WORK/issues-desired.jsonl"
  mkdir -p "$WORK/bodies"
  : > "$WORK/index.tsv"

  while IFS= read -r issue; do
    key="$(printf '%s' "$issue" | jq -r '.key')"
    matches="$(marker_matches "$key")"
    count="$(printf '%s' "$matches" | jq 'length')"
    if [ "$count" -gt 1 ]; then
      log "  warning: $count issues carry the marker for $key; using the lowest number"
    fi
    if [ "$count" -ge 1 ]; then
      printf '%s\t%s\t%s\n' "$key" \
        "$(printf '%s' "$matches" | jq -r '.[0].number')" \
        "$(printf '%s' "$matches" | jq -r '.[0].node_id')" >> "$WORK/index.tsv"
      # -j: the exact body bytes. -r would add a newline GitHub never stored.
      printf '%s' "$matches" | jq -j '.[0].body' > "$WORK/bodies/$key.md"
    fi
  done < "$WORK/issues-desired.jsonl"
}

# index_number KEY: issue number recorded for KEY, empty if none yet.
index_number() {
  awk -F '\t' -v k="$1" '$1 == k { print $2; exit }' "$WORK/index.tsv"
}

sync_labels() {
  log "== labels"
  gh_list "repos/$REPO/labels?per_page=100" > "$WORK/labels-existing.json"
  jq -c '.[]' "$LABELS_FILE" > "$WORK/labels-desired.jsonl"

  while IFS= read -r label; do
    name="$(printf '%s' "$label" | jq -r '.name')"
    color="$(printf '%s' "$label" | jq -r '.color')"
    description="$(printf '%s' "$label" | jq -r '.description')"
    # GitHub treats label names as case-insensitive, so "Bug" and "bug" are one
    # label and creating the second is a 422.
    existing="$(jq -c --arg n "$name" '[.[] | select((.name | ascii_downcase) == ($n | ascii_downcase))][0] // empty' "$WORK/labels-existing.json")"

    if [ -z "$existing" ]; then
      if [ "$APPLY" = 1 ]; then
        printf '%s' "$label" | jq -c '{name, color, description}' > "$WORK/req.json"
        gh api -X POST "repos/$REPO/labels" --input "$WORK/req.json" > /dev/null
        log "  created label $name"
      else
        log "  would create label $name"
      fi
      CREATED=$((CREATED + 1))
      continue
    fi

    # Present: never edited, since a human may have changed it. Drift is info.
    ename="$(printf '%s' "$existing" | jq -r '.name')"
    if [ "$ename" != "$name" ]; then
      log "  info: label $name exists as \"$ename\" (case differs from .github/labels.json). Left as it is."
      SAME=$((SAME + 1))
      continue
    fi
    ecolor="$(printf '%s' "$existing" | jq -r '.color')"
    edesc="$(printf '%s' "$existing" | jq -r '.description // ""')"
    if [ "$(lower "$ecolor")" = "$(lower "$color")" ] && [ "$edesc" = "$description" ]; then
      SAME=$((SAME + 1))
      continue
    fi

    log "  info: label $name differs from .github/labels.json (colour or description). Left as it is."
    SAME=$((SAME + 1))
  done < "$WORK/labels-desired.jsonl"
}

sync_milestones() {
  log "== milestones"
  gh_list "repos/$REPO/milestones?state=all&per_page=100" > "$WORK/milestones-existing.json"
  jq -c '.milestones[]' "$ROADMAP_FILE" > "$WORK/milestones-desired.jsonl"

  while IFS= read -r ms; do
    title="$(printf '%s' "$ms" | jq -r '.title')"
    description="$(printf '%s' "$ms" | jq -r '.description')"
    existing="$(jq -c --arg t "$title" '[.[] | select(.title == $t)][0] // empty' "$WORK/milestones-existing.json")"

    if [ -z "$existing" ]; then
      if [ "$APPLY" = 1 ]; then
        printf '%s' "$ms" | jq -c '{title, description}' > "$WORK/req.json"
        gh api -X POST "repos/$REPO/milestones" --input "$WORK/req.json" > /dev/null
        log "  created milestone $title"
      else
        log "  would create milestone $title"
      fi
      CREATED=$((CREATED + 1))
      continue
    fi

    # Present: never edited, since a human may have changed it. Drift is info.
    edesc="$(printf '%s' "$existing" | jq -r '.description // ""')"
    if [ "$edesc" = "$description" ]; then
      SAME=$((SAME + 1))
      continue
    fi

    log "  info: milestone $title has a different description from .github/roadmap.json. Left as it is."
    SAME=$((SAME + 1))
  done < "$WORK/milestones-desired.jsonl"
}

sync_issues() {
  log "== issues"
  load_existing

  while IFS= read -r issue; do
    key="$(printf '%s' "$issue" | jq -r '.key')"
    title="$(printf '%s' "$issue" | jq -r '.title')"
    milestone="$(printf '%s' "$issue" | jq -r '.milestone')"
    matches="$(marker_matches "$key")"
    count="$(printf '%s' "$matches" | jq 'length')"
    msnum="$(jq -r --arg t "$milestone" '[.[] | select(.title == $t)][0].number // empty' "$WORK/milestones-now.json")"

    if [ "$count" -eq 0 ]; then
      # No marker. An issue with the same title is probably this roadmap item
      # with its marker line removed: never create a second one. Warn and skip.
      dup="$(title_matches "$title" | jq -r '.[0].number // empty')"
      if [ -n "$dup" ]; then
        log "  warning: #$dup has the title \"$title\" but no marker line; not creating $key."
        log "           Restore the line <!-- cairn-roadmap: $key --> in #$dup, then re-run."
        SKIPPED=$((SKIPPED + 1))
        continue
      fi
      if [ "$APPLY" = 1 ]; then
        [ -n "$msnum" ] || die "milestone '$milestone' does not exist; run --only milestones --apply first"
        printf '%s' "$issue" | jq -c --argjson m "$msnum" \
          '{title, body, labels, milestone: $m}' > "$WORK/req.json"
        resp="$(gh api -X POST "repos/$REPO/issues" --input "$WORK/req.json")"
        number="$(printf '%s' "$resp" | jq -r '.number')"
        node="$(printf '%s' "$resp" | jq -r '.node_id')"
        printf '%s\t%s\t%s\n' "$key" "$number" "$node" >> "$WORK/index.tsv"
        printf '%s' "$issue" | jq -j '.body' > "$WORK/bodies/$key.md"
        log "  created #$number $key: $title"
      else
        log "  would create $key: $title"
      fi
      CREATED=$((CREATED + 1))
      continue
    fi

    # Labels and milestone are set on create only. An existing issue may have
    # been edited by a human since, so nothing here is written. Drift is
    # reported as information.
    number="$(printf '%s' "$matches" | jq -r '.[0].number')"
    have="$(printf '%s' "$matches" | jq -c '[.[0].labels[].name]')"
    want_labels="$(printf '%s' "$issue" | jq -c '.labels')"
    missing_labels="$(jq -cn --argjson a "$have" --argjson b "$want_labels" \
      '[$b[] | select(. as $x | ($a | any(. == $x)) | not)]')"
    current_ms="$(printf '%s' "$matches" | jq -r '.[0].milestone.number // empty')"
    ms_drift=0
    if [ -n "$msnum" ] && [ "$current_ms" != "$msnum" ]; then
      ms_drift=1
    fi

    if [ "$missing_labels" != "[]" ] || [ "$ms_drift" = 1 ]; then
      note=""
      if [ "$missing_labels" != "[]" ]; then
        note="labels not on the issue: $(printf '%s' "$missing_labels" | jq -r 'join(", ")')"
      fi
      if [ "$ms_drift" = 1 ]; then
        [ -z "$note" ] || note="$note; "
        note="${note}milestone is not $milestone"
      fi
      log "  info: #$number $key differs from the roadmap ($note). Left as it is."
    fi
    SAME=$((SAME + 1))
  done < "$WORK/issues-desired.jsonl"

  sync_children
}

# first_line_trimmed: first line of stdin with a CR and surrounding spaces or
# tabs removed, the same normalisation the marker search uses.
first_line_trimmed() {
  LC_ALL=C awk 'NR == 1 { sub(/\r$/, ""); sub(/^[ \t]+/, ""); sub(/[ \t]+$/, ""); print; exit }'
}

# sync_children: fill in the generated list in each epic. Touches only the
# lines between children-start and children-end, and only to fill what is
# missing. A line already in the block is kept exactly as written (its tick,
# its text, its place), whenever its leading token is a current child: "#N" or
# the roadmap key. The one change to such a line is a key token becoming "#N"
# once the child's issue exists. A child with no line gets "- [ ] #N" at the
# end of the block. A line that matches no child is kept too. The placeholder
# "_No child issues yet._" is dropped once there are children to list.
# A body whose block would not change is not written, so a tick-only difference
# from the roadmap causes no write.
#
# The write is refused, with a warning and a skip, unless the body carries
# exactly one start line and exactly one end line, start first. The new body is
# the original bytes up to and including the start line, the new block, and the
# original bytes from the end line on, so nothing outside the markers is
# re-encoded. A final check compares those outer bytes with the original.
sync_children() {
  local issue labels key desired children cur number
  local ctitle num child map gen counts ns ne sl el pend estart size plen slen
  local pre blk suf new tmp crlf total eol pl sl2 bm

  while IFS= read -r issue; do
    labels="$(printf '%s' "$issue" | jq -r '.labels | join(" ")')"
    case " $labels " in
      *" type:epic "*) ;;
      *) continue ;;
    esac

    key="$(printf '%s' "$issue" | jq -r '.key')"
    desired="$(printf '%s' "$issue" | jq -r '.body')"
    children="$(printf '%s\n' "$desired" | sed -n 's/^<!-- children:\(.*\) -->$/\1/p' | tr -d ' ')"

    cur="$WORK/bodies/$key.md"
    if [ ! -f "$cur" ]; then
      log "  $key: children list is set once the epic exists (created by --apply)"
      continue
    fi
    number="$(index_number "$key")"

    # Find the markers in the original bytes. A marker is a line of its own,
    # ignoring a CR and surrounding spaces or tabs. Byte offsets are taken in
    # the C locale so a multi-byte character counts as its bytes. Output:
    # starts, ends, first start line, first end line, offset just after the
    # first start line, offset of the first end line.
    counts="$(LC_ALL=C awk '
      {
        line = $0
        sub(/\r$/, "", line)
        sub(/^[ \t]+/, "", line)
        sub(/[ \t]+$/, "", line)
        if (line == "<!-- children-start -->") {
          ns++
          if (ns == 1) { sl = NR; send = off + length($0) + 1 }
        } else if (line == "<!-- children-end -->") {
          ne++
          if (ne == 1) { el = NR; estart = off }
        }
        off += length($0) + 1
      }
      END { print ns + 0, ne + 0, sl + 0, el + 0, send + 0, estart + 0 }
    ' "$cur")"
    set -- $counts
    ns="$1"; ne="$2"; sl="$3"; el="$4"; pend="$5"; estart="$6"
    if [ "$ns" -ne 1 ] || [ "$ne" -ne 1 ]; then
      log "  warning: #$number $key has $ns children-start and $ne children-end marker lines (need exactly one of each); list left as it is"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi
    if [ "$sl" -ge "$el" ]; then
      log "  warning: #$number $key has children-end (line $el) before children-start (line $sl); list left as it is"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    # Test hook, inert unless CAIRN_TEST_CUT_SHIFT is set (only the tests set
    # it): moves both cut points by that many bytes so the guards can be shown
    # to refuse a wrong cut.
    pend=$((pend + ${CAIRN_TEST_CUT_SHIFT:-0}))
    estart=$((estart + ${CAIRN_TEST_CUT_SHIFT:-0}))

    # Range check before any head/tail uses the offsets.
    size="$(wc -c < "$cur" | tr -d ' ')"
    if [ "$pend" -lt 0 ] || [ "$estart" -lt "$pend" ] || [ "$estart" -gt "$size" ]; then
      log "  warning: #$number $key: children marker offsets out of range (start $pend, end $estart, body $size bytes); nothing written"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    pre="$WORK/body-pre-$key.bin"
    blk="$WORK/body-blk-$key.bin"
    suf="$WORK/body-suf-$key.bin"
    head -c "$pend" "$cur" > "$pre"
    head -c "$estart" "$cur" | tail -c +"$((pend + 1))" > "$blk"
    tail -c +"$((estart + 1))" "$cur" > "$suf"

    # Child map for the block: key, tab, issue number (empty when none yet).
    map="$WORK/childmap-$key.tsv"
    : > "$map"
    if [ -n "$children" ]; then
      while IFS= read -r child; do
        num="$(index_number "$child")"
        if [ -z "$num" ]; then
          # A child whose marker is missing still has its issue. Link to it
          # rather than dropping the link. Nothing else about it is touched.
          ctitle="$(jq -r --arg k "$child" '.issues[] | select(.key == $k) | .title' "$ROADMAP_FILE")"
          num="$(title_matches "$ctitle" | jq -r '.[0].number // empty')"
        fi
        printf '%s\t%s\n' "$child" "$num" >> "$map"
      done < <(printf '%s\n' "$children" | tr ',' '\n')
    fi

    # The new block, LF-terminated lines. Existing lines pass through by their
    # leading token; children with no line are appended.
    gen="$WORK/children-$key.txt"
    tr -d '\r' < "$blk" | LC_ALL=C awk -v mapf="$map" '
      BEGIN {
        while ((getline l < mapf) > 0) {
          split(l, p, "\t")
          n++; ckey[n] = p[1]; cnum[n] = p[2]
          bykey[p[1]] = n
          if (p[2] != "") bynum["#" p[2]] = n
        }
        close(mapf)
      }
      {
        line = $0
        if (line == "_No child issues yet._" && n > 0) next
        if (match(line, /^- \[[ xX]\] +/)) {
          head = substr(line, 1, RLENGTH)
          rest = substr(line, RLENGTH + 1)
          tok = ""
          if (match(rest, /^#[0-9]+/)) {
            tl = RLENGTH
            tok = substr(rest, 1, tl)
            if (substr(rest, tl + 1, 1) ~ /[A-Za-z0-9]/) tok = ""
          } else if (match(rest, /^[A-Za-z0-9_-]+/)) {
            tl = RLENGTH
            tok = substr(rest, 1, tl)
          }
          if (tok != "") {
            after = substr(rest, tl + 1)
            if (tok in bynum) {
              seen[bynum[tok]] = 1
            } else if (tok in bykey) {
              i = bykey[tok]
              seen[i] = 1
              if (cnum[i] != "") line = head "#" cnum[i] after
            }
          }
        }
        print line
      }
      END {
        for (i = 1; i <= n; i++) {
          if (seen[i]) continue
          if (cnum[i] != "") print "- [ ] #" cnum[i]
          else print "- [ ] " ckey[i]
        }
      }
    ' > "$gen"

    # Unchanged block: nothing to write. The issue is already counted as
    # unchanged by sync_issues, so it is not counted again here.
    if tr -d '\r' < "$blk" | cmp -s - "$gen"; then
      continue
    fi

    # Line ending of the new block: the block's own lines, by majority. An
    # empty block takes the body's majority. A tie is LF.
    crlf="$(grep -c "$(printf '\r')\$" "$blk" || true)"
    total="$(tr -cd '\n' < "$blk" | wc -c | tr -d ' ')"
    if [ "$total" -eq 0 ]; then
      crlf="$(grep -c "$(printf '\r')\$" "$cur" || true)"
      total="$(tr -cd '\n' < "$cur" | wc -c | tr -d ' ')"
    fi
    if [ "$((crlf * 2))" -gt "$total" ]; then
      eol=CRLF
      awk '{ printf "%s\r\n", $0 }' "$gen" > "$WORK/children-eol-$key.txt"
    else
      eol=LF
      cp "$gen" "$WORK/children-eol-$key.txt"
    fi

    new="$WORK/body-new-$key.md"
    cat "$pre" "$WORK/children-eol-$key.txt" "$suf" > "$new"

    # Cut check: independent of the offsets' arithmetic, read the lines the cut
    # landed on. Start marker must end the prefix, end marker must begin the
    # suffix, and the old block must hold neither marker.
    pl="$(tail -n 1 "$pre" | first_line_trimmed)"
    sl2="$(head -n 1 "$suf" | first_line_trimmed)"
    bm="$(LC_ALL=C awk '
      { l = $0; sub(/\r$/, "", l); sub(/^[ \t]+/, "", l); sub(/[ \t]+$/, "", l)
        if (l == "<!-- children-start -->" || l == "<!-- children-end -->") c++ }
      END { print c + 0 }' "$blk")"
    if [ "$pl" != "<!-- children-start -->" ] || [ "$sl2" != "<!-- children-end -->" ] || [ "$bm" -ne 0 ]; then
      log "  warning: #$number $key: the cut around the children block is not on the marker lines; nothing written"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    # Copy check: the bytes before the start marker line and from the end marker
    # line on must be the original bytes. Anything else is a bug here, and
    # nothing is sent.
    plen="$pend"
    slen="$((size - estart))"
    tmp="$WORK/guard.bin"
    if ! { head -c "$plen" "$new" > "$tmp" && head -c "$plen" "$cur" | cmp -s - "$tmp" \
           && tail -c "$slen" "$new" > "$tmp" && tail -c "$slen" "$cur" | cmp -s - "$tmp"; }; then
      log "  warning: #$number $key: the text outside the children markers would change; nothing written"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    if [ "$APPLY" = 1 ]; then
      jq -Rs '{body: .}' "$new" > "$WORK/req.json"
      gh api -X PATCH "repos/$REPO/issues/$number" --input "$WORK/req.json" > /dev/null
      log "  rewrote children list of #$number $key ($eol line endings in the block; the rest is byte-for-byte unchanged)"
    else
      log "  would rewrite children list of #$number $key ($eol line endings in the block; the rest is byte-for-byte unchanged)"
    fi
    UPDATED=$((UPDATED + 1))
  done < "$WORK/issues-desired.jsonl"
}

# GraphQL documents. Variables are passed as JSON, so option names such as
# "0" stay strings.
Q_OWNER='query($login: String!) { repositoryOwner(login: $login) { id ... on ProjectV2Owner { projectsV2(first: 100) { nodes { id title number closed } } } } }'
Q_REPO='query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { id projectsV2(first: 100) { nodes { id } } } }'
Q_FIELDS='query($id: ID!) { node(id: $id) { ... on ProjectV2 { fields(first: 100) { nodes { ... on ProjectV2Field { id name } ... on ProjectV2SingleSelectField { id name options { id name color description } } } } } } }'
Q_ITEMS='query($id: ID!, $after: String) { node(id: $id) { ... on ProjectV2 { items(first: 100, after: $after) { pageInfo { hasNextPage endCursor } nodes { id content { ... on Issue { number repository { nameWithOwner } } } phase: fieldValueByName(name: "Phase") { ... on ProjectV2ItemFieldSingleSelectValue { name } } area: fieldValueByName(name: "Area") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } } } } }'
M_CREATE_PROJECT='mutation($o: ID!, $t: String!) { createProjectV2(input: {ownerId: $o, title: $t}) { projectV2 { id number } } }'
M_LINK='mutation($p: ID!, $r: ID!) { linkProjectV2ToRepository(input: {projectId: $p, repositoryId: $r}) { repository { id } } }'
M_CREATE_FIELD='mutation($p: ID!, $n: String!, $o: [ProjectV2SingleSelectFieldOptionInput!]!) { createProjectV2Field(input: {projectId: $p, dataType: SINGLE_SELECT, name: $n, singleSelectOptions: $o}) { projectV2Field { ... on ProjectV2SingleSelectField { id } } } }'
M_ADD_ITEM='mutation($p: ID!, $c: ID!) { addProjectV2ItemById(input: {projectId: $p, contentId: $c}) { item { id } } }'
M_SET_VALUE='mutation($p: ID!, $i: ID!, $f: ID!, $o: String!) { updateProjectV2ItemFieldValue(input: {projectId: $p, itemId: $i, fieldId: $f, value: {singleSelectOptionId: $o}}) { projectV2Item { id } } }'

# Colours cycled through when a new single-select option is added.
OPTION_COLOURS='["GRAY","BLUE","GREEN","YELLOW","ORANGE","RED","PINK","PURPLE"]'

# field_manual_steps NAME OPTIONS: what the operator does by hand when a field
# lacks options. Printed, never fatal.
field_manual_steps() {
  log "    add by hand: open the \"$PROJECT_TITLE\" project, open the menu on the"
  log "    \"$1\" field header, choose Edit field, and add: $2"
}

# ensure_field NAME NAMES_JSON: make sure single-select field NAME exists.
# A field this script creates gets every option in NAMES_JSON. An existing
# field is never changed. Replacing its option list can reset the values
# already set on its items, so missing options are reported with manual steps
# instead. Nothing here is fatal: a failure is reported and the run continues.
ensure_field() {
  local fname="$1" names="$2" fid existing missing opts
  fid="$(jq -r --arg f "$fname" '[.[] | select(.name == $f and has("options"))][0].id // empty' "$WORK/fields.json")"

  if [ -z "$fid" ]; then
    opts="$(jq -cn --argjson n "$names" --argjson c "$OPTION_COLOURS" \
      '[$n | to_entries[] | {name: .value, color: $c[.key % 8], description: ""}]')"
    if [ "$APPLY" = 1 ]; then
      if ( gql "$M_CREATE_FIELD" "$(jq -cn --arg p "$PID" --arg n "$fname" --argjson o "$opts" '{p: $p, n: $n, o: $o}')" > /dev/null ); then
        log "  created field $fname with $(printf '%s' "$opts" | jq 'length') options"
        CREATED=$((CREATED + 1))
      else
        log "  warning: could not create field $fname"
        field_manual_steps "$fname" "$(printf '%s' "$opts" | jq -r '[.[].name] | join(", ")')"
      fi
    else
      log "  would create field $fname with options $(printf '%s' "$opts" | jq -r '[.[].name] | join(", ")')"
      CREATED=$((CREATED + 1))
    fi
    return 0
  fi

  existing="$(jq -c --arg f "$fname" '[.[] | select(.name == $f and has("options")) | .options[] | {name, color, description: (.description // "")}]' "$WORK/fields.json")"
  missing="$(jq -cn --argjson n "$names" --argjson e "$existing" \
    '($e | map(.name | ascii_downcase)) as $have
     | [$n[] | select((ascii_downcase) as $x | ($have | any(. == $x)) | not)]')"

  if [ "$missing" = "[]" ]; then
    log "  field $fname: all options present"
    SAME=$((SAME + 1))
    return 0
  fi

  log "  field $fname: missing options $(printf '%s' "$missing" | jq -r 'join(", ")'). Not changed by this script."
  field_manual_steps "$fname" "$(printf '%s' "$missing" | jq -r 'join(", ")')"
}

sync_project() {
  log "== project"
  local scopes
  scopes="$(gh auth status 2>&1 || true)"
  case "$scopes" in
    *"'project'"*) ;;
    *) die "cannot confirm the project scope. gh shows a scope list only for gh logins; with GH_TOKEN or a fine-grained token no list is shown, so check by hand that the token can manage projects. For a gh login run: gh auth refresh -s project,read:project. To skip this section: --only labels,milestones,issues" ;;
  esac

  load_existing

  gql "$Q_OWNER" "$(jq -cn --arg l "$OWNER" '{login: $l}')" > "$WORK/owner.json"
  OWNER_ID="$(jq -r '.data.repositoryOwner.id // empty' "$WORK/owner.json")"
  [ -n "$OWNER_ID" ] || die "no GitHub user or organisation named $OWNER"
  PID="$(jq -r --arg t "$PROJECT_TITLE" \
    '[.data.repositoryOwner.projectsV2.nodes[] | select(.title == $t and (.closed | not))][0].id // empty' \
    "$WORK/owner.json")"

  gql "$Q_REPO" "$(jq -cn --arg o "$OWNER" --arg n "$NAME" '{owner: $o, name: $n}')" > "$WORK/repo.json"
  REPO_ID="$(jq -r '.data.repository.id' "$WORK/repo.json")"

  if [ -z "$PID" ]; then
    if [ "$APPLY" = 1 ]; then
      gql "$M_CREATE_PROJECT" "$(jq -cn --arg o "$OWNER_ID" --arg t "$PROJECT_TITLE" '{o: $o, t: $t}')" > "$WORK/create.json"
      PID="$(jq -r '.data.createProjectV2.projectV2.id' "$WORK/create.json")"
      log "  created project \"$PROJECT_TITLE\" for $OWNER"
    else
      log "  would create project \"$PROJECT_TITLE\" for $OWNER"
    fi
  else
    log "  found project \"$PROJECT_TITLE\" for $OWNER"
  fi

  if [ -n "$PID" ]; then
    linked="$(jq -e --arg p "$PID" '[.data.repository.projectsV2.nodes[].id] | any(. == $p)' "$WORK/repo.json" >/dev/null 2>&1 && echo yes || echo no)"
    if [ "$linked" = "no" ]; then
      if [ "$APPLY" = 1 ]; then
        gql "$M_LINK" "$(jq -cn --arg p "$PID" --arg r "$REPO_ID" '{p: $p, r: $r}')" > /dev/null
        log "  linked the project to $REPO"
      else
        log "  would link the project to $REPO"
      fi
    else
      log "  project already linked to $REPO"
    fi
  fi

  if [ -z "$PID" ]; then
    log "  fields, items and views follow once the project exists (run again with --apply)"
    print_views
    return 0
  fi

  gql "$Q_FIELDS" "$(jq -cn --arg id "$PID" '{id: $id}')" > "$WORK/fields-raw.json"
  jq '.data.node.fields.nodes' "$WORK/fields-raw.json" > "$WORK/fields.json"

  # Status is built in, so this only ever reports missing options. Matching is
  # case-insensitive, so "In Progress" matches "In progress" without a rename.
  ensure_field "Status" '["Todo","In progress","In review","Done"]'
  # Phase options come from the phase: labels. Milestone is GitHub's built-in
  # field, so it is not created here.
  ensure_field "Phase" "$(jq -c '[.[] | select(.name | startswith("phase:")) | .name | ltrimstr("phase:")]' "$LABELS_FILE")"
  ensure_field "Area" "$(jq -c '[.[] | select(.name | startswith("area:")) | .name]' "$LABELS_FILE")"

  if [ "$APPLY" = 1 ]; then
    gql "$Q_FIELDS" "$(jq -cn --arg id "$PID" '{id: $id}')" > "$WORK/fields-raw.json"
    jq '.data.node.fields.nodes' "$WORK/fields-raw.json" > "$WORK/fields.json"
  fi

  sync_items
  print_views
}

# sync_items: read every board item once, then add roadmap issues that are not
# on the board yet and fill their Phase and Area. A value that is already set,
# by this script or by hand, is never overwritten.
sync_items() {
  local after resp more item_id cur_phase cur_area want_phase want_area item
  local key number node line field fid oid

  : > "$WORK/items.jsonl"
  after=""
  while :; do
    jq -cn --arg id "$PID" --arg a "$after" '{id: $id, after: (if $a == "" then null else $a end)}' > "$WORK/vars.json"
    resp="$(gql "$Q_ITEMS" "$(cat "$WORK/vars.json")")"
    printf '%s' "$resp" | jq -c '.data.node.items.nodes[]' >> "$WORK/items.jsonl"
    more="$(printf '%s' "$resp" | jq -r '.data.node.items.pageInfo.hasNextPage')"
    [ "$more" = "true" ] || break
    after="$(printf '%s' "$resp" | jq -r '.data.node.items.pageInfo.endCursor')"
  done

  while IFS= read -r issue; do
    key="$(printf '%s' "$issue" | jq -r '.key')"
    want_phase="$(printf '%s' "$issue" | jq -r '(.phase // "") | tostring')"
    want_area="$(printf '%s' "$issue" | jq -r '.area // ""')"

    line="$(awk -F '\t' -v k="$key" '$1 == k { print; exit }' "$WORK/index.tsv")"
    if [ -z "$line" ]; then
      log "  $key: no roadmap issue matched, so nothing is set here (created by --apply, or skipped above for a missing marker)"
      continue
    fi
    number="$(printf '%s' "$line" | cut -f2)"
    node="$(printf '%s' "$line" | cut -f3)"

    item="$(jq -cs --argjson n "$number" --arg r "$REPO" \
      '[.[] | select(.content.number == $n and .content.repository.nameWithOwner == $r)][0] // empty' \
      "$WORK/items.jsonl")"

    if [ -z "$item" ]; then
      if [ "$APPLY" = 1 ]; then
        resp="$(gql "$M_ADD_ITEM" "$(jq -cn --arg p "$PID" --arg c "$node" '{p: $p, c: $c}')")"
        item_id="$(printf '%s' "$resp" | jq -r '.data.addProjectV2ItemById.item.id')"
        cur_phase=""
        cur_area=""
        log "  added #$number $key to the board"
      else
        log "  would add #$number $key to the board"
        continue
      fi
    else
      item_id="$(printf '%s' "$item" | jq -r '.id')"
      cur_phase="$(printf '%s' "$item" | jq -r '.phase.name // ""')"
      cur_area="$(printf '%s' "$item" | jq -r '.area.name // ""')"
    fi

    for field in Phase Area; do
      case "$field" in
        Phase) want="$want_phase"; cur="$cur_phase" ;;
        *) want="$want_area"; cur="$cur_area" ;;
      esac
      # Nothing to set when the roadmap gives no value. A Plan v1 issue has
      # phase: null, so its Phase stays empty.
      [ -n "$want" ] || continue
      [ "$cur" = "$want" ] && continue

      # Only an empty value is filled. A value someone set by hand stays.
      if [ -n "$cur" ]; then
        log "  kept $field=$cur on #$number $key (set by hand; roadmap says $want)"
        continue
      fi

      fid="$(jq -r --arg f "$field" '[.[] | select(.name == $f and has("options"))][0].id // empty' "$WORK/fields.json")"
      oid="$(jq -r --arg f "$field" --arg o "$want" \
        '[.[] | select(.name == $f and has("options")) | .options[] | select(.name == $o)][0].id // empty' \
        "$WORK/fields.json")"

      if [ -z "$fid" ] || [ -z "$oid" ]; then
        if [ "$APPLY" = 1 ]; then
          log "  warning: cannot set $field=$want on #$number $key; the field or option is missing (see above)"
        else
          log "  would set $field=$want on #$number $key once the field and option exist"
        fi
        continue
      fi

      if [ "$APPLY" = 1 ]; then
        gql "$M_SET_VALUE" "$(jq -cn --arg p "$PID" --arg i "$item_id" --arg f "$fid" --arg o "$oid" \
          '{p: $p, i: $i, f: $f, o: $o}')" > /dev/null
        log "  set $field=$want on #$number $key"
      else
        log "  would set $field=$want on #$number $key"
      fi
    done
  done < "$WORK/issues-desired.jsonl"
}

print_views() {
  cat <<EOF

Views cannot be created through the API. Do these once, by hand, in the
"$PROJECT_TITLE" project:
  1. New view > Board. Group by Status.
  2. New view > Table. Group by Milestone. Milestone is GitHub's built-in
     field; do not add a custom Milestone field.
  3. New view > Roadmap. Show the milestones. The roadmap layout is driven by
     date fields; add Start and Target date fields if you want bars.
EOF
}

if want labels; then sync_labels; fi
if want milestones; then sync_milestones; fi
if want issues; then sync_issues; fi
if want project; then sync_project; fi

echo
if [ "$APPLY" = 1 ]; then
  log "done: created $CREATED, updated $UPDATED, unchanged $SAME, skipped $SKIPPED (needs marker)"
else
  log "plan: create $CREATED, update $UPDATED, unchanged $SAME, skipped $SKIPPED (needs marker). Nothing was written."
fi
