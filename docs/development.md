# 开发指南

本文面向 zzauto 的贡献者：环境要求、构建与测试、目录结构、添加新 agent、提交规范、文档同步与 release 流程。

> 贡献流程详见仓库根目录 `CONTRIBUTING.md`；发版流程见 `RELEASE.md`。架构与 agent 详解见 [architecture.md](./architecture.md) 与 [agents.md](./agents.md)。

---

## 环境要求

- **Go**：1.26+（`go.mod` 声明 `go 1.26.4`）。
- **git**：开发与 Gittor 提交均依赖。
- **aiclibridge**：本地跑通流程需要 aiclibridge（见 [aiclibridge.md](./aiclibridge.md)）；纯单元测试可用注入的 mock `AIClient`，无需真实 aiclibridge。
- **操作系统**：macOS / Linux / Windows。CI/release 产物覆盖 darwin-amd64/arm64、linux-amd64/arm64、windows-amd64。

---

## 构建

```sh
# 编译全部包（验证可构建）
go build ./...

# 生成可执行二进制到当前目录
go build -o zzauto ./cmd/zzauto

# 运行
./zzauto version   # v0.4.0
./zzauto serve     # 前台启动（开发调试）
./zzauto start     # 后台 daemon 启动（生产）
./zzauto status    # 查看 daemon 状态
./zzauto stop      # 停止 daemon
```

> GitHub Actions 在 release 时通过 `-ldflags "-X main.Version=${GITHUB_REF_NAME}"` 覆盖 `main.Version`，使二进制版本号对应 git tag。本地 `go build` 产物版本号为源码常量 `v0.4.0`。

---

## 测试

```sh
# 全部测试
go test ./...

# 带竞态检测
go test -race ./...

# 静态检查
go vet ./...

# 格式化
gofmt -w .
```

测试要点：

- agent 测试使用注入的 mock `AIClient` 与本地 bare git 仓库，不依赖真实 AI 或远程仓库。参考 `internal/agents/*_test.go`。
- 编排器测试可通过 `SetMaxDiscussRounds` / `SetMaxEvalRetries` 缩小循环上限，加速用例。
- `registry.BuildOrchestratorWithDeps` 接受注入的 AI 与 git 客户端，便于测试。
- 端到端测试见 `internal/e2e/e2e_test.go`。

提交前确保 `go build ./...`、`go vet ./...`、`go test ./...` 全部通过。

---

## 目录结构

```
zzauto/
  cmd/zzauto/            # CLI 主入口（main.go）
  internal/
    agents/              # 9 个 agent 实现 + 接口/schema/哨兵错误 + 各 *_test.go
      agent.go           # Agent/AIClient/GittorClient 接口 + RunWithTracking（v0.3.0）
      schema.go          # 文档正文 schema 与常量
      errors.go          # ErrNoConsensus / ErrEvaluationFailed
      listener.go ... gittor_agent.go
    aicli/               # aiclibridge HTTP 客户端
      client.go          # Chat/Ask/AskWithModel/ChatWithModel/SetModel/Model/Models
      bootstrap.go       # EnsureInstalled（Health → lookPath → start daemon / install+start）+ StartDaemon
      runs.go            # v0.3.0 RunStream（SSE）+ GetRun
      stats.go           # v0.3.0 Usage/Prices/Summary/Concurrency
    config/              # 配置加载（zzauto.yaml + ZZAUTO_* env）+ Save + RoleModels
    daemon/              # v0.4.0 zzauto 自身 daemon 管理（Start/Stop/Restart/Status + PID 文件 + 日志重定向）
      daemon.go          # 跨平台核心
      daemon_unix.go     # Unix setsid + SIGTERM/SIGKILL
      daemon_windows.go  # Windows CREATE_NEW_PROCESS_GROUP + taskkill
    eventbus/            # 发布/订阅事件总线（含 agent_run_event）
    ghcli/               # v0.3.0 gh CLI 检测/安装提示/auth/Repos
    gittor/              # git CLI 隔离层
    installer/           # 自卸载与自升级
    orchestrator/        # 编排器与两个循环
    projects/            # v0.3.0 多项目 Registry + project.json 元数据
    registry/            # 组件装配（BuildOrchestrator / RegisterAgents 接收 roleModels）
    ui/                  # HTTP API + SSE + 内嵌 Web UI（4 页面 SPA）
      web/               # index.html / app.js / style.css
      embed.go           # embed 声明
      handler.go         # 含 projects/gh/settings/stats/runs 路由
    workspace/           # 工作区目录与文档协议（frontmatter 解析）
    e2e/                 # 端到端测试
  scripts/               # install.sh / install.ps1 一键安装脚本
  web/                   # （根目录 web/index.html，构建期资源以 internal/ui/web 为准）
  .github/               # workflows/release.yml、PR 模板
  .trae/specs/           # Trae 规格文档（spec/tasks/checklist）
  docs/                  # 本文档目录
  README.md / RELEASE.md / CONTRIBUTING.md / CHANGELOG.md
  go.mod / go.sum
```

