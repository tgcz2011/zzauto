// Package agents 中 Asker Agent 的实现。
//
// Asker 基于上游 Listener 产出的 desire.md，挑剔地向用户提问，澄清需求中
// 未明确的关键点（边界情况、非功能需求、技术约束、验收标准等），直到认为
// 需求已充分明确，再由 AI 把问答历史整理成符合 schema.go 约定的 need.md。
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// askReplyDoc 是默认提问回调轮询的用户回复文件名。
const askReplyDoc = "ask_reply.md"

// askReplyTimeout 是默认提问回调等待用户回复的超时时间。
const askReplyTimeout = 10 * time.Minute

// askPollInterval 是默认提问回调轮询 ask_reply.md 的间隔。
// 设为包级变量便于测试在不等待真实时间的前提下覆盖轮询逻辑。
var askPollInterval = time.Second

// maxAskerRounds 限制提问循环最大轮次，防止 AI 永不满足导致死循环。
const maxAskerRounds = 10

// AskFunc 是向用户提问的回调类型：传入问题，返回用户回答。
//
// 具体实现可由 UI 层提供（实时交互）；为 nil 时 Asker 在 Run 中使用默认
// 实现 askViaBus：发布 ask_user 事件后轮询 workspace 的 ask_reply.md，
// 无 UI 时人工写入该文件即可完成回答。
type AskFunc func(ctx context.Context, question string) (string, error)

// askerSystemPrompt 是 Asker 生成下一批问题时使用的系统提示词。
//
// 要求 AI 扮演挑剔、批判性的需求澄清专家，找出需求中尚未明确的关键点，
// 输出 JSON：{"questions":["..."], "satisfied": false}。
const askerSystemPrompt = `你是 zzauto 多层 agent 编程平台中的 Asker Agent，一名极其挑剔、批判性的需求澄清专家。

【职责】
阅读用户的 desire.md（原始欲望）与已收集的问答历史，找出需求中尚未明确、
会影响后续设计与实现的关键点，向用户提出尖锐、具体的问题，直到你认为需求
已充分明确、可以进入规划阶段。

【提问维度】请从以下维度批判性地审视需求，挑出真正未明确且影响重大的点：
- 边界情况：空输入、超长输入、极端并发、零值、越界、失败回退
- 非功能需求：性能指标（响应时间/吞吐）、容量预估、可用性、可观测性、兼容性
- 技术约束：技术栈、运行环境、依赖限制、数据存储、外部接口契约
- 验收标准：如何客观判定“完成”？关键场景的 WHEN/THEN 是什么？
- 用户与场景：目标用户是谁、核心使用场景、优先级与权衡
- 安全与合规：鉴权、越权、数据保护、审计
- 交互与流程：异常路径、取消/中断、状态流转

【提问原则】
1. 每次只问与当前未明确点最相关的 1-3 个问题，不要一次堆砌过多。
2. 问题必须具体、可回答，避免空泛的“你想要什么”。
3. 不要重复已经问过且已有回答的问题。
4. 不要问可以通过合理默认假设解决的问题，除非该假设影响重大。
5. 当所有关键不明确点都已澄清时，将 satisfied 设为 true 且 questions 为空数组。

【输出格式】严格输出如下 JSON，不要输出任何其他文字、不要包裹代码块：
{"questions":["问题1","问题2"], "satisfied": false}

- questions：本次要问用户的问题列表（可为空数组）
- satisfied：true 表示需求已充分明确无需再问，此时 questions 必须为空数组`

// askerSummaryPrompt 是 Asker 把问答历史整理成 need.md 时使用的系统提示词。
const askerSummaryPrompt = `你是 zzauto 多层 agent 编程平台中的 Asker Agent，现在需要把已澄清的问答整理成 need.md。

【输入】你会收到 desire.md（用户原始欲望）与已收集的问答历史。

【输出格式】严格遵循以下 Markdown 结构，不得增减标题、不得使用代码围栏、不得添加任何前后说明文字：

# 需求清单
- N1: <需求描述 1>
- N2: <需求描述 2>
- ...

【规则】
1. 每点以 "- Nx: " 开头，id 形如 N1/N2...，在文档内唯一且单调递增，从 N1 开始。
2. 需求描述须简洁、明确、可验证，综合 desire.md 与问答中确认的信息。
3. 覆盖所有关键功能点、非功能需求、约束与验收要点，不要遗漏。
4. 全文使用中文；不要输出 frontmatter，只输出正文。
5. 不要输出代码块围栏（连续三个反引号），直接输出 Markdown 正文。`

