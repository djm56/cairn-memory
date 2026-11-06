#!/usr/bin/env bash
# smoke-check.sh — a read-only operator check of a Cairn install (CONTRIBUTING.md).
#
# Local checks: the cairn binary is found and prints its version, and cairn doctor runs
# when the binary lists it. Server checks, when --server is given: GET /healthz answers;
# an unauthenticated call to the agent API is refused; an authenticated call is accepted;
# and cairn mcp starts and answers initialize and tools/list.
#
# Read-only by design. It writes no file, creates no project, and changes no task, step,
# event, learning or directive. It sends the API key only in an Authorization header, read
# from CAIRN_API_KEY, never a flag, so the key stays out of the shell history and the process
# list. Two calls can still write one thing each, and the operator should know it:
#   - the authenticated call sets last_used_at on the key, which the server does for every
#     call that uses a key (at most once a minute per key);
#   - cairn doctor, when a key is set, makes the same call as its reach check.
# A run without CAIRN_API_KEY makes neither.
#
# Usage:
#   CAIRN_API_KEY=cairn_... scripts/smoke-check.sh [--bin PATH] [--server URL]
#
# Exits 0 when no check failed, 1 when one did, 2 for a usage error. Each check prints one
# PASS, FAIL or SKIP line, and a summary line closes the run. The script runs on the bash
# that ships with macOS (3.2) as well as on Linux.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/smoke-check.sh [--bin PATH] [--server URL]

  --bin PATH     The cairn binary to check (default: cairn, found on PATH).
  --server URL   The server's base URL, http:// or https://. Enables the server checks:
                   healthz   GET /healthz answers 200 with status ok.
                   refused   POST /v1/agent/task_current with no key is refused, 401.
                   key       POST /v1/agent/task_current with CAIRN_API_KEY is accepted.
                             The call names no project, so the server refuses it before it
                             reads anything, with kind invalid. That answer shows the key was
                             accepted. A key is needed; without one the check is skipped.
                   mcp       cairn mcp starts and answers initialize and tools/list. It needs
                             CAIRN_API_KEY, since cairn mcp will not start without a key.
  --help         Show this help.

Local checks, always run: binary (the file is found), version (it prints "cairn ..."), and
doctor (cairn doctor, when cairn help lists it; exit 0 is PASS, and a failing line is printed).

Environment:
  CAIRN_API_KEY  The API key. Read from the environment only.

The API key is never printed. The output is one PASS, FAIL or SKIP line per check, then a
summary line.

Exit: 0 when no check failed, 1 when one did, 2 for a usage error.
EOF
}

# usage_error prints a message to stderr and exits 2. The message names no value that the
# operator passed, so a mistyped URL or key is not echoed into a log.
usage_error() {
  echo "smoke-check: $1 (see --help)" >&2
  exit 2
}

BIN="cairn"
SERVER=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --bin)
      [ "$#" -ge 2 ] || usage_error "--bin needs a path"
      BIN="$2"
      shift 2
      ;;
    --server)
      [ "$#" -ge 2 ] || usage_error "--server needs a URL"
      SERVER="$2"
      shift 2
      ;;
    --key)
      usage_error "the key is not a flag: set CAIRN_API_KEY in the environment"
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage_error "unknown argument: $1"
      ;;
  esac
done

if [ -n "$SERVER" ]; then
  # A trailing slash would double in the paths, so it goes. The URL is not echoed: it may hold
  # credentials, and the messages below never name it.
  SERVER="${SERVER%/}"
  case "$SERVER" in
    http://*|https://*) ;;
    *) usage_error "--server needs an http:// or https:// URL" ;;
  esac
  # cairn reads the server from CAIRN_SERVER, so doctor and mcp check the same one.
  export CAIRN_SERVER="$SERVER"
fi

KEY="${CAIRN_API_KEY:-}"
PASSED=0
FAILED=0
SKIPPED=0

# report STATUS NAME DETAIL counts one check and prints its line.
report() {
  case "$1" in
    PASS) PASSED=$((PASSED + 1)) ;;
    FAIL) FAILED=$((FAILED + 1)) ;;
    SKIP) SKIPPED=$((SKIPPED + 1)) ;;
  esac
  printf '%-4s  %-8s %s\n' "$1" "$2" "$3"
}

# ---- local checks -------------------------------------------------------------------------

BIN_PATH="$(command -v "$BIN" 2>/dev/null || true)"
if [ -z "$BIN_PATH" ]; then
  report FAIL binary "no cairn binary at '$BIN': install it, or pass --bin PATH"
  report SKIP version "no binary to ask"
  report SKIP doctor "no binary to ask"
else
  report PASS binary "found at $BIN_PATH"

  version_out="$("$BIN_PATH" version 2>&1 || true)"
  case "$version_out" in
    cairn\ *) report PASS version "${version_out%%$'\n'*}" ;;
    *) report FAIL version "the binary did not print 'cairn <version>': ${version_out%%$'\n'*}" ;;
  esac

  # doctor runs only when the binary lists it. help is read-only and prints to stdout.
  help_out="$("$BIN_PATH" help 2>/dev/null || true)"
  if grep -q '^  doctor ' <<< "$help_out"; then
    doctor_rc=0
    doctor_out="$("$BIN_PATH" doctor 2>&1)" || doctor_rc=$?
    case "$doctor_rc" in
      0)
        report PASS doctor "exit 0: no check failed"
        ;;
      1)
        # The failing lines, joined on one line for the report.
        fails="$(awk '$2 == "fail"' <<< "$doctor_out" | tr '\n' ';')"
        report FAIL doctor "exit 1: ${fails:-no failing line was printed}"
        ;;
      *)
        report FAIL doctor "exit $doctor_rc"
        ;;
    esac
  else
    report SKIP doctor "this binary does not list a doctor verb"
  fi
