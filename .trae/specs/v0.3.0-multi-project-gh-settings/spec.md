# Spec: v0.3.0 多项目 + gh CLI + 详细设置/统计/任务面板

## 背景

v0.2.0 已实现单项目编排、aiclibridge 自动引导、release 流程。用户实际会拿此平台并行跑多个项目，
且需要更细粒度的可观测性（每角色的模型配置、token 统计、agent 执行明细到思考过程与工具调用）。

## 目标

1. **多项目列表**：UI 显示所有项目，可创建/切换/删除；每个项目独立 workspace。
2. **gh CLI 集成**：GitHub 操作走 `gh` CLI；启动时检测，未装则拒绝启动并给出平台一键安装命令；
   aiclibridge 未装则继续自动安装（保持 v0.2.0 行为）。
3. **创建项目时选 gh 仓库**：调 `gh repo list` 拉取；未登录则提示 `gh auth login`。
4. **详细 settings**：可为每个 agent 角色配置独立模型（覆盖默认模型）。
5. **详细统计面板**：从 aiclibridge `/v1/stats/*` 拉取 token / 价格 / 并发数据并展示。
6. **详细任务面板**：每个 agent 的执行可细化到思考过程、工具调用、文本输出；按项目查看工作流程。

## 设计

### 1. 多项目 registry — `internal/projects`

- `ProjectMeta` struct: `ID, Name, Repo (owner/name), Branch, CreatedAt, UpdatedAt, Status, CurrentStage`
- 持久化到 `<workspaceDir>/projects/<id>/project.json`
- `Registry` struct: `List() / Get(id) / Create(name, repo, branch) / Delete(id) / Update(meta)`
- 创建项目时：生成 id、建目录、写 project.json、初始化空 input.md
- 与 workspace.Workspace 解耦：workspace 仍是单项目视图，Registry 维护多项目索引

### 2. gh CLI 集成 — `internal/ghcli`

- `EnsureInstalled() error`：exec.LookPath("gh")，失败返回带平台安装提示的错误
- `InstallHint()` string：按 GOOS 返回一键安装命令
  - darwin: `xcode-select --install` (含 gh) 或 `brew install gh`
  - linux: `apt install gh` / `dnf install gh` / `pacman -S github-cli`
  - windows: `winget install GitHub.cli` 或 `choco install gh`
- `AuthStatus(ctx) (bool, error)`：`gh auth status` 退出码判断
- `Repos(ctx) ([]Repo, error)`：`gh repo list --json nameWithOwner,isPrivate,url,description --limit 100`
- `LoginHint()` string：返回 `gh auth login` 引导
- main.go runServe：先 aicli 健康检查（不可达且未 --no-auto-install 则自动安装）→ 再 gh 检查（未装则拒绝启动）
- 删除原 gittor.go 中的 token 字段使用（改由 gh CLI 处理 auth）；保留 token 兼容路径但优先用 gh

### 3. 每角色模型配置 — `internal/config`

- 新增 `RoleModels map[string]string` 字段到 Config（key=stage，value=model name）
- 默认为空 map，agent 调 AI 时若该 stage 有配置则用之，否则用 aicli.DefaultModel
- API: `GET /api/settings/models`、`PUT /api/settings/models`
- 持久化到 `zzauto.yaml`（config 包新增 Save 方法）

### 4. aicli 扩展

- `Client.SetModel(model)` / `Client.Model() string`
- `Client.ChatWithModel(ctx, model, system, user) (string, error)` — 临时覆盖模型
- `Client.AskWithModel(ctx, model, system, user) (string, error)` — 同上
- 新增 `stats.go`：
  - `Usage(ctx) (*UsageResp, error)` — GET `/v1/stats/usage`
  - `Prices(ctx) (*PricesResp, error)` — GET `/v1/stats/prices`
  - `Summary(ctx) (*SummaryResp, error)` — GET `/v1/stats/summary`
  - `Concurrency(ctx) (*ConcurrencyResp, error)` — GET `/v1/stats/concurrency`
- 新增 `runs.go`：
  - `RunStream(ctx, model, system, user, onEvent func(RunEvent)) (runID string, err error)` — POST `/v1/runs` SSE
  - `GetRun(ctx, runID) (*RunDetail, error)` — GET `/v1/runs/{id}`
  - RunEvent 包含 Type（thinking/text/tool_use/tool_result/result/error）、Content、ToolName、ToolInput
- agent 调用 AI 时改用 RunStream，捕获 runID 与关键事件，通过 eventbus 推送到 UI
- registry.go BuildOrchestrator：为每个 agent 注入 stage 名，便于按角色查模型

