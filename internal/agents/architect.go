// Package agents 中 Architect Agent 的实现。
//
// Architect 合并了旧 Designer + Evaluator(讨论) 的职责，但单次执行（无讨论
// 循环），必须包含对 spec.md 的批判性分析，再基于分析结果设计 deal.md。
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

// architectSystemPrompt 是 Architect 调用 AI 时使用的系统提示词。
//
// 必须包含批判性思考要求：先对 spec.md 进行批判性分析（完整性/可行性/
// 一致性/风险），再基于分析结果设计 deal.md。deal.md 须包含设计决策、
// 验收标准（D1/D2/...）、风险点与缓解措施。
const architectSystemPrompt = `你是架构师。你的职责是设计完工协议（deal.md），但在此之前你必须对 spec.md 进行批判性分析：

1. 需求完整性：spec 是否遗漏了边界情况、错误处理、安全、性能需求？
2. 技术可行性：spec 描述的方案是否技术上可行？是否有更简单的替代方案？
3. 一致性：spec 内部是否有矛盾？
4. 风险评估：哪些部分风险最高？需要怎样的验收标准？

你必须先写出批判性分析，再基于分析结果设计 deal.md。
deal.md 必须包含：设计决策、验收标准（D1/D2/...）、风险点与缓解措施。

输出格式（严格遵循以下 Markdown 结构，不要包裹代码块、不要输出 frontmatter）：

# 完工协议
<协议概述：交付内容、范围、约束>

## 批判性分析
<对 spec.md 的批判性分析结果>

## 验收标准
- [ ] D1: <验收点 1>
- [ ] D2: <验收点 2>

## 风险点与缓解
- <风险 1>：<缓解措施>

规则：
1. 验收点 id 形如 D1/D2...，在文档内唯一且单调递增，从 D1 开始。
2. 每项验收点必须可被客观判定（能明确回答"做到了/没做到"），覆盖 spec 所有 Requirement。
3. 全文使用中文；不要输出 frontmatter，只输出正文。`

// Architect 架构师：批判性分析 spec.md 并设计 deal.md。
type Architect struct{}

// NewArchitect 构造一个 Architect 实例。
func NewArchitect() *Architect { return &Architect{} }

// Name 返回 agent 标识，与 workspace.StageArchitect 对应。
func (a *Architect) Name() string { return workspace.StageArchitect }

// Run 执行 Architect 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 spec.md；不存在返回错误
//  3. 调用 AI（system=architectSystemPrompt，user=spec.md 全文），产出 deal.md 正文
//  4. 用 RenderDoc 加 frontmatter 写入 deal.md
//  5. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (a *Architect) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, a.Name(), map[string]any{
		DataKeyStage: workspace.StageArchitect,
		DataKeyAgent: a.Name(),
	})

	// 2. 读取 spec.md；缺失即终止
	specContent, err := ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("spec.md 不存在")
		} else {
			failErr = fmt.Errorf("读取 spec.md 失败: %w", err)
		}
		return a.fail(bus, failErr)
	}

	// 3. 调用 AI 生成 deal.md 正文
	model := ResolveModel(resolver, workspace.StageArchitect)
	dealBody, _, err := RunWithTracking(ctx, ws, bus, ai, a.Name(), model, architectSystemPrompt, specContent)
	if err != nil {
		return a.fail(bus, fmt.Errorf("调用 AI 生成 deal 失败: %w", err))
	}

	// 4. 加 frontmatter 写入 deal.md
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageArchitect,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, dealBody)
	if err := ws.WriteDoc(workspace.DocDeal, rendered); err != nil {
		return a.fail(bus, fmt.Errorf("写入 deal.md 失败: %w", err))
	}

	// 5. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, a.Name(), map[string]any{
		DataKeyStage: workspace.StageArchitect,
		DataKeyAgent: a.Name(),
		"doc":        workspace.DocDeal,
	})
	publishEvent(bus, eventbus.EventAgentDone, a.Name(), map[string]any{
		DataKeyStage: workspace.StageArchitect,
		DataKeyAgent: a.Name(),
	})
	return nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (a *Architect) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
		DataKeyStage:  workspace.StageArchitect,
		DataKeyAgent:  a.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}
