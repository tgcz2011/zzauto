# Checklist

## aiclibridge 检测修复
- [x]bootstrap.go EnsureInstalled: Health → lookPath → 已装 StartDaemon / 未装 install+StartDaemon
- [x]bootstrap.go 新增 StartDaemon(ctx) exec aiclibridge start
- [x]bootstrap_test.go 覆盖已装未启 / 未装两分支

## aicli Models
- [x]client.go Models(ctx) GET /v1/models
- [x]测试覆盖 Models

## daemon 包
- [x]internal/daemon/daemon.go: Start/Stop/Restart/Status
- [x]Unix setsid + PID 文件 + 日志重定向
- [x]Windows CREATE_NEW_PROCESS_GROUP + taskkill
- [x]daemon_test.go: PID 文件读写 + Status

## main.go
- [x]无参数 = usage exit 0
- [x]新增 start/stop/restart/status 子命令
- [x]usage 更新

## UI
- [x]GET /api/aicli/models 代理
- [x]app.js availableModels 数组
- [x]Settings 页 model input datalist

## 文档
- [x]Version v0.4.0
- [x]README badge v0.4.0
- [x]docs/cli.md 增加 start/stop/restart/status
- [x]docs/aiclibridge.md 修复检测逻辑说明
- [x]docs/settings.md model 下拉
- [x]CHANGELOG v0.4.0

## 验证
- [x]go build ./... 通过
- [x]go vet ./... 通过
- [x]go test ./... 通过
- [x]zzauto 无参数打印 usage exit 0
- [x]zzauto start/status/stop 工作
- [x]aiclibridge 已装未启时 serve 自动 start 不重装
- [x]commit + tag v0.4.0 + push
