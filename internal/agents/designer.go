package agents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// dealReviewDoc 是 Evaluator 在上一轮讨论中产出的批评文档名。
//
// 该文档不在 workspace 的标准文档常量中（属于 Designer↔Evaluator 讨论
// 过程的中间产物），故在此单独声明，供 Designer 读取与编排器协调。
const dealReviewDoc = "deal_review.md"

// Designer 是完工协议设计阶段的 agent。
//
// 工作模式（由编排器在讨论循环中多次调用）：
//   - 第一轮：仅依据 spec.md 起草 deal.md 草案，含可客观判定的验收标准清单
//   - 后续轮：依据 Evaluator 上一轮的批评（deal_review.md）修订 deal.md，
//     吸收合理意见、反驳不合理意见，直到 Evaluator 达成共识
//
// 产出的 deal.md 以 StatusRunning 表示草案状态；Evaluator 共识后由其
// 改为 StatusDone。Designer 自身不负责标记完成。
type Designer struct {
	model string // 该角色配置的模型，空则用 aicli 默认
}

// NewDesigner 构造一个 Designer 实例。model 为该角色配置的模型名，空串表示用默认。
func NewDesigner(model string) *Designer { return &Designer{model: model} }

// Name 返回 agent 标识，与 workspace 阶段常量 StageDesigner 对应。
func (d *Designer) Name() string { return workspace.StageDesigner }

// designerSystemPrompt 是 Designer 调用 AI 时使用的系统提示。
//
// 该提示约束 AI 严格按 schema.go 中 deal.md 的结构产出（# 完工协议 +
// ## 验收标准 + `- [ ] Dx: ` 验收点清单），并要求验收标准可被 Evaluator
// 客观判定、覆盖 spec 所有 Requirements、关注可测试性/完整性/边界情况。
// 同时指导 AI 在收到批评时合理吸收或反驳。
const designerSystemPrompt = `你是一名资深的完工协议设计师（Designer），在 zzauto 多层 agent 平台中工作。

【核心职责】
根据 spec.md（项目规格）起草或修订 deal.md（完工协议）。完工协议定义项目
"做到什么算完成"的客观标准，供 Evaluator 验收与 Generator 执行时遵循。

【输出格式】严格遵循以下 Markdown 结构，不得增减标题、不得使用代码围栏包裹、
不得添加任何前后说明文字：

# 完工协议
<协议概述：交付内容、范围、约束，1-4 句>

## 验收标准
- [ ] D1: <验收点 1>
- [ ] D2: <验收点 2>
- ...

【验收标准制定规则】
1. 验收点 id 形如 D1/D2/D3...，在文档内唯一且单调递增，从 D1 开始。
2. 每项格式必须为 "- [ ] Dx: 描述"，使用未完成标记 [ ]。
3. 每项验收点必须可被客观判定：能明确回答"做到了/没做到"，避免主观措辞
   （如"用户体验好""代码优雅"），优先用可观测的输入输出、命令、状态、
   数值阈值描述。
4. 验收点必须覆盖 spec.md 中所有 ADDED Requirements，不可遗漏；
   每个 Requirement 至少对应一项验收点。
5. 关注可测试性、完整性与边界情况：除主流程外，需覆盖错误路径、空输入、
   越权、并发等与该需求切实相关的边界（不要泛泛罗列无关维度）。
6. 协议概述应说明交付范围与约束，使 Evaluator 能据此判断验收点是否充分。

【两种工作模式】
- 起草模式（上下文中无"上一轮 deal.md 草案"与"Evaluator 批评"）：
  从 spec.md 全新起草完工协议。
- 修订模式（上下文中含"上一轮 deal.md 草案"与"Evaluator 批评"）：
  针对 Evaluator 的批判点修订协议。对合理的批评应吸收并改进对应验收点；
  对不合理的批评应坚持原方案，可在协议概述或验收点描述中体现理由，
  但不要新增与协议无关的辩论段落——保持输出格式不变。

【约束】
1. 全文使用中文。
2. 不要输出 frontmatter（由系统自动添加）。
3. 不要输出代码块围栏（连续三个反引号），直接输出 Markdown 正文。
4. 不要添加 "## 验收标准" 与 "# 完工协议" 之外的标题或段落。`

