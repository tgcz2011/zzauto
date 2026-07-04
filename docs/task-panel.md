# 任务面板

zzauto v0.3.0 新增任务面板，按项目+agent 查看每次 run 的完整事件时间线（thinking/text/tool_use/tool_result/result/error/system），并支持 SSE 实时推送。本文说明面板布局、事件着色、数据来源、SSE 推送与故障排查。

> run 事件持久化机制见 [aiclibridge.md](./aiclibridge.md) 的 Runs 端点段；agent 接口见 [agents.md](./agents.md)。

---

## 概述

任务面板是 UI 顶部导航的第 4 个页面（项目 / 设置 / 统计 / 任务面板），用于：

- 查看每个项目下 9 个 agent 的全部 run 历史；
- 展开单个 run，看其完整事件流（AI 的 thinking、文本输出、工具调用、最终结果或错误）；
- 实时观察正在进行的 run（SSE 推送新事件）；
- 排查「为什么这个 agent 失败了」「AI 究竟输出了什么」。

数据分两层：zzauto 本地持久化的 run 摘要与事件（`<projectDir>/runs/<agent>/<runID>.json`），以及实时 SSE 推送。

---

## UI 布局

```
┌──────────────────────────────────────────────────────────────────────┐
│  任务面板              项目: [todo-app ▾]   agent: [all ▾]   [刷新]  │
├──────────────────────────────────────────────────────────────────────┤
│  Agent      Run ID         开始时间        状态      事件数  操作     │
├──────────────────────────────────────────────────────────────────────┤
│  generator  01HXY...       12:04:00        done      23      [展开]  │
│  evaluator  01HXY...       12:05:30        done      18      [展开]  │
│  asker      01HXW...       12:01:00        done      12      [展开]  │
│  generator  01HXV...       12:03:00        error     8       [展开]  │
└──────────────────────────────────────────────────────────────────────┘
```

- 顶部下拉：项目（默认当前选中项目）、agent（默认 all，可选 9 个 agent 之一）；
- 表格按 run 开始时间倒序，最新在上；
- 「状态」列：`running` / `done` / `error`；
- 「事件数」列：该 run 持久化的事件总数；
- 「展开」按钮：折叠/展开该 run 的完整事件时间线。

> 仅显示当前选中项目的 run；切换项目需在顶部下拉或返回项目页选中。

---

## 事件时间线

展开某 run 后显示事件列表，每条事件按类型着色：

```
┌──────────────────────────────────────────────────────────────────────┐
│  Run 01HXY... (generator)                                            │
├──────────────────────────────────────────────────────────────────────┤
│  [12:04:00] [system]    run started, model=gpt-5                     │
│  [12:04:01] [thinking]  我需要先分析 task.md 的任务清单...           │
│  [12:04:03] [text]      ```go:code/main.go\npackage main...          │
│  [12:04:05] [tool_use]  write_file(path=code/main.go)                │
│  [12:04:05] [tool_result] wrote 234 bytes                            │
│  [12:04:06] [text]      ```go:code/todo.go\npackage main...          │
│  [12:04:08] [tool_use]  write_file(path=code/todo.go)                │
│  [12:04:08] [tool_result] wrote 456 bytes                            │
│  [12:04:10] [result]    run completed, status=done                   │
└──────────────────────────────────────────────────────────────────────┘
```

### 事件类型与着色

| 事件类型 | 颜色 | 含义 |
| --- | --- | --- |
| `system` | 灰色 | 系统事件（run 开始、模型信息） |
| `thinking` | 蓝色 | AI 思考过程（CoT，可能很长） |
| `text` | 黑色 | AI 文本输出（代码、文档等） |
| `tool_use` | 橙色 | AI 调用工具（含 `tool_name` 与 `tool_input`） |
| `tool_result` | 绿色 | 工具返回结果 |
| `result` | 紫色 | run 最终结果（成功/失败状态） |
| `error` | 红色 | 错误事件（含 `error` 字段） |

每条事件显示：时间戳 + 类型徽章 + 内容。`tool_use` 额外显示 `tool_name` 与 `tool_input`；`error` 额外显示 `error` 文本。

> 着色由 `internal/ui/web/style.css` 的 CSS 类实现，类型徽章用 `<span class="badge badge-thinking">thinking</span>` 形式。

---

## 数据来源

### 持久化文件

每个 agent 的每次 run 在结束后写到 `<projectDir>/runs/<agent>/<runID>.json`，结构：

