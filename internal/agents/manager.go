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

// Manager 是任务拆解阶段的 agent：读取 desire/need/spec/deal 四份上游文档，
// 调用 AI 生成符合 schema.go 约定的 task.md（# Tasks + 可勾选任务列表，
// 每项任务拥有唯一 id T1/T2...），并写入 workspace。
//
// Manager 不与用户交互，仅做一次 AI 调用即可产出完整 task.md。
// 产出的任务清单须覆盖 spec.md 中所有 ADDED Requirements，并具备清晰验收点。
type Manager struct{}

// NewManager 构造一个 Manager 实例。
func NewManager() *Manager { return &Manager{} }

// Name 返回 agent 标识，与 workspace 阶段常量 StageManager 对应。
func (m *Manager) Name() string { return "manager" }

// managerSystemPrompt 是 Manager 调用 AI 时使用的系统提示。
//
// 该提示约束 AI 严格按 schema.go 中 task.md 的结构产出，确保每项任务
// 拥有唯一 id、可勾选格式与清晰验收点，便于后续 Executor / Generator
// 按任务逐项实现、Evaluator 客观判定。
const managerSystemPrompt = `你是一名资深的工程任务拆解专家（Manager）。

你的职责：阅读上游四份文档（desire.md 用户欲望、need.md 需求清单、
spec.md 项目规格、deal.md 完工协议），将其拆解为一份可执行的任务清单
task.md，供后续 Executor / Generator 按任务逐项实现。

输出必须严格遵循以下 Markdown 结构（不要输出任何额外说明、不要包裹代码块）：

# Tasks
- [ ] T1: <任务描述>（验收点：<可客观判定的验收标准>）
- [ ] T2: <任务描述>（验收点：<验收标准>）

规则：
1. 任务 id 形如 T1/T2/T3...，在文档内唯一且单调递增，从 T1 开始。
2. 每项任务格式必须为 "- [ ] Tx: 描述（验收点：...）"，使用未完成标记 [ ]。
3. 任务必须覆盖 spec.md 中所有 ADDED Requirements，不可遗漏任何一个 Requirement；
   每个 Requirement 至少对应一项任务。
4. 任务粒度适中：既不过细（避免拆成无意义的微步骤如"新建文件""写注释"），
   也不过粗（避免一个任务涵盖多个互不相关的功能模块，导致无法独立验收）。
5. 每项任务的验收点须清晰、可被客观判定（能明确回答"做到了/没做到"），
   且应能映射到 deal.md 中的验收标准，不得凭空编造验收点。
6. 任务顺序应符合合理的实现依赖关系：基础设施/公共模块在前，核心业务逻辑
   居中，集成与收尾（如测试、文档）在末。
7. 全文使用中文；不要输出 frontmatter，只输出正文。
8. 不要输出代码块围栏（连续三个反引号），直接输出 Markdown 正文。`

// Run 执行 Manager 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 desire.md、need.md、spec.md、deal.md；任一缺失返回错误
//  3. 将四份文档内容拼装为上下文，调用 AI 生成 task.md 正文
//  4. 用 RenderDoc 加 frontmatter（Stage=manager, Status=done）写入 task.md
//  5. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (m *Manager) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, m.Name(), map[string]any{
		DataKeyStage: workspace.StageManager,
		DataKeyAgent: m.Name(),
	})

	// 2. 读取四份上游文档，任一缺失或出错即终止
	desireContent, err := m.readUpstreamDoc(ws, bus, workspace.DocDesire)
	if err != nil {
		return err
	}
	needContent, err := m.readUpstreamDoc(ws, bus, workspace.DocNeed)
	if err != nil {
		return err
	}
	specContent, err := m.readUpstreamDoc(ws, bus, workspace.DocSpec)
	if err != nil {
		return err
	}
	dealContent, err := m.readUpstreamDoc(ws, bus, workspace.DocDeal)
	if err != nil {
		return err
	}

	// 3. 拼装上下文：按 desire → need → spec → deal 顺序组合
	combined := fmt.Sprintf(
		"# desire.md（用户欲望）\n%s\n\n# need.md（需求清单）\n%s\n\n# spec.md（项目规格）\n%s\n\n# deal.md（完工协议）\n%s",
		desireContent, needContent, specContent, dealContent,
	)

	// 4. 调用 AI 生成 task.md 正文
	taskBody, err := ai.Ask(ctx, managerSystemPrompt, combined)
	if err != nil {
		failErr := fmt.Errorf("调用 AI 生成 task 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, m.Name(), map[string]any{
			DataKeyStage:  workspace.StageManager,
			DataKeyAgent:  m.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 5. 加 frontmatter 写入 task.md
	meta := workspace.DocMeta{
		Stage:     workspace.StageManager,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}
	rendered := workspace.RenderDoc(meta, taskBody)
	if err := ws.WriteDoc(workspace.DocTask, rendered); err != nil {
		failErr := fmt.Errorf("写入 task.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, m.Name(), map[string]any{
			DataKeyStage:  workspace.StageManager,
			DataKeyAgent:  m.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 6. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, m.Name(), map[string]any{
		DataKeyStage: workspace.StageManager,
		DataKeyAgent: m.Name(),
		"doc":        workspace.DocTask,
	})
	publishEvent(bus, eventbus.EventAgentDone, m.Name(), map[string]any{
		DataKeyStage: workspace.StageManager,
		DataKeyAgent: m.Name(),
	})

	return nil
}

// readUpstreamDoc 读取一份上游文档。文件不存在或读取失败时发布 agent_failed
// 并返回包装后的错误，避免在 Run 中重复编写四遍相同的错误处理逻辑。
func (m *Manager) readUpstreamDoc(ws *workspace.Workspace, bus *eventbus.Bus, name string) (string, error) {
	content, err := ws.ReadDoc(name)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("%s 不存在", name)
		} else {
			failErr = fmt.Errorf("读取 %s 失败: %w", name, err)
		}
		publishEvent(bus, eventbus.EventAgentFailed, m.Name(), map[string]any{
			DataKeyStage:  workspace.StageManager,
			DataKeyAgent:  m.Name(),
			DataKeyReason: failErr.Error(),
		})
		return "", failErr
	}
	return content, nil
}
