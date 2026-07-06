# Official Go SDK and OAuth Plan

## Context

`lazy-mcp-wrapper` currently implements its MCP proxy stack directly:

- JSON-RPC framing lives in `internal/jsonrpc`.
- Client-facing proxy behavior lives in `internal/wrapper/proxy.go`.
- Stdio upstream handling lives in `internal/wrapper/proxy.go`.
- Streamable HTTP upstream handling lives in `internal/wrapper/proxy_http.go`.
- Local HTTP wrapper serving lives in `internal/wrapper/http_server.go`.

This keeps the binary small and the lazy-start behavior explicit, but it also means OAuth-capable remote MCP servers require protocol and auth logic that the project does not currently own. The official Go SDK (`github.com/modelcontextprotocol/go-sdk`) provides MCP client/server transports and OAuth helper primitives that can reduce that risk.

## Decision

Do not rewrite the entire proxy around the SDK in one step.

Use a staged migration:

1. Validate SDK fit without changing runtime behavior.
2. Replace only remote Streamable HTTP upstream handling with the SDK.
3. Add OAuth support only after the SDK-backed upstream path is stable.
4. Keep stdio and daemon sharing behavior unchanged unless a later phase proves a concrete benefit.

This preserves the core product behavior: lazy startup, shared daemon mode, setup automation, and `tools/list` caching.

## Non-Goals

- Do not replace stdio proxying in the first SDK phase.
- Do not replace the daemon/client Unix socket protocol in the first SDK phase.
- Do not commit to wrapping Figma by default until a real OAuth flow works against Figma's MCP server.
- Do not share OAuth-authenticated remote MCP upstream sessions across independent AI client sessions.
- Do not store OAuth tokens in wrapper config files.

## Phase 0: SDK Fit Probe

Goal: prove the official Go SDK can be built and tested in this repository without changing runtime behavior.

Status: complete. The repository now has a compile-time probe for the SDK API surface in `internal/mcpsdkprobe`, and no runtime wrapper path uses the SDK yet.

Tasks:

- Add the official SDK dependency.
- Add a small internal probe package or test that imports the SDK's MCP and auth packages.
- Verify the SDK exposes the required primitives:
  - `mcp.Client`
  - `mcp.StreamableClientTransport`
  - `auth.OAuthHandler`
  - authorization-code OAuth handler support
- Keep all existing runtime code paths untouched.

Acceptance:

- `go test ./...` passes.
- No existing wrapper behavior changes.
- The probe documents the exact SDK API surface this project intends to use.

## Phase 1: SDK-Backed Remote HTTP Upstream

Goal: replace the hand-written remote Streamable HTTP upstream client while keeping the external wrapper behavior unchanged.

Status: in progress. The implementation now has an opt-in SDK-backed backend behind `http_backend: "sdk"`. The default remains the existing native HTTP backend.

Current SDK backend coverage:

- Uses `mcp.NewClient` and `mcp.StreamableClientTransport` for Streamable HTTP upstreams.
- Preserves configured HTTP headers through the SDK transport.
- Supports the standard typed MCP methods listed below.
- Re-broadcasts SDK server notifications back to the existing proxy frontend:
  - `notifications/tools/list_changed`
  - `notifications/prompts/list_changed`
  - `notifications/resources/list_changed`
  - `notifications/resources/updated`
  - `notifications/message`
  - `notifications/progress`
- Uses the SDK's standalone SSE path for server-initiated Streamable HTTP notifications.

Remaining before making it default:

- Manual verification against real non-OAuth remote Streamable HTTP MCP servers.
- Final decision on unknown or future MCP methods: native fallback versus explicit `method not found`.
- Compatibility review for client notifications beyond `notifications/initialized` and `notifications/cancelled`.

Scope:

- Introduce a new `realBackend` implementation for Streamable HTTP using:
  - `mcp.NewClient`
  - `mcp.StreamableClientTransport`
  - SDK client session methods for standard MCP methods
