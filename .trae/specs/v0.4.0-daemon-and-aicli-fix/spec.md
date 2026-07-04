# Spec: v0.4.0 修复 aiclibridge 检测 + zzauto daemon 化

## 背景

- v0.3.0 的 EnsureInstalled 用 Health 失败就装，但 aiclibridge 已装但未启动会被误装。
- aiclibridge 真实子命令：`serve`(前台) / `start`(后台 daemon) / `stop` / `restart` / `upgrade` / `uninstall` / `update` / `run` / `agents` / `models` / `cancel` / `get` / `sessions` / `version`。无参数 = 打印 usage + exit 0。
- zzauto 当前无参数默认 serve，且无 daemon 管理；用户希望 zzauto 自身也支持 start/restart/stop 后台运行，terminal 可关闭。

## 目标

1. **修复 aiclibridge 检测**：Health 失败 → lookPath → 已装则 `aiclibridge start` 启动 daemon → 未装才装+start
2. **用对 aiclibridge**：新增 `/v1/models` 客户端，Settings 页模型改下拉选择
3. **zzauto daemon 化**：新增 `start`/`stop`/`restart`/`status` 子命令；`zzauto` 无参数 = `-h`（打印 usage exit 0）

## 设计

### 1. internal/aicli/bootstrap.go 修复

`EnsureInstalled(ctx, addr, apiKey)` 新逻辑：
```
1. Health OK → return nil
2. Health 失败:
   a. lookPath("aiclibridge")
   b. 已装 → StartDaemon(ctx): exec "aiclibridge start"，等 2s，轮询 Health 至通过或 30s 超时
   c. 未装 → installFunc（默认 installAiclibridge）→ StartDaemon → 轮询 Health
```

新增 `StartDaemon(ctx context.Context) error`：exec `aiclibridge start`，捕获输出，失败返回错误。

### 2. internal/aicli: ListModels

新增 `Models(ctx) (*ModelsResp, error)` — GET `/v1/models`，返回 `{data: [{id, owned_by}]}`（OpenAI 兼容）。
handler 增加 `GET /api/aicli/models` 代理。
前端 Settings 页 model input 改为 datalist（input + 下拉选项来自 /api/aicli/models）。

### 3. internal/daemon 包（新建）

```go
package daemon

// PIDFile 默认路径 ~/.zzauto/zzauto.pid
// LogFile 默认路径 ~/.zzauto/zzauto.log

func Start(serveArgs []string) error       // fork zzauto serve，detach，写 PID
func Stop() error                          // 读 PID，SIGTERM，等 5s，仍存活 SIGKILL
func Restart(serveArgs []string) error     // Stop + Start
func Status() (running bool, pid int, listen string, err error)
```

Unix 实现：
- fork 子进程：`os/exec.Command(os.Args[0], append([]string{"serve"}, serveArgs...)...)`
- SetSysProcAttr: Setsid=true（脱离控制终端）
- stdin = /dev/null，stdout/stderr = LogFile
- 写 PID 文件 `<home>/.zzauto/zzauto.pid`
- LogFile 记录 daemon 输出

Windows 实现：
- CREATE_NEW_PROCESS_GROUP + DETACHED_PROCESS
- 无优雅 SIGTERM，Stop 用 taskkill /PID /T /F

### 4. main.go 改造

```
zzauto                        # 打印 usage，exit 0（等同 -h）
zzauto -h | --help | help     # 打印 usage，exit 0
zzauto serve [flags]          # 前台启动（开发调试）
zzauto start [flags]          # 后台 daemon，flags 透传给 serve
zzauto stop                   # 停止 daemon
zzauto restart [flags]        # 重启 daemon
zzauto status                 # 查看 daemon 状态
zzauto upgrade                # 升级（已有）
zzauto uninstall              # 卸载（已有）
zzauto version                # 版本（已有）
```

runStart: 解析 flags（与 serve 相同），调 daemon.Start(flags)
runStop: 调 daemon.Stop()
runRestart: 解析 flags，调 daemon.Restart(flags)
runStatus: 调 daemon.Status()，打印 running/pid/listen

usage 更新：
```
zzauto v0.4.0 - 多层 agent 协作的 AI 自主编程平台

用法:
  zzauto [command]

命令:
  serve       前台启动 HTTP 服务（开发调试）
  start       后台启动 daemon（terminal 可关闭）
  stop        停止后台 daemon
  restart     重启后台 daemon
  status      查看 daemon 状态
  upgrade     从 GitHub releases 升级
  uninstall   移除二进制与配置（保留项目数据）
  version     打印版本号

无参数等同 -h。daemon 日志: ~/.zzauto/zzauto.log
```

### 5. UI: Settings 页模型下拉

`GET /api/aicli/models` 代理 aiclibridge `/v1/models`。
app.js loadModels 时同时拉 /api/aicli/models 填充 `availableModels` 数组。
Settings 页每个 model input 改为 `<input list="models-<stage>">` + `<datalist>`，用户可输入或选下拉项。
失败（aiclibridge 不可达）时退化为纯 input。

### 6. 文档更新

- README.md badge v0.3.0 → v0.4.0
- docs/cli.md 增加 start/stop/restart/status 章节
- docs/aiclibridge.md 修复检测逻辑说明（Health → lookPath → start daemon → install）
- docs/settings.md 增加 model 下拉说明
- CHANGELOG.md 增加 v0.4.0 条目
- Version 常量 v0.3.0 → v0.4.0

## 验证

- `go build/vet/test ./...` 全通过
- `zzauto` 无参数打印 usage exit 0
- `zzauto start` 后台启动，terminal 关闭后 daemon 仍在
- `zzauto status` 显示 running + pid
- `zzauto stop` 停止 daemon
- aiclibridge 已装未启动时 `zzauto serve` 自动 `aiclibridge start`，不再重装
