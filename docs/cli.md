# CLI 命令参考

zzauto 是单二进制 CLI，主入口在 `cmd/zzauto/main.go`，版本号常量 `Version = "v0.1.0"`。

## 用法

```
zzauto [command]
```

命令：

| 命令 | 说明 |
| --- | --- |
| `serve` | 启动 HTTP 服务与编排器（默认） |
| `uninstall` | 移除二进制与配置（保留项目数据） |
| `upgrade` | 从 GitHub releases 升级 zzauto 二进制 |
| `version` | 打印版本号 |
| `-h` / `--help` / `help` | 打印用法 |

**默认行为**：不带任何子命令（直接 `zzauto`）等同 `serve`。未知子命令打印用法并以退出码 2 退出。

> 配置见 [configuration.md](./configuration.md)；上手见 [quickstart.md](./quickstart.md)。

---

## serve

启动 HTTP 服务与编排器。

### 用法

```
zzauto serve [--listen <addr>] [--no-auto-install]
zzauto              # 等同 serve
```

### 参数

| Flag | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--listen` | string | `""`（用配置值） | 监听地址（覆盖配置 `listen`，格式 `host:port`） |
| `--no-auto-install` | bool | `false` | aiclibridge 不可达时不自动安装，仅提示安装命令并退出（跳过自动安装流程） |

### 行为

1. 解析 flag；`config.Load()` 加载 `zzauto.yaml` + 环境变量；`--listen` 非空则覆盖 `cfg.Listen`。
2. 创建事件总线 `eventbus.New()`。
3. `workspace.NewProject(cfg.WorkspaceDir)` 生成新 projectID 并创建工作区，`EnsureDirs()` 建目录。
4. **aiclibridge 健康检查**：`aicli.New(cfg.AicliAddr, cfg.AicliKey).Health(ctx)`（5 秒超时）。
   - 可达：继续启动。
   - 不可达且 `--no-auto-install=true`：打印日志与安装提示，`os.Exit(1)` 退出。
   - 不可达且 `--no-auto-install=false`（默认）：调用 `aicli.EnsureInstalled` 自动安装（macOS/Linux 走 `curl -fsSL ... | sh`，Windows 走 `irm ... | iex`），安装后每 2 秒轮询健康检查、最长等待 30 秒。成功后继续启动；失败时打印失败原因与手动安装命令并以 `os.Exit(1)` 退出。
5. 创建 UI handler，把 `handler.AskUser` 适配为 `agents.AskFunc`（供 Asker 提问）。
6. `registry.BuildOrchestrator` 装配编排器：创建 aicli 客户端、gittor、`gitClient.EnsureRepo` 初始化 git 仓库、注册全部 9 个 agent。
7. 注册 HTTP 路由：`GET /healthz`（返回 `zzauto running`）+ UI 全部路由。
8. `signal.NotifyContext` 监听 `SIGINT`/`SIGTERM`，用于取消 context。
9. 后台 goroutine 执行 `orch.Run(runCtx)`；主 goroutine `http.ListenAndServe(cfg.Listen, mux)`。
10. 成功启动打印 `zzauto v0.1.0 监听 <addr>`。

### 退出码

- 配置加载失败：`log.Fatalf`，非 0。
- 工作区目录创建失败：`log.Fatalf`，非 0。
- aiclibridge 不可达且 `--no-auto-install=true`：`os.Exit(1)`。
- aiclibridge 自动安装失败（30 秒健康轮询未通过或安装脚本出错）：`os.Exit(1)`。
- 编排器装配失败（如 git 仓库初始化失败）：`log.Fatalf`，非 0。
- HTTP 服务退出：`log.Fatalf`，非 0。
- 收到 `SIGINT`/`SIGTERM`：context 取消，编排器退出，HTTP 服务随后退出。

### HTTP 路由（serve 暴露）

| 方法 路径 | 说明 |
| --- | --- |
| `GET /` | 首页 index.html |
| `GET /static/` | 静态资源（app.js / style.css） |
| `GET /healthz` | 健康检查，返回 `zzauto running` |
| `GET /api/state` | 流程状态（9 agent 的 pending/running/done/failed） |
| `GET /api/docs/{name}` | 读取文档（desire/need/spec/deal/task），返回 raw/body/meta |
| `POST /api/input` | 提交用户原始需求，写入 `input.md` |
| `GET /api/asks` | 待回答问题列表 |
| `POST /api/ask/{id}` | 回答指定问题 |
| `POST /api/github` | 配置 GitHub（内存更新） |
| `GET /api/config` | 读取配置（token 脱敏） |
| `GET /api/events` | SSE 事件流 |

---

## uninstall

移除 zzauto 二进制与配置文件，**保留** `workspace/projects` 项目数据。

### 用法

```
zzauto uninstall
```

### 行为（`internal/installer/installer.go`）

1. 获取当前二进制路径（`os.Executable` + 解析符号链接），删除之；不存在则提示（可能通过 `go run` 运行）。
2. 删除配置：
   - `./zzauto.yaml`（当前目录下）；
   - `~/.zzauto`（用户主目录下，若存在）。
3. 打印已删除文件/目录清单。
4. 出现错误时累计，最终若有多处失败返回错误并以退出码 1 退出。

### 退出码

- 成功：0。
- 卸载过程出现错误：1（仍会打印已删除项）。

> 项目数据（`workspace/projects/`）不会被删除，可手动清理。

---

## upgrade

从 GitHub releases 直链下载最新版并原子替换当前二进制。

### 用法

```
zzauto upgrade
```

### 行为（`internal/installer/installer.go`）

1. 打印当前版本（`installer.CurrentVersion`，由 main 包覆写为 `Version`）。
2. **获取最新版本号**：请求 `https://github.com/tgcz2011/zzauto/releases/latest`（不跟随重定向，捕获 302 的 `Location` 头解析 tag 名）。**不调用 gh api**，避免频率限制。URL 受 `GITHUB_MIRROR` 前缀影响。
3. 若最新版本等于当前版本，打印「已是最新版本」并返回。
4. 探测平台（`runtime.GOOS`/`GOARCH`），构造资产名：darwin/linux → `zzauto-<os>-<arch>.tar.gz`，windows → `zzauto-windows-amd64.zip`。不支持的平台返回 `ErrUnsupportedPlatform`。
5. 下载压缩包到临时目录。
6. 下载 `.sha256` 校验文件并校验（获取失败则警告并跳过校验）。
7. 从压缩包提取二进制到与当前二进制同目录的临时文件 `.zzauto.new`，`chmod 0755`。
8. **原子替换**：备份当前二进制为 `*.bak` → `rename` 新二进制到目标路径 → 删除备份；替换失败则回滚。
9. 打印 `升级完成: <旧版本> -> <新版本>`。
10. **同步升级 aiclibridge**：调用 `aicli.UpgradeAiclibridge`（执行 `aiclibridge upgrade` 子命令）。失败仅打印 `警告: aiclibridge 同步升级失败: <err>`，不阻塞 zzauto 升级结果。aiclibridge 不在 PATH 时同样仅警告。此步骤在「已是最新版本」与「实际升级」两条路径上都会执行。

