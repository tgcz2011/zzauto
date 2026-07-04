# 架构

zzauto 是多层 agent 协作的 AI 自主编程平台，采用**文档驱动 + 上下文隔离 + 哨兵错误驱动循环**的设计。本文自顶向下描述分层架构与核心组件。

> 9 个 agent 的逐个详解见 [agents.md](./agents.md)；端到端流程见 [workflow.md](./workflow.md)。

---

## 总体架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                        浏览器 Web UI                             │
│              (内嵌 index.html / app.js / style.css)              │
└───────────────────────────┬─────────────────────────────────────┘
                            │ HTTP / SSE
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                      HTTP API (internal/ui)                      │
│  /  /static/  /api/state  /api/docs  /api/input  /api/asks       │
│  /api/ask  /api/github  /api/config  /api/events(SSE) /healthz   │
└───────────────────────────┬─────────────────────────────────────┘
                            │ 事件订阅/发布
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                   事件总线 (internal/eventbus)                   │
│        发布/订阅 channel，缓冲 256，非阻塞投递，SSE 推送           │
└───────────────────────────┬─────────────────────────────────────┘
                            │ 调度
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                   编排器 (internal/orchestrator)                  │
│   状态机：Listener→Asker→Planner→(Designer↔Evaluator)            │
│           →Manager→Executor→(Generator→Evaluator)→Gittor         │
│   循环：讨论循环(≤5) / 评估循环(≤3)，哨兵 error 驱动               │
└──────┬──────────────────────────┬───────────────────┬───────────┘
       │ 读写文档                  │ 调用 AI            │ 调用 git
       ▼                          ▼                   ▼
┌──────────────┐   ┌──────────────────────┐   ┌──────────────────┐
│ workspace    │   │ aicli 客户端          │   │ gittor 隔离层     │
│ 文档协议+目录 │   │ (internal/aicli)      │   │ (internal/gittor) │
│ desire/need/ │   │ OpenAI/Anthropic 兼容 │   │ git CLI 封装      │
│ spec/deal/   │   └──────────┬───────────┘   │ token 不落盘      │
│ task/reports │              │ HTTP            └────────┬─────────┘
└──────────────┘              ▼                         │ git CLI
                  ┌──────────────────────┐              ▼
                  │  aiclibridge (外部)   │         远端仓库
                  │  本地 AI 网关 HTTP    │       (GitHub 等)
                  │  /v1/chat/completions │
                  │  /v1/messages         │
                  │  /healthz             │
                  └──────────┬───────────┘
                             │
                             ▼
                       各 AI 后端
