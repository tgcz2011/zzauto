# 端到端流程

本文以一次完整交付为例，详解 zzauto 从用户需求到 git 提交的全流程、各阶段输入/输出文档、两个循环的驱动机制，以及人类介入点。

> 9 个 agent 的逐个职责见 [agents.md](./agents.md)；架构与编排器见 [architecture.md](./architecture.md)。

---

## 阶段图

```
用户提交需求
      │
      ▼
  Listener ──desire.md──▶ Asker ──need.md──▶ Planner ──spec.md──┐
                                                                │
                 ┌────────────── 讨论循环（最多 5 轮）───────────┘
                 ▼
             Designer ──deal.md──▶ Evaluator
               起草/修订            批判评估
                 ▲                    │
                 │ deal_review.md     │ 未共识 → ErrNoConsensus
                 └────────────────────┘
                                      │ 共识（deal.md status=done）
                                      ▼
                                   Manager ──task.md──▶ Executor ──instruction.md──┐
                                                                                 │
                 ┌──────────────── 评估循环（最多 3 次）──────────────────────────┘
                 ▼
             Generator ──code/ + report──▶ Evaluator
               生成/修复                   代码评估
                 ▲                          │
                 │ 评审问题(append report)  │ 不合格 → ErrEvaluationFailed
                 └──────────────────────────┘
                                            │ 通过（spec.md 打勾 + report 归档）
                                            ▼
                                        Gittor ── git commit & push
```

文档流转主线：

```
input.md → desire.md → need.md → spec.md → deal.md → task.md → instruction.md
 (UI提交)  (Listener)  (Asker)   (Planner) (Designer) (Manager) (Executor)
                                                                      │
                                                                      ▼
                                                            code/ + reports/generator.md
                                                            (Generator)
                                                                      │
                                                                      ▼
                                                  spec.md 打勾 + report → evaluated/
                                                  (Evaluator)
                                                                      │
                                                                      ▼
                                                            git 提交推送 (Gittor)
```

---

## 各阶段输入/输出文档

| 阶段 | agent | 输入文档 | 输出文档 | frontmatter stage/status |
| --- | --- | --- | --- | --- |
| 提交需求 | UI | 用户输入 | `input.md` | listener / pending |
| 1 | Listener | `input.md` | `desire.md` | listener / done |
| 2 | Asker | `desire.md` | `need.md` | asker / done |
| 3 | Planner | `need.md` | `spec.md` | planner / done |
| 4 | Designer | `spec.md`（+`deal.md`/`deal_review.md` 可选） | `deal.md` | designer / **running** |
| 4' | Evaluator（讨论） | `spec.md` + `deal.md` | `deal.md`（done）或 `deal_review.md` | evaluator |
| 5 | Manager | `desire/need/spec/deal` | `task.md` | manager / done |
| 6 | Executor | `task.md` + `spec.md` | `agents/generator/instruction.md` | executor / done |
| 7 | Generator | `instruction.md`（隔离） | `code/*` + `reports/generator.md` | generator / done |
| 7' | Evaluator（代码） | `deal/spec/task` + report + `code/` | `spec.md` 打勾 + report 归档 | evaluator |
| 8 | Gittor | `spec.md`（复核打勾） | git 提交 `code/` | gittor / done |

---

## 文档示例

### input.md（UI 提交，由 `/api/input` 写入）

```markdown
---
stage: listener
status: pending
updated_at: 2026-07-04T12:00:00Z
---
做一个命令行 todo app，支持增删改查与按优先级排序。
```

### desire.md（Listener 产出）

```markdown
---
stage: listener
status: done
updated_at: 2026-07-04T12:00:05Z
---
# 用户需求
做一个命令行 todo app，支持增删改查与按优先级排序。

# 改进点
- 空输入与超长输入的校验与友好提示
- 重复添加相同 todo 的处理策略
- 优先级相同时的稳定排序
- 数据持久化到本地文件，重启不丢失
```

### need.md（Asker 产出）

```markdown
---
stage: asker
status: done
updated_at: 2026-07-04T12:01:00Z
---
# 需求清单
- N1: 支持新增/删除/修改/查询 todo 项
- N2: 支持 todo 项标记优先级并按优先级排序
- N3: todo 数据持久化到本地文件，重启可恢复
- N4: 空输入、超长输入、重复项需有明确错误提示
```

### spec.md（Planner 产出，Evaluator 打勾）

```markdown
---
stage: planner
status: done
updated_at: 2026-07-04T12:01:30Z
---
# Todo App Spec
## Why
为命令行用户提供轻量的待办管理能力，按优先级聚焦重要事项。

## What Changes
- 新增 todo 项的增删改查命令
- 新增优先级字段与排序输出

## Impact
涉及命令行参数解析、本地文件存储、排序逻辑；无外部依赖。

## ADDED Requirements
### Requirement: 增删改查
该需求 SHALL 支持通过命令新增、删除、修改、查询 todo 项。
#### Scenario
- WHEN 用户执行 add "买牛奶" THEN 列表新增一项
- WHEN 用户执行 del 2 THEN 第 2 项被删除

### Requirement: 优先级排序
该需求 SHALL 支持为 todo 标记优先级并按优先级升序输出。
#### Scenario
- WHEN 用户执行 list THEN 输出按优先级排序的列表
```