// Run 执行 Designer 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 spec.md；不存在返回错误
//  3. 尝试读取上一轮 deal.md 草案（可能不存在，第一轮）
//  4. 尝试读取 Evaluator 上一轮批评 deal_review.md（可能不存在，第一轮）
//  5. 拼装上下文（spec 全文 + 上一轮 deal + 批评），调用 AI 起草/修订
//  6. 用 RenderDoc 加 frontmatter（Stage=designer, Status=running）写入 deal.md
//  7. 发布 doc_update（doc=deal.md）与 agent_done；失败发布 agent_failed
func (d *Designer) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, d.Name(), map[string]any{
		DataKeyStage: workspace.StageDesigner,
		DataKeyAgent: d.Name(),
	})

	// 2. 读取 spec.md（必需）；不存在或读取失败即终止
	specContent, err := ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		failErr := d.wrapReadErr(err, workspace.DocSpec)
		publishEvent(bus, eventbus.EventAgentFailed, d.Name(), map[string]any{
			DataKeyStage:  workspace.StageDesigner,
			DataKeyAgent:  d.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 3. 尝试读取上一轮 deal.md 草案（可选，第一轮不存在）
	prevDeal, hasPrevDeal := d.tryReadOptional(ws, workspace.DocDeal)

	// 4. 尝试读取 Evaluator 上一轮批评（可选，第一轮不存在）
	review, hasReview := d.tryReadOptional(ws, dealReviewDoc)

	// 5. 拼装上下文并调用 AI
	combined := d.buildContext(specContent, prevDeal, hasPrevDeal, review, hasReview)
	dealBody, _, err := RunWithTracking(ctx, ws, bus, ai, d.Name(), d.model, designerSystemPrompt, combined)
	if err != nil {
		failErr := fmt.Errorf("调用 AI 生成 deal 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, d.Name(), map[string]any{
			DataKeyStage:  workspace.StageDesigner,
			DataKeyAgent:  d.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 6. 加 frontmatter 写入 deal.md（草案用 StatusRunning，共识后由 Evaluator 改 done）
	meta := workspace.DocMeta{
		Stage:     workspace.StageDesigner,
		Status:    workspace.StatusRunning,
		UpdatedAt: time.Now(),
	}
	rendered := workspace.RenderDoc(meta, dealBody)
	if err := ws.WriteDoc(workspace.DocDeal, rendered); err != nil {
		failErr := fmt.Errorf("写入 deal.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, d.Name(), map[string]any{
			DataKeyStage:  workspace.StageDesigner,
			DataKeyAgent:  d.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 7. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, d.Name(), map[string]any{
		DataKeyStage: workspace.StageDesigner,
		DataKeyAgent: d.Name(),
		"doc":        workspace.DocDeal,
	})
	publishEvent(bus, eventbus.EventAgentDone, d.Name(), map[string]any{
		DataKeyStage: workspace.StageDesigner,
		DataKeyAgent: d.Name(),
	})

	return nil
}

// tryReadOptional 尝试读取一份可选文档。文件不存在视为"无该文档"，
// 返回 ok=false 且不报错；其他读取错误也按"无该文档"处理（容错），
// 以保证讨论循环不被中间产物损坏而中断。
func (d *Designer) tryReadOptional(ws *workspace.Workspace, name string) (string, bool) {
	content, err := ws.ReadDoc(name)
	if err != nil {
		return "", false
	}
	return content, true
}

// buildContext 根据可用文档拼装发给 AI 的 user 上下文。
//
// spec.md 始终包含；上一轮 deal.md 与 Evaluator 批评按存在性追加，
// 使 AI 能区分"起草模式"与"修订模式"。
func (d *Designer) buildContext(specContent string, prevDeal string, hasPrevDeal bool, review string, hasReview bool) string {
	var b strings.Builder
	b.WriteString("# spec.md（项目规格）\n")
	b.WriteString(specContent)

	if hasPrevDeal {
		b.WriteString("\n\n# 上一轮 deal.md 草案\n")
		b.WriteString(prevDeal)
	}
	if hasReview {
		b.WriteString("\n\n# Evaluator 批评（deal_review.md）\n")
		b.WriteString(review)
	}
	return b.String()
}

// wrapReadErr 将 spec.md 的读取错误包装为更清晰的失败信息。
func (d *Designer) wrapReadErr(err error, name string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s 不存在", name)
	}
	return fmt.Errorf("读取 %s 失败: %w", name, err)
}
