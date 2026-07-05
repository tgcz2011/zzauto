package agents

// 本文件定义 zzauto 各文档的正文格式约定（schema），作为各 agent 实现的
// 依据。所有文档均以 Markdown 表示，正文（frontmatter 之后）须遵循下述结构。
//
// v0.6.0 文档协议（与 workspace/doc.go 对齐）：
//   - input.md：用户原始需求输入（无固定 schema）
//   - spec.md：Analyst 产出的结构化规格
//   - deal.md：Architect 设计的完工协议（含批判性分析、验收标准、风险点）
//   - task.md：Planner 拆解的任务清单
//   - requirements_queue.md：异步需求队列（用户随时追加的新需求）
//   - reports/coder.md：Coder 生成代码后的自评报告
//   - reports/reviewer.md：Reviewer 审查报告
//   - reports/progress.md：Mixor 进度报告
//
// 通用约定：
//   - 列表项统一使用 `- `（减号 + 空格）开头
//   - 需唯一 id 的项，id 形如 D1/D2、T1/T2 等，编号在单文档内单调
//   - 勾选项语法：`- [ ] ID: ...`（未完成）/ `- [x] ID: ...`（已完成）
//   - 文档缺失可选段落时保留空段落标题，便于解析

// ── spec.md（Analyst 产出，Reviewer 打勾）──────────────────────
//
// 结构：
//
//	# <项目名> Spec
//	## Why
//	<为何要做>
//	## What Changes
//	- <变更点>
//	## Impact
//	<影响范围>
//	## Requirements
//	### [ ] Requirement: <需求名>
//	<需求详述与验收场景>
//
// 完成标记：当 Reviewer 验收通过某 Requirement，将其标题改为：
//
//	### [x] Requirement: <需求名>
//
// 约定：项目名由 Analyst 自定；Requirements 段下每个 Requirement 用三级标题，
// 名称需在文档内唯一；未完成用 `### [ ]`、已完成用 `### [x]`。
const (
	// SpecSectionWhy / What Changes / Impact / Requirements 段标题。
	SpecSectionWhy      = "## Why"
	SpecSectionChanges  = "## What Changes"
	SpecSectionImpact   = "## Impact"
	SpecSectionAddedReq = "## Requirements"
)

// SpecRequirementPrefix Requirement 三级标题前缀（未完成）。
const SpecRequirementPrefix = "### [ ] Requirement: "

// SpecRequirementDonePrefix Requirement 三级标题前缀（已完成）。
const SpecRequirementDonePrefix = "### [x] Requirement: "

// ── deal.md（Architect 产出）───────────────────────────────────
//
// 结构：
//
//	# 完工协议
//	<协议概述：交付内容、范围、约束>
//	## 批判性分析
//	<对 spec.md 的批判性分析>
//	## 验收标准
//	- [ ] D1: <验收点 1>
//	- [ ] D2: <验收点 2>
//	## 风险点与缓解
//	- <风险 1>：缓解措施
//
// 约定：每个验收点以 `- [ ] Dx: ` 开头（未完成），id D1/D2... 唯一单调。
const (
	// DealSectionTitle 完工协议主标题。
	DealSectionTitle = "# 完工协议"
	// DealSectionAcceptance 验收标准段标题。
	DealSectionAcceptance = "## 验收标准"
)

// DealItemPrefix 验收点前缀（未完成）。
const DealItemPrefix = "- [ ] D"

// DealItemDonePrefix 验收点前缀（已完成）。
const DealItemDonePrefix = "- [x] D"

// ── task.md（Planner 产出）─────────────────────────────────────
//
// 结构：
//
//	# Tasks
//	- [ ] T1: <任务描述>（引用 D1/D2）
//	- [ ] T2: <任务描述>（引用 D1）
//
// 完成标记：完成后改为 `- [x] T1: ...`。
// 约定：每项 id T1/T2... 唯一单调；任务描述应可映射到 deal.md 验收点。
const (
	// TaskSectionTitle Tasks 主标题。
	TaskSectionTitle = "# Tasks"
)

// TaskItemPrefix 任务项前缀（未完成）。
const TaskItemPrefix = "- [ ] T"

// TaskItemDonePrefix 任务项前缀（已完成）。
const TaskItemDonePrefix = "- [x] T"

// 事件 Data 字段使用的键名（供编排器与 UI 约定）。
const (
	// DataKeyRound 当前评估/重试轮次（int）。
	DataKeyRound = "round"
	// DataKeyMaxRound 最大轮次（int）。
	DataKeyMaxRound = "max_round"
	// DataKeyReason 失败/未通过原因（string）。
	DataKeyReason = "reason"
	// DataKeyStage 阶段名（string）。
	DataKeyStage = "stage"
	// DataKeyAgent agent 名称（string）。
	DataKeyAgent = "agent"
)
