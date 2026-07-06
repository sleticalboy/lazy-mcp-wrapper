# lazy-mcp-wrapper Roadmap

## 方向一：分发（Homebrew tap）

**目标**：让"发现 → 安装 → setup"变成五分钟的事。

- [x] **1. 版本号嵌入**：Makefile 加 `ldflags -X main.version`，`--version` 输出版本号，建立 git tag 规范（`v0.x.x`）
- [x] **2. 跨平台发布构建**：支持 `darwin/arm64`、`darwin/amd64`、`linux/amd64`，每个打包成 `.tar.gz`，Makefile 加 `dist` target
- [x] **3. GitHub Actions Release 流程**：push tag 触发，三平台构建 → sha256 → 上传 GitHub Release assets
- [x] **4. 新建 homebrew-tap 仓库**：`sleticalboy/homebrew-tap`，写 `lazy-mcp-wrapper.rb` Formula（url、sha256、install、test）
- [x] **5. Formula 自动更新**：Release 后 Actions 自动给 homebrew-tap 提 PR 更新 sha256（需配置 `HOMEBREW_TAP_TOKEN`）
- [x] **6. README 打磨**：醒目的 Install 段落（三行 brew 命令）、"What it does" 说明、setup 截图/录屏

---

## 方向二：setup 反向操作（uninstall / update）

**目标**：让 setup 形成完整闭环，用户能干净地升级和卸载。

- [x] **1. `setup status`**：检查当前安装状态，展示哪些 client 已经 wrap、daemon 是否在跑
- [x] **2. `setup uninstall`**：还原各 client 配置（从 backup 恢复或移除 wrapper 引用）、停止并卸载 LaunchAgent、可选删除 wrapper config 文件
- [x] **3. `setup update`**：重新扫描各 client 配置，检测新增/移除的 MCP server，diff 展示变化，按需更新 wrapper config 和 daemon config，reload daemon

---

## 方向三：HTTP/SSE 协议支持

**目标**：覆盖 stdio 之外的 MCP server，扩大工具适用范围。

- [x] **1. HTTP/SSE lazy proxy（阶段一）**：`realHTTPClient` + `ProxyHTTPServer`，支持旧版 SSE 和 Streamable HTTP，`realBackend` 接口抽象 stdio/http 两条路径，Config 扩展 `url/protocol/headers` 字段
- [x] **2. setup 支持 HTTP/SSE server 检测（阶段二）**：打开 `isWrappable` 的 HTTP/SSE 开关，`buildWrapperConfig` 生成 HTTP 类型 config，`replaceWithWrapperRefs` 替换为本地 wrapper 地址
- [x] **3. daemon 支持 HTTP/SSE server 管理（阶段二）**：daemon 启动 `ProxyHTTPServer` 并管理其生命周期，状态展示包含 HTTP server 地址
- [x] **4. 端口分配**：setup 自动分配本地监听端口（从 54300 起递增检测可用性），写入 Config `local_port` 字段

---

## 方向五：官方 Go SDK 与远程 OAuth MCP

**目标**：降低 Streamable HTTP 和 OAuth 远程 MCP 的协议维护成本，同时保留 lazy proxy 的核心行为。详见 [go-sdk-oauth-plan.md](go-sdk-oauth-plan.md)。

- [x] **0. SDK 适配探针**：引入官方 Go SDK 的最小编译/测试探针，不改变现有运行行为
- [x] **1. SDK-backed remote HTTP upstream**：已增加 opt-in SDK Streamable HTTP backend，保留当前 stdio proxy、daemon、setup 模型
- [x] **2. OAuth-aware remote HTTP upstream 基础**：已增加 `auth login/logout/status`、文件凭据存储、Bearer 注入、刷新、setup 凭据门控
- [ ] **3. Figma 验证**：动态 client registration 已实测 403；下一步验证 Codex-style `oauth.client_id` 预注册 client 是否可用，并决定默认策略

---

## 方向四：Windows 支持

**目标**：覆盖 Windows 开发者，扩大用户群。详见 [plan-windows.md](plan-windows.md)。

- [x] **优先级一：构建 + 核心运行**：条件编译修复 `syscall.SIGTERM` 和进程信号问题，Makefile + GitHub Actions 加 `windows/amd64` 构建，Release 上传 `.exe` + `.zip`
- [x] **优先级二：路径跨平台**：新增 `paths.go` 封装跨平台路径，AI client 配置路径按平台适配，Windows 下跳过 LaunchAgent 并提示手动启动，setup 命令在 Windows 可用
- [x] **优先级三：Windows 系统服务**：`setup` 一键安装 daemon 为 Windows Service，Scoop/winget 分发渠道
