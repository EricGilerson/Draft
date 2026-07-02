#!/usr/bin/env bash
#
# kill-daemon.sh — stop a running Draft daemon using its statefile.
#
# The Draft daemon records its address, auth token, and PID in a JSON
# statefile (see internal/daemon/paths.go, internal/daemon/server.go):
#
#   { "addr": "127.0.0.1:xxxxx", "token": "...", "pid": 12345 }
#
# This script reads that statefile, sends SIGTERM to the recorded PID,
# waits briefly, escalates to SIGKILL if needed, then verifies the
# process is actually gone. Exits non-zero if the kill could not be
# confirmed.
#
# Usage:
#   ./scripts/kill-daemon.sh            # stop the daemon for the current user
#   ./scripts/kill-daemon.sh -f         # skip the live-ping check, force kill
#   ./scripts/kill-daemon.sh --keep     # do not remove the stale statefile
#   ./scripts/kill-daemon.sh -h         # show this help

set -u

usage() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
  exit 0
}

force=0
keep_state=0
for arg in "$@"; do
  case "$arg" in
    -h|--help) usage ;;
    -f|--force) force=1 ;;
    --keep) keep_state=1 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

# --- locate the daemon statefile ---------------------------------------------

config_dir_for_os() {
  case "$(uname -s)" in
    Darwin)
      echo "${HOME}/Library/Application Support"
      ;;
    Linux)
      if [ -n "${XDG_CONFIG_HOME:-}" ]; then
        echo "${XDG_CONFIG_HOME}"
      else
        echo "${HOME}/.config"
      fi
      ;;
    *)
      # Best-effort fallback for other unix-likes.
      if [ -n "${XDG_CONFIG_HOME:-}" ]; then
        echo "${XDG_CONFIG_HOME}"
      else
        echo "${HOME}/.config"
      fi
      ;;
  esac
}

STATE_FILE="$(config_dir_for_os)/Draft/daemon.json"

if [ ! -f "$STATE_FILE" ]; then
  echo "no daemon statefile at: $STATE_FILE"
  echo "nothing to kill."
  exit 0
fi

# --- parse the PID out of the JSON statefile ---------------------------------
# Prefer jq, fall back to python3, then to a greedy grep. The statefile is
# small and machine-written so the grep fallback is safe in practice.

extract_pid() {
  local file="$1"
  if command -v jq >/dev/null 2>&1; then
    jq -r '.pid // empty' "$file" 2>/dev/null
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(d.get("pid",""))' "$file" 2>/dev/null
  else
    grep -oE '"pid"[[:space:]]*:[[:space:]]*[0-9]+' "$file" \
      | grep -oE '[0-9]+$'
  fi
}

PID="$(extract_pid "$STATE_FILE" | tr -d '[:space:]')"

if [ -z "$PID" ]; then
  echo "statefile exists but contains no pid:"
  echo "  $STATE_FILE"
  if [ "$keep_state" -eq 0 ]; then
    rm -f "$STATE_FILE" && echo "removed stale statefile."
  fi
  exit 0
fi

if ! [[ "$PID" =~ ^[0-9]+$ ]]; then
  echo "statefile pid is not a positive integer: '$PID'" >&2
  exit 1
fi

echo "draft daemon statefile: $STATE_FILE"
echo "recorded pid:           $PID"

# --- is the process even alive? ----------------------------------------------

process_alive() {
  kill -0 "$1" >/dev/null 2>&1
}

if ! process_alive "$PID"; then
  echo "pid $PID is not running; daemon already stopped."
  if [ "$keep_state" -eq 0 ]; then
    rm -f "$STATE_FILE" && echo "removed stale statefile."
  fi
  exit 0
fi

# --- optional sanity check: the pid should be a Draft daemon -----------------
# Avoid clobbering an unrelated process that happens to reuse the pid. We
# confirm by hitting /health, which only a Draft daemon would answer. With
# -f we skip this check (e.g. the daemon is hung and not answering).

if [ "$force" -eq 0 ]; then
  ADDR="$(jq -r '.addr // empty' "$STATE_FILE" 2>/dev/null || true)"
  if [ -n "$ADDR" ]; then
    if curl -fsS --max-time 2 "http://${ADDR}/health" >/dev/null 2>&1; then
      echo "health check ok at $ADDR — confirmed Draft daemon."
    else
      echo "warning: pid $PID is alive but did not answer /health at ${ADDR:-<no addr>}."
      echo "         rerun with -f to force-kill without the health check."
      exit 1
    fi
  fi
fi

# --- terminate: SIGTERM, then SIGKILL, then verify ---------------------------

echo "sending SIGTERM to pid $PID ..."
kill -TERM "$PID" 2>/dev/null || true

deadline=$(( $(date +%s) + 8 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
  if ! process_alive "$PID"; then
    break
  fi
  sleep 0.25
done

if process_alive "$PID"; then
  echo "SIGTERM did not stop pid $PID within 8s; escalating to SIGKILL."
  kill -KILL "$PID" 2>/dev/null || true
  deadline=$(( $(date +%s) + 4 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if ! process_alive "$PID"; then
      break
    fi
    sleep 0.25
  done
fi

# --- final verification ------------------------------------------------------

if process_alive "$PID"; then
  echo "FAILED: pid $PID is still running after SIGTERM + SIGKILL." >&2
  exit 1
fi

echo "confirmed: pid $PID is no longer running."

if [ "$keep_state" -eq 0 ]; then
  rm -f "$STATE_FILE" && echo "removed stale statefile: $STATE_FILE"
else
  echo "kept statefile (--keep): $STATE_FILE"
fi

echo "done."
