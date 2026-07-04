package agents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// Planner 是规划阶段的 agent：读取 Asker 产出的 need.md，调用 AI
// 生成符合 schema.go 约定的 spec.md（Why / What Changes / Impact /
// ADDED Requirements 结构），并写入 workspace。
//
// Planner 不与用户交互，仅做一次 AI 调用即可产出完整 spec。
type Planner struct {
	model string // 该角色配置的模型，空则用 aicli 默认
}

// NewPlanner 构造一个 Planner 实例。model 为该角色配置的模型名，空串表示用默认。
func NewPlanner(model string) *Planner { return &Planner{model: model} }

// Name 返回 agent 标识，与 workspace 阶段常量 StagePlanner 对应。
func (p *Planner) Name() string { return "planner" }

// plannerSystemPrompt 是 Planner 调用 AI 时使用的系统提示。
//
// 该提示约束 AI 严格按 schema.go 中 spec.md 的结构产出，避免后续
// Evaluator / Manager 解析失败。
const plannerSystemPrompt = `你是一名资深的软件需求规划师（Planner）。

你的职责：阅读用户提供的 need.md（需求清单），将其提炼为一份结构化、
可被下游 Designer / Evaluator / Manager 直接消费的 spec.md。

输出必须严格遵循以下 Markdown 结构（不要输出任何额外说明、不要包裹代码块）：

# <项目名> Spec
## Why
<为何要做这件事：业务背景与动机，1-3 句>

## What Changes
- <变更点 1：用一句话描述要新增/修改/删除的能力>
- <变更点 2>

## Impact
<影响范围：涉及哪些模块、接口、数据或用户行为；潜在风险简述>

## ADDED Requirements
### Requirement: <需求名 1>
该需求 SHALL <用 SHALL 句式描述系统的强制行为>。
#### Scenario
- WHEN <触发条件> THEN <期望结果>
- WHEN <另一触发条件> THEN <期望结果>

### Requirement: <需求名 2>
该需求 SHALL <描述>。
#### Scenario
- WHEN <条件> THEN <结果>

规则：
1. 项目名从 need.md 内容中提炼（取核心关键词，简洁有意义）。
2. What Changes 至少 1 个变更点，使用 "- " 列表项。
3. ADDED Requirements 至少 1 个 Requirement；每个 Requirement 名称在文档内唯一。
4. 每个 Requirement 必须包含 SHALL 描述与至少一个 #### Scenario，
   Scenario 使用 "- WHEN ... THEN ..." 句式。
5. Requirement 标题使用 "### Requirement: <名>"（不要带 [x] 或 [ ] 标记，
   完成标记由 Evaluator 后续填写）。
6. 全文使用中文；不要输出 frontmatter，只输出正文。
7. 不要输出代码块围栏（连续三个反引号），直接输出 Markdown 正文。`

// Run 执行 Planner 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 need.md；不存在则返回错误
//  3. 调用 AI 生成 spec.md 正文
//  4. 用 RenderDoc 加 frontmatter（Stage=planner, Status=done）写入 spec.md
//  5. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (p *Planner) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, p.Name(), map[string]any{
		DataKeyStage: workspace.StagePlanner,
		DataKeyAgent: p.Name(),
	})

	// 2. 读取 need.md；若不存在返回错误（先发 agent_failed 再返回）
	needContent, err := ws.ReadDoc(workspace.DocNeed)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			failErr := fmt.Errorf("need.md 不存在")
			publishEvent(bus, eventbus.EventAgentFailed, p.Name(), map[string]any{
				DataKeyStage: workspace.StagePlanner,
				DataKeyAgent: p.Name(),
				DataKeyReason: failErr.Error(),
			})
			return failErr
		}
		failErr := fmt.Errorf("读取 need.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, p.Name(), map[string]any{
			DataKeyStage:  workspace.StagePlanner,
			DataKeyAgent:  p.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 3. 调用 AI 生成 spec.md 正文
	specBody, _, err := RunWithTracking(ctx, ws, bus, ai, p.Name(), p.model, plannerSystemPrompt, needContent)
	if err != nil {
		failErr := fmt.Errorf("调用 AI 生成 spec 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, p.Name(), map[string]any{
			DataKeyStage:  workspace.StagePlanner,
			DataKeyAgent:  p.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 4. 加 frontmatter 写入 spec.md
	meta := workspace.DocMeta{
		Stage:     workspace.StagePlanner,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}
	rendered := workspace.RenderDoc(meta, specBody)
	if err := ws.WriteDoc(workspace.DocSpec, rendered); err != nil {
		failErr := fmt.Errorf("写入 spec.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, p.Name(), map[string]any{
			DataKeyStage:  workspace.StagePlanner,
			DataKeyAgent:  p.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 5. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, p.Name(), map[string]any{
		DataKeyStage: workspace.StagePlanner,
		DataKeyAgent: p.Name(),
		"doc":        workspace.DocSpec,
	})
	publishEvent(bus, eventbus.EventAgentDone, p.Name(), map[string]any{
		DataKeyStage: workspace.StagePlanner,
		DataKeyAgent: p.Name(),
	})

	return nil
}

// publishEvent 向总线发布一条事件。bus 为 nil 时安全跳过，
// 便于在测试或不需事件总线的场景下复用 agent。
func publishEvent(bus *eventbus.Bus, typ, agent string, data map[string]any) {
	if bus == nil {
		return
	}
	bus.Publish(eventbus.Event{
		Type:  typ,
		Agent: agent,
		Data:  data,
	})
}
