# Troubleshooting

## `connection closed: initialize response`

If a client reports an MCP startup error like this:

```text
MCP client for `context7` failed to start: MCP startup failed:
handshaking with MCP server failed: connection closed: initialize response
```

first check whether the client config and daemon config disagree.

```bash
lazy-mcp-wrapper setup status
lazy-mcp-wrapper setup update --dry-run
lazy-mcp-wrapper status --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --format table
```

If the daemon status shows `Last error: unknown MCP name: context7`, the client
is still pointing at `lazy-mcp-wrapper client --name context7`, but the daemon
does not have a matching wrapper config loaded.

In `lazy-mcp-wrapper v0.5.4` and newer, `setup status` also reports this
configuration mismatch directly in the client `Notes` column:

```text
missing daemon wrapper: context7
```

Recovery:

```bash
lazy-mcp-wrapper setup update --yes
lazy-mcp-wrapper reload --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --graceful
```

Then verify the server is listed again:

```bash
lazy-mcp-wrapper status --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock --format table
```

For configs that intentionally live outside `~/.lazy-mcp-wrapper/wrappers`,
use `lazy-mcp-wrapper v0.5.3` or newer. Older `setup update` versions could drop
external daemon config paths during unrelated cleanup.
