#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LABEL="${LABEL:-com.binlee.lazy-mcp-wrapper}"
PLIST="${HOME}/Library/LaunchAgents/${LABEL}.plist"
SOCKET="${SOCKET:-${HOME}/.lazy-mcp-wrapper/lazy-mcpd.sock}"
BIN="${BIN:-${HOME}/.local/bin/lazy-mcp-wrapper}"
CONTEXT7_CONFIG="${CONTEXT7_CONFIG:-${ROOT_DIR}/examples/context7.json}"
MASTERGO_CONFIG="${MASTERGO_CONFIG:-${ROOT_DIR}/configs.local/mastergo-magic-mcp.json}"
LOG_DIR="${LOG_DIR:-${HOME}/Library/Logs/lazy-mcp-wrapper}"
PATH_VALUE="${PATH_VALUE:-${PATH}}"

if [[ ! -x "${BIN}" ]]; then
  echo "binary not executable: ${BIN}" >&2
  exit 1
fi
if [[ ! -f "${CONTEXT7_CONFIG}" ]]; then
  echo "context7 config not found: ${CONTEXT7_CONFIG}" >&2
  exit 1
fi
if [[ ! -f "${MASTERGO_CONFIG}" ]]; then
  echo "mastergo config not found: ${MASTERGO_CONFIG}" >&2
  exit 1
fi

mkdir -p "$(dirname "${PLIST}")" "$(dirname "${SOCKET}")" "${LOG_DIR}"

launchctl bootout "gui/$(id -u)" "${PLIST}" >/dev/null 2>&1 || true
launchctl remove "${LABEL}" >/dev/null 2>&1 || true
rm -f "${SOCKET}"

cat > "${PLIST}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${LABEL}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${BIN}</string>
    <string>daemon</string>
    <string>--socket</string>
    <string>${SOCKET}</string>
    <string>--config</string>
    <string>${CONTEXT7_CONFIG}</string>
    <string>--config</string>
    <string>${MASTERGO_CONFIG}</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>${PATH_VALUE}</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
  <key>StandardOutPath</key>
  <string>${LOG_DIR}/daemon.out.log</string>
  <key>StandardErrorPath</key>
  <string>${LOG_DIR}/daemon.err.log</string>
</dict>
</plist>
EOF

plutil -lint "${PLIST}" >/dev/null
launchctl bootstrap "gui/$(id -u)" "${PLIST}"
launchctl enable "gui/$(id -u)/${LABEL}"
launchctl kickstart -k "gui/$(id -u)/${LABEL}"

for _ in {1..50}; do
  if [[ -S "${SOCKET}" ]]; then
    cat <<EOF
LaunchAgent installed:
  label:  ${LABEL}
  plist:  ${PLIST}
  socket: ${SOCKET}
  logs:   ${LOG_DIR}
EOF
    exit 0
  fi
  sleep 0.1
done

echo "daemon socket was not created: ${SOCKET}" >&2
launchctl print "gui/$(id -u)/${LABEL}" >&2 || true
exit 1
