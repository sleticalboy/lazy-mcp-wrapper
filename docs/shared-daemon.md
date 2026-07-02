# Shared MCP Daemon Design

## 背景

当前 `lazy-mcp-wrapper` 已经解决了真实 MCP 服务常驻的问题：

```text
Codex CLI
-> lazy-mcp-wrapper
-> 按需启动真实 MCP
-> 空闲后关闭真实 MCP
```

这个模式可以避免 `context7`、`playwright`、`mastergo` 等真实 MCP 长时间占用内存。

但当本机同时启动多个 Codex CLI 会话时，每个 Codex CLI 仍然会启动一套 wrapper：

```text
Codex CLI A -> wrapper A -> real MCP A
Codex CLI B -> wrapper B -> real MCP B
Codex CLI C -> wrapper C -> real MCP C
```

wrapper 本身很轻，但运行态状态仍然被复制了多份。多个会话同时触发同一个 MCP 时，也可能重复拉起真实 MCP。

## 要解决的痛点

共享 daemon 模式主要解决这些问题：

1. 多会话重复管理 MCP 生命周期  
   每个 Codex CLI 都独立维护 wrapper、真实 MCP 进程、超时和日志。

2. 真实 MCP 可能重复启动  
   多个 Codex CLI 同时触发 `context7` 时，可能同时启动多个 `@upstash/context7-mcp`。

3. 运行态状态分散  
   工具列表缓存虽然可以落盘共享，但真实 MCP 进程、空闲计时、请求日志仍然分散在不同 wrapper 进程中。

4. 排查困难  
   多会话并发时，不容易判断哪个会话触发了真实 MCP、真实 MCP 什么时候启动、什么时候回收。

5. 全局空闲判断不准确  
   单个 wrapper 只能知道自己是否空闲，无法知道其他 Codex CLI 是否还在使用同一个 MCP。

## 目标架构

共享 daemon 模式拆成两类进程：

```text
多个 Codex CLI
-> 各自启动很薄的 stdio client
-> 连接同一个 lazy-mcp daemon
-> daemon 统一管理共享 MCP Proxy
-> Proxy 按需启动真实 MCP
```

目标形态：

```text
Codex CLI A -> lazy-mcp-wrapper client context7 ┐
Codex CLI B -> lazy-mcp-wrapper client context7 ├-> lazy-mcp-wrapper daemon -> real context7
Codex CLI C -> lazy-mcp-wrapper client context7 ┘
```

Codex 仍然只需要 stdio MCP server，所以 `client` 进程必须保留。但 client 只做协议转发，不再独立管理真实 MCP。

## 普适性

适合共享真实实例的 MCP：

- 文档查询类 MCP，例如 `context7`
- 设计稿读取类 MCP，例如 `mastergo`
- 纯查询、只读、无会话状态的 MCP
- 远程 API 封装类 MCP
- 数据库 schema 查询类 MCP

不适合第一阶段直接共享真实实例的 MCP：

- 浏览器自动化，例如 `playwright`
- 依赖页面、登录态、浏览器上下文的 MCP
- 会修改本地文件或远程状态的 MCP
- 强依赖当前项目 `cwd` 或会话上下文的 MCP
- 需要和单个 Codex 会话绑定的 MCP

因此第一阶段只共享无状态或近似无状态 MCP。

## 第一阶段范围

第一阶段实现：

- `daemon` 模式：启动一个本机 Unix domain socket 服务。
- `client` 模式：Codex 启动的 stdio MCP server，连接 daemon 并转发 JSON-RPC 消息。
- daemon 内按 MCP name 维护共享 `Proxy`。
- 支持共享 `context7`。
- 支持共享 `mastergo-magic-mcp`。
- `playwright` 暂不进入共享 daemon，继续使用当前每会话懒加载模式。
- 保留现有直接 wrapper 模式，避免破坏已有 Codex 配置。

第一阶段不实现：

- 多用户隔离。
- HTTP API 或管理面板。
- Playwright 会话隔离。
- 请求级权限控制。

## 命令设计

保留原有命令：

```bash
lazy-mcp-wrapper --config ./examples/context7.json
```

新增 daemon：

```bash
lazy-mcp-wrapper daemon \
  --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock \
  --config ./examples/context7.json \
  --config ./configs.local/mastergo-magic-mcp.json
```

也可以使用 daemon 配置文件：

