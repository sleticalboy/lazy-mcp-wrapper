# Session Sharing Design

## 背景

shared daemon 当前适合无状态或只读 MCP，例如 `context7` 和 `mastergo-magic-mcp`。这些 MCP 可以让多个 Codex CLI 共享同一个 proxy 和真实 MCP 进程。

`playwright` 不适合直接共享真实 MCP：

- 浏览器页面、context、cookie、localStorage 和登录态是会话状态。
- 多个 Codex CLI 同时操作同一个浏览器实例会互相影响。
- 一个会话关闭页面、导航或清理状态，可能破坏另一个会话的任务。

因此需要 session sharing 模式。

## 配置

每个 MCP 配置增加 `sharing` 字段：

```json
{
  "name": "playwright",
  "sharing": "session"
}
```

取值：

- `shared`：默认值。所有 client 共享 daemon 当前 generation 的同一个 proxy。
- `session`：每个 client connection 创建独立 proxy，真实 MCP 生命周期绑定到该连接。

## 第一阶段行为

第一阶段先跑通策略骨架：

- `shared` 继续沿用现有逻辑。
- `session` 每个 client connection 单独创建 proxy。
- session client 断开时关闭该 proxy 和它启动的真实 MCP。
- MCP 级别 calls/errors/latency 仍按 MCP name 聚合。
- `status` 在 server 行展示 `sharing` 和 `active_sessions`。
- `active_clients` 展示 client 所属 `sharing` 和 generation。

## Reload 语义

`reload` 会替换当前 generation 的配置集合。

对于 `shared`：

- 普通 reload 在 active client 存在时返回 busy。
- `--graceful` 时旧连接继续用旧 generation 的 proxy，新连接走新 proxy。
- `--force` 会立即关闭旧 proxy。

对于 `session`：

- 已存在的 session client 持有创建时的独立 proxy。
- `--graceful` 后，新 session 使用新配置创建 proxy。
- 旧 session 断开时自然关闭自己的 proxy。

## 验证

Playwright 示例配置已切到 `sharing: "session"`。真实 Playwright daemon session smoke：

```bash
make smoke-playwright-session
```

真实 Playwright 的验证重点是浏览器上下文、页面状态和登录态隔离。
