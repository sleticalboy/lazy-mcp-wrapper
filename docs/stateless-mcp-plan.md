# Stateless MCP Compatibility Plan

## Context

The MCP specification is moving toward a stateless protocol core. The 2026-07-28
Release Candidate was announced on 2026-05-21, and its direction is clear:
remove protocol-level `initialize` / `initialized`, remove protocol sessions, and
make each request self-describing through request metadata.

Official reference:

- https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/

This is not the same as `lazy-mcp-wrapper`'s current `sharing: "session"` mode.
In this project, `sharing: "session"` means lifecycle isolation: one client
connection gets its own proxy and real MCP process. It protects stateful servers
such as Playwright from sharing browser context, cookies, pages, or login state.
That lifecycle isolation remains useful even if the MCP wire protocol becomes
stateless.

## Current Project State

`lazy-mcp-wrapper` currently behaves as a legacy MCP compatibility proxy.

Current client-facing behavior:

- `Proxy.Run` accepts JSON-RPC messages from the AI client.
- `initialize` is answered by the wrapper.
- `tools/list` can be served from the wrapper cache.
- Other supported methods are forwarded to the real MCP server.

Current upstream behavior:

- Stdio upstreams are started lazily and initialized by sending `initialize`.
- Native HTTP upstreams are also initialized by sending `initialize`.
- SDK-backed HTTP upstreams use the official Go SDK `Client.Connect`, which is
  still a client-session style abstraction.

This means the current implementation is not stateless at the MCP protocol
layer. It can, however, become a compatibility bridge:

- New stateless clients can talk to the wrapper.
- The wrapper can continue to initialize old stateful or legacy upstream MCP
  servers internally.
- Stateless upstream support can be added later without breaking existing stdio
  and local daemon behavior.

## Design Goals

1. Preserve current lazy-start behavior.
2. Preserve shared daemon mode and per-client lifecycle isolation.
3. Support stateless clients before requiring stateless upstreams.
4. Keep legacy MCP support for existing stdio servers.
5. Prefer official SDK behavior for remote HTTP when the SDK supports the new
   protocol surface.
6. Treat cache semantics as part of protocol compatibility, especially `ttlMs`
   and `cacheScope` once upstreams expose them.

## Non-Goals

- Do not remove legacy `initialize` support in the near term.
- Do not rename `sharing` immediately; this would create avoidable config churn.
- Do not make all MCP servers globally shared just because the protocol becomes
  stateless. Some servers still manage external state.
- Do not use Figma as the primary target for stateless work. Figma remains a
  remote-auth exception until its supported auth path is clear.
- Do not replace the daemon/client socket protocol as part of the first
  stateless phase.

## Terminology

| Term | Meaning in this project |
|---|---|
| Protocol stateless | MCP requests do not require a prior protocol session or `initialize` handshake. |
| Lifecycle shared | Multiple client connections reuse one wrapper proxy and one real MCP process. |
| Lifecycle session | Each client connection gets a separate wrapper proxy and real MCP process. |
| Legacy upstream | A real MCP server that still requires `initialize` before other methods. |
| Stateless upstream | A real MCP server that can process self-contained requests without protocol session state. |

## Phase 0: Documentation and Decision Boundary

Goal: make the project direction explicit before changing behavior.

Tasks:

- Document the difference between protocol statelessness and lifecycle
  isolation.
- Add roadmap entries for stateless client compatibility, `server/discover`,
  cache metadata, and stateless HTTP upstream support.
- Update README wording later so `sharing: "session"` is not confused with MCP
  protocol sessions.

Acceptance:

- This plan exists and is linked from `docs/roadmap.md`.
- Roadmap priorities are explicit.
- No runtime behavior changes.

Status: complete.

## Phase 1: Stateless Client Inbound Compatibility

Goal: allow a future stateless client to call wrapper methods without first
sending `initialize`.

Proposed behavior:

- If a client sends `initialize`, keep current behavior.
- If a client sends `tools/list`, `tools/call`, `resources/*`, `prompts/*`,
  `ping`, or future supported methods before `initialize`, accept the request.
- If upstream is legacy, synthesize default upstream initialization metadata
  internally before the first forwarded request.
- Keep cached `tools/list` working without requiring client initialization.

