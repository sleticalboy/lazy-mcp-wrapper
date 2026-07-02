#!/usr/bin/env bash
set -euo pipefail

LABEL="${LABEL:-com.binlee.lazy-mcp-wrapper}"
PLIST="${HOME}/Library/LaunchAgents/${LABEL}.plist"
SOCKET="${SOCKET:-${HOME}/.lazy-mcp-wrapper/lazy-mcpd.sock}"

launchctl bootout "gui/$(id -u)" "${PLIST}" >/dev/null 2>&1 || true
launchctl remove "${LABEL}" >/dev/null 2>&1 || true
rm -f "${SOCKET}"

cat <<EOF
LaunchAgent uninstalled:
  label:  ${LABEL}
  plist:  ${PLIST}
  socket: ${SOCKET}
EOF
