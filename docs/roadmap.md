# lazy-mcp-wrapper Roadmap

## 方向一：分发（Homebrew tap）

**目标**：让"发现 → 安装 → setup"变成五分钟的事。

- [x] **1. 版本号嵌入**：Makefile 加 `ldflags -X main.version`，`--version` 输出版本号，建立 git tag 规范（`v0.x.x`）
- [x] **2. 跨平台发布构建**：支持 `darwin/arm64`、`darwin/amd64`、`linux/amd64`，每个打包成 `.tar.gz`，Makefile 加 `dist` target
- [ ] **3. GitHub Actions Release 流程**：push tag 触发，三平台构建 → sha256 → 上传 GitHub Release assets
- [ ] **4. 新建 homebrew-tap 仓库**：`binlee/homebrew-tap`，写 `lazy-mcp-wrapper.rb` Formula（url、sha256、install、test）
- [ ] **5. Formula 自动更新**：Release 后 Actions 自动给 homebrew-tap 提 PR 更新 sha256
- [ ] **6. README 打磨**：醒目的 Install 段落（三行 brew 命令）、"What it does" 说明、setup 截图/录屏

---

## 方向二：setup 反向操作（uninstall / update）

**目标**：让 setup 形成完整闭环，用户能干净地升级和卸载。

- [x] **1. `setup status`**：检查当前安装状态，展示哪些 client 已经 wrap、daemon 是否在跑
- [x] **2. `setup uninstall`**：还原各 client 配置（从 backup 恢复或移除 wrapper 引用）、停止并卸载 LaunchAgent、可选删除 wrapper config 文件
- [x] **3. `setup update`**：重新扫描各 client 配置，检测新增/移除的 MCP server，diff 展示变化，按需更新 wrapper config 和 daemon config，reload daemon

---

## 方向三：HTTP/SSE 协议支持

**目标**：覆盖 stdio 之外的 MCP server，扩大工具适用范围。

- [ ] **1. HTTP/SSE lazy proxy**：本地监听一个端口，收到请求时才启动真实 HTTP/SSE server（或按需转发），空闲超时后停止
- [ ] **2. setup 支持 HTTP/SSE server 检测**：`RawServer.Type` 已预留，打开 `IsWrappable` 开关，生成对应 wrapper config
- [ ] **3. daemon 支持 HTTP/SSE server 管理**：daemon 统一管理 stdio + http/sse server 的生命周期和状态展示
- [ ] **4. 测试覆盖**：fake-mcp 支持 HTTP/SSE 模式，补充对应集成测试