### 5. HTTP API — `internal/ui/handler.go` 扩展

新增路由：
- `GET    /api/projects`               项目列表
- `POST   /api/projects`               创建项目（body: name, repo, branch）
- `GET    /api/projects/{id}`          项目详情
- `DELETE /api/projects/{id}`          删除项目
- `POST   /api/projects/{id}/input`    给指定项目提交需求
- `POST   /api/projects/{id}/start`    启动某项目编排
- `GET    /api/gh/status`              gh 安装 + auth 状态
- `GET    /api/gh/repos`               拉取仓库列表
- `GET    /api/settings/models`        读角色模型配置
- `PUT    /api/settings/models`        更新角色模型配置
- `GET    /api/stats/usage`            代理 aiclibridge
- `GET    /api/stats/summary`          代理 aiclibridge
- `GET    /api/stats/concurrency`      代理 aiclibridge
- `GET    /api/projects/{id}/runs`     项目下所有 agent run 列表
- `GET    /api/projects/{id}/runs/{rid}` 单个 run 详情（含 thinking + tool calls）

保留旧路由兼容（`/api/state`、`/api/input`、`/api/docs/{name}` 等改为按当前选中项目）。

### 6. main.go serve 改造

启动顺序：
1. 加载 config
2. aicli 健康检查 — 不可达且非 --no-auto-install → 自动安装
3. **新增**：ghcli.EnsureInstalled — 失败则打印平台安装提示，退出 1
4. **新增**：ghcli.AuthStatus — 未登录则提示 `gh auth login`，**不**自动登录（交互式）
5. 启动 Registry、UI、HTTP 服务
6. 不再在启动时立即 BuildOrchestrator；改为按需：用户在 UI 点 "start" 时才为该项目装配 orchestrator

### 7. UI 改造

页面切换（Alpine.js SPA 风格）：
- **项目列表页**：表格（名称/仓库/状态/创建时间），"新建项目"按钮，点击进入详情
- **项目详情页**：原 v0.2.0 的流程/文档/Asker/GitHub 配置；"启动编排"按钮
- **新建项目弹窗**：名称输入 + 仓库下拉（来自 /api/gh/repos）+ 分支输入；未登录则显示 `gh auth login` 提示
- **Settings 页**：9 行表单（每行 stage + model input），保存按钮
- **统计面板页**：4 个卡片（总 token / 总 USD / 并发 active/queued / 模型分布）+ 表格
- **任务面板页**：项目下拉 + agent 列表 + 选中 agent 后展示其 runs（时间倒序）+ 点击 run 展开 thinking / tool_use / tool_result / text 事件流

### 8. 兼容性

- 删除 v0.2.0 中 `cfg.Github.Token` 的硬依赖（仍保留字段供 gittor 兜底，但默认走 gh CLI）
- 旧 `/api/state` 等 API 保留并按"当前选中项目"工作（前端首次进入选第一个项目）

## 文件结构

```
internal/
  projects/
    registry.go           # ProjectMeta + Registry
    registry_test.go
  ghcli/
    ghcli.go              # EnsureInstalled/AuthStatus/Repos/InstallHint
    ghcli_test.go
  aicli/
    stats.go              # Usage/Prices/Summary/Concurrency
    runs.go               # RunStream/GetRun
    stats_test.go
    runs_test.go
  config/
    config.go             # 增加 RoleModels + Save
    config_test.go
  ui/
    handler.go            # 新增路由
    web/
      index.html          # SPA 改造
      app.js              # 多页面 + 多项目状态
      style.css
  agents/                 # 各 agent 改用 ai.AskWithModel(ctx, stage, ...)
  orchestrator/           # 不变
  registry/               # BuildOrchestrator 改为按需装配
cmd/zzauto/main.go        # serve 增加 gh 检查
docs/
  multi-projects.md       # 新增
  gh-integration.md       # 新增
  settings.md             # 新增
  stats.md                # 新增
  task-panel.md           # 新增
```

## 验证

- `go build/vet/test ./...` 全通过
- `zzauto serve` 无 gh 时退出 1 并打印平台安装命令
- `zzauto serve` gh 未登录时打印 `gh auth login` 提示并退出 1
- 创建项目时若 gh 未登录返回 401 + login 提示
- 切换项目后 /api/state 反映该项目状态
- /api/settings/models 可读写
- /api/stats/* 返回 aiclibridge 数据
- /api/projects/{id}/runs 返回该项目的 agent run 列表
