# Changelog

本文件记录 zzauto 所有版本的变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)。

## [Unreleased]

## [v0.3.0] - 2026-07-04
### Added
- 多项目支持：`internal/projects` 包 `Registry` 管理多项目，UI 项目列表/新建/切换/删除；每项目对应 `<workspaceDir>/projects/<id>/project.json` 元数据
- gh CLI 集成：`internal/ghcli` 包封装 `EnsureInstalled`/`AuthStatus`/`Repos`/`InstallHint`/`LoginHint`；serve 启动时强制检查 gh 已装且已登录，未装打印平台安装命令并退出 1，未登录提示 `gh auth login` 并退出 1
- 创建项目选 gh 仓库：UI 新建项目弹窗从 `GET /api/gh/repos` 拉取仓库下拉，未登录返回 401 + login 提示
- 每角色模型配置：`config.RoleModels` map[string]string + Settings 页（9 行表单）+ env `ZZAUTO_ROLE_MODEL_<STAGE>` 覆盖 + `cfg.Save("zzauto.yaml")` 持久化；`registry.RegisterAgents` 接收 roleModels 注入到各 agent
- 统计面板：从 aiclibridge `/v1/stats/*` 拉取 token/USD/并发数据，UI 4 卡片（总览/用量/并发/定价）+ 模型分布表，每 30 秒自动刷新
- 任务面板：每个 agent 的 run 详情（thinking/text/tool_use/tool_result/result/error/system 事件流），按项目+agent 查看，SSE 实时推送 `agent_run_event` 事件
- aiclibridge 端点扩展：`POST /v1/runs`（SSE 流式）+ `GET /v1/runs/{id}`（详情）+ `GET /v1/stats/{usage,prices,summary,concurrency}`
- `aicli` 客户端扩展：`ChatWithModel`/`AskWithModel`/`SetModel`/`Model`（每角色模型）+ `RunStream`（SSE 解析）+ `GetRun` + `Usage`/`Prices`/`Summary`/`Concurrency`
- `agents.AIClient` 接口扩展为四方法：`Ask`/`AskWithModel`/`RunStream`/`GetRun`
- `agents.RunWithTracking` 辅助函数：流式调用 AI + 事件持久化到 `<projectDir>/runs/<agent>/<runID>.json` + `agent_run_event` 事件广播
- eventbus 新增 `EventAgentRunEvent` 事件类型
- HTTP API 17 新路由：`/api/projects/*`（list/create/get/delete/input/start/select/runs）+ `/api/gh/*`（status/repos）+ `/api/settings/models`（GET/PUT）+ `/api/stats/*`（usage/summary/concurrency）
- 前端 4 页面 SPA：项目 / 设置 / 统计 / 任务面板
- 5 篇新文档：`docs/multi-projects.md`、`docs/gh-integration.md`、`docs/settings.md`、`docs/stats.md`、`docs/task-panel.md`

### Changed
- `internal/ui.Handler` 持 `projects.Registry` 而非单个 workspace；`currentWS()` 按当前选中项目动态构造
- `internal/registry.BuildOrchestrator` 改为按需装配（UI 点「启动编排」触发 `POST /api/projects/{id}/start`），不再 serve 启动时全局装配
- serve 启动顺序：aicli 健康检查 → aicli 自动安装 → ghcli.EnsureInstalled → ghcli.AuthStatus → HTTP 服务
- `cmd/zzauto/main.go` Version 常量 `v0.2.0` → `v0.3.0`

## [v0.2.0] - 2026-07-04
### Added
- aiclibridge 自动引导：`zzauto serve` 启动时若 aiclibridge 不可达，自动执行安装脚本（curl|sh / irm|iex）并 30s 健康轮询
- `--no-auto-install` flag：serve 时跳过自动安装，保持原有提示退出行为
- `zzauto upgrade` 同步升级 aiclibridge：zzauto 升级后自动调用 `aiclibridge upgrade`，失败仅警告不阻塞
- 详细文档：README.md + docs/ 下 8 篇（quickstart/architecture/agents/configuration/cli/aiclibridge/workflow/development）
- RELEASE.md：release 流程规范，tag 格式 `x.x.x`（大功能.小功能.bug修复）
- `.github/workflows/release.yml`：打 tag 自动构建多平台二进制并发布 GitHub Release
- CHANGELOG.md：按版本记录变更
- CONTRIBUTING.md + PR 模板：约定「每次代码修改同步更新文档」

### Changed
- `internal/aicli` 新增 bootstrap.go：EnsureInstalled（自动安装+健康轮询）与 UpgradeAiclibridge（调 aiclibridge upgrade CLI）
- `internal/installer` Upgrade 末尾同步调用 aiclibridge upgrade（mock 注入点便于测试）

## [v0.1.0] - 2026-07-03
### Added
- 初始化多层 agent 协作的 AI 自主编程平台
- 9 个 agent：Listener/Asker/Planner/Designer/Evaluator/Manager/Executor/Generator/Gittor
- 编排器：状态机 + 讨论循环 + 评估循环
- aiclibridge 客户端集成
- Gittor 隔离层（git CLI，conventional commits）
- Web UI（go:embed + Alpine.js + Tailwind，SSE 推送）
- 多平台安装脚本（curl|sh / PowerShell，uninstall/upgrade 走 releases 直链）
- 端到端集成测试
