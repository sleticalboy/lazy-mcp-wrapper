# 产品化阻碍清单

## P0：会直接让新用户放弃的

### 1. PATH 问题 ✅ 已修复（702806e）
daemon 作为系统服务运行时，PATH 是 `setup` 时通过 `os.Getenv("PATH")` 捕获的 shell PATH。
`npx`、`uvx` 等工具通常通过 nvm/pyenv/mise 安装在 `~/.nvm/versions/node/.../bin` 等路径下，不在系统 PATH 里。

**修复**：`enrichPATH()` 在系统 PATH 基础上追加 nvm 各版本 bin、pyenv、mise、asdf、Homebrew、Cargo、Go、`~/.local/bin`。

---

### 2. 首次使用没有验证步骤 ✅ 已修复（702806e）
setup 完成后用户不知道链路是否真正通。

**修复**：新增 `lazy-mcp-wrapper setup verify`，对每个已注册 server 发 `tools/list`，输出 server 名、tool 数量、耗时，失败时 exit 1。setup 完成后自动提示用户运行 verify。

---

## P1：影响留存的

### 3. 错误信息不够可操作 ✅ 已修复（8d1dda5）
真实 MCP server 启动失败时用户只看到 `failed to start`，没有下一步指引。

**修复**：`ensureStdioReal` / `ensureHTTPReal` 失败时错误信息附带 `check logs: tail -f <path>`。

---

### 4. 升级路径不明确 ✅ 已修复（8d1dda5）
`setup update` 存在但用户不知道；二进制更新后 daemon 不自动重启。

**修复**：`setup update` 已自动 graceful reload daemon；setup 完成后提示 `setup verify`。README 可进一步补充升级流程（待做）。

---

### 5. HTTP/SSE 未对接真实远程 server 验证 ⚠️ 部分解决
SSE（HTTP+SSE）已在 `69f7844` 中完全移除，不再是问题。

**残余风险**：`streamable-http` 协议从未对接过真实远程 MCP server，存在协议细节风险。

**待做**：手动冒烟一次真实 Streamable HTTP MCP server（非代码任务）。

---

## P2：规模化后才会暴露的

### 6. 没有崩溃上报 ✅ 已修复（f57c7d8）
daemon 静默挂掉时，用户只知道工具不好使，不会来报 issue。

**修复**：`handleConn` 每个连接 goroutine 加 `recover()`，panic 写入 `~/.lazy-mcp-wrapper/panic.log`（追加模式）；`setup status` 检测到 panic.log 时输出 WARNING 提示。

---

### 7. config 格式没有版本号 ✅ 已修复（8d1dda5）
`config.json` 没有 schema version 字段，格式变化时无迁移路径。

**修复**：`Config` 加 `SchemaVersion int` 字段，setup 生成的 config 写入版本 1，`LoadConfig` 检测到更新版本时给出升级提示而非静默失败。