- Keep the existing `Proxy.Run` client-facing JSON-RPC proxy.
- Keep stdio upstream behavior unchanged.
- Keep setup behavior unchanged.

Compatibility work:

- Preserve request/response mapping for:
  - `tools/list`
  - `tools/call`
  - `resources/list`
  - `resources/read`
  - `resources/templates/list`
  - `prompts/list`
  - `prompts/get`
  - `completion/complete`
  - `logging/setLevel`
  - `resources/subscribe`
  - `resources/unsubscribe`
  - `ping`
- Decide how to handle unknown or future MCP methods:
  - either keep the existing raw HTTP path as fallback
  - or explicitly return `method not found` until a typed SDK route exists

SDK coverage found in Phase 0:

| MCP method | Go SDK `ClientSession` method |
|---|---|
| `tools/list` | `ListTools` |
| `tools/call` | `CallTool` |
| `resources/list` | `ListResources` |
| `resources/read` | `ReadResource` |
| `resources/templates/list` | `ListResourceTemplates` |
| `prompts/list` | `ListPrompts` |
| `prompts/get` | `GetPrompt` |
| `completion/complete` | `Complete` |
| `logging/setLevel` | `SetLoggingLevel` |
| `resources/subscribe` | `Subscribe` |
| `resources/unsubscribe` | `Unsubscribe` |
| `ping` | `Ping` |

Acceptance:

- Existing stdio tests pass unchanged.
- Existing HTTP proxy tests pass or are intentionally updated to equivalent SDK-backed expectations.
- SDK backend tests cover JSON responses, configured header forwarding, and standalone SSE server notifications.
- Manual `setup verify` succeeds for existing non-OAuth remote HTTP servers.

Current opt-in config:

```json
{
  "name": "remote",
  "url": "https://example.test/mcp",
  "protocol": "streamable-http",
  "http_backend": "sdk"
}
```

When `http_backend` is empty or `"native"`, the existing hand-written HTTP backend is used.

## Phase 2: OAuth-Aware Remote HTTP Upstream

Goal: support remote OAuth MCP servers using the SDK auth primitives.

Status: in progress. The config model can now represent OAuth remote MCPs, the CLI has a file-backed credential store, and `auth login` runs an authorization-code flow through the official Go SDK. If a wrapper config sets `auth: "oauth"`, the SDK-backed HTTP transport can read an existing local credential and attach a Bearer token; without a stored token, a 401/403 response produces a clear login-required error.

Current OAuth foundation:

- Wrapper config accepts `auth: "oauth"`, `oauth_client_id`, `oauth_resource`, and `oauth_scopes`.
- Codex-style client config metadata is recognized: top-level `auth`, `oauth_resource`, `scopes`, and nested `oauth.client_id`.
- JSON client configs can also provide `oauth.client_id`, `oauth_resource`, and `scopes`; setup carries those values into generated wrapper configs.
- `lazy-mcp-wrapper auth login <name>` can load either a wrapper config or an original client MCP config via `--config`; when no wrapper config exists, it scans installed client configs for the named server.
- OAuth configs default to `sharing: "session"` and reject `sharing: "shared"`.
- `lazy-mcp-wrapper auth status [name]` reads local credential status without printing tokens.
- `lazy-mcp-wrapper auth logout <name>` deletes local credentials.
- `lazy-mcp-wrapper auth login <name>` runs a local callback server and SDK authorization-code flow, then stores the resulting token.
- File-backed credential storage lives under `~/.lazy-mcp-wrapper/auth/` with private permissions.
- The SDK Streamable HTTP backend has an OAuth handler that reads stored credentials and injects `Authorization: Bearer <token>`.
- Expired stored tokens are refreshed and written back when the credential has both `refresh_token` and `token_url`.
- OAuth configs require the SDK HTTP backend; explicit `http_backend: "native"` is rejected.
- `setup` and `setup update` wrap OAuth-managed remotes only when a non-expired local credential exists.
- OAuth-managed remotes without credentials remain direct in the client config and get an `auth login` blocker hint.
- Remote MCPs configured with `auth = "chatgpt"` are not wrapped. Codex implements this by passing its internal ChatGPT `AuthProvider` into the Streamable HTTP client and adding those headers at request time; lazy-mcp-wrapper does not own that session provider.

