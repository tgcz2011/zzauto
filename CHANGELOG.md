# Changelog

本文件记录 zzauto 所有版本的变更。格式参考 [Keep a Changelog](https://keepachangelog.com/)。

## [Unreleased]

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
