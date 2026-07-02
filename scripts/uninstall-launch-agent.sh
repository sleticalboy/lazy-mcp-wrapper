#!/usr/bin/env bash
set -euo pipefail

LABEL="${LABEL:-com.binlee.lazy-mcp-wrapper}"
PLIST="${HOME}/Library/LaunchAgents/${LABEL}.plist"
SOCKET="${SOCKET:-${HOME}/.lazy-mcp-wrapper/lazy-mcpd.sock}"
DAEMON_CONFIG="${DAEMON_CONFIG:-${HOME}/.lazy-mcp-wrapper/config.json}"

launchctl bootout "gui/$(id -u)" "${PLIST}" >/dev/null 2>&1 || true
launchctl remove "${LABEL}" >/dev/null 2>&1 || true
rm -f "${SOCKET}"

cat <<EOF
LaunchAgent uninstalled:
  label:  ${LABEL}
  plist:  ${PLIST}
  config: ${DAEMON_CONFIG} (kept)
  socket: ${SOCKET}
EOF
