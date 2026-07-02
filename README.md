# lazy-mcp-wrapper

中文文档: [README.zh-CN.md](./README.zh-CN.md)

`lazy-mcp-wrapper` is a lightweight stdio MCP proxy. Codex starts this wrapper, and the wrapper starts the real MCP server only when a forwarded method is needed.

The intended use case is reducing idle memory usage for MCP servers such as Context7, Playwright, and MasterGo.

## Build

```bash
make build
make test
```

## Install

Install locally to `~/.local/bin/lazy-mcp-wrapper`:

```bash
make install
```

or:

```bash
./scripts/install-local.sh
```

Override the prefix when needed:

```bash
PREFIX=/opt/lazy-mcp-wrapper ./scripts/install-local.sh
```

## Config

Example:

```json
{
  "name": "context7",
  "command": "npx",
  "args": ["-y", "@upstash/context7-mcp"],
  "real_protocol_version": "2024-11-05",
  "real_framing": "jsonl",
  "cache_dir": "/Users/you/Library/Caches/lazy-mcp-wrapper",
  "idle_timeout": "15s",
  "startup_timeout": "30s",
  "call_timeout": "120s",
  "log_file": "/tmp/lazy-mcp-wrapper-context7.log"
}
```

Codex config:

```toml
[mcp_servers.context7]
type = "stdio"
command = "/absolute/path/to/lazy-mcp-wrapper"
args = ["--config", "/absolute/path/to/context7.json"]
```

## Behavior

- `initialize` is answered by the wrapper.
- `tools/list`, `tools/call`, `prompts/*`, and `resources/*` are forwarded to the real MCP server.
- The real MCP server is stopped after `idle_timeout`.
- Logs go to `log_file`; stdout is reserved for MCP frames.
- `real_protocol_version` can pin the protocol version sent to the real MCP server when a client sends a newer version that the real server does not support.
- `real_framing` controls how the wrapper talks to the real MCP server:
  - `header` uses MCP `Content-Length` framing and is the default.
  - `jsonl` uses one JSON-RPC message per line. Context7 v3.2.2, Playwright MCP 1.62.0-alpha, and MasterGo Magic MCP currently use this mode.
- `tools/list` is cached by default. Cache files are stored under the OS user cache directory unless `cache_dir` is set. Set `disable_cache` to `true` to always query the real MCP server.

## Cache and Inspect

Refresh cache without starting a Codex session:

```bash
lazy-mcp-wrapper --config ./examples/context7.json --refresh-cache
```

Inspect resolved configuration and cache status:

```bash
lazy-mcp-wrapper --config ./examples/context7.json --inspect
```

Inspect shared daemon runtime status:

```bash
lazy-mcp-wrapper status --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock
lazy-mcp-wrapper status --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --format table
```

Control the shared daemon:

```bash
lazy-mcp-wrapper stop --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock
lazy-mcp-wrapper reload --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock
lazy-mcp-wrapper reload --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --force
```

`status` includes daemon config path, daemon pid, start time, uptime, active client sessions, forwarded calls, last error, and per-MCP metrics such as calls, errors, last method, last error, and latency.

`reload` hot-reloads the daemon config only when the daemon was started with `--daemon-config`. Manual `daemon --config ...` mode has no reload source and returns an explicit error. By default, reload returns busy when active clients are connected; use `--force` to replace proxies and close old real MCP processes anyway.

## Notes

`node_repl` is intentionally not a good fit for this wrapper because it keeps state between calls. Keep it configured directly unless you are fine with losing REPL state.

## Shared Daemon Mode

Multiple Codex CLI sessions can share stateless MCP servers through one daemon:

```bash
lazy-mcp-wrapper daemon \
  --socket /Users/you/.lazy-mcp-wrapper/lazy-mcpd.sock \
  --config ./examples/context7.json \
  --config ./configs.local/mastergo-magic-mcp.json
```

Or use a daemon config file:

```json
{
  "socket": "/Users/you/.lazy-mcp-wrapper/lazy-mcpd.sock",
  "configs": [
    "./examples/context7.json",
    "./configs.local/mastergo-magic-mcp.json"
  ]
}
```

```bash
lazy-mcp-wrapper daemon --daemon-config /Users/you/.lazy-mcp-wrapper/config.json
```

Codex client config:

```toml
[mcp_servers.context7]
type = "stdio"
command = "/Users/you/.local/bin/lazy-mcp-wrapper"
args = ["client", "--socket", "/Users/you/.lazy-mcp-wrapper/lazy-mcpd.sock", "--name", "context7"]
```

Phase 1 is intended for stateless or read-only MCP servers such as Context7 and MasterGo. Keep Playwright in direct lazy wrapper mode until session isolation is implemented.

On macOS, install the shared daemon as a user LaunchAgent:

```bash
make install-agent
```

Uninstall it with:

```bash
make uninstall-agent
```

The installer writes the current `PATH` into the plist so the daemon can find `npx`.

Use `lazy-mcp-wrapper stop --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock` to ask the daemon to exit. If LaunchAgent management is enabled, launchd will start it again from the installed config.

## Verification

Run unit and integration tests:

```bash
GOCACHE=/private/tmp/lazy-mcp-wrapper-gocache go test ./...
```

The integration test builds and runs `cmd/fake-mcp` to verify stdio framing, child process startup, `initialize`, and `tools/list` forwarding without network access.

To smoke-test a real MCP server:

```bash
go build -o bin/lazy-mcp-wrapper ./cmd/lazy-mcp-wrapper
go run ./cmd/mcp-smoke ./bin/lazy-mcp-wrapper ./examples/context7.json
go run ./cmd/mcp-smoke \
  --call-tool resolve-library-id \
  --call-args '{"query":"Go gin web framework routing middleware","libraryName":"Gin"}' \
  ./bin/lazy-mcp-wrapper ./examples/context7.json
```

Verified examples:

```bash
go run ./cmd/mcp-smoke ./bin/lazy-mcp-wrapper ./examples/context7.json
go run ./cmd/mcp-smoke ./bin/lazy-mcp-wrapper ./examples/playwright.json
go run ./cmd/mcp-smoke ./bin/lazy-mcp-wrapper ./examples/mastergo-magic-mcp.json
```

Run the full local smoke suite:

```bash
./scripts/smoke.sh
```

Run the shared daemon smoke test with a local fake MCP:

```bash
make smoke-shared-daemon
```

The MasterGo smoke uses `configs.local/mastergo-magic-mcp.json` when present, because the committed example intentionally contains no real token. Its tool call allows a validation error result because MasterGo design tools require a real `fileId`/`layerId`; that still verifies the wrapper can initialize the real MCP and forward `tools/call`.

If a real MCP server does not respond, check the configured `log_file`. A failure in `cmd/mcp-smoke` with no response from direct MCP startup usually means the upstream MCP command is not entering stdio server mode, not that the wrapper transport is broken.
