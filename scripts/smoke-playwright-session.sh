#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WRAPPER="${ROOT_DIR}/bin/lazy-mcp-wrapper"
TMP_DIR="$(mktemp -d)"
DAEMON_PID=""

dump_debug() {
  echo "== daemon stderr ==" >&2
  cat "${TMP_DIR}/daemon.err" >&2 2>/dev/null || true
  echo "== playwright wrapper log ==" >&2
  cat "${TMP_DIR}/playwright.log" >&2 2>/dev/null || true
  echo "== playwright smoke output ==" >&2
  cat "${TMP_DIR}/playwright-smoke.out" >&2 2>/dev/null || true
  echo "== status ==" >&2
  cat "${STATUS_JSON}" >&2 2>/dev/null || true
}

cleanup() {
  if [[ -n "${DAEMON_PID}" ]]; then
    "${WRAPPER}" stop --socket "${SOCKET}" >/dev/null 2>&1 || true
    wait "${DAEMON_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMP_DIR}"
}
trap 'status=$?; if [[ ${status} -ne 0 ]]; then dump_debug; fi' ERR
trap cleanup EXIT

SOCKET="${TMP_DIR}/lazy-mcpd.sock"
PLAYWRIGHT_CONFIG="${TMP_DIR}/playwright.json"
DAEMON_CONFIG="${TMP_DIR}/daemon.json"
STATUS_JSON="${TMP_DIR}/status.json"

cat >"${PLAYWRIGHT_CONFIG}" <<JSON
{
  "name": "playwright",
  "sharing": "session",
  "command": "npx",
  "args": ["@playwright/mcp@latest"],
  "real_protocol_version": "2024-11-05",
  "real_framing": "jsonl",
  "disable_cache": true,
  "idle_timeout": "5m",
  "startup_timeout": "45s",
  "call_timeout": "180s",
  "log_file": "${TMP_DIR}/playwright.log"
}
JSON

cat >"${DAEMON_CONFIG}" <<JSON
{
  "socket": "${SOCKET}",
  "configs": ["${PLAYWRIGHT_CONFIG}"]
}
JSON

"${WRAPPER}" daemon --daemon-config "${DAEMON_CONFIG}" >"${TMP_DIR}/daemon.out" 2>"${TMP_DIR}/daemon.err" &
DAEMON_PID=$!

for _ in {1..100}; do
  if [[ -S "${SOCKET}" ]]; then
    break
  fi
  sleep 0.1
done
if [[ ! -S "${SOCKET}" ]]; then
  echo "daemon socket was not created" >&2
  cat "${TMP_DIR}/daemon.err" >&2 || true
  exit 1
fi

go run "${ROOT_DIR}/cmd/mcp-smoke" \
  --socket "${SOCKET}" \
  --name playwright \
  --call-tool browser_snapshot \
  --call-args '{}' \
  "${WRAPPER}" >"${TMP_DIR}/playwright-smoke.out"

"${WRAPPER}" status --socket "${SOCKET}" >"${STATUS_JSON}"
grep -q '"name": "playwright"' "${STATUS_JSON}"
grep -q '"sharing": "session"' "${STATUS_JSON}"
grep -q '"calls": 2' "${STATUS_JSON}"

"${WRAPPER}" stop --socket "${SOCKET}" >"${TMP_DIR}/stop.out"
wait "${DAEMON_PID}"
DAEMON_PID=""

echo "playwright session smoke ok"
