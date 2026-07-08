# MCP Hub 借鉴落地计划

## 背景

`ravitemer/mcp-hub` 是完整 MCP 网关：客户端只连一个 `/mcp` endpoint，Hub 聚合所有被管理 MCP server 的工具、资源和提示词能力，自动给能力命名空间化，提供管理 API，监听配置变化，并承接远程 MCP 的 OAuth 流程。

`lazy-mcp-wrapper` 的产品边界更窄：每个 MCP server 仍作为独立 client 配置存在，工具名保持原样，真实 MCP server 延迟启动，`tools/list` 默认走缓存，并且 Playwright 这类有状态 server 使用 `sharing: "session"` 保持会话隔离。

结论：借鉴 MCP Hub 的运维和管理能力，但不把本项目改成单入口聚合 Hub。

## 产品决策

继续保留当前 wrapper 模型：

- 不把多个 MCP server 聚合成默认单 `/mcp` endpoint。
- 不默认给所有工具名加命名空间。
- 不在 daemon 启动时拉起所有 enabled MCP server。
- 不全局共享有状态 MCP server。
- Playwright 类和 OAuth 远程 MCP 继续默认使用 `sharing: "session"`。

只吸收能增强当前模型的能力：配置兼容、缓存正确性、setup 诊断、低影响 reload。

## 推荐落地项

### 1. Client Config Compatibility

目标：让 `setup` 能理解更多真实世界里的 MCP 配置文件，同时不改变 wrapper 运行时模型。

当前状态：进行中。已完成 `mcpServers` / `servers` 双 key 读取与按原 key 写回；JSON 写回 wrapper ref 时会保留原 server 上未知字段；读取支持 JSON-with-comments 和 trailing comma，存在改写需求时会明确报错而不是静默丢注释；已支持安全占位符解析并拒绝 `${cmd: ...}`。

范围：

- JSON client config 同时接受 `mcpServers` 和 VS Code 风格的 `servers` key。（已完成）
- 写回 JSON config 时保留未知的 top-level 字段和 per-server 字段。（已完成）
- 在安全可 round-trip 的前提下支持 JSON-with-comments，包括注释和 trailing comma。（读取已完成；改写时先阻断）
- 在显式命令里支持多个 config 输入，按后者覆盖前者的规则 merge。
- 解析扫描配置里的常见占位符：
  - `${env:VAR}`（已完成）
  - `${VAR}`（已完成）
  - `${userHome}`（已完成）
  - `${workspaceFolder}`，仅在能确定稳定 workspace root 时启用（全局扫描中已明确拒绝）
  - `${/}` 或 `${pathSeparator}`（已完成）

非目标：

- 自动 `setup` 扫描时默认不执行 `${cmd: ...}`。命令替换可以执行任意代码，如果后续要做，必须显式 opt-in。（已完成）
- 除非文件本来就要因为 wrapping 被更新，否则不把配置重写成另一种风格。

验收：

- 现有 JSON config 测试继续通过。
- 新增测试覆盖 `servers`、`mcpServers`、JSON-with-comments（如支持）、未知字段保留和占位符解析。
- `setup --dry-run` 遇到无法解析的占位符时给出明确提示，而不是静默生成坏的 wrapper config。

### 2. Cache Invalidation and Cache Boundaries

目标：保留 lazy startup 收益，同时避免工具列表缓存过期。

当前状态：已完成。`notifications/tools/list_changed` 会失效对应 `tools/list` cache，`CacheInfo` 会展示 notification invalidation 状态；resources/prompts 继续实时转发，不默认缓存。

范围：

- 真实 MCP server 发送 `notifications/tools/list_changed` 时，先清理该 server 的 `tools/list` cache，再把通知转发给客户端。（已完成）
- cache 状态里增加字段，展示 cache 是否曾因 server notification 被失效。（已完成）
- 现阶段只默认缓存 `tools/list`。（已完成）
- 文档明确 resources 和 prompts 默认实时转发，除非后续显式增加缓存。（已完成）

非目标：

- 本阶段不默认缓存 `resources/list`、`resources/read` 或 `prompts/list`。这些接口更可能依赖用户数据或 workspace 状态。

验收：

- proxy 测试先写入 tools cache，再模拟 `notifications/tools/list_changed`，验证 cache 被删除。
- cache 失效后的下一次 `tools/list` 会访问真实 MCP server 并刷新 cache。
- 通知仍然能到达已连接客户端。

### 3. Structured Setup Diagnostics

目标：让 `setup` 和 `setup --dry-run` 清楚解释每个 server 为什么被包装、跳过或保持直连。

范围：

- 为 plan 决策增加结构化 reason code：
  - `wrapped-stdio`
  - `wrapped-local-http`
  - `wrapped-explicit-auth`
  - `wrapped-auth-none`
  - `wrapped-oauth-credential`
  - `skipped-node-repl`
  - `skipped-stateful-direct`，仅在未来需要该规则时使用
  - `skipped-url-only-remote`
  - `skipped-oauth-missing-credential`
  - `skipped-figma-dynamic-client-rejected`
  - `skipped-chatgpt-auth`
  - `skipped-existing-wrapper`
  - `skipped-invalid-config`
- dry-run 输出展示这些原因。
- 保留现有人类可读 blocker，但尽量从同一份结构化 reason 派生。
- 只有在能复用同一 plan model 时，再考虑增加机器可读 JSON 输出。

验收：

- Figma、ChatGPT-auth remote、URL-only remote、Playwright、node_repl、已包装 server 的 dry-run 输出都有明确原因。
- 测试断言 reason code，而不是只断言自由文本片段。
- `setup` 继续保持当前保守策略。

### 4. Lower-Impact Daemon Reload

目标：daemon 配置变化时，避免替换未变化的 proxy。

范围：

- 为每个 MCP wrapper config 计算稳定 fingerprint。
- reload 时，如果 name 和 fingerprint 都未变化，保留现有 proxy instance。
- 对新增或配置变化的 server 启动新 proxy。
- 对移除或配置变化的旧 proxy，按现有 reload 模式关闭：
  - 默认模式：有活跃 client 时仍返回 busy。
  - `--graceful`：新 client 使用新 generation，旧活跃 client 自然 drain。
  - `--force`：立即关闭旧真实 MCP 进程。
- 未变化 proxy 的 runtime stats 保留。

验收：

- reload 测试证明未变化的 shared proxy 保持真实 MCP 进程和统计数据。
- 配置变化的 server 生成新的 proxy generation。
- 被移除的 server 在 reload 后从 status 消失。
- 现有 busy、graceful、force 语义不变。

## 低优先级

这些能力有价值，但不需要排在推荐落地项之前：

- 本地只读 HTTP status API 或 SSE event stream，用于外部 dashboard。
- OAuth UX 打磨，例如更友好的 callback 页面和更清晰的 `auth status` 输出。
- Marketplace 或 registry 集成。

## 不建议做

除非产品方向明确改变，否则不做：

- 默认单 `/mcp` 聚合入口。
- 默认工具命名空间化。
- daemon 启动时拉起所有 enabled MCP server。
- 全局共享有状态 MCP server。
- 自动 config 扫描时执行命令占位符。

## 建议执行顺序

1. Client config compatibility。
2. Cache invalidation and cache boundary documentation。
3. Structured setup diagnostics。
4. Lower-impact daemon reload。

每个阶段结束时都要输出：

- 针对变更行为的测试结果。
- `go test ./...` 结果。
- 当前阶段状态。
- 下一阶段计划和第一项具体任务。