fi

# ---- server checks ------------------------------------------------------------------------

if [ -z "$SERVER" ]; then
  report SKIP healthz "no --server given"
  report SKIP refused "no --server given"
  report SKIP key "no --server given"
  report SKIP mcp "no --server given"
else
  CURL_PATH="$(command -v curl 2>/dev/null || true)"
  if [ -z "$CURL_PATH" ]; then
    report FAIL healthz "curl is not installed; the server checks need it"
    report SKIP refused "curl is not installed"
    report SKIP key "curl is not installed"
  else
    # Each call below prints the body, then a line with the status code. The code is 000 when
    # no answer came, such as a server that is down.
    resp="$(curl -sS --max-time 5 -w '\n%{http_code}' "$SERVER/healthz" 2>/dev/null || true)"
    code="${resp##*$'\n'}"
    body="${resp%$'\n'*}"
    if [ "$code" = "200" ] && [[ "$body" == *'"status":"ok"'* ]]; then
      report PASS healthz "GET /healthz answered 200 with status ok"
    elif [ "$code" = "000" ]; then
      report FAIL healthz "no answer from the server: it is down, or the URL is wrong"
    else
      report FAIL healthz "GET /healthz answered $code, not 200 with status ok"
    fi

    # refused: a call to the agent API with no Authorization header. The server must answer 401
    # before it reads the body, and the call writes nothing.
    resp="$(curl -sS --max-time 5 -X POST -w '\n%{http_code}' "$SERVER/v1/agent/task_current" 2>/dev/null || true)"
    code="${resp##*$'\n'}"
    body="${resp%$'\n'*}"
    if [ "$code" = "401" ] && [[ "$body" == *'"kind":"unauthenticated"'* ]]; then
      report PASS refused "POST /v1/agent/task_current with no key answered 401 unauthenticated"
    elif [ "$code" = "000" ]; then
      report FAIL refused "no answer from the server"
    else
      report FAIL refused "POST /v1/agent/task_current with no key answered $code, not 401 unauthenticated"
    fi

    # key: the same call with the key, in a config read from stdin, so the key is never in the
    # process list. The key's shape is checked first, so a value that would break the config is
    # refused without being sent.
    if [ -z "$KEY" ]; then
      report SKIP key "CAIRN_API_KEY is not set, so no authenticated call was made"
    else
      key_shape='^cairn_[A-Za-z0-9_-]+$'
      if ! [[ "$KEY" =~ $key_shape ]]; then
        report FAIL key "CAIRN_API_KEY is not a Cairn key: a Cairn key starts with cairn_ and holds letters, digits, _ and -"
      else
        resp="$(printf '%s\n' "header = \"Authorization: Bearer $KEY\"" \
          | curl -sS --max-time 5 -K - -X POST -w '\n%{http_code}' "$SERVER/v1/agent/task_current" 2>/dev/null || true)"
        code="${resp##*$'\n'}"
        body="${resp%$'\n'*}"
        kind="$(sed -n 's/.*"kind":"\([a-z_]*\)".*/\1/p' <<< "$body")"
        if [ "$code" = "000" ]; then
          report FAIL key "no answer from the server"
        elif [ "$code" = "401" ] || [ "$code" = "403" ]; then
          report FAIL key "the server refused the key ($code ${kind:-no kind}): check CAIRN_API_KEY, and whether the key was revoked"
        elif [ "$kind" = "invalid" ]; then
          report PASS key "the key was accepted: the call named no project, so the server refused it before any read (kind invalid)"
        else
          report FAIL key "POST /v1/agent/task_current with the key answered $code (kind ${kind:-none}), not kind invalid"
        fi
      fi
    fi
  fi

  # mcp: cairn mcp is started with the server set, and sent initialize and tools/list. It makes
  # no server call for either, so this check also writes nothing. It needs a key to start.
  if [ -z "$BIN_PATH" ]; then
    report SKIP mcp "no binary to ask"
  elif [ -z "$KEY" ]; then
    report SKIP mcp "CAIRN_API_KEY is not set, and cairn mcp will not start without one"
  else
    init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke-check","version":"1"}}}'
    inited='{"jsonrpc":"2.0","method":"notifications/initialized"}'
    list='{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
    # The input is held open for a second after the requests. The MCP SDK ends the session when
    # stdin closes, and a reply still in flight is then lost, so a bare pipe gets no answer.
    mcp_out="$({ printf '%s\n' "$init" "$inited" "$list"; sleep 1; } | "$BIN_PATH" mcp 2>&1 || true)"
    if grep -q '"serverInfo"' <<< "$mcp_out" && grep -q '"name":"cairn"' <<< "$mcp_out" && grep -q '"tools"' <<< "$mcp_out"; then
      report PASS mcp "cairn mcp answered initialize and tools/list"
    else
      report FAIL mcp "cairn mcp did not answer initialize and tools/list: ${mcp_out%%$'\n'*}"
    fi
  fi
fi

echo "smoke-check: $PASSED passed, $FAILED failed, $SKIPPED skipped"
if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
exit 0