Scope:

- Add wrapper config fields for OAuth remote servers:
  - `auth: "oauth"`
  - optional `oauth_client_id`
  - optional `oauth_resource`
  - optional `oauth_scopes`
- Add user-facing commands:
  - `lazy-mcp-wrapper auth login <name>`: implemented for authorization-code OAuth; supports `--config`, `--url`, `--client-id`, `--token-url`, `--scope`, `--callback-port`, and `--no-open`. `--config` may point to either a wrapper config or an original client MCP config.
  - `lazy-mcp-wrapper auth logout <name>`: implemented for local file-backed credentials
  - `lazy-mcp-wrapper auth status [name]`: implemented for local file-backed credentials
- Use OS keychain where practical, with an explicit file fallback only when configured.
- Never write access tokens or refresh tokens into wrapper configs, daemon configs, logs, status output, or cache files.

Runtime rules:

- OAuth-backed remote MCPs must default to `sharing: "session"`.
- Shared daemon mode may still manage the local listener and lifecycle, but each client session must use its own OAuth-authenticated upstream session unless token ownership is explicitly proven safe.
- Token refresh must be serialized per server/user to avoid duplicate refresh races.
- ChatGPT-auth remote MCPs must stay direct in Codex unless the wrapper gets an explicit, supported way to receive equivalent per-session headers from the client.

Acceptance:

- OAuth configs default to `sharing: "session"` and reject `sharing: "shared"`.
- `setup` skips OAuth-managed remotes, including Figma, until a stored credential exists; after login, it can wrap them as session-scoped SDK HTTP proxies.
- OAuth login works against a local test OAuth-protected Streamable HTTP MCP server.
- Expired access tokens refresh without exposing secrets in logs.
- `logout` removes credentials.
- `setup` can identify OAuth remote MCPs and explain whether they are skipped, direct, or wrapper-managed.

## Phase 3: Figma Validation

Goal: decide whether Figma should be supported by wrapper-managed OAuth or left as a direct Codex-managed MCP.

Current finding:

- Dynamic client registration against `https://mcp.figma.com/mcp` returned HTTP 403 in a real probe, so Figma cannot be treated as a generic dynamically registered OAuth MCP.
- Figma-style support needs a pre-registered OAuth client ID from the original client config, or Figma should remain direct in the AI client.
- Codex can also support `auth = "chatgpt"` remotes by attaching its own ChatGPT auth provider. That path is not portable to lazy-mcp-wrapper today.

Tasks:

- Test Figma login with a pre-registered `oauth.client_id` from Codex-style config in a non-destructive local config.
- Confirm whether Figma accepts this wrapper when it presents that client ID.
- Compare startup cost:
  - direct Codex Figma MCP
  - wrapper-managed OAuth
  - direct but disabled until needed
- Document the final recommendation.

Acceptance:

- If Figma works reliably, add explicit docs and keep default conservative.
- If Figma rejects third-party wrapper OAuth, keep skipping Figma by default and document why.

## Estimated Work

| Phase | Estimate | Risk |
|---|---:|---|
| Phase 0: SDK fit probe | 0.5-1 day | Low |
| Phase 1: SDK-backed remote HTTP upstream | 3-6 days | Medium |
| Phase 2: OAuth-aware remote HTTP upstream | 5-10 days | Medium-high |
| Phase 3: Figma validation | 1-3 days | High external dependency |

## Open Questions

- Does the SDK expose enough raw request support for unknown MCP methods, or should the existing raw HTTP path remain as fallback?
- Should OAuth credential storage use a new wrapper-specific keychain service name or a shared MCP credential namespace?
- Should `setup` offer a choice for OAuth remotes, or should it keep skipping them until `auth login` has been completed?
- Is wrapper-managed OAuth worth the complexity for remote MCPs that already work directly in Codex?