### v0.3.0 新增/变更的内部包

- **`internal/projects`**：多项目注册表。`Registry` 持工作区根目录，提供 `List/Get/Create/Update/Delete/ProjectDir`。每项目对应 `<rootDir>/projects/<id>/project.json`，`ProjectMeta` 含 ID/Name/Repo/Branch/CreatedAt/UpdatedAt/Status/CurrentStage。`Create` 时建项目目录与 `agents/reports/runs/` 子目录、写空 `input.md`、写 `project.json`。详见 [multi-projects.md](./multi-projects.md)。
- **`internal/ghcli`**：gh CLI 封装。`EnsureInstalled`（`exec.LookPath`）+ `InstallHint`（按 `runtime.GOOS` 返回 macOS/Linux/Windows 安装命令）+ `AuthStatus`（`gh auth status` 退出码）+ `LoginHint` + `Repos`（`gh repo list --json`，未登录返回 `ErrNotAuthenticated`）。详见 [gh-integration.md](./gh-integration.md)。
- **`internal/aicli`**：v0.3.0 扩展 `ChatWithModel`/`AskWithModel`/`SetModel`/`Model`（每角色模型）、新增 `runs.go`（`RunStream` SSE + `GetRun`）与 `stats.go`（`Usage`/`Prices`/`Summary`/`Concurrency`）。
- **`internal/agents`**：v0.3.0 `AIClient` 接口扩展为 `Ask`/`AskWithModel`/`RunStream`/`GetRun` 四方法；新增 `RunWithTracking` 辅助函数封装 RunStream + 事件持久化（`<projectDir>/runs/<agent>/<runID>.json`）+ `agent_run_event` 事件广播。
- **`internal/config`**：v0.3.0 新增 `RoleModels map[string]string` 字段、`Save(path)` 方法、`ZZAUTO_ROLE_MODEL_<STAGE>` env 解析。
- **`internal/eventbus`**：v0.3.0 新增 `EventAgentRunEvent` 事件类型。
- **`internal/registry`**：v0.3.0 `RegisterAgents` 接收 `roleModels` 参数注入到各 agent；`BuildOrchestrator` 改为接收 `cfg/ws/bus/askFunc`，由调用方（UI handler）按项目装配。
- **`internal/ui`**：v0.3.0 `Handler` 持 `projects.Registry` 而非单个 workspace；`currentWS()` 按当前选中项目动态构造；新增 17 路由覆盖 projects/gh/settings/stats/runs；前端 SPA 改造为 4 页面切换。

### v0.4.0 新增/变更的内部包

