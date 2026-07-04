# aiclibridge

aiclibridge 是 zzauto 的统一 AI 调用桥——一个本地 HTTP 服务，对外暴露 OpenAI 兼容与 Anthropic 兼容接口，对内屏蔽各 AI 后端差异。zzauto 所有 agent 通过它调用 AI，**不直接耦合**任何 AI SDK 或 CLI。

> 配置字段见 [configuration.md](./configuration.md)；客户端实现见 `internal/aicli/client.go`。

---

## 是什么

aiclibridge 是独立部署的本地 HTTP 网关（默认监听 `127.0.0.1:8787`），提供：

- **OpenAI 兼容**：`POST /v1/chat/completions`（请求体 `{model, messages:[{role,content}]}`，响应 `{choices:[{message:{content}}]}`）。
- **Anthropic 兼容**：`POST /v1/messages`（请求体 `{model, max_tokens, system, messages:[{role,content}]}`，响应 `{content:[{type,text}]}`）。
- **Run SSE 流（v0.3.0）**：`POST /v1/runs`（请求体 `{model, system, messages, stream:true}`，响应 `text/event-stream`，按 `data: <json>\n\n` 帧推送 `RunEvent`：thinking/text/tool_use/tool_result/result/error/system）。
- **Run 详情（v0.3.0）**：`GET /v1/runs/{id}`，返回单个 run 的完整事件时间线（id/model/status/created_at/events）。
- **统计端点（v0.3.0）**：
  - `GET /v1/stats/usage`：各模型用量（prompt/completion/total tokens、requests、usd）+ 汇总。
  - `GET /v1/stats/prices`：各模型每 1M token 定价。
  - `GET /v1/stats/summary`：总览（total_requests/total_tokens/total_usd/uptime）。
  - `GET /v1/stats/concurrency`：并发（active/queued/max）。
- **健康检查**：`GET /healthz`，2xx 视为可达。
- **鉴权**：`api_key` 非空时请求带 `Authorization: Bearer <key>`。

zzauto 的 `internal/aicli/` 封装了上述调用：

- 默认模型 `claude/anthropic/claude-sonnet-4.5`，默认 `max_tokens=4096`。
- HTTP 客户端超时 5 分钟（AI 推理较慢）。
- `Ask(ctx, system, user)` 是 agent 实际使用的便捷方法，默认走 OpenAI 兼容接口（即 `Chat`），满足 `agents.AIClient` 接口。
- **每角色模型（v0.3.0）**：`ChatWithModel`/`AskWithModel` 用传入 model 覆盖本次请求；`SetModel`/`Model` 读写默认模型。
- **Run 流（v0.3.0，`runs.go`）**：`RunStream` SSE 解析 + 回调，`GetRun` 拉取详情。
- **Stats（v0.3.0，`stats.go`）**：`Usage`/`Prices`/`Summary`/`Concurrency` 四个方法对应四个端点。
- 地址归一化：去掉 `http://`/`https://` 前缀与尾部斜杠。

---

## 为什么用 aiclibridge

| 不用 aiclibridge | 用 aiclibridge |
| --- | --- |
| 每个 agent 直接耦合某 AI SDK/CLI，模型切换要改多处 | agent 只依赖 `AIClient` 接口，模型/后端切换只改 aiclibridge 配置 |
| 各 AI CLI 调用方式不一，并发控制各自为政 | 统一 HTTP 接口，并发与限流由 aiclibridge 收口 |
| 鉴权凭据散落在调用方 | 凭据集中在 aiclibridge，zzauto 侧仅持可选 `api_key` |
| 测试需 mock 各 SDK | 测试用注入的 mock `AIClient`，生产用 aicli 客户端，解耦干净 |

核心收益：**解耦** + **统一** + **可测试**。

---

## 与 serve 的关系（健康检查与自动安装）

`zzauto serve` 启动时会向 aiclibridge 做健康检查（`cmd/zzauto/main.go`）：

```go
aiClient := aicli.New(cfg.AicliAddr, cfg.AicliKey)
healthCtx, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
if err := aiClient.Health(healthCtx); err != nil {
    log.Printf("aiclibridge 不可达: %v", err)
    if *noAutoInstall {
        // 仅提示并退出
        fmt.Fprintln(os.Stderr, "请先安装并启动 aiclibridge：")
        fmt.Fprintln(os.Stderr, "  curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh")
        os.Exit(1)
    }
    // 自动安装
    log.Println("正在自动安装 aiclibridge...")
    if err := aicli.EnsureInstalled(installCtx, cfg.AicliAddr, cfg.AicliKey); err != nil {
        fmt.Fprintf(os.Stderr, "aiclibridge 自动安装失败: %v\n", err)
        os.Exit(1)
    }
    log.Println("aiclibridge 安装完成，继续启动")
}
```

- 健康检查超时 5 秒。
- 不可达时，默认走自动安装流程（见下一节）；`--no-auto-install` flag 为 true 时仅打印安装提示并以退出码 1 退出，不自动安装。
- 自动安装成功后继续启动 serve；失败时打印失败原因与手动安装命令并以退出码 1 退出。