```

分层职责：

- **UI 层**：embed 进单二进制的 Web UI，SSE 实时推送 agent 事件，Asker 交互问答经浏览器完成。
- **HTTP API 层**：REST 接口 + SSE 端点，桥接 UI 与编排器/工作区。
- **事件总线层**：发布/订阅解耦，agent 生命周期与文档更新事件广播给订阅者。
- **编排器层**：固定流程状态机 + 两个循环（讨论、评估），哨兵 error 驱动循环继续/终止。
- **workspace 层**：项目目录与文档协议，agent 间通过文件系统传递上下文。
- **aicli 客户端层**：HTTP 调用 aiclibridge，统一 AI 调用入口。
- **gittor 隔离层**：git CLI 封装，所有 git 操作唯一入口。

---

## 编排器（orchestrator）

`internal/orchestrator/orchestrator.go` 实现多 agent 编排，是 zzauto 的「指挥中枢」。

### 状态机

`Run` 按固定顺序执行各阶段：

```
1. Listener
2. Asker
3. Planner
4. Designer↔Evaluator 讨论循环（最多 maxDisc 轮，默认 5）
5. Manager
6. Executor
7. Generator → Evaluator 评估循环（最多 maxEval 次，默认 3）
8. Gittor
```

- 顺序阶段（1/2/3/5/6/8）由 `runStage` 统一调度：发布 `agent_start` → 执行 `Agent.Run` → 成功发布 `agent_done`、失败发布 `agent_failed` 并返回错误。
- 某阶段 agent 未注册时返回 `agent <stage> not registered`。
- 任一阶段返回非哨兵 error 即终止整个 `Run`。

### 讨论循环（Designer ↔ Evaluator）

`runDiscussLoop` 每轮先调 Designer（起草/修订 `deal.md`），再调 Evaluator（批判评估）：

| Evaluator 返回 | 含义 | 编排器动作 |
| --- | --- | --- |
| `nil` | 达成共识 | 结束循环，发布 log 事件，进入下一阶段 |
| `ErrNoConsensus` | 未共识 | 发布 log 事件，进入下一轮 |
| 其他 error | 真实失败 | 包装错误并返回，终止流程 |

达到最大轮数（默认 5）仍未共识，返回错误 `讨论达到最大轮数 N 仍未达成共识`。最大轮数可由 `SetMaxDiscussRounds` 覆盖（测试用）。

### 评估循环（Generator → Evaluator）

`runEvalLoop` 每次先调 Generator（生成/修复代码与 report），再调 Evaluator（代码评估）：

| Evaluator 返回 | 含义 | 编排器动作 |
| --- | --- | --- |
| `nil` | 评估通过 | 结束循环，进入 Gittor |
| `ErrEvaluationFailed` | 不合格 | 发布 log 事件，回到 Generator 重试 |
| 其他 error | 真实失败 | 包装错误并返回，终止流程 |

达到最大重试次数（默认 3）仍未通过，返回错误 `评估达到最大重试次数 N 仍未通过`。最大次数可由 `SetMaxEvalRetries` 覆盖（测试用）。

> 哨兵错误定义在 `internal/agents/errors.go`：`ErrNoConsensus`、`ErrEvaluationFailed`。Agent 接口的 `Run` 仅返回 `error`，编排器用 `errors.Is` 区分「正常完成」「需继续循环」「真实失败」。

### 装配（registry）

`internal/registry/registry.go` 负责组件装配：

- `BuildOrchestrator`（生产用）：创建 aicli 客户端、gittor、初始化 git 仓库（`EnsureRepo`），注册全部 9 个 agent。
- `BuildOrchestratorWithDeps`（测试用）：AI 与 git 客户端由调用方注入，便于用 mock。
- `RegisterAgents`：按阶段顺序注册 Listener/Asker/Planner/Designer/Evaluator/Manager/Executor/Generator/Gittor。

---

## 事件总线（eventbus）

`internal/eventbus/eventbus.go` 提供发布/订阅机制，解耦编排器与 UI/日志。

### 事件类型

| 常量 | 含义 |
| --- | --- |
| `EventAgentStart` | agent 开始执行 |
| `EventAgentDone` | agent 成功完成 |
| `EventAgentFailed` | agent 执行失败 |
| `EventDocUpdate` | 文档被写入/更新 |
| `EventAskUser` | 需要向用户提问（Asker 交互） |
| `EventLog` | 通用日志事件 |

`Event` 结构：`Type`、`Agent`、`Data any`、`Time`（零值时自动填充当前时间）。

### 设计要点

- **多订阅者**：每个 `Subscribe()` 返回独立的只读 channel。
- **不阻塞发布者**：订阅者 channel 满（缓冲 256）时直接丢弃事件（`select default`），保证 agent 不因 UI 慢而卡住。
- **幂等关闭**：`Close()` 关闭所有订阅者 channel，此后 `Publish` 为空操作。

### SSE 推送

UI 的 `/api/events` 端点订阅 bus，把事件序列化为 JSON 通过 SSE（`text/event-stream`）推送给浏览器；客户端断连（请求上下文取消）时退出。UI handler 另起一个订阅者把事件转换为流程状态更新（`/api/state`）。

---

## workspace

`internal/workspace/` 定义项目工作区与文档协议。

### 目录结构

```
<WorkspaceDir>/projects/<projectID>/
  input.md            # 用户通过 UI 提交的原始需求（带 frontmatter）
  desire.md           # Listener 产出
  need.md             # Asker 产出
  spec.md             # Planner 产出，Evaluator 打勾
  deal.md             # Designer+Evaluator 讨论产出
  deal_review.md      # Evaluator 讨论批评（中间产物）
  task.md             # Manager 产出
  agents/generator/
    instruction.md    # Executor 准备的隔离指令
  code/               # Generator 产出的代码
  reports/
    generator.md      # Generator 的自评 report
    evaluated/
      generator.md    # Evaluator 通过后 report 移动到此
```

- `projectID` 由 `GenerateProjectID()` 生成：`20060102-150405-<6位hex>`。
- `EnsureDirs()` 创建 `projectDir`、`agents/`、`reports/`。

### 文档协议

每份文档由 **frontmatter + 正文** 组成。frontmatter 为 YAML：

```yaml
---
stage: listener
status: done
updated_at: 2026-07-04T12:00:00Z
---
```

- `DocMeta` 字段：`Stage`、`Status`（pending/running/done/failed）、`UpdatedAt`。
- `ParseDoc` 分离 frontmatter 与正文；`RenderDoc` 把元信息与正文渲染为带 frontmatter 的文档。
- 各文档正文格式约定见 `internal/agents/schema.go`（`DesireTemplate`/`NeedTemplate`/`SpecTemplate`/`DealTemplate`/`TaskTemplate`/`ReportTemplate`），完整示例见 [workflow.md](./workflow.md)。

### 阶段与状态常量

阶段：`listener`/`asker`/`planner`/`designer`/`evaluator`/`manager`/`executor`/`generator`/`gittor`。
状态：`pending`/`running`/`done`/`failed`。

---

## aicli 客户端

`internal/aicli/client.go` 封装 aiclibridge 的 HTTP 调用，满足 `agents.AIClient` 接口（鸭子类型，`Ask` 方法）。

- **默认模型**：`claude/anthropic/claude-sonnet-4.5`；**默认 max_tokens**：4096（Anthropic 接口）。
- **HTTP 超时**：5 分钟（AI 推理较慢）。
- **健康检查**：`Health(ctx)` GET `/healthz`，2xx 视为可达。
- **OpenAI 兼容**：`Chat` 调 `/v1/chat/completions`。
- **Anthropic 兼容**：`Messages` 调 `/v1/messages`。
- **便捷方法**：`Ask(ctx, system, user)` 默认走 OpenAI 兼容接口（即 `Chat`），是 agent 实际使用的方法。
- **鉴权**：`api_key` 非空时设置 `Authorization: Bearer <key>`。
- 地址归一化：去掉 `http://`/`https://` 前缀与尾部斜杠，统一为 `host:port`。

