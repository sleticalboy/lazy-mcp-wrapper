#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WRAPPER="${ROOT_DIR}/bin/lazy-mcp-wrapper"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

go build -o "${WRAPPER}" "${ROOT_DIR}/cmd/lazy-mcp-wrapper"

HOME_DIR="${TMP_DIR}/home"
CODEX_CONFIG="${TMP_DIR}/codex-config.toml"
EXTERNAL_CONFIG="${TMP_DIR}/external/context7.json"
DAEMON_CONFIG="${HOME_DIR}/.lazy-mcp-wrapper/config.json"
SOCKET="${HOME_DIR}/.lazy-mcp-wrapper/lazy-mcpd.sock"

mkdir -p "$(dirname "${CODEX_CONFIG}")" "$(dirname "${EXTERNAL_CONFIG}")" "$(dirname "${DAEMON_CONFIG}")"

cat >"${CODEX_CONFIG}" <<TOML
[mcp_servers.context7]
type = "stdio"
command = "${WRAPPER}"
args = ["client", "--socket", "${SOCKET}", "--name", "context7"]
TOML
cp "${CODEX_CONFIG}" "${TMP_DIR}/codex-config.before.toml"

cat >"${EXTERNAL_CONFIG}" <<JSON
{
  "schema_version": 1,
  "name": "context7",
  "sharing": "shared",
  "command": "npx",
  "args": ["-y", "@upstash/context7-mcp"]
}
JSON

cat >"${DAEMON_CONFIG}" <<JSON
{
  "socket": "${SOCKET}",
  "configs": ["${EXTERNAL_CONFIG}"]
}
JSON

"${WRAPPER}" setup update \
  --home "${HOME_DIR}" \
  --config "${CODEX_CONFIG}" \
  --bin "${WRAPPER}" \
  --yes >"${TMP_DIR}/update.out"

grep -Fq "${EXTERNAL_CONFIG}" "${DAEMON_CONFIG}"
cmp -s "${CODEX_CONFIG}" "${TMP_DIR}/codex-config.before.toml"

if grep -q 'removed from all clients' "${TMP_DIR}/update.out"; then
  echo "external daemon config was unexpectedly marked for removal"
  cat "${TMP_DIR}/update.out"
  exit 1
fi

echo "setup update external config smoke ok"
