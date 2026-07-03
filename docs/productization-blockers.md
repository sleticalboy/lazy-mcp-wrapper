# 产品化阻碍清单

## P0：会直接让新用户放弃的

### 1. PATH 问题（已修复：见 git log）
daemon 作为系统服务运行时，PATH 是 `setup` 时通过 `os.Getenv("PATH")` 捕获的 shell PATH。
`npx`、`uvx` 等工具通常通过 nvm/pyenv/mise 安装在 `~/.nvm/versions/node/.../bin` 等路径下，不在系统 PATH 里。
用户 `setup --yes` 成功，实际调工具时报 `command not found`，完全不知道原因。

**修复方案**：`defaultLaunchAgentPlan` 里在 `os.Getenv("PATH")` 基础上追加常见工具路径（nvm、pyenv、mise、homebrew、cargo、go 等），确保 daemon 能找到用户 shell 环境里的工具。

**文件**：`internal/setup/setup.go:441`

---

### 2. 首次使用没有验证步骤
setup 完成后用户不知道链路是否真正通。缺少 `setup verify` 命令，对每个已包装的 MCP server 发一个 `tools/list` 请求，验证 daemon → realClient → tools/list 全链路。

**修复方案**：新增 `lazy-mcp-wrapper setup verify` 子命令，连接 daemon socket，对每个 server 发 `tools/list`，输出结果摘要（server 名、tool 数量、耗时）。

**文件**：`cmd/lazy-mcp-wrapper/main.go`、`internal/setup/verify.go`（新建）

---

## P1：影响留存的

### 3. 错误信息不够可操作
真实 MCP server 启动失败时用户只看到 `failed to start`，没有下一步指引。

**修复方案**：失败时明确输出日志文件路径（`tail -f <log>` 提示），让用户知道去哪里看详情。

**文件**：`internal/wrapper/proxy.go`（startReal 失败路径）

---

### 4. 升级路径不明确
`setup update` 存在但用户不知道。二进制更新后 daemon 不会自动重启，wrapper config 格式变化会静默失败。

**修复方案**：README 补充升级流程说明；`setup update` 完成后自动提示 reload daemon（或自动 reload）。

---

### 5. HTTP/SSE 未对接真实远程 server 验证
整个 HTTP/SSE 链路只对 fake-mcp 测试过，从未对接真实远程 MCP server（context7、Smithery 等）。存在协议细节问题的风险。

**修复方案**：手动冒烟一次真实 HTTP/SSE server；补充集成测试用 `httptest` 覆盖 Streamable HTTP 完整握手流程。

---

## P2：规模化后才会暴露的

### 6. 没有崩溃上报
daemon 静默挂掉时，用户只知道工具不好使，不会来报 issue。

**修复方案**：daemon 退出时把 panic stacktrace 写入日志文件；考虑接入轻量崩溃上报（如写本地文件 + `setup status` 展示）。

### 7. config 格式没有版本号
`config.json` 没有 schema version 字段，将来格式变了没有迁移路径。

**修复方案**：`wrapper.Config` 加 `SchemaVersion int` 字段，`LoadConfig` 检测版本并做兼容处理。
