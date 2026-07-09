# lazy-mcp-wrapper

中文文档: [README.zh-CN.md](./README.zh-CN.md)

`lazy-mcp-wrapper` is a lightweight MCP proxy for keeping local AI clients fast to start. Codex, Cursor, Claude Code, and Claude Desktop can start the wrapper immediately; the wrapper starts the real MCP server only when a tool call actually needs it.

## Install

```bash
brew tap sleticalboy/tap
brew install lazy-mcp-wrapper
lazy-mcp-wrapper setup
```

Check the result:

```bash
lazy-mcp-wrapper setup status
```

![setup status](./docs/setup-status.svg)

## What It Does

- Reduces idle memory usage from MCP servers such as Context7, Playwright, and MasterGo.
- Keeps stdio and remote HTTP MCP servers out of the client startup path until `tools/call`, `resources/*`, or `prompts/*` needs the real server.
- Caches `tools/list` so clients can discover tools without repeatedly starting heavy MCP processes.
- Shares stateless MCP servers through a local daemon across multiple Codex CLI sessions.
- Preserves stateful MCP isolation with `sharing: "session"` for servers such as Playwright.
- Provides `setup`, `setup status`, `setup update`, and `setup uninstall` for reversible client configuration.

Current scope: stdio MCP servers and remote HTTP MCP servers. Remote HTTP defaults to `streamable-http`; legacy HTTP+SSE is supported only for compatibility.

## Build

```bash
make build
make test
```

## Build From Source

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
  "sharing": "shared",
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

### Remote HTTP MCP Servers

`lazy-mcp-wrapper` can proxy remote MCP servers over HTTP.

The recommended protocol is `streamable-http` (MCP spec 2025-03-26):

```json
{
  "name": "my-remote-mcp",
  "url": "https://example.com/mcp",
  "protocol": "streamable-http",
  "auth": "none",
  "upstream_protocol_mode": "auto"
}
```

When `setup` wraps a remote HTTP server, it starts a local HTTP proxy through the shared daemon and rewrites the client config to a local URL such as `http://127.0.0.1:54300`. Ports are assigned automatically from `54300` upward and stored as `local_port` in the generated wrapper config.

Remote HTTP setup is intentionally conservative. `setup` wraps remote HTTP servers only when the authentication model is explicit:

- Local HTTP MCP servers.
- Public unauthenticated remote MCP servers marked with `auth: "none"`.
- Remote MCP servers with explicit credential headers such as `Authorization` or `X-API-Key`.
- Standard OAuth remote MCP servers only after `lazy-mcp-wrapper auth login <name>` has created a non-expired local credential.

These remote MCP servers stay configured directly in the client:

- URL-only remote MCP servers, because `setup` cannot know whether they are public, OAuth-protected, or tied to a specific client.
- Figma MCP, unless a supported pre-registered OAuth client and local credential are already configured. A real probe against `https://mcp.figma.com/mcp` returned HTTP 403 for dynamic OAuth client registration, so Figma should stay direct in Codex by default.
- Remote MCP servers configured with `auth: "chatgpt"`, because Codex handles them by attaching its internal ChatGPT auth provider headers at request time. The wrapper does not own those per-session credentials.

OAuth credentials are bound to the remote MCP config. At runtime and during `setup`, the stored credential must match the configured server URL, and must also match `oauth_client_id`, `oauth_resource`, and `oauth_scopes` when those fields are configured. If those values change, rerun `lazy-mcp-wrapper auth login <name> --config <client-mcp-config>`.

The `protocol` field accepts:

| Value | Description |
|-------|-------------|
| `streamable-http` | **Recommended.** MCP 2025-03-26 standard. Default when `protocol` is omitted. |
| `sse` | **Deprecated.** Legacy HTTP+SSE transport. Supported for compatibility only. |

> **`sse` (HTTP+SSE) is deprecated** by the MCP specification. It is supported for compatibility with legacy servers but should not be used for new deployments.

The optional `upstream_protocol_mode` field controls how the wrapper talks to a remote HTTP upstream:

| Value | Description |
|-------|-------------|
| `auto` | Default. Keeps current compatibility behavior and initializes the upstream. |
| `legacy` | Forces legacy upstream initialization. |
| `stateless` | Native `streamable-http` only. Skips the upstream `initialize` request. SDK-backed OAuth remotes do not support this mode yet. |

## Behavior