// qaPair 一组问答，用于在提问循环中累积历史。
type qaPair struct {
	Question string
	Answer   string
}

// Asker 基于 desire.md 向用户提问澄清需求、产出 need.md 的 agent。
//
// ask 持有向用户提问的回调，可为 nil：为 nil 时在 Run 中使用默认实现
// askViaBus，通过事件总线与 ask_reply.md 文件完成无 UI 场景下的人工交互。
type Asker struct {
	ask   AskFunc
	model string // 该角色配置的模型，空则用 aicli 默认
}

// NewAsker 创建一个 Asker 实例。ask 为 nil 时使用默认的 askViaBus 实现。
// model 为该角色配置的模型名，空串表示用默认。
func NewAsker(ask AskFunc, model string) *Asker {
	return &Asker{ask: ask, model: model}
}

// Name 返回 agent 标识 "asker"，与 workspace 阶段常量 StageAsker 对应。
func (a *Asker) Name() string {
	return "asker"
}

// Run 执行 Asker 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 desire.md；不存在或出错则发布 agent_failed 并返回错误
//  3. 进入提问循环（最多 maxAskerRounds 轮）：
//     - 调用 AI 生成下一批问题（JSON：questions + satisfied）
//     - 若 satisfied 且无问题，跳出循环
//     - 对每个问题调用提问回调收集回答并累积历史
//     - 若 satisfied，跳出循环
//  4. 调用 AI 把问答历史整理成 need.md 正文
//  5. 用 RenderDoc 加 frontmatter（Stage=asker, Status=done）写入 need.md
//  6. 发布 doc_update 与 agent_done；任一步骤失败发布 agent_failed
func (a *Asker) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, a.Name(), map[string]any{
		DataKeyStage: workspace.StageAsker,
		DataKeyAgent: a.Name(),
	})

	// 2. 读取 desire.md；缺失或出错即终止
	desireContent, err := ws.ReadDoc(workspace.DocDesire)
	if err != nil {
		failErr := fmt.Errorf("读取 desire.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
			DataKeyStage:  workspace.StageAsker,
			DataKeyAgent:  a.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 3. 解析提问回调：ask 为 nil 时使用默认 askViaBus（依赖 ws 与 bus）
	ask := a.ask
	if ask == nil {
		ask = func(ctx context.Context, question string) (string, error) {
			return a.askViaBus(ctx, ws, bus, question)
		}
	}

	// 提问循环
	var history []qaPair
	for round := 0; round < maxAskerRounds; round++ {
		userCtx := buildAskerContext(desireContent, history)

		resp, _, err := RunWithTracking(ctx, ws, bus, ai, a.Name(), a.model, askerSystemPrompt, userCtx)
		if err != nil {
			failErr := fmt.Errorf("调用 AI 生成问题失败: %w", err)
			publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
				DataKeyStage:  workspace.StageAsker,
				DataKeyAgent:  a.Name(),
				DataKeyReason: failErr.Error(),
			})
			return failErr
		}

		questions, satisfied := parseAskerResponse(resp)

		// 需求已充分明确且无待问问题，直接结束提问
		if satisfied && len(questions) == 0 {
			break
		}

		// 对每个问题向用户收集回答
		for _, q := range questions {
			answer, err := ask(ctx, q)
			if err != nil {
				failErr := fmt.Errorf("向用户提问失败: %w", err)
				publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
					DataKeyStage:  workspace.StageAsker,
					DataKeyAgent:  a.Name(),
					DataKeyReason: failErr.Error(),
				})
				return failErr
			}
			history = append(history, qaPair{Question: q, Answer: answer})
		}

		// 已满足，结束提问循环
		if satisfied {
			break
		}
	}

	// 4. 调用 AI 把问答历史整理成 need.md 正文
	summaryCtx := buildAskerContext(desireContent, history)
	needBody, _, err := RunWithTracking(ctx, ws, bus, ai, a.Name(), a.model, askerSummaryPrompt, summaryCtx)
	if err != nil {
		failErr := fmt.Errorf("调用 AI 生成 need.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
			DataKeyStage:  workspace.StageAsker,
			DataKeyAgent:  a.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 5. 加 frontmatter 写入 need.md
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageAsker,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, needBody)
	if err := ws.WriteDoc(workspace.DocNeed, rendered); err != nil {
		failErr := fmt.Errorf("写入 need.md 失败: %w", err)
		publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
			DataKeyStage:  workspace.StageAsker,
			DataKeyAgent:  a.Name(),
			DataKeyReason: failErr.Error(),
		})
		return failErr
	}

	// 6. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, a.Name(), map[string]any{
		DataKeyStage: workspace.StageAsker,
		DataKeyAgent: a.Name(),
		"doc":        workspace.DocNeed,
	})
	publishEvent(bus, eventbus.EventAgentDone, a.Name(), map[string]any{
		DataKeyStage: workspace.StageAsker,
		DataKeyAgent: a.Name(),
	})

	return nil
}

