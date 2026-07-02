# Codex MCP Migration Draft

This document is a migration draft only. Do not edit `~/.codex/config.toml` until you are ready to switch Codex to `lazy-mcp-wrapper`.

## Goal

Replace heavy MCP server entries with a lightweight wrapper:

```text
Codex -> lazy-mcp-wrapper -> real MCP server
```

Codex keeps a small wrapper process registered. The real MCP server starts only when a forwarded method is needed. `tools/list` is cached by default, so new Codex sessions can discover tools without starting the real MCP server after the cache is warm.

## Prerequisites

Install the wrapper to a stable path:

```bash
cd /Users/binlee/code/open-source/lazy-mcp-wrapper
make install
```

Default install path:

```text
/Users/binlee/.local/bin/lazy-mcp-wrapper
```

Verify the installed binary:

```bash
/Users/binlee/.local/bin/lazy-mcp-wrapper --config /Users/binlee/code/open-source/lazy-mcp-wrapper/examples/context7.json --inspect
```

Warm caches before switching Codex:

```bash
/Users/binlee/.local/bin/lazy-mcp-wrapper --config /Users/binlee/code/open-source/lazy-mcp-wrapper/examples/context7.json --refresh-cache
/Users/binlee/.local/bin/lazy-mcp-wrapper --config /Users/binlee/code/open-source/lazy-mcp-wrapper/examples/playwright.json --refresh-cache
```

For MasterGo, use a private config because the committed example intentionally contains no real token:

```text
/Users/binlee/code/open-source/lazy-mcp-wrapper/configs.local/mastergo-magic-mcp.json
```

Warm MasterGo cache only when the private config exists:

```bash
/Users/binlee/.local/bin/lazy-mcp-wrapper --config /Users/binlee/code/open-source/lazy-mcp-wrapper/configs.local/mastergo-magic-mcp.json --refresh-cache
```

## Backup

Before changing Codex config:

```bash
cp ~/.codex/config.toml ~/.codex/config.toml.bak-$(date +%Y%m%d%H%M%S)
```

## Replacement Snippets

Replace the existing MCP server blocks with these wrapper blocks.

### Context7

```toml
[mcp_servers.context7]
type = "stdio"
command = "/Users/binlee/.local/bin/lazy-mcp-wrapper"
args = ["--config", "/Users/binlee/code/open-source/lazy-mcp-wrapper/examples/context7.json"]
```

### Playwright

```toml
[mcp_servers.playwright]
type = "stdio"
command = "/Users/binlee/.local/bin/lazy-mcp-wrapper"
args = ["--config", "/Users/binlee/code/open-source/lazy-mcp-wrapper/examples/playwright.json"]
```

### MasterGo

Use the private config path, not the committed example path:

```toml
[mcp_servers.mastergo-magic-mcp]
type = "stdio"
command = "/Users/binlee/.local/bin/lazy-mcp-wrapper"
args = ["--config", "/Users/binlee/code/open-source/lazy-mcp-wrapper/configs.local/mastergo-magic-mcp.json"]
```

## Keep node_repl Direct

Do not wrap `node_repl` for now. It keeps REPL state between calls, so lazy shutdown would lose state and break browser/plugin workflows.

## Verification After Switching

Start a new Codex session after editing `~/.codex/config.toml`, then verify:

```bash
tail -f /tmp/lazy-mcp-wrapper-context7.log
tail -f /tmp/lazy-mcp-wrapper-playwright.log
tail -f /tmp/lazy-mcp-wrapper-mastergo.log
```

Expected behavior:

- New session tool discovery should hit cache for `tools/list`.
- First actual tool call starts the real MCP process.
- Repeated calls within `idle_timeout` reuse the same real MCP process.
- The real MCP process exits after `idle_timeout` or when the wrapper exits.

Local smoke commands:

```bash
cd /Users/binlee/code/open-source/lazy-mcp-wrapper
./scripts/smoke.sh
```

## Rollback

Restore the backup config:

```bash
cp ~/.codex/config.toml.bak-YYYYMMDDHHMMSS ~/.codex/config.toml
```

Then restart Codex or start a new Codex session.

## Notes

- `real_framing = "jsonl"` is required for the currently verified Context7, Playwright, and MasterGo MCP servers.
- Logs redact `--token=...`, `--api-key=...`, secrets, and token-like environment variables.
- `configs.local/` is ignored by git and is the intended location for real local secrets.
