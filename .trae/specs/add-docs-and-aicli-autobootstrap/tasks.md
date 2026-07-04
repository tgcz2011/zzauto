# Tasks

- [x] Task 1: 创建 aiclibridge 自动引导包
  - [x] SubTask 1.1: `internal/aicli/bootstrap.go`：`EnsureInstalled(ctx, addr) error`——Health 检查不可达时执行安装脚本（macOS/Linux curl|sh、Windows irm|iex），安装后轮询健康检查最多 30s
  - [x] SubTask 1.2: `UpgradeAiclibridge() error`——执行 `aiclibridge upgrade` CLI，捕获输出，失败返回错误
  - [x] SubTask 1.3: `bootstrap_test.go`：httptest mock + 临时 PATH 测试 EnsureInstalled 与 UpgradeAiclibridge 的命令构造（不实际联网）

- [x] Task 2: 修改 installer.Upgrade 同步升级 aiclibridge
  - [x] SubTask 2.1: `internal/installer/installer.go` `Upgrade()` 末尾调用 `aicli.UpgradeAiclibridge()`，失败仅 log.Printf 警告不返回错误
  - [x] SubTask 2.2: 更新 installer_test.go 验证同步调用（mock aiclibridge 命令）

- [x] Task 3: 修改 main.go serve 自动安装 aiclibridge
  - [x] SubTask 3.1: `cmd/zzauto/main.go runServe`：添加 `--no-auto-install` flag；aiclibridge 不可达时调用 `aicli.EnsureInstalled`，成功后重新健康检查；失败退出 1
  - [x] SubTask 3.2: 自动安装过程打印进度日志

- [x] Task 4: 写 README.md
  - [x] SubTask 4.1: 项目介绍（一句话 + 详细）、特性列表、架构图（ASCII）、9 agent 流程图
  - [x] SubTask 4.2: 快速开始（安装、配置 aiclibridge、启动、提交需求）
  - [x] SubTask 4.3: 命令参考（serve/uninstall/upgrade/version + flag）
  - [x] SubTask 4.4: 配置说明（zzauto.yaml + env）
  - [x] SubTask 4.5: 文档索引（链接 docs/ 各文件）、许可证

- [x] Task 5: 写 docs/ 详细文档
  - [x] SubTask 5.1: `docs/quickstart.md`：5 分钟上手（含 aiclibridge 准备、首个项目）
  - [x] SubTask 5.2: `docs/architecture.md`：分层架构、编排器、事件总线、workspace、文档协议
  - [x] SubTask 5.3: `docs/agents.md`：9 个 agent 详解（职责、输入/输出文档、system prompt 要点）
  - [x] SubTask 5.4: `docs/configuration.md`：完整配置字段表 + env 覆盖 + 示例 yaml
  - [x] SubTask 5.5: `docs/cli.md`：所有子命令与 flag 详解
  - [x] SubTask 5.6: `docs/aiclibridge.md`：aiclibridge 集成说明、自动安装、同步升级
  - [x] SubTask 5.7: `docs/workflow.md`：端到端流程（desire→need→spec→deal→task→report→评估→提交）
  - [x] SubTask 5.8: `docs/development.md`：开发指南（构建、测试、目录结构、添加新 agent）

- [x] Task 6: 写 RELEASE.md 与 release workflow
  - [x] SubTask 6.1: `RELEASE.md`：release 流程规范（tag 格式 x.x.x、版本号递进规则、打 tag 步骤、changelog 要求）
  - [x] SubTask 6.2: `.github/workflows/release.yml`：打 tag 触发，构建多平台二进制（darwin/linux × amd64/arm64 + windows-amd64），生成 sha256，发布 GitHub Release
  - [x] SubTask 6.3: `CHANGELOG.md`：初始化 v0.1.0 条目（记录已完成的平台核心）

- [x] Task 7: 写 CONTRIBUTING.md 与 PR 模板
  - [x] SubTask 7.1: `CONTRIBUTING.md`：约定「每次代码修改都要同步更新文档」（README/docs/CHANGELOG）、提交规范（conventional commits）、PR 检查项
  - [x] SubTask 7.2: `.github/pull_request_template.md`：PR 描述模板含「已更新文档」「已更新 CHANGELOG」检查项

- [x] Task 8: 验证与发布 v0.2.0
  - [x] SubTask 8.1: `go build/vet/test ./...` 全部通过
  - [x] SubTask 8.2: 手动验证 `zzauto serve --no-auto-install`（aicli 不可达时退出 1）与 `zzauto upgrade`（同步调用 aiclibridge upgrade）
  - [x] SubTask 8.3: 提交所有变更，打 tag v0.2.0，推送触发 release workflow

# Task Dependencies
- Task 2 depends on Task 1
- Task 3 depends on Task 1
- Task 8 depends on Task 1, 2, 3, 4, 5, 6, 7
