package agents

// 本文件定义 zzauto 各文档的正文格式约定（schema），作为后续 Task 5-11
// 各 agent 实现的依据。所有文档均以 Markdown 表示，正文（frontmatter 之后）
// 须遵循下述结构。
//
// 通用约定：
//   - 列表项统一使用 `- `（减号 + 空格）开头
//   - 需唯一 id 的项，id 形如 N1/N2、D1/D2、T1/T2 等，编号在单文档内单调
//   - 勾选项语法：`- [ ] ID: ...`（未完成）/ `- [x] ID: ...`（已完成）
//   - 文档缺失可选段落时保留空段落标题，便于解析
//
// 以下常量给出各文档的初始模板，agent 可在其基础上填充内容。

// ── desire.md（Listener 产出）────────────────────────────────────
//
// 结构：
//
//	# 用户需求
//	<用户原始需求文本，可多行>
//
//	# 改进点
//	- <改进点 1，如可访问性 / 错误处理 / 边界情况>
//	- <改进点 2>
//
// 约定：原始需求段保留用户原话；改进点为 Listener 自动补充。
const DesireTemplate = `# 用户需求
<用户原始需求文本>

# 改进点
- <改进点 1，如可访问性 / 错误处理 / 边界情况>
- <改进点 2>
`

// DesireSectionNeed 用户原始需求段标题。
const DesireSectionNeed = "# 用户需求"

// DesireSectionImprovements 改进点段标题。
const DesireSectionImprovements = "# 改进点"

// ── need.md（Asker 产出）─────────────────────────────────────────
//
// 结构：
//
//	# 需求清单
//	- N1: <需求描述 1>
//	- N2: <需求描述 2>
//
// 约定：每点以 `- Nx: ` 开头，id 形如 N1/N2...，单文档内唯一且单调递增。
const NeedTemplate = `# 需求清单
- N1: <需求描述 1>
- N2: <需求描述 2>
`

// NeedSectionList 需求清单段标题。
const NeedSectionList = "# 需求清单"

// NeedItemPrefix 需求项前缀，用于解析与生成（如 "- N1: "）。
const NeedItemPrefix = "- N"

// ── spec.md（Planner 产出，Evaluator 打勾）──────────────────────
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
//	## ADDED Requirements
//	### Requirement: <需求名>
//	<需求详述与验收场景>
//
// 完成标记：当 Evaluator 验收通过某 Requirement，将其标题改为：
//
//	### [x] Requirement: <需求名>
//
// 约定：项目名由 Planner 自定（可取 desire.md 中的关键词）；Requirements
// 段下每个 Requirement 用三级标题，名称需在文档内唯一。
const SpecTemplate = `# <项目名> Spec
## Why
<为何要做>

## What Changes
- <变更点 1>

## Impact
<影响范围>

## ADDED Requirements
### Requirement: <需求名>
<需求详述与验收场景>
`

// SpecSectionWhy / What Changes / Impact / ADDED Requirements 段标题。
const (
	SpecSectionWhy      = "## Why"
	SpecSectionChanges  = "## What Changes"
	SpecSectionImpact   = "## Impact"
	SpecSectionAddedReq = "## ADDED Requirements"
)

// SpecRequirementPrefix Requirement 三级标题前缀（未完成）。
const SpecRequirementPrefix = "### Requirement: "

// SpecRequirementDonePrefix Requirement 三级标题前缀（已完成）。
const SpecRequirementDonePrefix = "### [x] Requirement: "

// ── deal.md（Designer+Evaluator 讨论产出）───────────────────────
//
// 结构：
//
//	# 完工协议
//	<协议概述：交付内容、范围、约束>
//
//	## 验收标准
//	- [ ] D1: <验收点 1>
//	- [ ] D2: <验收点 2>
//
// 约定：每个验收点以 `- [ ] Dx: ` 开头（未完成），id D1/D2... 唯一单调；
// 完成后改为 `- [x] Dx: `。验收点须可被 Evaluator 客观判定。
const DealTemplate = `# 完工协议
<协议概述：交付内容、范围、约束>

## 验收标准
- [ ] D1: <验收点 1>
- [ ] D2: <验收点 2>
`

// DealSectionTitle 完工协议主标题。
const DealSectionTitle = "# 完工协议"

// DealSectionAcceptance 验收标准段标题。
const DealSectionAcceptance = "## 验收标准"

// DealItemPrefix 验收点前缀（未完成）。
const DealItemPrefix = "- [ ] D"

// DealItemDonePrefix 验收点前缀（已完成）。
const DealItemDonePrefix = "- [x] D"

// ── task.md（Manager 产出）──────────────────────────────────────
//
// 结构：
//
//	# Tasks
//	- [ ] T1: <任务描述 1>
//	- [ ] T2: <任务描述 2>
//
// 完成标记：Executor/Generator 完成某任务后改为 `- [x] T1: ...`。
// 约定：每项 id T1/T2... 唯一单调；任务描述应可映射到 deal.md 验收点。
const TaskTemplate = `# Tasks
- [ ] T1: <任务描述 1>
- [ ] T2: <任务描述 2>
`

// TaskSectionTitle Tasks 主标题。
const TaskSectionTitle = "# Tasks"

// TaskItemPrefix 任务项前缀（未完成）。
const TaskItemPrefix = "- [ ] T"

// TaskItemDonePrefix 任务项前缀（已完成）。
const TaskItemDonePrefix = "- [x] T"

// ── report（Generator 产出，存于 reports/ 目录）─────────────────
//
// 结构：
//
//	# Generator <id> Report
//	## 完成内容
//	- <完成项 1>
//	- <完成项 2>
//
//	## 自评
//	<对"你觉得你的项目合格了吗？"的回答及理由>
//
// 约定：<id> 为该 Generator 实例标识（如 A、B 或任务 id）；report 文件名
// 形如 reports/<id>.md，由 Generator 写入 workspace.ReportsDir()。
const ReportTemplate = `# Generator <id> Report
## 完成内容
- <完成项 1>
- <完成项 2>

## 自评
<对"你觉得你的项目合格了吗？"的回答及理由>
`

// ReportTitlePrefix report 主标题前缀。
const ReportTitlePrefix = "# Generator "

// ReportSectionDone 完成内容段标题。
const ReportSectionDone = "## 完成内容"

// ReportSectionSelfReview 自评段标题。
const ReportSectionSelfReview = "## 自评"

// SelfReviewQuestion 系统向 Generator 询问的标准问题。
const SelfReviewQuestion = "你觉得你的项目合格了吗？"

// 讨论与评估相关事件 Data 字段使用的键名（供编排器与 UI 约定）。
// Evaluator 通过返回哨兵 error（见 errors.go）表达讨论/评估状态。
const (
	// DataKeyRound 当前讨论/评估轮次（int）。
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