> aiclibridge 是外部本地 HTTP 服务，详见 [aiclibridge.md](./aiclibridge.md)。

---

## gittor 隔离层

`internal/gittor/gittor.go` 封装所有 git 操作（init/add/commit/push/status），使用 `git` CLI（`os/exec`）而非 `gh api`，避免频率限制。其他 agent 不直接碰 git，只通过 `GittorClient` 接口请求，确保 git 操作上下文隔离。

### 核心方法

- `EnsureRepo`：无 `.git` 则 `git init`；`origin` 未设置则 `git remote add origin <remote>`；`checkout` 到目标分支（不存在则创建）。
- `CommitAndPush`：校验 commit message 前缀（conventional commits）→ `git add`（paths 为空时 `add -A`）→ `git commit -m` → `push`。
- `Status`：`git status --porcelain` 摘要。

### token 安全

- 使用 HTTPS remote 且配置了 token 时，push 直接推到内嵌 token 的 URL：`https://x-access-token:<token>@github.com/owner/repo.git`，**不写入 git config**，避免 token 落盘泄露。
- `runGit` 的所有输出经 `redact` 处理，把 token 替换为 `***`，避免泄露到日志/事件。
- commit message 校验：允许 `feat`/`fix`/`docs`/`style`/`refactor`/`perf`/`test`/`build`/`ci`/`chore`/`revert`，支持 `feat(scope)!:` 等变体。

> Gittor agent（`internal/agents/gittor_agent.go`）在评估通过后调用 `CommitAndPush(ctx, ["code/"], "feat: 实现 <projectID> 项目代码")`。

---

## 设计原则

### 1. 上下文隔离

每个 agent 只读它该读的文档，互不污染：

- **Listener** 只读 `input.md`，写 `desire.md`。
- **Generator** 只读 `agents/generator/instruction.md`（Executor 投喂的隔离指令），**不读** `desire/need/spec/deal/task.md`——保证它只能基于指令工作，无法触达用户原始欲望与上游讨论过程，避免「越权发挥」。
- **git 操作**仅由 Gittor 经 `GittorClient` 在隔离层执行，其他 agent 不直接调 git CLI。

### 2. 文档驱动

agent 间不直接传内存对象，而是通过 workspace 文件系统传递文档：

```
desire.md → need.md → spec.md → deal.md → task.md → instruction.md → code/+report → spec 打勾 → git 提交
```

文档即协议、即进度、即审计。每份文档有 frontmatter（stage/status/updated_at）与严格正文 schema（见 `schema.go`），便于解析与 UI 展示。

### 3. 哨兵错误驱动循环

Agent 接口的 `Run` 仅返回 `error`，编排器用哨兵 error 表达循环状态：

- `ErrNoConsensus`：讨论未共识，继续下一轮。
- `ErrEvaluationFailed`：评估不合格，回到 Generator 重试。
- 非哨兵 error 一律视为真实失败，终止流程。

这让循环逻辑与 agent 实现解耦：agent 只需返回正确的哨兵，编排器决定循环边界。

### 4. 非阻塞事件总线

agent 通过 bus 发布事件但不阻塞——订阅者 channel 满即丢弃，保证 AI 推理与流程推进不被 UI 慢消费拖累。UI 通过 SSE 订阅同一总线获得实时更新。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `cmd/zzauto/main.go` | CLI 入口，子命令分发与 serve 装配 |
| `internal/orchestrator/orchestrator.go` | 编排器与循环 |
| `internal/registry/registry.go` | 组件装配 |
| `internal/agents/*.go` | 9 个 agent 实现与接口/schema/哨兵错误 |
| `internal/workspace/*.go` | 工作区与文档协议 |
| `internal/eventbus/eventbus.go` | 事件总线 |
| `internal/aicli/client.go` | aiclibridge HTTP 客户端 |
| `internal/gittor/gittor.go` | git 隔离层 |
| `internal/ui/handler.go` | HTTP API 与 SSE |
| `internal/installer/installer.go` | 自卸载与自升级 |
| `internal/config/config.go` | 配置加载 |