Implementation notes:

- `Proxy` already stores client protocol, client info, and capabilities from
  `initialize`. Add defaults for stateless clients.
- `startReal` and `startHTTPReal` already accept `initRequest`; keep that as the
  legacy upstream bootstrap object.
- Add tests where the client sends `tools/list` first and receives a normal
  response.

Acceptance:

- Existing legacy tests still pass.
- New tests cover `tools/list` and `tools/call` before `initialize`.
- Cached `tools/list` still avoids starting the real MCP server.
- Stdio upstreams that require `initialize` still work.

Status: complete. The existing proxy path already accepted these requests before
client `initialize`; regression coverage now locks this behavior for direct
proxy, cache-hit, and daemon-client paths.

Suggested release: `v0.5.0`.

## Phase 2: `server/discover`

Goal: expose wrapper and upstream capability discovery through the new discovery
shape without requiring legacy initialization.

Proposed behavior:

- Add `server/discover` handling in `Proxy.handleRequest`.
- Return wrapper identity, supported protocol modes, and known capabilities.
- Include lazy/cache metadata where meaningful.
- If real upstream is not started, return wrapper-known capabilities without
  forcing startup unless the request explicitly asks for upstream details.

Current response shape:

- `serverInfo`: wrapper server name and version.
- `protocol`: bridge mode, supported client modes, legacy upstream mode, and
  current protocol version.
- `capabilities`: wrapper-supported method families.
- `lifecycle`: configured `sharing` mode.
- `upstream`: known upstream type and protocol without starting it.
- `cache`: current `tools/list` cache state.
- `starts_upstream: false`: discovery itself does not lazy-start the real MCP.
- `experimental: true`: the response may be adjusted when the final MCP
  discovery shape is stable.

Open decisions:

- How much of upstream capability data can be cached safely.
- How to represent mixed mode: stateless client to legacy upstream.

Acceptance:

- `server/discover` works before `initialize`.
- It does not start heavy upstreams by default.
- Docs describe what is wrapper-derived versus upstream-derived.

Status: complete for wrapper-derived discovery. Upstream-derived discovery
remains intentionally out of scope until a safe cache/explicit-start policy is
defined.

Suggested release: `v0.5.x`.

## Phase 3: Cache Metadata (`ttlMs` and `cacheScope`)

Goal: align `tools/list` cache behavior with stateless MCP cache metadata.

Current behavior:

- `tools/list` is cached locally unless disabled.
- `notifications/tools/list_changed` invalidates the cache.
- Resources and prompts are forwarded live.

Proposed behavior:

- If upstream list responses contain `ttlMs`, use it to expire the cache.
- If upstream responses contain `cacheScope`, map it to wrapper cache safety:
  - `global` / shared-compatible scope can be reused by shared daemon entries.
  - `user`, `session`, or private scopes should not be shared across unsafe
    boundaries.
- If no metadata is present, keep current conservative behavior.
- Preserve manual `--clear-cache` and `--refresh-cache`.

Current behavior:

- Legacy cache records without metadata remain valid.
- `ttlMs` is stored with the cache record and expires entries relative to
  `created_at`; expired entries are removed on read.
- `cacheScope` values `global`, `shared`, `public`, `user`, and an empty value
  are reusable.
- `cacheScope` values `session`, `private`, or unknown values are not written to
  disk. Existing records with those scopes are treated as cache misses.
- `CacheInfo` exposes `ttlMs`, `cacheScope`, `expires_at`, and `expired` when a
  cached record exists.

Acceptance:

- Cache entries can expire by TTL.
- Cache key includes enough identity to avoid leaking private/session-scoped
  tool metadata across clients.
- Existing invalidation notification behavior still works.

Status: complete for `tools/list` cache records. Private/session-scoped tool
metadata is intentionally not cached instead of trying to create a per-client
cache namespace.

Suggested release: `v0.5.x` or `v0.6.0` depending on upstream availability.

## Phase 4: Stateless HTTP Upstream

Goal: support real remote HTTP MCP servers that no longer require initialize or
protocol session state.

Proposed behavior:

- Add upstream protocol mode:
  - `auto`
  - `legacy`
  - `stateless`