---

## 自动安装

`aicli.EnsureInstalled`（`internal/aicli/bootstrap.go`）负责安装并就绪探测：

1. **预探测**：再做一次健康检查，若已可达直接返回（避免重复安装）。
2. **执行安装脚本**（按平台分发，输出实时打印到 stderr 便于用户看到进度）：
   - macOS / Linux：`sh -c "curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh"`
   - Windows：`powershell -Command "irm https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.ps1 | iex"`
3. **安装后健康轮询**：每 2 秒探测一次 `/healthz`，最长等待 30 秒（`healthCheckTimeout`）。通过即返回；超时返回 `aiclibridge 安装后健康检查超时`；context 取消则返回取消错误。
4. serve 调用 `EnsureInstalled` 时传入 5 分钟超时 context，足以覆盖下载 + 启动 + 轮询全过程。

自动安装失败时 serve 会打印失败原因与手动安装命令后退出，用户可手动重试。

> 跳过自动安装：`zzauto serve --no-auto-install`。该 flag 默认 false。为 true 时 aiclibridge 不可达仅提示并退出码 1，不自动安装。

---

## 手动安装与配置

### 安装（macOS / Linux）

```sh
curl -fsSL https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh | sh
```

安装后启动 aiclibridge，使其监听 `127.0.0.1:8787`（默认地址）。具体启动方式与参数请参考 aiclibridge 仓库文档。

### 安装（Windows）

```powershell
irm https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.ps1 | iex
```

具体参数与启动方式请参考 aiclibridge 仓库文档。

### 配置 zzauto 连接 aiclibridge

在 `zzauto.yaml` 中配置：

```yaml
aicli_addr: 127.0.0.1:8787   # aiclibridge 地址，可带 http:// 前缀
aicli_key: ""                 # aiclibridge 要求鉴权时填写
```

或用环境变量覆盖：

```sh
ZZAUTO_AICLI_ADDR=127.0.0.1:8787
ZZAUTO_AICLI_KEY=sk-xxx
```

若 aiclibridge 部署在远程或非默认端口，改 `aicli_addr` 即可，例如 `aicli_addr: 10.0.0.5:8787`。

### 验证连通性

```sh
# 直接探测 aiclibridge 健康端点
curl http://127.0.0.1:8787/healthz

# 启动 zzauto，观察是否通过健康检查
zzauto serve
```

---

## 关于 agents 启用

aiclibridge 侧负责配置各 AI 后端（模型路由、API key、并发等）。zzauto 侧的「agents 启用」是指编排器注册的 9 个 agent 是否参与流程——当前版本由 `registry.RegisterAgents` 固定注册全部 9 个 agent，未提供按需启停单个 agent 的配置开关。

### 每角色模型（v0.3.0）

v0.3.0 起 zzauto 支持为 9 个 agent 各自配置模型，无需改 aiclibridge 配置：

- `cfg.RoleModels`（`map[string]string`，key=stage 小写）持久化到 `zzauto.yaml` 的 `role_models` 段；env `ZZAUTO_ROLE_MODEL_<STAGE>` 覆盖（key 大写转小写）。
- `RegisterAgents(orch, askFunc, roleModels)` 把各 stage 的模型注入到对应 agent；agent 调 AI 时用 `AskWithModel(ctx, roleModel, system, user)`，roleModel 为空串时 aiclibridge 用默认模型。
- UI 在「Settings」页提供 9 行表单（`GET/PUT /api/settings/models`），PUT 时调 `cfg.Save("zzauto.yaml")` 落盘。

详见 [settings.md](./settings.md) 与 [configuration.md](./configuration.md)。

---

## Stats 与 Runs 端点（v0.3.0）

aiclibridge 在 v0.3.0 暴露 stats 与 runs 端点，供 zzauto 统计面板与任务面板使用：

### Stats（统计面板数据源）

| 端点 | 用途 | zzauto 客户端方法 | zzauto HTTP 代理路由 |
| --- | --- | --- | --- |
| `GET /v1/stats/usage` | 各模型 token 用量与 USD，含汇总 | `aicli.Usage(ctx)` | `GET /api/stats/usage` |
| `GET /v1/stats/prices` | 各模型每 1M token 定价 | `aicli.Prices(ctx)` | （仅客户端，未代理） |
| `GET /v1/stats/summary` | 总览（requests/tokens/usd/uptime） | `aicli.Summary(ctx)` | `GET /api/stats/summary` |
| `GET /v1/stats/concurrency` | 并发（active/queued/max） | `aicli.Concurrency(ctx)` | `GET /api/stats/concurrency` |

UI 统计面板每 30 秒自动刷新（可在 UI 关闭自动刷新），数据源为上述代理路由。详见 [stats.md](./stats.md)。

### Runs（任务面板数据源）