- `initialize` is answered by the wrapper.
- `server/discover` is answered by the wrapper before `initialize` and does not start the real MCP server.
- Stateless clients may call wrapper methods such as `tools/list` and `tools/call` before `initialize`; legacy upstreams are still initialized internally when needed.
- `tools/list`, `tools/call`, `prompts/*`, and `resources/*` are forwarded to the real MCP server.
- The real MCP server is stopped after `idle_timeout`.
- Logs go to `log_file`; stdout is reserved for MCP frames.
- `real_protocol_version` can pin the protocol version sent to the real MCP server when a client sends a newer version that the real server does not support.
- `real_framing` controls how the wrapper talks to the real MCP server:
  - `header` uses MCP `Content-Length` framing and is the default.
  - `jsonl` uses one JSON-RPC message per line. Context7 v3.2.2, Playwright MCP 1.62.0-alpha, and MasterGo Magic MCP currently use this mode.
- `tools/list` is cached by default. Cache files are stored under the OS user cache directory unless `cache_dir` is set. `notifications/tools/list_changed` invalidates the cache before the notification is forwarded. Set `disable_cache` to `true` to always query the real MCP server. If an upstream `tools/list` result includes `ttlMs`, the cache expires by that TTL. `cacheScope: "session"` and `cacheScope: "private"` results are not persisted.
- `sharing` controls daemon sharing strategy. `shared` reuses one proxy per MCP name. `session` creates one proxy per client connection for stateful MCPs.

## Cache and Inspect

`resources/*` and `prompts/*` are forwarded live by default; only `tools/list` is cached.

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
lazy-mcp-wrapper reload --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --graceful
lazy-mcp-wrapper reload --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --force
```

`status` includes daemon config path, daemon pid, start time, uptime, active client sessions, forwarded calls, last error, and per-MCP metrics such as calls, errors, last method, last error, and latency.

`reload` hot-reloads the daemon config only when the daemon was started with `--daemon-config`. Manual `daemon --config ...` mode has no reload source and returns an explicit error. Unchanged wrapper configs keep their existing proxy instance, real MCP process, and runtime stats; only added, removed, or changed configs are replaced. By default, reload returns busy when active clients are connected. Use `--graceful` to route new clients to the new proxy set while existing clients keep their old proxies until disconnect. Use `--force` to replace changed/removed proxies and close old real MCP processes immediately.

## Notes

`node_repl` is intentionally not a good fit for this wrapper because it keeps state between calls. Keep it configured directly unless you are fine with losing REPL state.

## Automated Setup

`setup` scans installed AI clients, creates wrapper configs, installs the macOS LaunchAgent, and updates client MCP configs with backups:

```bash
lazy-mcp-wrapper setup --dry-run
lazy-mcp-wrapper setup
lazy-mcp-wrapper setup --yes
lazy-mcp-wrapper setup watch
```

To scan explicit client config files instead of known client locations, pass `--config PATH`. The flag can be repeated; later files override earlier files by MCP server name:

```bash
lazy-mcp-wrapper setup --config ./base-mcp.json --config ./local-mcp.toml --dry-run
```

Supported clients:

- Codex: `~/.codex/config.toml`
- Cursor: `~/.cursor/mcp.json`
- Claude Code: `~/.claude/settings.json`
- Claude Desktop: `~/Library/Application Support/Claude/claude_desktop_config.json`

The command wraps stdio MCP servers, skips `node_repl`, and uses `sharing: "session"` for Playwright. Remote HTTP MCP servers are conservative by default: local HTTP servers, remote servers with explicit auth headers, remote servers marked with `auth: "none"`, and standard OAuth remotes with an existing local wrapper credential can be wrapped. URL-only remotes, Figma, and `auth: "chatgpt"` remotes stay configured directly in the client.

`setup watch` polls known client MCP config files, the wrapper config directory, and the daemon config. When a change is detected, it prints the same diff as `setup update --dry-run`. It does not write files by default:

```bash
lazy-mcp-wrapper setup watch --interval 2s
```

Use `--apply` only when you want detected changes to run `setup update --yes` automatically:

```bash
lazy-mcp-wrapper setup watch --apply
```

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
    "./examples/playwright.json",
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

Use `sharing: "shared"` for stateless or read-only MCP servers such as Context7 and MasterGo. Use `sharing: "session"` for stateful MCP servers such as Playwright; Codex sessions share the daemon entrypoint, while each client connection gets its own real MCP process.

`sharing: "session"` is lifecycle isolation inside `lazy-mcp-wrapper`; it is not the MCP protocol session concept that the 2026 stateless MCP direction removes.

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

Run the real Playwright daemon session smoke test:

```bash
make smoke-playwright-session
```

The MasterGo smoke uses `configs.local/mastergo-magic-mcp.json` when present, because the committed example intentionally contains no real token. Its tool call allows a validation error result because MasterGo design tools require a real `fileId`/`layerId`; that still verifies the wrapper can initialize the real MCP and forward `tools/call`.

If a real MCP server does not respond, check the configured `log_file`. A failure in `cmd/mcp-smoke` with no response from direct MCP startup usually means the upstream MCP command is not entering stdio server mode, not that the wrapper transport is broken.

## License

MIT. See [LICENSE](./LICENSE).
