# 文档完善与 aiclibridge 自动引导 Spec

## Why
当前 zzauto 缺少面向用户的详细文档与 README，新用户难以入手；`zzauto serve` 在 aiclibridge 不可达时仅提示安装命令后退出，体验不流畅；`zzauto upgrade` 只升级自身不同步升级 aiclibridge，导致依赖版本漂移；项目缺少标准化的 release 流程（tag、changelog、文档同步），迭代质量难保障。

## What Changes
- 新建 `README.md`：项目介绍、快速开始、架构、9 agent 流程图、配置、命令、文档索引
- 新建 `docs/` 目录与详细文档：`docs/quickstart.md`、`docs/architecture.md`、`docs/agents.md`（9 agent 详解）、`docs/configuration.md`、`docs/cli.md`、`docs/aiclibridge.md`、`docs/workflow.md`（端到端流程）、`docs/development.md`（开发指南）
- 新建 `RELEASE.md`：release 流程规范，tag 格式 `x.x.x`（大功能.小功能.bug修复），每次代码有变化都发布 release
- 新建 `.github/workflows/release.yml`：打 tag 自动构建多平台二进制并发布 GitHub Release（含 changelog）
- 新建 `CONTRIBUTING.md`：约定「每次修改都要更新文档」（README/docs/CHANGELOG），PR 检查项
- 新建 `CHANGELOG.md`：按版本记录变更
- **MODIFIED** `internal/installer/installer.go`：`Upgrade()` 升级 zzauto 后，自动检测并同步运行 `aiclibridge upgrade`（用 exec 调 aiclibridge CLI），失败不致命仅警告
- **MODIFIED** `cmd/zzauto/main.go runServe`：aiclibridge 不可达时，自动执行 aiclibridge 安装脚本（`curl | sh`），安装后重新健康检查；仍失败再退出。可用 `--no-auto-install` flag 跳过
- **ADDED** `internal/aicli/bootstrap.go`：`EnsureInstalled(ctx, addr) error`——检测 aiclibridge 是否可达，不可达则执行安装脚本（macOS/Linux: `curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh`；Windows: PowerShell `irm | iex`），安装后等待健康检查通过（最多 30s 轮询）
- **MODIFIED** `internal/installer/installer.go`：`Upgrade()` 末尾调用 `aicli.UpgradeAiclibridge()` 同步升级依赖

## Impact
- Affected specs: `build-platform-core`（aicli 集成、安装脚本、UI 等需求被细化）
- Affected code: `cmd/zzauto/main.go`（serve auto-install）、`internal/installer/installer.go`（upgrade 同步）、新增 `internal/aicli/bootstrap.go`、新增 `docs/` 与根级文档、新增 `.github/workflows/release.yml`
- 文档为新增，不破坏现有功能；auto-install 与 sync-upgrade 为行为增强，可 flag 关闭

## ADDED Requirements

### Requirement: 项目文档与 README
系统 SHALL 提供 README.md 与 docs/ 目录下的详细文档，覆盖：快速开始、架构、9 agent 详解、配置、CLI、aiclibridge 集成、端到端流程、开发指南。文档与代码同步更新。

#### Scenario: 新用户上手
- **WHEN** 新用户克隆仓库
- **THEN** 阅读 README.md 即可在 5 分钟内完成安装并跑通一次流程
- **AND** docs/ 提供深入各主题的详细说明

### Requirement: Release 流程规范
系统 SHALL 定义 release 流程：tag 格式 `x.x.x`（大功能更新.小功能更新.bug修复），每次代码有变化都发布对应 release。打 tag 触发 GitHub Actions 自动构建多平台二进制并发布 Release（含 changelog 段）。

#### Scenario: 发布新版本
- **WHEN** 代码有变化并合并到 main
- **THEN** 维护者按 `x.x.x` 规则打 tag（如 v0.2.0），推送 tag
- **AND** GitHub Actions 自动构建 darwin/linux × amd64/arm64 + windows-amd64 二进制，发布 GitHub Release
- **AND** CHANGELOG.md 记录该版本变更

### Requirement: 文档同步更新约定
系统 SHALL 在 CONTRIBUTING.md 中约定「每次代码修改都要同步更新相关文档」（README、docs、CHANGELOG），PR 模板含文档检查项。

#### Scenario: 提交修改
- **WHEN** 贡献者提交代码修改
- **THEN** PR 描述需包含「已更新文档」检查项，CI 检查 CHANGELOG.md 是否有对应版本条目

### Requirement: upgrade 同步升级 aiclibridge
`zzauto upgrade` SHALL 在升级 zzauto 二进制后，自动检测并同步运行 `aiclibridge upgrade`，确保依赖版本对齐。同步升级失败不致命，仅打印警告。

#### Scenario: 同步升级成功
- **WHEN** 用户执行 `zzauto upgrade` 且本机已安装 aiclibridge
- **THEN** zzauto 升级完成后自动执行 `aiclibridge upgrade`
- **AND** 打印 aiclibridge 升级结果

#### Scenario: 同步升级失败
- **WHEN** aiclibridge 未安装或升级失败
- **THEN** 打印警告「aiclibridge 同步升级失败：<原因>」，zzauto 升级仍视为成功

### Requirement: aiclibridge 自动安装
`zzauto serve` SHALL 在 aiclibridge 不可达时自动执行 aiclibridge 安装脚本（curl|sh / irm|iex），安装后重新健康检查（最多等待 30s）。仍失败再退出并提示。可用 `--no-auto-install` flag 跳过自动安装。

#### Scenario: 自动安装成功
- **WHEN** 用户执行 `zzauto serve` 且 aiclibridge 未安装
- **THEN** 系统自动下载并执行 aiclibridge 安装脚本
- **AND** 安装后健康检查通过，继续启动 serve
- **AND** 打印安装日志

#### Scenario: 跳过自动安装
- **WHEN** 用户执行 `zzauto serve --no-auto-install` 且 aiclibridge 不可达
- **THEN** 系统仅打印安装提示并退出（保持原有行为）

#### Scenario: 自动安装失败
- **WHEN** 自动安装后健康检查仍不通过（如网络问题）
- **THEN** 打印失败原因与手动安装命令，退出 1

## MODIFIED Requirements

### Requirement: aiclibridge 集成（原 build-platform-core）
系统 SHALL 通过 aiclibridge HTTP API 调用所有 AI，不直接调用任何 AI CLI/SDK。`zzauto serve` SHALL 在 aiclibridge 不可达时**自动安装** aiclibridge（除非 `--no-auto-install`）。`zzauto upgrade` SHALL **同步升级** aiclibridge。

## REMOVED Requirements
无
