#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WRAPPER="${ROOT_DIR}/bin/lazy-mcp-wrapper"

go build -o "${WRAPPER}" "${ROOT_DIR}/cmd/lazy-mcp-wrapper"

echo "== context7 tools/call =="
go run "${ROOT_DIR}/cmd/mcp-smoke" \
  --call-tool resolve-library-id \
  --call-args '{"query":"Go gin web framework routing middleware","libraryName":"Gin"}' \
  "${WRAPPER}" "${ROOT_DIR}/examples/context7.json"

echo "== playwright tools/call =="
go run "${ROOT_DIR}/cmd/mcp-smoke" \
  --call-tool browser_snapshot \
  --call-args '{}' \
  "${WRAPPER}" "${ROOT_DIR}/examples/playwright.json"

MASTERGO_CONFIG="${ROOT_DIR}/configs.local/mastergo-magic-mcp.json"
if [[ -f "${MASTERGO_CONFIG}" ]]; then
  echo "== mastergo transport smoke =="
  go run "${ROOT_DIR}/cmd/mcp-smoke" \
    --call-tool mcp__getMeta \
    --call-args '{}' \
    --allow-tool-error \
    "${WRAPPER}" "${MASTERGO_CONFIG}"
else
  echo "== mastergo transport smoke skipped: ${MASTERGO_CONFIG} not found =="
fi
