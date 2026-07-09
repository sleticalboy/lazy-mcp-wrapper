# Release Notes

## v0.5.4

- Enhanced `setup status` to report client/daemon mismatches such as `missing daemon wrapper: context7`.
- This makes stale client wrapper references visible before a new AI client reports an MCP startup warning.

## v0.5.3

- Fixed `setup update` so existing daemon config paths outside the managed wrapper directory are preserved when the matching client entry already points at `lazy-mcp-wrapper`.
- This prevents valid entries such as an external `context7.json` from being dropped during unrelated cleanup updates.

## v0.5.2

- Fixed `setup update` so HTTP wrapper configs scheduled for removal no longer leak their local port back into client config rewrites.
- This keeps skipped remote MCP servers, including Figma, configured as direct remote MCP entries when their old wrapper config is cleaned up.

## v0.5.1

- Switched `setup watch` from polling to `fsnotify`.
- `setup watch --interval` now controls the debounce window after filesystem events.
- Missing config files are watched through their nearest existing parent directory, so later file creation is detected.

## v0.5.0

- Added stateless MCP compatibility handling and protocol alignment controls.
- Kept OAuth remotes conservative by default: Figma and `auth = "chatgpt"` remote MCP servers stay direct unless wrapper-managed credentials are explicitly available.
