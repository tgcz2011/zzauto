# Settings（每角色模型配置）

zzauto v0.3.0 起支持为 9 个 agent 各自配置模型，无需改 aiclibridge 配置即可让不同角色用不同模型。本文说明配置字段、UI 操作、env 覆盖、持久化与生效时机。

> 字段定义见 [configuration.md](./configuration.md)；agent 接口扩展见 [agents.md](./agents.md)；aiclibridge 端能力见 [aiclibridge.md](./aiclibridge.md)。

---

## 概述

v0.3.0 之前所有 agent 共用 aiclibridge 默认模型 `claude/anthropic/claude-sonnet-4.5`。v0.3.0 引入 `cfg.RoleModels map[string]string`，key 为 stage 小写（9 个 agent 各一个），value 为模型名（空串走默认）。模型注入流程：

1. 加载 `zzauto.yaml` 的 `role_models` 段；
2. `ZZAUTO_ROLE_MODEL_<STAGE>` env 覆盖对应 stage；
3. `registry.RegisterAgents(orch, askFunc, roleModels)` 把模型注入到对应 agent；
4. agent 调 AI 时用 `ai.AskWithModel(ctx, roleModel, system, user)`，roleModel 为空串时 aiclibridge 用默认模型。

---

## 9 个角色 stage

| stage | agent | 典型模型选择建议 |
| --- | --- | --- |
| `listener` | Listener | 中等模型（需求丰富任务，无需最强） |
| `asker` | Asker | 强模型（挑剔提问需推理能力） |
| `planner` | Planner | 强模型（规划需结构化输出） |
| `designer` | Designer | 强模型（草案质量影响后续） |
| `evaluator` | Evaluator | 强模型（批判评估需判断力） |
| `manager` | Manager | 中等模型（任务拆解相对机械） |
| `executor` | Executor | 中等模型（指令组装） |
| `generator` | Generator | 强代码模型（代码生成主战场） |
| `gittor` | Gittor | 轻量模型（commit message 生成） |

> stage 名严格小写，与 `workspace.Stage*` 常量对应。`role_models` map 的 key 大小写敏感，写入时统一小写。

---

## 配置字段

```yaml
role_models:
  listener: ""
  asker: ""
  planner: ""
  designer: ""
  evaluator: ""
  manager: ""
  executor: ""
  generator: ""
  gittor: ""
```

| 字段 | 类型 | 默认 | env 覆盖 | 说明 |
| --- | --- | --- | --- | --- |
| `role_models` | map[string]string | `{}`（全空串） | `ZZAUTO_ROLE_MODEL_<STAGE>` | key=stage 小写，value=模型名；空串走默认 |

`ZZAUTO_ROLE_MODEL_<STAGE>` 中 `<STAGE>` 大写，如 `ZZAUTO_ROLE_MODEL_GENERATOR=gpt-5` → `role_models["generator"]="gpt-5"`。env 非空覆盖 yaml 同名 key。

---

## UI

UI 顶部导航有「Settings」页（v0.3.0 新增），布局：

```
┌────────────────────────────────────────────────────────────┐
│  Settings — 每角色模型配置                          [刷新]  │
├────────────────────────────────────────────────────────────┤
│  为每个 agent 配置模型名（留空走默认                          │
│  claude/anthropic/claude-sonnet-4.5）                       │
├────────────────────────────────────────────────────────────┤
│  Listener     [______________________________] 提示文字      │
│  Asker        [______________________________]              │
│  Planner      [______________________________]              │
│  Designer     [______________________________]              │
│  Evaluator    [______________________________]              │
│  Manager      [______________________________]              │
│  Executor     [______________________________]              │
│  Generator    [______________________________]              │
│  Gittor       [______________________________]              │
├────────────────────────────────────────────────────────────┤
│                                          [重置] [保存]      │
└────────────────────────────────────────────────────────────┘
```