```json
{
  "socket": "/Users/binlee/.lazy-mcp-wrapper/lazy-mcpd.sock",
  "configs": [
    "/Users/binlee/code/open-source/lazy-mcp-wrapper/examples/context7.json",
    "/Users/binlee/code/open-source/lazy-mcp-wrapper/configs.local/mastergo-magic-mcp.json"
  ]
}
```

启动：

```bash
lazy-mcp-wrapper daemon --daemon-config ~/.lazy-mcp-wrapper/config.json
```

新增 client：

```bash
lazy-mcp-wrapper client \
  --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock \
  --name context7
```

查看 daemon 状态：

```bash
lazy-mcp-wrapper status \
  --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock
```

状态输出包含：

- daemon pid。
- daemon 启动时间和运行时长。
- 当前连接 client 数。
- 已转发调用数。
- 最近错误。
- 已注册 MCP 名称。
- 真实 MCP 是否已启动。
- 真实 MCP pid。
- 最近使用时间。

停止 daemon：

```bash
lazy-mcp-wrapper stop \
  --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock
```

如果 daemon 由 LaunchAgent 管理，launchd 会按已安装配置重新拉起。

热重载命令预留如下：

```bash
lazy-mcp-wrapper reload \
  --socket ~/.lazy-mcp-wrapper/lazy-mcpd.sock
```

第一阶段还不支持热重载，命令会返回明确错误。配置变更后用 `make install-agent` 重新安装并重启 LaunchAgent。

Codex 配置示例：

```toml
[mcp_servers.context7]
type = "stdio"
command = "/Users/binlee/.local/bin/lazy-mcp-wrapper"
args = ["client", "--socket", "/Users/binlee/.lazy-mcp-wrapper/lazy-mcpd.sock", "--name", "context7"]
```

MasterGo：

```toml
[mcp_servers.mastergo-magic-mcp]
type = "stdio"
command = "/Users/binlee/.local/bin/lazy-mcp-wrapper"
args = ["client", "--socket", "/Users/binlee/.lazy-mcp-wrapper/lazy-mcpd.sock", "--name", "mastergo-magic-mcp"]
```

Playwright 暂时保持：

```toml
[mcp_servers.playwright]
type = "stdio"
command = "/Users/binlee/.local/bin/lazy-mcp-wrapper"
args = ["--config", "/Users/binlee/code/open-source/lazy-mcp-wrapper/examples/playwright.json"]
```

## 协议设计

client 和 daemon 之间使用 Unix domain socket。

socket 连接建立后，client 先发送一行绑定请求：

```json
{"name":"context7"}
```

daemon 根据 `name` 找到对应共享 `Proxy`。

绑定成功后，后续 socket 内容就是 MCP JSON-RPC 流。client 不理解具体 MCP 方法，只负责双向拷贝：

```text
Codex stdin  -> daemon socket
daemon socket -> Codex stdout
```

daemon 侧把 socket 当作一个 MCP client 连接，调用对应 `Proxy.Run(ctx, conn, conn)`。

## 日志

daemon 继续使用每个 MCP 配置里的 `log_file`。

第一阶段日志粒度：

- daemon 启动。
- MCP name 注册。
- client 连接和断开。
- client 请求方法。
- 真实 MCP 启动、调用、退出继续沿用现有 `Proxy` 日志。

## 失败处理

client 连接 daemon 失败时：

- 向 stderr 输出明确错误。
- 退出非 0。

daemon 找不到指定 MCP name 时：

- 向 client 返回错误。
- 关闭连接。

第一阶段不做自动 fallback 到直接 wrapper。原因是 fallback 会掩盖 daemon 未启动的问题，排查更困难。

## macOS LaunchAgent

可以用用户级 LaunchAgent 管理 daemon：

```bash
make install-agent
```

默认安装信息：

```text
label:  com.binlee.lazy-mcp-wrapper
plist:  ~/Library/LaunchAgents/com.binlee.lazy-mcp-wrapper.plist
config: ~/.lazy-mcp-wrapper/config.json
socket: ~/.lazy-mcp-wrapper/lazy-mcpd.sock
logs:   ~/Library/Logs/lazy-mcp-wrapper
```

卸载：

```bash
make uninstall-agent
```

安装脚本会把当前 shell 的 `PATH` 写进 plist。这个细节很重要：LaunchAgent 的默认 `PATH` 通常只有系统目录，直接启动 daemon 时可能找不到 `npx`。

## 后续阶段

第二阶段可以考虑：

- 热重载 daemon 配置。
- client id。
- MCP 级别请求数和错误数。
- Playwright session 隔离。
- 与其他 MCP 客户端共享，不局限于 Codex。