### 退出码

- 成功（含已是最新，且 aiclibridge 同步升级无论成败）：0。
- zzauto 自身升级失败（网络/校验/平台/替换）：1。

> **注意**：aiclibridge 同步升级是 best-effort，失败仅警告不改变退出码。如需单独手动升级 aiclibridge，参考 [aiclibridge.md](./aiclibridge.md) 的「关于同步升级」一节。

---

## version

打印版本号。

### 用法

```
zzauto version
```

### 行为

输出 `Version` 常量，当前为 `v0.1.0`。

> GitHub Actions 构建时通过 `-ldflags "-X main.Version=${GITHUB_REF_NAME}"` 覆盖该值，故 release 二进制的版本号对应 git tag。`go install` / `go run` 场景下为源码常量值。

### 退出码

- 始终 0。

---

## 示例

```sh
zzauto version                          # v0.1.0
zzauto                                  # 等同 serve
zzauto serve                            # 默认 127.0.0.1:8788，aiclibridge 不可达时自动安装
zzauto serve --listen 0.0.0.0:8788      # 监听所有网卡
zzauto serve --no-auto-install          # 跳过 aiclibridge 自动安装，仅提示
ZZAUTO_LISTEN=0.0.0.0:8788 zzauto serve # 用环境变量覆盖
zzauto upgrade                          # 升级到最新 release，并尝试同步升级 aiclibridge
zzauto uninstall                        # 卸载（保留项目数据）
```

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `cmd/zzauto/main.go` | CLI 入口与子命令分发 |
| `internal/installer/installer.go` | `uninstall` / `upgrade` 实现 |
| `internal/config/config.go` | `serve` 加载的配置 |
| `internal/ui/handler.go` | `serve` 暴露的 HTTP 路由 |
| `scripts/install.sh` / `install.ps1` | 一键安装脚本（非 CLI 子命令） |
