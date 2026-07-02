#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WRAPPER="${ROOT_DIR}/bin/lazy-mcp-wrapper"
FAKE_MCP="${ROOT_DIR}/bin/fake-mcp"
TMP_DIR="$(mktemp -d)"

cleanup() {
  if [[ -n "${HELD_CLIENT_PID:-}" ]] && kill -0 "${HELD_CLIENT_PID}" 2>/dev/null; then
    kill "${HELD_CLIENT_PID}" 2>/dev/null || true
    wait "${HELD_CLIENT_PID}" 2>/dev/null || true
  fi
  if [[ -n "${DAEMON_PID:-}" ]] && kill -0 "${DAEMON_PID}" 2>/dev/null; then
    kill "${DAEMON_PID}" 2>/dev/null || true
    wait "${DAEMON_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

go build -o "${WRAPPER}" "${ROOT_DIR}/cmd/lazy-mcp-wrapper"
go build -o "${FAKE_MCP}" "${ROOT_DIR}/cmd/fake-mcp"

SOCKET="${TMP_DIR}/lazy-mcpd.sock"
MCP_CONFIG="${TMP_DIR}/fake.json"
DAEMON_CONFIG="${TMP_DIR}/daemon.json"
DAEMON_LOG="${TMP_DIR}/daemon.log"
CLIENT_OUT="${TMP_DIR}/client.out"
STATUS_JSON="${TMP_DIR}/status.json"

cat >"${MCP_CONFIG}" <<JSON
{
  "name": "fake",
  "command": "${FAKE_MCP}",
  "idle_timeout": "5s",
  "startup_timeout": "5s",
  "call_timeout": "5s",
  "log_file": "${TMP_DIR}/fake.log"
}
JSON

cat >"${DAEMON_CONFIG}" <<JSON
{
  "socket": "${SOCKET}",
  "configs": ["${MCP_CONFIG}"]
}
JSON

"${WRAPPER}" daemon --daemon-config "${DAEMON_CONFIG}" >"${DAEMON_LOG}" 2>&1 &
DAEMON_PID=$!

for _ in {1..100}; do
  [[ -S "${SOCKET}" ]] && break
  sleep 0.05
done
if [[ ! -S "${SOCKET}" ]]; then
  echo "daemon socket was not created"
  cat "${DAEMON_LOG}" || true
  exit 1
fi

printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"shared-daemon-smoke","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | "${WRAPPER}" client --socket "${SOCKET}" --name fake >"${CLIENT_OUT}"

grep -Eq '"name"[[:space:]]*:[[:space:]]*"echo"' "${CLIENT_OUT}"

"${WRAPPER}" status --socket "${SOCKET}" >"${STATUS_JSON}"
grep -q '"total_calls": 1' "${STATUS_JSON}"
grep -q '"calls": 1' "${STATUS_JSON}"
grep -q '"last_method": "tools/list"' "${STATUS_JSON}"
grep -q '"last_latency_ms":' "${STATUS_JSON}"

"${WRAPPER}" status --socket "${SOCKET}" --format table | grep -q 'fake'
"${WRAPPER}" reload --socket "${SOCKET}" | grep -q '"ok": true'

mkfifo "${TMP_DIR}/client.stdin"
"${WRAPPER}" client --socket "${SOCKET}" --name fake <"${TMP_DIR}/client.stdin" >"${TMP_DIR}/held-client.out" &
HELD_CLIENT_PID=$!
exec 3>"${TMP_DIR}/client.stdin"
printf '%s\n' '{"jsonrpc":"2.0","id":10,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"held-client","version":"0"}}}' >&3

for _ in {1..100}; do
  "${WRAPPER}" status --socket "${SOCKET}" >"${STATUS_JSON}"
  grep -q '"active_clients":' "${STATUS_JSON}" && break
  sleep 0.05
done
grep -q '"active_clients":' "${STATUS_JSON}"

if "${WRAPPER}" reload --socket "${SOCKET}" >"${TMP_DIR}/reload-busy.out" 2>/dev/null; then
  echo "reload without --force unexpectedly succeeded with an active client"
  exit 1
fi
grep -q 'reload busy' "${TMP_DIR}/reload-busy.out"
"${WRAPPER}" reload --socket "${SOCKET}" --force | grep -q '"ok": true'
exec 3>&-
wait "${HELD_CLIENT_PID}" 2>/dev/null || true

"${WRAPPER}" stop --socket "${SOCKET}" | grep -q '"ok": true'
wait "${DAEMON_PID}"
DAEMON_PID=""

echo "shared daemon smoke ok"
