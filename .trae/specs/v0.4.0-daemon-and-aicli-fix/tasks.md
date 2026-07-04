# Tasks

- [x]Task 1: 修复 aiclibridge 检测逻辑
  - [x]SubTask 1.1: bootstrap.go EnsureInstalled 改为 Health → lookPath → 已装则 StartDaemon / 未装则 install+StartDaemon
  - [x]SubTask 1.2: 新增 StartDaemon(ctx) error：exec aiclibridge start
  - [x]SubTask 1.3: 更新 bootstrap_test.go 覆盖新分支（已装未启 / 未装）

- [x]Task 2: aicli Models 客户端
  - [x]SubTask 2.1: client.go 新增 Models(ctx) (*ModelsResp, error) GET /v1/models
  - [x]SubTask 2.2: stats_test.go 或新文件覆盖 Models

- [x]Task 3: internal/daemon 包
  - [x]SubTask 3.1: daemon.go: Start(serveArgs)/Stop/Restart/Status，Unix setsid + PID 文件
  - [x]SubTask 3.2: Windows 分支（CREATE_NEW_PROCESS_GROUP + taskkill）
  - [x]SubTask 3.3: daemon_test.go: 测试 PID 文件读写 + Status（不实际 fork）

- [x]Task 4: main.go 改造
  - [x]SubTask 4.1: 无参数 = usage exit 0（不再默认 serve）
  - [x]SubTask 4.2: 新增 start/stop/restart/status 子命令
  - [x]SubTask 4.3: usage 更新

- [x]Task 5: UI Settings 模型下拉
  - [x]SubTask 5.1: handler.go 新增 GET /api/aicli/models 代理
  - [x]SubTask 5.2: app.js loadModels 同时拉 /api/aicli/models 填 availableModels
  - [x]SubTask 5.3: index.html Settings 页 model input 改 datalist

- [x]Task 6: 文档与发布
  - [x]SubTask 6.1: Version v0.3.0 → v0.4.0
  - [x]SubTask 6.2: README badge + docs/cli.md + docs/aiclibridge.md + docs/settings.md + CHANGELOG
  - [x]SubTask 6.3: go build/vet/test 通过
  - [x]SubTask 6.4: commit + tag v0.4.0 + push

# Task Dependencies
- Task 4 depends on Task 3
- Task 5 depends on Task 2
- Task 6 depends on all