| 端点 | 用途 | zzauto 客户端方法 |
| --- | --- | --- |
| `POST /v1/runs` | 发起一次 run，SSE 流式返回事件 | `aicli.RunStream(ctx, model, system, user, onEvent)` |
| `GET /v1/runs/{id}` | 拉取指定 run 的完整事件时间线 | `aicli.GetRun(ctx, runID)` |

`RunEvent` 类型字段：`type`（thinking/text/tool_use/tool_result/result/error/system）、`content`、`tool_name`、`tool_input`、`run_id`、`error`。

zzauto 的 `agents.RunWithTracking` 用 RunStream 调用 AI，把每个事件：

1. 通过 bus 发布 `agent_run_event` 事件（含 run_id/event_type/content/tool_name/tool_input）；
2. 累积到内存切片，结束后写到 `<projectDir>/runs/<agent>/<runID>.json`。

UI 任务面板通过 `GET /api/projects/{id}/runs` 列出 run 摘要，`GET /api/projects/{id}/runs/{rid}` 读取完整事件时间线，按事件类型着色展示。详见 [task-panel.md](./task-panel.md)。

---

## 关于同步升级

`zzauto upgrade` 在 zzauto 自身升级完成后，会自动调用 `aiclibridge upgrade` 子命令同步升级 aiclibridge（见 `internal/installer/installer.go` 末尾对 `aicli.UpgradeAiclibridge` 的调用）。该步骤为 best-effort：

- aiclibridge 未在 PATH 时返回 `aiclibridge 未安装，无法同步升级`，仅打印警告不阻塞。
- `aiclibridge upgrade` 执行失败时仅打印 `警告: aiclibridge 同步升级失败: <err>`，不影响 zzauto 升级结果（zzauto 升级成功仍以退出码 0 返回）。
- 「已是最新版本」分支同样会触发 aiclibridge 同步升级。

如需单独手动升级 aiclibridge，可参考其仓库的升级方式自行执行 `aiclibridge upgrade`。

---

## 故障排查

### 健康检查失败（serve 启动退出）

现象：`serve` 打印 `aiclibridge 不可达: ...` 并退出（仅当 `--no-auto-install` 启用，或自动安装 30 秒轮询仍未通过健康检查时出现）。

排查：

1. 确认 aiclibridge 已启动且监听在 `aicli_addr` 指定的地址（默认 `127.0.0.1:8787`）。
2. 手动 `curl http://<aicli_addr>/healthz` 是否返回 2xx。
3. 检查 `zzauto.yaml` 或 `ZZAUTO_AICLI_ADDR` 是否指向正确地址（拼写、端口、协议前缀）。
4. 若 aiclibridge 在远程主机，确认网络可达且未被防火墙拦截。
5. 若自动安装失败：检查网络能否访问 `https://github.com/tgcz2011/aiclibridge/raw/main/scripts/install.sh`（可设 `GITHUB_MIRROR` 加速），或改用 `--no-auto-install` 并手动预装。

### 端口冲突

- aiclibridge 默认 `8787`，zzauto HTTP 默认 `8788`。若端口被占用：
  - 改 aiclibridge 监听端口，并同步更新 zzauto 的 `aicli_addr`；
  - 或用 `zzauto serve --listen <addr>` 改 zzauto 监听端口。

### AI 调用超时

- zzauto 的 aicli 客户端 HTTP 超时为 5 分钟。若 AI 后端响应更慢，会在 agent 侧返回 `调用 AI ... 失败: ...` 错误并终止该阶段。
- 排查 aiclibridge 自身日志、模型可用性、上游 AI 服务状态。

### 鉴权失败

- 若 aiclibridge 要求 `api_key` 而 zzauto 未配置（`aicli_key` 为空），请求会返回鉴权错误。
- 确认 `aicli_key` 与 aiclibridge 侧配置一致；`GET /api/config` 不会回显明文 token（仅 `***`）。

### 地址格式

- `aicli_addr` 可带 `http://`/`https://` 前缀与尾部斜杠，客户端归一化为 `host:port`，请求时自动补 `http://`。
- 若需 HTTPS，请在 aiclibridge 侧配置 TLS，并在 `aicli_addr` 写完整地址（当前客户端请求固定补 `http://`，HTTPS 支持以 aiclibridge 仓库说明为准）。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/aicli/client.go` | aiclibridge HTTP 客户端（Health/Chat/Messages/Ask/AskWithModel/ChatWithModel/SetModel/Model） |
| `internal/aicli/runs.go`（v0.3.0） | RunStream（SSE）+ GetRun |
| `internal/aicli/stats.go`（v0.3.0） | Usage/Prices/Summary/Concurrency |
| `cmd/zzauto/main.go` | serve 启动时的健康检查与提示 |
| `internal/config/config.go` | `aicli_addr`/`aicli_key` 字段 + `RoleModels` |
| `internal/agents/agent.go` | `AIClient` 接口定义 + `RunWithTracking` |
| `internal/ui/handler.go` | `/api/stats/*` 代理 + `/api/projects/{id}/runs/*` |
