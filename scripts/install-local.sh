#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${PREFIX:-${HOME}/.local}"

make -C "${ROOT_DIR}" PREFIX="${PREFIX}" install

cat <<EOF

lazy-mcp-wrapper installed to:
  ${PREFIX}/bin/lazy-mcp-wrapper

Make sure this directory is on PATH, or reference the absolute path from Codex config.
EOF