```json
{
  "run_id": "01HXY...",
  "agent": "generator",
  "model": "gpt-5",
  "started_at": "2026-07-04T12:04:00Z",
  "ended_at": "2026-07-04T12:04:10Z",
  "status": "done",
  "events": [
    {"type":"system","content":"run started, model=gpt-5","run_id":"01HXY..."},
    {"type":"thinking","content":"我需要先分析 task.md..."},
    {"type":"text","content":"```go:code/main.go\\npackage main..."},
    {"type":"result","content":"run completed, status=done","run_id":"01HXY..."}
  ]
}
```

由 `agents.RunWithTracking` 辅助函数封装：调 `aicli.RunStream` 时每个事件通过回调累积到内存切片，run 结束后整体写到上述文件。

### SSE 实时推送

正在进行中的 run，其事件通过 eventbus 实时广播：

- `agents.RunWithTracking` 在每个事件回调里发布 `eventbus.EventAgentRunEvent` 事件（含 run_id/agent/event_type/content/tool_name/tool_input）；
- UI 通过 `/api/events` SSE 订阅全部事件，`agent_run_event` 类型即 run 事件；
- 任务面板收到 `agent_run_event` 时，若该 run 已展开，追加到时间线末尾；若未展开，更新表格的「事件数」与「状态」。

> SSE 推送只覆盖进行中的 run；历史 run 的完整事件从持久化文件读取。

---

## API

| 方法 | 路由 | 用途 |
| --- | --- | --- |
| GET | `/api/projects/{id}/runs` | 列出该项目全部 run 摘要（agent/run_id/started_at/status/events_count） |
| GET | `/api/projects/{id}/runs/{rid}` | 返回指定 run 的完整事件时间线 |
| GET | `/api/events` | SSE 流，订阅 `agent_run_event` 等全部事件 |

### `GET /api/projects/{id}/runs`

可选 query `?agent=generator` 过滤 agent。返回：

```json
[
  {"agent":"generator","run_id":"01HXY...","started_at":"2026-07-04T12:04:00Z","status":"done","events_count":23},
  {"agent":"evaluator","run_id":"01HXY...","started_at":"2026-07-04T12:05:30Z","status":"done","events_count":18}
]
```

按 `started_at` 倒序。读取流程：扫描 `<projectDir>/runs/*/` 子目录，每个子目录下的 JSON 文件即一个 run，提取摘要字段。

### `GET /api/projects/{id}/runs/{rid}`

返回完整 run 详情（同上文持久化文件结构）。`rid` 不存在返回 404。

### SSE 推送

`/api/events` 是 v0.2.0 既有路由，v0.3.0 新增 `agent_run_event` 事件类型：

```
event: agent_run_event
data: {"agent":"generator","run_id":"01HXY...","event_type":"thinking","content":"我需要先分析...","tool_name":"","tool_input":""}
```

UI 任务面板订阅 SSE，收到 `agent_run_event` 时：

1. 若当前展开的 run 与 `data.run_id` 匹配，追加渲染到时间线；
2. 否则仅刷新表格的「事件数」与「状态」列。

---

## 示例场景

### 场景 1：排查 Generator 失败

1. 任务面板选中失败的项目与 `generator` agent；
2. 找到 `status=error` 的 run，点「展开」；
3. 滚动到末尾看 `error` 事件，错误信息形如 `调用 AI 失败: context deadline exceeded`；
4. 也可看 `thinking` 事件，确认 AI 是否在某个工具调用前就超时。

### 场景 2：观察进行中的 run

1. 在项目页启动编排后切到任务面板；
2. 选中该项目与 `all` agent；
3. 新 run 出现时表格自动新增行，`status=running`；
4. 点「展开」，事件实时追加到时间线（SSE 推送）；
5. run 结束后状态变为 `done` 或 `error`。

### 场景 3：对比 Evaluator 在讨论循环与代码评估的 run

1. 任务面板选中项目与 `evaluator` agent；
2. 列表会显示该 agent 的全部 run（含讨论模式与代码评估模式）；
3. 展开 run 看 `thinking` 事件：讨论模式 thinking 围绕 `deal.md` 批判；代码评估模式围绕 `code/` 与 `reports/generator.md` 评估。

---

## 故障排查

### 表格为空

- 该项目尚未启动编排器（无 run 产生）；
- agent 下拉选了具体 agent 但该 agent 未触发（如 Evaluator 在讨论共识前不触发代码评估 run）；
- 切换项目下拉确认选中的是目标项目。

### 展开后无事件

- run 持久化文件损坏或被手动删除；
- 调 `GET /api/projects/{id}/runs/{rid}` 看返回内容，若 404 则文件不存在；
- run 异常中断（如 serve 崩溃）可能未写出完整文件，仅保留内存中的部分事件。

### SSE 不实时推送

- 检查 `/api/events` 连接是否建立（浏览器 Network 面板看 EventSource 状态）；
- serve 重启后 SSE 连接需重新建立，刷新任务面板页；
- 部分浏览器对 SSE 有连接数限制，关闭其他 zzauto 标签页再试。

### 事件内容乱码

- `thinking` 与 `text` 事件可能含多行代码与中文，UI 用 `<pre>` 渲染保留换行；
- 若代码围栏 ``` 没闭合，渲染可能错位，属 AI 输出问题而非面板 bug。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/agents/agent.go` | `RunWithTracking` 辅助函数（RunStream + 事件持久化 + bus 广播） |
| `internal/aicli/runs.go` | `RunStream`（SSE 解析）+ `GetRun`（详情拉取） |
| `internal/eventbus/eventbus.go` | `EventAgentRunEvent` 事件常量 |
| `internal/ui/handler.go` | `/api/projects/{id}/runs`、`/api/projects/{id}/runs/{rid}` 路由 + `/api/events` SSE |
| `internal/ui/web/app.js` | 任务面板前端渲染与 SSE 订阅 |
| `internal/ui/web/style.css` | 事件类型着色 CSS |

---

## 相关文档

- [aiclibridge.md](./aiclibridge.md) — `/v1/runs` SSE 与 `/v1/runs/{id}` 详情端点
- [agents.md](./agents.md) — `AIClient` 接口与 `RunWithTracking`
- [multi-projects.md](./multi-projects.md) — run 文件位于 `<projectDir>/runs/<agent>/`
- [stats.md](./stats.md) — 另一个 v0.3.0 新页面
- [architecture.md](./architecture.md) — `agent_run_event` 事件在事件总线中的位置