- **`internal/daemon`（新增）**：zzauto 自身后台 daemon 管理。`Start(serveArgs)` fork `zzauto serve` 子进程脱离终端（Unix `setsid` / Windows `CREATE_NEW_PROCESS_GROUP`），stdout/stderr 重定向到 `~/.zzauto/zzauto.log`，PID 写入 `~/.zzauto/zzauto.pid`；`Stop()` 读 PID 发 SIGTERM、5s 后 SIGKILL（Windows 用 `taskkill /T /F`）；`Restart()` = Stop + Start；`Status()` 返回 running/pid/listen。包级变量 `pidFilePath`/`logFilePath`/`processAlive`/`signalProcess`/`termSignal`/`killSignal` 可被测试覆写注入临时路径与 fake 信号。详见 [architecture.md](./architecture.md) 的「daemon 管理」一节与 [cli.md](./cli.md) 的 `start`/`stop`/`restart`/`status` 章节。
- **`internal/aicli`**：v0.4.0 `bootstrap.go` 修复 `EnsureInstalled` 检测逻辑（Health → `lookPath` → 已装 `StartDaemon` / 未装 install+`StartDaemon`，不再误装已装未启的 aiclibridge）；新增 `StartDaemon(ctx)` 调 `aiclibridge start` 子命令（`startDaemonFunc` 包变量便于测试注入）；`client.go` 新增 `Models(ctx)` GET `/v1/models`（OpenAI 兼容 `ModelsResp`）。
- **`internal/ui`**：v0.4.0 新增 `GET /api/aicli/models` 代理 aiclibridge `/v1/models`（`handleAicliModels`，失败返回 502）；前端 `app.js` `loadModels` 拉 `/api/aicli/models` 填 `availableModels`，Settings 页 model input 改 `<input list>` + `<datalist>`，aiclibridge 不可达时退化为纯 input。
- **`cmd/zzauto/main.go`**：v0.4.0 `Version` 常量 `v0.3.0` → `v0.4.0`；无参数 = `usage()` exit 0（不再默认 serve）；新增 `start`/`stop`/`restart`/`status` 子命令分发；`usage` 更新列出全部子命令。

---

## 添加新 agent

以新增一个虚构的 `Reviewer` agent 为例：

### 1. 在 `internal/agents/` 新建 `reviewer.go` 实现 `Agent` 接口

```go
package agents

import (
    "context"
    "github.com/tgcz2011/zzauto/internal/eventbus"
    "github.com/tgcz2011/zzauto/internal/workspace"
)

type Reviewer struct{}

func NewReviewer() *Reviewer { return &Reviewer{} }

func (r *Reviewer) Name() string { return "reviewer" }

func (r *Reviewer) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
    publishEvent(bus, eventbus.EventAgentStart, r.Name(), map[string]any{
        DataKeyStage: "reviewer", DataKeyAgent: r.Name(),
    })
    // 读上游文档、调用 AI、写下游文档 ...
    publishEvent(bus, eventbus.EventAgentDone, r.Name(), map[string]any{
        DataKeyStage: "reviewer", DataKeyAgent: r.Name(),
    })
    return nil
}
```

约定：

- `Name()` 返回与 workspace 阶段常量对应的标识。
- 复用 `publishEvent` 辅助函数（bus 为 nil 时安全跳过，便于测试）。
- 失败时发布 `agent_failed` 事件并返回包装错误；如需参与循环，返回相应哨兵 error（见 `errors.go`）。
- system prompt 约束 AI 严格按 schema 输出，不加代码围栏、不加额外说明。

### 2. 在 `internal/registry/registry.go` 注册

在 `RegisterAgents` 中追加：

```go
orch.Register(workspace.StageReviewer, agents.NewReviewer())
```

并按流程顺序调整编排器调用（见下一步）。

### 3. 在 `internal/workspace/doc.go` 加阶段常量（如需）

```go
const StageReviewer = "reviewer"
```

并在编排器 `Run`（`internal/orchestrator/orchestrator.go`）的顺序阶段或循环中插入调用；若引入新循环，参考 `runDiscussLoop` / `runEvalLoop` 的哨兵驱动模式。若新增文档，在 `doc.go` 加文档名常量（如 `DocReview = "review.md"`）并在 `schema.go` 加正文模板与段标题常量。

### 4. 写测试

参考 `internal/agents/*_test.go`：用 mock `AIClient`（返回预设回答）+ 临时 workspace，断言产出文档内容与事件序列。若涉及循环，用 `SetMaxDiscussRounds` / `SetMaxEvalRetries` 控制轮数。

### 5. 更新 `docs/agents.md`

按既有格式补一节：职责、输入文档、输出文档、system prompt 要点、交互、关键设计。

> 若 agent 暴露新配置项或 CLI flag，同步更新 [configuration.md](./configuration.md) 与 [cli.md](./cli.md)，并在 `CHANGELOG.md` 的 `[Unreleased]` 段登记。

