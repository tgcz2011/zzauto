# 9 个 Agent 详解

zzauto 把一次软件交付拆成 9 个固定角色，按文档驱动的流程顺序协作。每个 agent 都有清晰的输入文档与产出文档，互不污染上下文。

> 流程与文档流转见 [workflow.md](./workflow.md)；架构总览见 [architecture.md](./architecture.md)。

## 统一接口

所有 agent 实现 `agents.Agent` 接口：

```go
type Agent interface {
    Name() string
    Run(ctx, ws, ai, git, bus) error
}
```

- `Name()` 返回与 workspace 阶段常量对应的标识（如 `"listener"`）。
- `Run` 读取上游文档、调用 AI、产出下游文档、通过 bus 发布事件。失败返回非 nil error。
- 依赖以接口注入：`AIClient`（AI 调用）、`GittorClient`（git 操作）、`*eventbus.Bus`（事件）。

事件约定：每个 agent 在执行前后发布 `agent_start` / `agent_done` / `agent_failed`，文档更新时发布 `doc_update`。

---

## 1. Listener 倾听者

- **职责**：听取用户通过 UI 提交的原始需求，在其基础上补充工程改进点，产出 `desire.md`。
- **输入文档**：`input.md`（用户经 UI 提交，带 frontmatter）。
- **输出文档**：`desire.md`（`# 用户需求` + `# 改进点` 列表，frontmatter stage=listener/status=done）。
- **system prompt 要点**：保留用户原始需求原话不改写；从可访问性、错误处理、边界情况、性能、安全、可维护性六个维度补充与需求切实相关的改进点；严格输出 schema 约定的 Markdown，不加代码围栏、不加额外说明。
- **交互**：若 `input.md` 不存在，发布 `ask_user` 事件提示「请先通过 UI 提交需求」并返回错误。
- **关键设计**：原话保留 + 自动补改进点，为 Asker 提供「挑剔」的素材。

## 2. Asker 询问者

- **职责**：基于 `desire.md` 挑剔地向用户提问，澄清未明确的关键点，直到需求充分明确，再把问答历史整理成 `need.md`。
- **输入文档**：`desire.md`。
- **输出文档**：`need.md`（`# 需求清单` + `- N1: ...` 列表，frontmatter stage=asker/status=done）。
- **system prompt 要点**：扮演极其挑剔、批判性的需求澄清专家；从边界情况、非功能需求、技术约束、验收标准、用户与场景、安全合规、交互流程七维度审视；每次只问 1–3 个最相关问题，不重复已问问题，不问可合理默认的问题；输出 JSON `{"questions":[...], "satisfied": bool}`。
- **交互**：通过 `AskFunc` 回调向用户提问（UI 经 `/api/asks` + `/api/ask/{id}` 回答）。回调为 nil 时用默认 `askViaBus`：发布 `ask_user` 事件后轮询 `ask_reply.md` 文件（1 秒间隔，10 分钟超时），无 UI 时人工写入该文件即可回答。
- **关键设计（挑剔模式）**：
  - 提问循环最多 `maxAskerRounds = 10` 轮，防止 AI 永不满足导致死循环。
  - AI 输出 JSON 容错解析：提取首个 `{` 到末个 `}` 之间的子串；解析失败则把整段当一个问题问用户。
  - `satisfied=true` 且 `questions` 为空时结束循环；调用第二个 system prompt（`askerSummaryPrompt`）把问答历史整理成 `need.md`。
  - 问答历史以 `## 第 N 轮` / `Q: ... / A: ...` 累积拼装为 AI 上下文。

## 3. Planner 规划者

- **职责**：读取 `need.md`，调用 AI 生成结构化 `spec.md`（Why / What Changes / Impact / ADDED Requirements）。
- **输入文档**：`need.md`。
- **输出文档**：`spec.md`（frontmatter stage=planner/status=done）。
- **system prompt 要点**：项目名从 need 内容提炼；What Changes 至少 1 个变更点；每个 Requirement 必须含 SHALL 描述与至少一个 `#### Scenario`（`- WHEN ... THEN ...` 句式）；Requirement 标题用 `### Requirement: <名>`，**不带** `[x]`/`[ ]`（完成标记由 Evaluator 后续填写）；全文中文，不输出代码围栏。
- **交互**：不与用户交互，仅一次 AI 调用产出完整 spec。
- **关键设计**：spec 的 Requirement 标题前缀是 Evaluator 打勾约定的基础（`### Requirement: ` → `### [x] Requirement: `）。

## 4. Designer 设计者