> Evaluator 代码评估通过后，`### Requirement: ` 会被替换为 `### [x] Requirement: `（见下文「spec.md 打勾约定」）。

### deal.md（Designer+Evaluator 讨论产出）

```markdown
---
stage: evaluator
status: done
updated_at: 2026-07-04T12:02:20Z
---
# 完工协议
交付一个命令行 todo 程序，支持增删改查、优先级排序与本地持久化，所有异常路径有友好提示。

## 验收标准
- [ ] D1: add 命令新增 todo 项并写入本地文件
- [ ] D2: del 命令按序号删除指定项
- [ ] D3: list 命令按优先级升序输出
- [ ] D4: 空输入、超长输入、重复项返回明确错误提示
```

> 草案阶段 `status=running`，共识后 Evaluator 改为 `status=done`。验收点用 `- [ ] Dx: ` 标记，达成后可改为 `- [x] Dx: `。

### task.md（Manager 产出）

```markdown
---
stage: manager
status: done
updated_at: 2026-07-04T12:02:50Z
---
# Tasks
- [ ] T1: 实现命令行参数解析与分发（验收点：各子命令可被识别并路由）
- [ ] T2: 实现 todo 数据结构与本地文件持久化（验收点：重启后数据可恢复）
- [ ] T3: 实现 add/del/update/list 命令（验收点：对应 D1/D2）
- [ ] T4: 实现优先级字段与排序输出（验收点：list 按优先级升序）
- [ ] T5: 输入校验与错误提示（验收点：D4）
```

### instruction.md（Executor 准备的隔离指令）

```markdown
# 任务指令
# Tasks
- [ ] T1: 实现命令行参数解析与分发（验收点：各子命令可被识别并路由）
...（task.md 正文全文）

# Spec 要点
# Todo App Spec
...（spec.md 正文全文）

# 输出路径
- 代码输出到 `code/` 目录（相对项目根）
- report 输出到 `reports/generator.md`
```

> 注意：指令刻意**不含** desire/need/deal，保证 Generator 上下文隔离。

### reports/generator.md（Generator 自评 report）

```markdown
---
stage: generator
status: done
updated_at: 2026-07-04T12:04:00Z
---
# Generator A Report
## 完成内容
- code/main.go
- code/todo.go
- code/storage.go

## 自评
合格。已实现全部任务，命令可运行，list 按优先级排序，异常路径有提示。
```

> Evaluator 代码评估不通过时，会在 report 末尾追加 `## 评审问题` 段并列出 issues；通过后 report 移动到 `reports/evaluated/generator.md`。

---

## 讨论循环（Designer ↔ Evaluator，最多 5 轮）

由编排器 `runDiscussLoop` 驱动（`internal/orchestrator/orchestrator.go`），每轮：

1. **Designer**：读 `spec.md`（必需）+ 可选的上一轮 `deal.md` 与 `deal_review.md`，调用 AI 起草/修订 `deal.md`（`status=running`）。
2. **Evaluator**（讨论模式）：读 `spec.md` + `deal.md`，调用 AI 批判性评估，输出 JSON `{consensus, critique}`。
   - `consensus=true`：把 `deal.md` 的 `status` 改为 `done`，返回 `nil` → 循环结束。
   - `consensus=false`：把 `critique` 写入 `deal_review.md`，返回 `ErrNoConsensus` → 进入下一轮。
   - 其他错误：终止流程。

**驱动机制**：Evaluator 返回的哨兵 `ErrNoConsensus` 是「继续下一轮」的信号；编排器用 `errors.Is` 判定。达到最大轮数（`defaultMaxDiscussRounds = 5`）仍未共识，返回错误 `讨论达到最大轮数 5 仍未达成共识`，流程终止。

第一轮时 `deal.md` 与 `deal_review.md` 不存在，Designer 进入「起草模式」；后续轮进入「修订模式」。

---

## 评估循环（Generator → Evaluator，最多 3 次）

由编排器 `runEvalLoop` 驱动，每次：

1. **Generator**：仅读 `agents/generator/instruction.md`（隔离），调用 AI 生成代码（` ```lang:path ` 围栏），写入 `code/`，再调用 AI 自评，写 `reports/generator.md`。
2. **Evaluator**（代码评估模式）：读 `deal/spec/task` + report + `code/` 文件，调用 AI 评估，输出 JSON `{pass, issues}`。
   - `pass=true`：把 `spec.md` 所有 `### Requirement: ` 改为 `### [x] Requirement: `，把 report 从 `reports/generator.md` 移到 `reports/evaluated/generator.md`，返回 `nil` → 循环结束，进入 Gittor。
   - `pass=false`：把 issues 追加到 report 的 `## 评审问题` 段，返回 `ErrEvaluationFailed` → 回到 Generator 重试。
   - 其他错误：终止流程。

