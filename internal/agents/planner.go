// Package agents 中 Planner Agent 的实现。
//
// Planner（旧 Manager 重命名）：读取 spec.md + deal.md，调用 AI 拆解为
// 可执行的 task.md 任务清单。每项任务可独立完成，并引用相关验收点 D1/D2。
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

// plannerSystemPrompt 是 Planner 调用 AI 时使用的系统提示词。
const plannerSystemPrompt = `你是任务规划者。基于 spec 和 deal，拆解为可执行的任务清单 task.md。每项任务格式：- [ ] T1: 任务描述（引用相关验收点 D1/D2）。任务应粒度适中，每个可独立完成。

输出格式（严格遵循以下 Markdown 结构，不要包裹代码块、不要输出 frontmatter）：

# Tasks
- [ ] T1: <任务描述>（引用 D1/D2）
- [ ] T2: <任务描述>（引用 D1）

规则：
1. 任务 id 形如 T1/T2...，在文档内唯一且单调递增，从 T1 开始。
2. 每项格式必须为 "- [ ] Tx: 描述（引用 Dx）"，使用未完成标记 [ ]。
3. 任务必须覆盖 spec.md 中所有 Requirement，每个 Requirement 至少对应一项任务。
4. 任务粒度适中：既不过细也不过粗，每个可独立完成。
5. 任务描述应引用 deal.md 中的验收点（如 D1/D2），便于追溯。
6. 任务顺序应符合合理的实现依赖关系。
7. 全文使用中文；不要输出 frontmatter，只输出正文。`

// Planner 任务规划者：基于 spec + deal 拆解为 task.md。
type Planner struct{}

// NewPlanner 构造一个 Planner 实例。
func NewPlanner() *Planner { return &Planner{} }

// Name 返回 agent 标识，与 workspace.StagePlanner 对应。
func (p *Planner) Name() string { return workspace.StagePlanner }

// Run 执行 Planner 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 spec.md + deal.md；任一缺失返回错误
//  3. 拼装上下文，调用 AI 生成 task.md 正文
//  4. 用 RenderDoc 加 frontmatter 写入 task.md
//  5. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (p *Planner) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, p.Name(), map[string]any{
		DataKeyStage: workspace.StagePlanner,
		DataKeyAgent: p.Name(),
	})

	// 2. 读取 spec.md + deal.md
	specContent, err := p.readUpstreamDoc(ws, bus, workspace.DocSpec)
	if err != nil {
		return err
	}
	dealContent, err := p.readUpstreamDoc(ws, bus, workspace.DocDeal)
	if err != nil {
		return err
	}

	// 3. 拼装上下文：spec → deal
	combined := fmt.Sprintf("# spec.md（项目规格）\n%s\n\n# deal.md（完工协议）\n%s", specContent, dealContent)
	model := ResolveModel(resolver, workspace.StagePlanner)
	taskBody, _, err := RunWithTracking(ctx, ws, bus, ai, p.Name(), model, plannerSystemPrompt, combined)
	if err != nil {
		return p.fail(bus, fmt.Errorf("调用 AI 生成 task 失败: %w", err))
	}

	// 4. 加 frontmatter 写入 task.md
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StagePlanner,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, taskBody)
	if err := ws.WriteDoc(workspace.DocTask, rendered); err != nil {
		return p.fail(bus, fmt.Errorf("写入 task.md 失败: %w", err))
	}

	// 5. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, p.Name(), map[string]any{
		DataKeyStage: workspace.StagePlanner,
		DataKeyAgent: p.Name(),
		"doc":        workspace.DocTask,
	})
	publishEvent(bus, eventbus.EventAgentDone, p.Name(), map[string]any{
		DataKeyStage: workspace.StagePlanner,
		DataKeyAgent: p.Name(),
	})
	return nil
}

// readUpstreamDoc 读取一份上游文档。文件不存在或读取失败时发布 agent_failed
// 并返回包装后的错误。
func (p *Planner) readUpstreamDoc(ws *workspace.Workspace, bus *eventbus.Bus, name string) (string, error) {
	content, err := ws.ReadDoc(name)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("%s 不存在", name)
		} else {
			failErr = fmt.Errorf("读取 %s 失败: %w", name, err)
		}
		return "", p.fail(bus, failErr)
	}
	return content, nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (p *Planner) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, p.Name(), map[string]any{
		DataKeyStage:  workspace.StagePlanner,
		DataKeyAgent:  p.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}
