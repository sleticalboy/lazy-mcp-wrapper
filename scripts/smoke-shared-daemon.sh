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
SESSION_CONFIG="${TMP_DIR}/fake-session.json"
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

cat >"${SESSION_CONFIG}" <<JSON
{
  "name": "fake-session",
  "sharing": "session",
  "command": "${FAKE_MCP}",
  "disable_cache": true,
  "idle_timeout": "5s",
  "startup_timeout": "5s",
  "call_timeout": "5s",
  "log_file": "${TMP_DIR}/fake-session.log"
}
JSON

cat >"${DAEMON_CONFIG}" <<JSON
{
  "socket": "${SOCKET}",
  "configs": ["${MCP_CONFIG}", "${SESSION_CONFIG}"]
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

cat >"${TMP_DIR}/client.in" <<'JSONL'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"shared-daemon-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
JSONL
"${WRAPPER}" client --socket "${SOCKET}" --name fake <"${TMP_DIR}/client.in" >"${CLIENT_OUT}"

grep -Eq '"name"[[:space:]]*:[[:space:]]*"echo"' "${CLIENT_OUT}"

"${WRAPPER}" status --socket "${SOCKET}" >"${STATUS_JSON}"
grep -q '"total_calls": 1' "${STATUS_JSON}"
grep -q '"calls": 1' "${STATUS_JSON}"
grep -q '"last_method": "tools/list"' "${STATUS_JSON}"
grep -q '"last_latency_ms":' "${STATUS_JSON}"

"${WRAPPER}" status --socket "${SOCKET}" --format table >"${TMP_DIR}/status-table.out"
grep -q 'fake' "${TMP_DIR}/status-table.out"
"${WRAPPER}" reload --socket "${SOCKET}" >"${TMP_DIR}/reload.out"
grep -q '"ok": true' "${TMP_DIR}/reload.out"

for i in 1 2; do
  cat >"${TMP_DIR}/session-client-${i}.in" <<'JSONL'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"session-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
JSONL
  "${WRAPPER}" client --socket "${SOCKET}" --name fake-session <"${TMP_DIR}/session-client-${i}.in" >"${TMP_DIR}/session-client-${i}.out"
done
"${WRAPPER}" status --socket "${SOCKET}" >"${STATUS_JSON}"
grep -q '"sharing": "session"' "${STATUS_JSON}"
grep -q '"calls": 2' "${STATUS_JSON}"

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
  echo "reload without --graceful/--force unexpectedly succeeded with an active client"
  exit 1
fi
grep -q 'reload busy' "${TMP_DIR}/reload-busy.out"
"${WRAPPER}" reload --socket "${SOCKET}" --graceful >"${TMP_DIR}/reload-graceful.out"
grep -q '"ok": true' "${TMP_DIR}/reload-graceful.out"
exec 3>&-
wait "${HELD_CLIENT_PID}" 2>/dev/null || true
"${WRAPPER}" reload --socket "${SOCKET}" --force >"${TMP_DIR}/reload-force.out"
grep -q '"ok": true' "${TMP_DIR}/reload-force.out"

"${WRAPPER}" stop --socket "${SOCKET}" >"${TMP_DIR}/stop.out"
grep -q '"ok": true' "${TMP_DIR}/stop.out"
wait "${DAEMON_PID}"
DAEMON_PID=""

echo "shared daemon smoke ok"