- 9 行表单按 stage 顺序排列，每行 label + input + 提示；
- **model input 为 datalist 下拉（v0.4.0）**：每个 input 关联一个 `<datalist>`，选项来自 `availableModels` 数组（由 `GET /api/aicli/models` 代理 aiclibridge `/v1/models` 拉取，详见下文「模型下拉数据源」）。用户可从下拉选模型，也可手动输入自定义模型名（datalist 兼容自由输入）；
- 「保存」调 `PUT /api/settings/models`，成功后 toast 提示；
- 「重置」恢复到上次 GET 的值（未保存的修改丢弃）。

> Settings 页修改仅影响**新建编排器**（即下次点「启动编排」装配的 agent）。已运行的编排器不会热更新模型——如需对运行中项目生效，停止该项目编排器再重新启动。

### 模型下拉数据源（v0.4.0）

v0.4.0 起 Settings 页加载时（`loadModels`）同时拉取 `GET /api/aicli/models`，把响应 `data[].id` 填入 `availableModels` 数组：

```js
async loadModels() {
  // 拉取已保存的 role_models（GET /api/settings/models）
  // ...
  // 拉取可用模型清单（GET /api/aicli/models，代理 aiclibridge /v1/models）
  try {
    const resp = await fetch('/api/aicli/models');
    const data = await resp.json();
    this.availableModels = (data.data || []).map(m => m.id);
  } catch (e) {
    this.availableModels = [];  // 退化为纯 input
  }
}
```

- **成功**：`availableModels` 填充模型 id 列表，每个 model input 渲染为 `<input list="models-<stage>">` + `<datalist>`，用户可下拉选或自由输入。
- **失败（aiclibridge 不可达 / 返回非 200）**：`availableModels = []`，datalist 无选项，input 退化为纯文本输入（功能不残，只是没有下拉提示），用户仍可手动填写模型名。

> 该路由只是**辅助下拉**，模型配置的持久化仍走 `PUT /api/settings/models`（写入 `zzauto.yaml` 的 `role_models` 段）。aiclibridge 侧的模型清单由其自身配置决定，zzauto 不修改。详见 [aiclibridge.md](./aiclibridge.md) 的「Models」一节。

---

## API

### `GET /api/settings/models`

返回当前 `cfg.RoleModels`：

```json
{
  "listener": "",
  "asker": "",
  "planner": "",
  "designer": "",
  "evaluator": "",
  "manager": "",
  "executor": "gpt-5",
  "generator": "",
  "gittor": ""
}
```

未配置的 stage 返回空串（非 null）。

### `PUT /api/settings/models`

请求体同上结构。处理流程：

1. 反序列化为 `map[string]string`；
2. 校验 key 必须是 9 个合法 stage 之一（否则 400 `invalid stage: <key>`）；
3. 更新 `cfg.RoleModels`；
4. 调 `cfg.Save("zzauto.yaml")` 落盘；
5. 返回 200 + 落盘后的完整 `RoleModels`。

```sh
curl -X PUT http://127.0.0.1:8788/api/settings/models \
  -H 'Content-Type: application/json' \
  -d '{"generator":"gpt-5","evaluator":"claude/anthropic/claude-opus-4"}'
```

> `Save` 用 `yaml.Marshal` 重写整个 `zzauto.yaml`，保留所有字段。若 yaml 文件由其他工具维护的手写注释，会被覆盖丢失——建议在 zzauto 启动前用 yaml 文件做配置主源，运行期修改走 Settings UI。

### `GET /api/aicli/models`（v0.4.0，模型下拉数据源）

代理 aiclibridge `GET /v1/models`，返回 OpenAI 兼容格式的可用模型清单，供 Settings 页 model input 的 datalist 下拉填充：

```json
{
  "object": "list",
  "data": [
    {"id": "claude/anthropic/claude-sonnet-4.5", "object": "model", "owned_by": "anthropic"},
    {"id": "openai/gpt-4o", "object": "model", "owned_by": "openai"}
  ]
}
```

处理流程（`internal/ui/handler.go` `handleAicliModels`）：