**驱动机制**：Evaluator 返回的哨兵 `ErrEvaluationFailed` 是「回到 Generator 重试」的信号。达到最大次数（`defaultMaxEvalRetries = 3`）仍未通过，返回错误 `评估达到最大重试次数 3 仍未通过`，流程终止。

**模式判定**：Evaluator 启动时检查 `reports/generator.md` 存在且 `reports/evaluated/generator.md` 不存在 → 代码评估模式；否则 → 讨论模式。因此同一个 Evaluator agent 能在两个循环中复用。

---

## spec.md 打勾约定

`internal/agents/schema.go` 定义两个前缀常量：

- `SpecRequirementPrefix = "### Requirement: "`（未完成）
- `SpecRequirementDonePrefix = "### [x] Requirement: "`（已完成）

Evaluator 代码评估通过时，执行字符串替换：

```go
updatedSpec := strings.Replace(specContent, SpecRequirementPrefix, SpecRequirementDonePrefix, -1)
```

即把所有 `### Requirement: ` 改为 `### [x] Requirement: `。Gittor 在提交前复核 `spec.md` 中**不含** `### Requirement: ` 前缀（已完成的是 `### [x] Requirement: `，前缀不同不会误匹配），存在未打勾项则失败。

类似地：

- `deal.md` 验收点：`- [ ] Dx: `（未完成）/ `- [x] Dx: `（已完成）。
- `task.md` 任务项：`- [ ] Tx: `（未完成）/ `- [x] Tx: `（已完成）。

---

## 人类介入点

zzauto 全流程自动，但在三处需要人类介入：

### 1. UI 提交需求（流程起点）

用户在浏览器 `http://<listen>` 输入需求，`POST /api/input` 写入 `input.md`（带 frontmatter），触发 Listener。未提交时 Listener 会发布 `ask_user` 事件提示「请先通过 UI 提交需求」并返回错误。

### 2. Asker 问答（澄清需求）

Asker 以挑剔模式提出 1–3 个问题，经 `AskFunc` 回调到达 UI：

- **UI 模式**：`Handler.AskUser` 生成 askID，发布 `ask_user` 事件，阻塞等待 channel 回复；用户在 UI 待回答列表看到问题，`POST /api/ask/{id}` 回答，回答投递到 channel，Asker 继续。单次提问等待最长 10 分钟（`askTimeout`），超时返回错误。
- **无 UI 模式**（`askViaBus`）：发布 `ask_user` 事件后轮询 `ask_reply.md`（1 秒间隔，10 分钟超时），人工写入该文件即视为回答。

提问循环最多 10 轮（`maxAskerRounds`），防止 AI 永不满足导致死循环。

### 3. GitHub 配置（提交前置）

Gittor 提交需要 git 远程仓库配置：

- **UI**：`POST /api/github` 填写 remote/branch/token（仅内存更新，不落盘）。
- **zzauto.yaml**：启动前写入 `github` 段。
- **环境变量**：`ZZAUTO_GITHUB_REMOTE` / `ZZAUTO_GITHUB_BRANCH` / `ZZAUTO_GITHUB_TOKEN`。

未配置 remote/branch 时，Gittor 的 `CommitAndPush` 会在 push 阶段失败（推到空 origin）。配置详见 [configuration.md](./configuration.md) 的「GitHub 配置说明」。

---

## 完整时序（一次成功交付）

```
1.  用户 UI 提交需求            → input.md
2.  Listener 调用 AI 丰富        → desire.md
3.  Asker 提问 → 用户回答（×N轮）→ need.md
4.  Planner 规划                 → spec.md
5.  Designer 起草 deal.md        → deal.md(running)
6.  Evaluator 批判               → deal_review.md, ErrNoConsensus
7.  Designer 修订                → deal.md(running)
8.  Evaluator 共识               → deal.md(done)
9.  Manager 拆解任务             → task.md
10. Executor 准备隔离指令        → instruction.md
11. Generator 生成代码 + 自评    → code/*, reports/generator.md
12. Evaluator 代码评估           → spec.md 打勾, report → evaluated/
13. Gittor 提交推送              → git commit & push
```

任一阶段非哨兵错误即终止；两个循环达到上限即终止。成功路径下，用户从提交需求到看到 git 提交全程无需干预（除 Asker 问答与 GitHub 配置）。

---

## 相关文件

| 文件 | 职责 |
| --- | --- |
| `internal/orchestrator/orchestrator.go` | 流程状态机与两个循环 |
| `internal/agents/schema.go` | 文档 schema 与打勾前缀常量 |
| `internal/agents/errors.go` | `ErrNoConsensus` / `ErrEvaluationFailed` 哨兵 |
| `internal/ui/handler.go` | UI 介入点（`/api/input`、`AskUser`、`/api/github`） |
| `internal/workspace/doc.go` | 阶段/状态/文档名常量与 frontmatter 解析 |
