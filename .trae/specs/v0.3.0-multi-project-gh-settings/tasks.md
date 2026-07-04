# Tasks

- [x] Task 1: internal/projects 包 — ProjectMeta + Registry
  - [x] SubTask 1.1: registry.go: ProjectMeta struct (ID/Name/Repo/Branch/CreatedAt/UpdatedAt/Status/CurrentStage), Registry (List/Get/Create/Delete/Update)
  - [x] SubTask 1.2: 持久化到 `<workspaceDir>/projects/<id>/project.json`，Create 时建目录+空 input.md
  - [x] SubTask 1.3: registry_test.go 覆盖 Create/List/Get/Delete/Update（用 t.TempDir）

- [x] Task 2: internal/ghcli 包
  - [x] SubTask 2.1: ghcli.go: EnsureInstalled (exec.LookPath), InstallHint 按 GOOS 返回一键命令
  - [x] SubTask 2.2: AuthStatus (gh auth status 退出码), Repos (gh repo list --json), LoginHint
  - [x] SubTask 2.3: ghcli_test.go: mock exec.LookPath + httptest/file 模拟 gh 输出

- [x] Task 3: config 扩展 RoleModels + Save
  - [x] SubTask 3.1: config.go: 增加 RoleModels map[string]string + env ZZAUTO_ROLE_MODELS_xxx
  - [x] SubTask 3.2: Save(path) 方法写回 zzauto.yaml
  - [x] SubTask 3.3: config_test.go 覆盖 Save + Load 往返

- [x] Task 4: aicli 扩展（stats + runs + ChatWithModel）
  - [x] SubTask 4.1: client.go: ChatWithModel/AskWithModel/SetModel/Model
  - [x] SubTask 4.2: stats.go: Usage/Prices/Summary/Concurrency (GET /v1/stats/*)
  - [x] SubTask 4.3: runs.go: RunStream (POST /v1/runs SSE) + GetRun (GET /v1/runs/{id})
  - [x] SubTask 4.4: 单测覆盖 ChatWithModel + stats (httptest) + runs (mock SSE)

- [x] Task 5: agents 改用 AskWithModel
  - [x] SubTask 5.1: agent.go: AIClient 接口增加 AskWithModel(ctx, model, system, user)
  - [x] SubTask 5.2: 9 个 agent Run 改为 ai.AskWithModel(ctx, roleModel, system, user)，roleModel 由 stage 决定
  - [x] SubTask 5.3: aicli.Client 实现新接口；mock AI 在 e2e 测试中实现

- [x] Task 6: agent runs 跟踪
  - [x] SubTask 6.1: 改 agents 调用为 RunStream，捕获 runID + 关键事件
  - [x] SubTask 6.2: 把 runID + 事件写入 `<projectDir>/runs/<agent>/<runID>.json`
  - [x] SubTask 6.3: eventbus 发布 `agent_run_event` 事件含 runID/agent/type/content

- [x] Task 7: HTTP API 路由扩展
  - [x] SubTask 7.1: /api/projects (GET/POST), /api/projects/{id} (GET/DELETE)
  - [x] SubTask 7.2: /api/projects/{id}/input, /api/projects/{id}/start
  - [x] SubTask 7.3: /api/gh/status, /api/gh/repos
  - [x] SubTask 7.4: /api/settings/models (GET/PUT) + 持久化
  - [x] SubTask 7.5: /api/stats/usage, /api/stats/summary, /api/stats/concurrency
  - [x] SubTask 7.6: /api/projects/{id}/runs, /api/projects/{id}/runs/{rid}

- [x] Task 8: main.go serve 改造
  - [x] SubTask 8.1: 启动顺序: aicli 健康检查 → aicli 自动安装 → ghcli.EnsureInstalled → ghcli.AuthStatus
  - [x] SubTask 8.2: gh 未装/未登录打印提示并退出 1
  - [x] SubTask 8.3: 不再启动时 BuildOrchestrator，改为按需装配（UI 点 start）

- [x] Task 9: 前端 SPA 改造
  - [x] SubTask 9.1: 多项目列表页 + 新建项目弹窗（含仓库下拉）
  - [x] SubTask 9.2: 项目详情页（沿用 v0.2.0 流程/文档/Asker，加 start 按钮）
  - [x] SubTask 9.3: Settings 页（9 行表单 + 保存）
  - [x] SubTask 9.4: 统计面板页（4 卡片 + 模型分布表）
  - [x] SubTask 9.5: 任务面板页（agent 列表 + runs 时间线 + 事件展开）

- [x] Task 10: 测试与文档
  - [x] SubTask 10.1: go build/vet/test ./... 全通过
  - [x] SubTask 10.2: 手动验证: serve 无 gh 退出 1, gh 未登录退出 1, 创建项目选仓库
  - [x] SubTask 10.3: 更新 README + docs/ + CHANGELOG + RELEASE
  - [x] SubTask 10.4: 提交 + 打 tag v0.3.0 + push 触发 release

# Task Dependencies
- Task 3 depends on Task 1
- Task 5 depends on Task 3, Task 4
- Task 6 depends on Task 4
- Task 7 depends on Task 1, 2, 3, 4, 6
- Task 8 depends on Task 2
- Task 9 depends on Task 7
- Task 10 depends on all