1. 用当前 `cfg.AicliAddr` / `cfg.AicliKey` 构造 aicli 客户端；
2. 调 `aicli.Models(ctx)` GET aiclibridge `/v1/models`；
3. 成功：200 + 原样转发响应体；
4. aiclibridge 不可达 / 返回非 2xx：502 + 错误信息（前端 catch 后 `availableModels = []` 退化为纯 input）。

```sh
curl http://127.0.0.1:8788/api/aicli/models
```

> 该路由**只读**、**不持久化**：仅用于辅助用户在 Settings 页选模型。模型配置的读写仍走 `GET/PUT /api/settings/models`。

---

## 持久化

`config.Save(path)` 流程：

1. `yaml.Marshal(cfg)` 序列化整个 `Config` 结构；
2. `os.WriteFile(path, data, 0o644)` 覆盖写入 `./zzauto.yaml`；
3. 失败返回错误，HTTP 层翻译为 500。

落盘后下次 serve 启动时 `Load()` 读回同一文件，模型配置持久生效。env `ZZAUTO_ROLE_MODEL_*` 仍会在启动时覆盖文件值，适合 CI/容器场景固定某 stage 模型。

---

## 生效时机

| 操作 | 影响范围 | 生效时机 |
| --- | --- | --- |
| 改 `zzauto.yaml` 后重启 serve | 全部 agent | 下次 serve 启动 |
| 设 `ZZAUTO_ROLE_MODEL_*` env 启动 | 对应 stage | 下次 serve 启动 |
| UI Settings 保存 | 全部 agent（更新 cfg） | **下次装配编排器**（点「启动编排」） |
| UI Settings 保存 + 已有运行编排器 | 已运行编排器不变 | 停止该项目并重启编排器后才生效 |

`registry.RegisterAgents` 在每次 `BuildOrchestrator` 时被调用，读取当前 `cfg.RoleModels` 注入到 9 个 agent。已运行的编排器持有旧 agent 实例，不会自动重新装配。

---

## 示例场景

### 场景 1：Generator 用强代码模型，其余默认

UI Settings 页填 `generator = gpt-5`，保存。新建项目启动编排后，Generator 调 AI 时 model 参数为 `gpt-5`，其余 8 个 agent 走 aiclibridge 默认模型。

### 场景 2：CI 固定 Planner 模型

```sh
export ZZAUTO_ROLE_MODEL_PLANNER=claude/anthropic/claude-opus-4
zzauto serve
```

启动时 `cfg.RoleModels["planner"]` 被覆盖为 `claude/anthropic/claude-opus-4`，所有项目装配的 Planner agent 用此模型。

### 场景 3：全空串（恢复默认）

UI Settings 页全部清空保存，或 `zzauto.yaml` 的 `role_models` 段全空串。所有 agent 走 aiclibridge 默认模型 `claude/anthropic/claude-sonnet-4.5`。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/config/config.go` | `RoleModels` 字段 + `Save(path)` + env 解析 |
| `internal/registry/registry.go` | `RegisterAgents` 接收 `roleModels` 注入到各 agent |
| `internal/agents/agent.go` | `AIClient.AskWithModel` 接口 + `RunWithTracking` |
| `internal/aicli/client.go` | `AskWithModel`/`ChatWithModel`/`SetModel`/`Model`/`Models` 实现（v0.4.0 加 `Models`） |
| `internal/ui/handler.go` | `/api/settings/models` GET/PUT 路由 + `/api/aicli/models` 代理（v0.4.0） |
| `internal/ui/web/app.js` | `loadModels` 拉 `/api/aicli/models` 填 `availableModels`（v0.4.0） |
| `internal/ui/web/index.html` | Settings 页 model input 改 `<input list>` + `<datalist>`（v0.4.0） |

---

## 相关文档

- [configuration.md](./configuration.md) — `role_models` 字段定义与 env 表
- [agents.md](./agents.md) — 9 个 agent 接口与每角色模型注入
- [aiclibridge.md](./aiclibridge.md) — `AskWithModel` 与默认模型
- [multi-projects.md](./multi-projects.md) — 按需装配编排器（Settings 生效时机相关）
- [cli.md](./cli.md) — `ZZAUTO_ROLE_MODEL_<STAGE>` env 示例