- In `auto`, prefer SDK behavior when the official Go SDK supports the new
  stateless surface.
- Add required or recommended request metadata headers once the final spec is
  stable, including protocol version and method/name metadata.
- Keep native HTTP backend only where it adds compatibility not yet available in
  the SDK.

Current behavior:

- `upstream_protocol_mode` is accepted for HTTP configs with values `auto`,
  `legacy`, or `stateless`.
- `auto` remains compatible with the existing behavior and still initializes
  upstreams.
- `stateless` is supported for native `streamable-http` upstreams and skips the
  upstream `initialize` request.
- SDK-backed HTTP and OAuth remotes reject `stateless` mode for now because the
  current SDK path still uses `Client.Connect` session semantics.

Possible config shape:

```json
{
  "name": "remote",
  "url": "https://example.test/mcp",
  "protocol": "streamable-http",
  "upstream_protocol_mode": "auto"
}
```

Acceptance:

- A stateless HTTP test server can handle `tools/list` with no prior
  `initialize`.
- Legacy remote HTTP servers still work in `legacy` mode.
- SDK-backed behavior is preferred when equivalent.

Status: complete for native `streamable-http` upstreams. SDK-backed stateless
upstream support remains intentionally deferred until the official Go SDK
exposes an equivalent stateless client surface.

Suggested release: `v0.6.0`.

## Phase 5: OAuth Hardening for Stateless MCP

Goal: align wrapper-managed OAuth with the security hardening in the newer MCP
direction.

Tasks:

- Store issuer metadata with credentials when available.
- Bind stored credentials to server URL, resource, issuer, and client ID.
- Include dynamic client registration metadata required by the final spec.
- Validate authorization responses according to the final OAuth/OIDC guidance.
- Keep `auth = "chatgpt"` remotes direct unless clients provide a supported way
  to forward equivalent per-session headers.

Current behavior:

- Stored OAuth credentials are validated against the current wrapper config
  before use.
- Runtime token injection checks `server_url`, and checks `client_id`,
  `resource`, and `scopes` when those fields are configured.
- Scope comparison is set-based, so ordering differences do not invalidate a
  credential.
- `setup` uses the same binding check before deciding that an OAuth remote can
  be wrapped.
- Credentials missing required binding fields are treated as not usable; users
  should rerun `lazy-mcp-wrapper auth login <name> --config <client-mcp-config>`.

Acceptance:

- Existing OAuth tests still pass.
- New tests cover issuer/resource binding and credential mismatch rejection.
- Figma remains direct unless a supported, verified auth path exists.

Status: complete for URL/client/resource/scope binding. Issuer metadata and
authorization-response validation remain dependent on the OAuth metadata exposed
by upstream servers and the official SDK surface.

Suggested release: `v0.6.x`.

## Priority Order

1. Phase 0: documentation and roadmap.
2. Phase 1: stateless client inbound compatibility.
3. Phase 2: `server/discover`.
4. Phase 3: cache metadata.
5. Phase 4: stateless HTTP upstream.
6. Phase 5: OAuth hardening.

## Implementation Notes by Current File

- `internal/wrapper/proxy.go`
  - Keep support for requests before `initialize`.
  - Add `server/discover`.
  - Keep legacy `initialize` response.
- `internal/wrapper/proxy_http.go`
  - Add upstream protocol mode later.
  - Keep legacy initialize path for existing servers.
- `internal/wrapper/proxy_http_sdk.go`
  - Track official Go SDK support before duplicating new protocol logic.
- `internal/wrapper/cache.go`
  - Add TTL and cache-scope metadata once response shapes are stable.
- `internal/setup/setup.go`
  - Later expose structured skip/direct/wrap reasons in a way that mentions
    protocol mode separately from lifecycle sharing.
- `README.md` and `README.zh-CN.md`
  - Clarify that `sharing: "session"` is lifecycle isolation, not MCP protocol
    session state.

## Risk

- The final 2026-07-28 spec may still change before release.
- The official Go SDK may lag the final protocol surface.
- Existing MCP servers will remain legacy for a while, so removing initialize
  support too early would break real users.
- Some servers are protocol-stateless but application-stateful. Playwright is
  the main example: it still needs lifecycle isolation even if requests become
  self-contained.