- **职责**：根据 `spec.md` 起草或修订 `deal.md`（完工协议 + 可客观判定的验收标准清单）。
- **输入文档**：`spec.md`（必需）；`deal.md` 草案、`deal_review.md`（Evaluator 批评，均为可选，第一轮不存在）。
- **输出文档**：`deal.md`（`# 完工协议` + `## 验收标准` + `- [ ] D1: ...` 列表，frontmatter stage=designer/**status=running**）。
- **system prompt 要点**：验收点 id 形如 D1/D2… 单调递增；每项格式 `- [ ] Dx: 描述`；必须可被客观判定（避免「用户体验好」等主观措辞）；必须覆盖 spec 所有 Requirement，每个 Requirement 至少一项验收点；关注错误路径/空输入/越权/并发等边界。
- **两种工作模式**：
  - **起草模式**：上下文无上一轮草案与批评 → 从 spec 全新起草。
  - **修订模式**：上下文含上一轮 `deal.md` 与 Evaluator 批评 → 吸收合理批评、反驳不合理批评，但保持输出格式不变，不新增辩论段落。
- **交互**：在讨论循环中被编排器多次调用，每轮读 `deal_review.md` 决定修订方向。
- **关键设计**：Designer 自身不标记完成，`deal.md` 的 `status=running`；共识后由 Evaluator 改为 `status=done`。

## 5. Evaluator 评估者

- **职责**：在讨论循环与评估循环都由这同一个 agent 承担，依据 workspace 状态智能判定模式。
- **输入文档**：
  - 讨论模式：`spec.md` + `deal.md`。
  - 代码评估模式：`deal.md` + `spec.md` + `task.md` + `reports/generator.md` + `code/` 下代码文件。
- **输出文档**：
  - 讨论模式：共识时把 `deal.md` 的 `status` 改为 `done`；未共识时写 `deal_review.md`（批评）。
  - 代码评估模式：通过时把 `spec.md` 所有 `### Requirement: ` 改为 `### [x] Requirement: `，并把 report 从 `reports/generator.md` 移到 `reports/evaluated/generator.md`；不通过时把 issues 追加到 report 的 `## 评审问题` 段。
- **模式判定**：`reports/generator.md` 存在且 `reports/evaluated/generator.md` 不存在 → 代码评估模式；否则 → 讨论模式。
- **system prompt 要点**：
  - 讨论模式（`discussEvalPrompt`）：挑剔评估 deal 是否覆盖 spec 所有 Requirement、验收点是否可客观判定、是否遗漏边界；输出 JSON `{"consensus": bool, "critique": "..."}`。
  - 代码评估模式（`codeEvalPrompt`）：严格对照 deal 验收点评估代码与 report 是否合格；输出 JSON `{"pass": bool, "issues": [...]}`。
- **哨兵错误**：
  - 讨论未共识 → 返回 `ErrNoConsensus`（编排器进入下一轮）。
  - 代码评估不通过 → 返回 `ErrEvaluationFailed`（编排器回到 Generator 重试）。
  - 其他错误视为真实失败终止流程。
- **关键设计（双模式）**：单一 agent + 状态判定，避免拆成两个 agent；JSON 容错解析（提取首个 `{` 到末个 `}`）；代码评估通过即打勾 spec 并归档 report，是流程收尾的「闸门」。

## 6. Manager 管理者

- **职责**：读取上游四份文档，拆解为可勾选的任务清单 `task.md`。
- **输入文档**：`desire.md`、`need.md`、`spec.md`、`deal.md`（四份均必需，任一缺失即终止）。
- **输出文档**：`task.md`（`# Tasks` + `- [ ] T1: <描述>（验收点：...）` 列表，frontmatter stage=manager/status=done）。
- **system prompt 要点**：任务 id T1/T2… 单调递增；必须覆盖 spec 所有 Requirement，每个至少一项；粒度适中（不过细如「新建文件」、不过粗如多模块混一）；验收点须可客观判定且能映射到 deal 验收标准，不得凭空编造；顺序符合依赖（基础设施→核心逻辑→集成收尾）。
- **交互**：不与用户交互，仅一次 AI 调用。
- **关键设计**：上下文按 `desire → need → spec → deal` 顺序拼装；任务验收点映射 deal，保证后续 Evaluator 可对照判定。

## 7. Executor 执行者

- **职责**：读取 `task.md` 与 `spec.md`，为 Generator 构造隔离工作目录与指令文件（**不含** desire/need/deal），保证 Generator 上下文隔离。
- **输入文档**：`task.md`、`spec.md`。
- **输出文档**：`agents/generator/instruction.md`（`# 任务指令` + `# Spec 要点` + `# 输出路径`）；并创建 `agents/generator/` 与 `code/` 目录。
- **system prompt 要点**：无——Executor **不调用 AI**，一次性准备完指令即交付。
- **指令内容**：直接复制 task.md 正文（任务项）+ 直接复制 spec.md 全文（Generator 需知道验收标准）+ 输出路径（代码到 `code/`、report 到 `reports/generator.md`）。
- **关键设计（隔离）**：指令刻意不含 desire/need/deal，使 Generator 只能基于 Executor 投喂的指令工作，无法触达用户原始欲望与讨论过程。这是上下文隔离原则的核心落点。

## 8. Generator 生成者

- **职责**：在隔离目录仅读 Executor 准备的指令文件，调用 AI 生成代码、写入指定输出路径，并回答自评问题后交付 report。
- **输入文档**：`agents/generator/instruction.md`（隔离，**不读** desire/need/spec/deal/task.md）。
- **输出文档**：`code/` 下代码文件 + `reports/generator.md`（`# Generator A Report` + `## 完成内容` + `## 自评`，frontmatter stage=generator/status=done）。
- **system prompt 要点**：
  - 生成代码（`generatorSystemPrompt`）：只按指令写代码、不提问；每个文件用一个围栏代码块标注，起始行格式 `` ```lang:path ``（如 `` ```go:code/main.go ``）；代码必须可直接运行、符合 spec 验收点；不输出代码块外的说明文字。
  - 自评（`selfReviewPrompt`）：回答标准问题「你觉得你的项目合格了吗？」，1–3 句如实回答。
- **关键设计（隔离 + 容错）**：
  - `parseCodeBlocks` 解析 `` ```lang:path `` 围栏，仅识别含冒号且冒号后非空路径的围栏；未闭合也宽容记录已收集内容。
  - 路径以 `code/` 开头则去掉前缀（输出根已是 `code/`）。
  - **AI 未按格式输出（files 为空）时，把整段输出当作 `code/README.md` 写入**，避免内容丢失。
  - MVP 单 Generator 模式：一次性实现指令中所有任务，`generatorID = "A"`。
  - 自评前构造代码摘要（文件清单 + 各文件前 5 行预览）供 AI 对照。

## 9. Gittor 提交者

- **职责**：评估通过后确认 `spec.md` 所有 Requirement 已打勾，将 `code/` 目录提交并推送到远端。
- **输入文档**：`spec.md`（复核打勾状态）。
- **输出文档**：无文档产出；通过注入的 `GittorClient` 执行 git 操作。
- **system prompt 要点**：无——Gittor 不调用 AI。
- **交互**：调用 `git.CommitAndPush(ctx, ["code/"], "feat: 实现 <projectID> 项目代码")`。
- **关键设计**：
  - 复核 `spec.md` 中**不含** `### Requirement: ` 前缀（已完成的是 `### [x] Requirement: `，前缀不同不会误匹配）；若存在未打勾项则失败。
  - commit message 遵循 conventional commits（由 gittor 隔离层校验前缀）。
  - 不直接碰 git CLI，经 `GittorClient` 接口请求，保证隔离。

---

## agent 间的交互关系

```
Listener ──desire.md──▶ Asker ──need.md──▶ Planner ──spec.md──▶ Designer
                                                          │      ▲
                                                          ▼      │ deal_review.md
                                                       Evaluator ──┘
                                                          │ deal.md(done)
                                                          ▼
                            Manager ──task.md──▶ Executor ──instruction.md──▶ Generator
                                                                       │ code/+report
                                                                       ▼
                                                                   Evaluator
                                                                       │ spec 打勾
                                                                       ▼
                                                                   Gittor ──git push
```

- **顺序依赖**：每个 agent 依赖上游文档存在；上游缺失即终止。
- **讨论循环**：Designer ↔ Evaluator，最多 5 轮，`ErrNoConsensus` 驱动。
- **评估循环**：Generator → Evaluator，最多 3 次，`ErrEvaluationFailed` 驱动。
- **隔离边界**：Executor 是隔离分水岭——它之前的 agent 能看到用户原始需求，之后的 Generator 只能看指令。
- **人类介入点**：UI 提交需求（Listener 上游）、Asker 问答（`/api/asks`）、GitHub 配置（`/api/github`）。

---

## 相关文件

| agent | 实现文件 |
| --- | --- |
| Listener | `internal/agents/listener.go` |
| Asker | `internal/agents/asker.go` |
| Planner | `internal/agents/planner.go` |
| Designer | `internal/agents/designer.go` |
| Evaluator | `internal/agents/evaluator.go` |
| Manager | `internal/agents/manager.go` |
| Executor | `internal/agents/executor.go` |
| Generator | `internal/agents/generator.go` |
| Gittor | `internal/agents/gittor_agent.go` |
| 接口/schema/哨兵 | `internal/agents/agent.go`、`schema.go`、`errors.go` |