---

## 提交规范（conventional commits）

所有提交信息使用 conventional commits 前缀（gittor 隔离层会校验 commit message 前缀，贡献者提交也遵循同样规范）：

| 前缀 | 含义 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | bug 修复 |
| `docs` | 文档 |
| `refactor` | 重构（不改行为） |
| `test` | 测试 |
| `chore` | 杂项/构建/依赖 |
| `style` / `perf` / `build` / `ci` / `revert` | 其他允许类型 |

格式：`<type>: <简要描述>`，支持 `feat(scope)!:` 等变体。示例：

- `feat: 新增 Asker 挑剔模式`
- `fix: 修复 Executor 在空输入时的 panic`
- `docs: 补充 agents 架构说明`
- `refactor: 抽取 planner 公共校验逻辑`

分支命名建议用语义前缀：`feat/xxx`、`fix/xxx`、`docs/xxx`、`refactor/xxx`、`test/xxx`、`chore/xxx`。

---

## 文档同步要求

> **核心约定**：每次代码修改都要同步更新相关文档。代码与文档不一致是社区协作最大的隐患。

按改动范围同步更新：

| 改动类型 | 需要更新的文档 |
| --- | --- |
| 用户可见的功能变化 | `README.md` |
| agent 行为 / 架构调整 | `docs/agents.md` 或 `docs/architecture.md` |
| 配置项 / 环境变量 | `docs/configuration.md` |
| CLI 子命令 / 参数 | `docs/cli.md` |
| 任何修改 | `CHANGELOG.md` 的 `[Unreleased]` 段 |

`CHANGELOG` 写法（在 `[Unreleased]` 段下按类型分组）：

```
### Added
- 新增 Asker 挑剔模式（feat: ...）

### Fixed
- 修复 Executor 空输入 panic（fix: ...）

### Changed
- 重构 planner 公共校验逻辑（refactor: ...）
```

PR 描述检查项须勾选「已更新相关文档」「已更新 CHANGELOG.md」。纯重构未改行为可注明「无需更新文档」并简述理由，仍需勾选以示确认。

---

## release 流程

release 由维护者执行，普通贡献者无需关心。要点（完整流程见 `RELEASE.md`）：

1. **确保测试通过**：`go test ./...`。
2. **更新 `CHANGELOG.md`**：在顶部新增本次版本条目（`[v0.x.x] - YYYY-MM-DD`，按 Added/Changed/Fixed/Removed 分组）。
3. **更新版本常量**：编辑 `cmd/zzauto/main.go` 的 `const Version = "v0.x.x"`，与即将发布的 tag 一致。
4. **提交**：`git add CHANGELOG.md cmd/zzauto/main.go && git commit -m "chore: release v0.x.x"`。
5. **打 tag 并推送**：`git tag v0.x.x && git push origin v0.x.x`（也建议 `git push origin main`）。
6. **自动构建**：tag `v*` 触发 `.github/workflows/release.yml`，在 5 个平台构建、打包（tar.gz/zip）、生成 sha256、创建 GitHub Release。
7. **验证**：确认 Release 资产含 5 个二进制 + 5 个 sha256 文件；干净环境 `curl | sh -s -- --version v0.x.x` 验证。

版本号采用语义化版本 `v<major>.<minor>.<patch>`，预发布用 `-rc.N`（不会成为 `releases/latest`，不影响 `zzauto upgrade`）。回滚删 Release 与 tag，但**禁止复用 tag**重新发版，应递增 patch。

> install.sh / install.ps1 / `zzauto upgrade` 均依赖 `releases/latest/download` 直链与资产命名约定，因此 release 资产文件名必须与 `scripts/install.sh` 严格一致。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `CONTRIBUTING.md` | 贡献流程、提交规范、文档同步、测试要求 |
| `RELEASE.md` | 发版流程规范 |
| `CHANGELOG.md` | 变更日志（每次发版更新） |
| `.github/workflows/release.yml` | tag 触发的自动构建发布 |
| `.github/pull_request_template.md` | PR 描述模板 |
| `cmd/zzauto/main.go` | `Version` 常量 |