// askViaBus 是 AskFunc 的默认实现：发布 ask_user 事件后轮询 workspace 的
// ask_reply.md，直到其内容相对提问前发生变化或文件出现，超时 askReplyTimeout。
//
// 无 UI 时人工写入 ask_reply.md 即可完成回答；文件内容相对提问前快照变化
// （或从不存在变为存在）即视为用户已回复，返回该文件内容。
func (a *Asker) askViaBus(ctx context.Context, ws *workspace.Workspace, bus *eventbus.Bus, question string) (string, error) {
	// 记录提问前 ask_reply.md 的内容与是否存在
	prev, prevErr := ws.ReadDoc(askReplyDoc)
	prevExists := prevErr == nil

	// 发布 ask_user 事件，等待 UI 或人工写入 ask_reply.md
	publishEvent(bus, eventbus.EventAskUser, a.Name(), map[string]any{"question": question})

	timeout := time.NewTimer(askReplyTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(askPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout.C:
			return "", fmt.Errorf("等待用户回复超时（%s）", askReplyTimeout)
		case <-ticker.C:
			cur, err := ws.ReadDoc(askReplyDoc)
			if err != nil {
				// 文件尚未出现或读取失败，继续等待
				continue
			}
			// 文件此前不存在（出现）或内容已变化，视为用户已回复
			if !prevExists || cur != prev {
				return cur, nil
			}
		}
	}
}

// buildAskerContext 把 desire.md 全文与已收集的问答历史拼装为 AI 上下文。
func buildAskerContext(desire string, history []qaPair) string {
	var sb strings.Builder
	sb.WriteString("# desire.md（用户欲望）\n")
	sb.WriteString(desire)
	sb.WriteString("\n\n# 已收集的问答历史\n")
	if len(history) == 0 {
		sb.WriteString("（暂无）\n")
		return sb.String()
	}
	for i, h := range history {
		fmt.Fprintf(&sb, "## 第 %d 轮\n", i+1)
		fmt.Fprintf(&sb, "Q: %s\nA: %s\n", h.Question, h.Answer)
	}
	return sb.String()
}

// parseAskerResponse 容错地解析 AI 输出的提问响应。
//
// AI 可能输出多余文本或包裹代码块，故先提取首个 '{' 与末个 '}' 之间的子串
// 再尝试 JSON 解析；解析失败则把整段（去除首尾空白）当作单个问题返回，
// satisfied 一律视为 false（仍有问题待问）。
func parseAskerResponse(raw string) (questions []string, satisfied bool) {
	s := strings.TrimSpace(raw)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		jsonStr := s[start : end+1]
		var resp struct {
			Questions []string `json:"questions"`
			Satisfied bool     `json:"satisfied"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil {
			return resp.Questions, resp.Satisfied
		}
	}
	// 解析失败：把整段当一个问题问用户
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		return []string{trimmed}, false
	}
	return nil, false
}
