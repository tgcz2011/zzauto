# Checklist

## 后端
- [x] internal/projects/registry.go: ProjectMeta + Registry (List/Get/Create/Delete/Update)
- [x] projects: 持久化 project.json，Create 时建目录 + 空 input.md
- [x] internal/ghcli/ghcli.go: EnsureInstalled + InstallHint (按 GOOS)
- [x] ghcli: AuthStatus + Repos (gh repo list --json) + LoginHint
- [x] internal/config: 增加 RoleModels map[string]string
- [x] config: Save(path) 写回 zzauto.yaml
- [x] aicli: ChatWithModel / AskWithModel / SetModel / Model
- [x] aicli/stats.go: Usage / Prices / Summary / Concurrency
- [x] aicli/runs.go: RunStream (SSE) + GetRun
- [x] agents: AIClient 接口增加 AskWithModel
- [x] 9 个 agent Run 改用 AskWithModel，roleModel 由 stage 决定
- [x] agents: 调用改为 RunStream，捕获 runID + 事件
- [x] runs 持久化到 <projectDir>/runs/<agent>/<runID>.json
- [x] eventbus: 发布 agent_run_event 含 runID/agent/type/content

## API
- [x] /api/projects (GET list, POST create)
- [x] /api/projects/{id} (GET, DELETE)
- [x] /api/projects/{id}/input (POST)
- [x] /api/projects/{id}/start (POST)
- [x] /api/gh/status (GET)
- [x] /api/gh/repos (GET)
- [x] /api/settings/models (GET, PUT)
- [x] /api/stats/usage (GET 代理)
- [x] /api/stats/summary (GET 代理)
- [x] /api/stats/concurrency (GET 代理)
- [x] /api/projects/{id}/runs (GET)
- [x] /api/projects/{id}/runs/{rid} (GET)

## main.go
- [x] serve: aicli 健康检查 → 自动安装 → gh 检查 → auth 检查
- [x] gh 未装: 打印平台安装命令，退出 1
- [x] gh 未登录: 打印 `gh auth login` 提示，退出 1
- [x] 不再启动时 BuildOrchestrator，按需装配

## 前端
- [x] 多项目列表页（表格 + 新建按钮）
- [x] 新建项目弹窗（名称 + 仓库下拉 + 分支）
- [x] 项目详情页（流程/文档/Asker + 启动编排按钮）
- [x] Settings 页（9 行模型表单 + 保存）
- [x] 统计面板页（4 卡片 + 模型分布）
- [x] 任务面板页（agent 列表 + runs 时间线 + 事件展开）

## 平台安装提示
- [x] macOS: xcode-select --install / brew install gh
- [x] Linux: apt install gh / dnf install gh / pacman -S github-cli
- [x] Windows: winget install GitHub.cli / choco install gh

## 验证
- [x] go build ./... 通过
- [x] go vet ./... 通过
- [x] go test ./... 通过
- [x] serve 无 gh 时退出 1
- [x] serve gh 未登录时退出 1
- [x] 创建项目时 gh 未登录返回 401 + login 提示
- [x] /api/settings/models 可读写并持久化
- [x] /api/stats/* 代理 aiclibridge 返回数据
- [x] /api/projects/{id}/runs 返回该项目的 agent run 列表

## 文档
- [x] README.md 更新（多项目、gh、settings、stats、task panel 章节）
- [x] docs/multi-projects.md
- [x] docs/gh-integration.md
- [x] docs/settings.md
- [x] docs/stats.md
- [x] docs/task-panel.md
- [x] docs/cli.md / configuration.md / agents.md / aiclibridge.md 同步更新
- [x] CHANGELOG.md 增加 v0.3.0 条目
- [x] 提交 + tag v0.3.0 + push 触发 release workflow
