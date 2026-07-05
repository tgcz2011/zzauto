// Package agents 中 Analyst Agent 的实现。
//
// Analyst 合并了旧 Listener + Asker + Planner 的职责：读取 input.md，
// 调用 AI 分析需求并补充遗漏点（边界/错误处理/安全/性能/可维护性），
// 按需向用户提问澄清，最终产出结构化 spec.md。
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

// maxAnalystRounds 限制 Analyst 提问循环最大轮次，防止 AI 永不满足导致死循环。
const maxAnalystRounds = 5

// analystSystemPrompt 是 Analyst 调用 AI 时使用的系统提示词。
//
// 要求 AI 扮演需求分析师，分析用户需求、补充遗漏点、按需提出澄清问题，
// 最终产出结构化 spec.md。输出 JSON：{"questions":[...], "spec":"..."}。
const analystSystemPrompt = `你是需求分析师。分析用户需求，补充遗漏点（边界/错误处理/安全/性能/可维护性），按需提出澄清问题。最终产出结构化 spec.md，格式为 Why / What Changes / Impact / Requirements（每项用 ### [ ] Requirement: 标记）。

输出格式（严格 JSON，不要包裹代码块、不要输出任何额外说明文字）：
{"questions":["问题1","问题2"], "spec": ""}

规则：
1. 若仍有需要澄清的关键点，questions 列出 1-3 个具体问题，spec 留空字符串。
2. 若需求已充分明确、无需再问，questions 为空数组，spec 填入完整 spec.md 正文。
3. spec 正文须严格遵循以下 Markdown 结构：
   # <项目名> Spec
   ## Why
   <为何要做>
   ## What Changes
   - <变更点>
   ## Impact
   <影响范围>
   ## Requirements
   ### [ ] Requirement: <需求名>
   <需求详述与验收场景>
4. Requirement 名称在文档内唯一；用 ### [ ] 标记未完成。
5. 全文使用中文；spec 内不要输出 frontmatter。`

// analystRound 一轮 Analyst 与 AI 的交互记录，用于累积上下文。
type analystRound struct {
	Questions []string
	Answers   []string
}

// Analyst 分析者：读取 input.md，按需向用户提问澄清需求，产出结构化 spec.md。
//
// ask 持有向用户提问的回调，可为 nil：为 nil 时使用默认实现 askViaBus，
// 通过事件总线与 ask_reply.md 文件完成无 UI 场景下的人工交互。
type Analyst struct {
	ask AskFunc
}

// NewAnalyst 创建一个 Analyst 实例。ask 为 nil 时使用默认的 askViaBus 实现。
func NewAnalyst(ask AskFunc) *Analyst { return &Analyst{ask: ask} }

// Name 返回 agent 标识 "analyst"，与 workspace.StageAnalyst 对应。
func (a *Analyst) Name() string { return workspace.StageAnalyst }

// Run 执行 Analyst 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 input.md；缺失返回错误
//  3. 进入提问循环（最多 maxAnalystRounds 轮）：
//     - 调用 AI（system=analystSystemPrompt），传入 input.md 与已收集问答
//     - 若 questions 非空且 ask 可用：逐个向用户提问、累积回答、再次调 AI
//     - 若 questions 为空或轮数用完：取最后返回的 spec 作为最终 spec.md
//  4. 用 RenderDoc 加 frontmatter 写入 spec.md
//  5. 发布 doc_update 与 agent_done；任一步骤失败发布 agent_failed
func (a *Analyst) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, a.Name(), map[string]any{
		DataKeyStage: workspace.StageAnalyst,
		DataKeyAgent: a.Name(),
	})

	// 2. 读取 input.md；缺失即终止
	inputContent, err := ws.ReadDoc(workspace.DocInput)
	if err != nil {
		return a.fail(bus, fmt.Errorf("读取 input.md 失败: %w", err))
	}

	// 解析提问回调：ask 为 nil 时使用默认 askViaBus
	ask := a.ask
	if ask == nil {
		ask = func(ctx context.Context, question string) (string, error) {
			return askViaBus(ctx, ws, bus, a.Name(), question)
		}
	}

	// 3. 提问循环
	model := ResolveModel(resolver, workspace.StageAnalyst)
	var rounds []analystRound
	var lastSpec string

	for round := 0; round < maxAnalystRounds; round++ {
		userCtx := buildAnalystContext(inputContent, rounds)
		resp, _, err := RunWithTracking(ctx, ws, bus, ai, a.Name(), model, analystSystemPrompt, userCtx)
		if err != nil {
			return a.fail(bus, fmt.Errorf("调用 AI 分析需求失败: %w", err))
		}

		questions, spec := parseAnalystResponse(resp)
		if spec != "" {
			lastSpec = spec
		}

		// 无需再问或 ask 不可用：结束循环
		if len(questions) == 0 {
			break
		}

		// 收集本轮问答
		var answers []string
		for _, q := range questions {
			ans, err := ask(ctx, q)
			if err != nil {
				return a.fail(bus, fmt.Errorf("向用户提问失败: %w", err))
			}
			answers = append(answers, ans)
		}
		rounds = append(rounds, analystRound{Questions: questions, Answers: answers})

		// 若已是最后一轮，跳出（lastSpec 可能已由本轮给出）
		if round == maxAnalystRounds-1 {
			break
		}
	}

	// 4. 若循环结束后仍无 spec，再调一次 AI 让其产出最终 spec（不再提问）
	if lastSpec == "" {
		userCtx := buildAnalystContext(inputContent, rounds)
		resp, _, err := RunWithTracking(ctx, ws, bus, ai, a.Name(), model, analystSystemPrompt, userCtx)
		if err != nil {
			return a.fail(bus, fmt.Errorf("调用 AI 生成 spec 失败: %w", err))
		}
		_, lastSpec = parseAnalystResponse(resp)
	}
	if strings.TrimSpace(lastSpec) == "" {
		return a.fail(bus, fmt.Errorf("AI 未产出有效 spec"))
	}

	// 5. 加 frontmatter 写入 spec.md
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageAnalyst,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, lastSpec)
	if err := ws.WriteDoc(workspace.DocSpec, rendered); err != nil {
		return a.fail(bus, fmt.Errorf("写入 spec.md 失败: %w", err))
	}

	// 6. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, a.Name(), map[string]any{
		DataKeyStage: workspace.StageAnalyst,
		DataKeyAgent: a.Name(),
		"doc":        workspace.DocSpec,
	})
	publishEvent(bus, eventbus.EventAgentDone, a.Name(), map[string]any{
		DataKeyStage: workspace.StageAnalyst,
		DataKeyAgent: a.Name(),
	})
	return nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (a *Analyst) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, a.Name(), map[string]any{
		DataKeyStage:  workspace.StageAnalyst,
		DataKeyAgent:  a.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// buildAnalystContext 把 input.md 全文与已收集的问答历史拼装为 AI 上下文。
func buildAnalystContext(input string, rounds []analystRound) string {
	var sb strings.Builder
	sb.WriteString("# input.md（用户原始需求）\n")
	sb.WriteString(input)
	if len(rounds) == 0 {
		sb.WriteString("\n\n# 已收集的问答历史\n（暂无）\n")
		return sb.String()
	}
	sb.WriteString("\n\n# 已收集的问答历史\n")
	for i, r := range rounds {
		fmt.Fprintf(&sb, "## 第 %d 轮\n", i+1)
		for j, q := range r.Questions {
			ans := ""
			if j < len(r.Answers) {
				ans = r.Answers[j]
			}
			fmt.Fprintf(&sb, "Q: %s\nA: %s\n", q, ans)
		}
	}
	return sb.String()
}

// parseAnalystResponse 容错地解析 AI 输出。
//
// 期望 JSON：{"questions":[...], "spec":"..."}。
// 容错：AI 可能包裹代码块或附加说明文字，截取首个 '{' 到末个 '}' 之间解析。
// 解析失败时把整段当作 spec 返回（questions 为空）。
func parseAnalystResponse(raw string) (questions []string, spec string) {
	s := strings.TrimSpace(raw)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		jsonStr := s[start : end+1]
		var resp struct {
			Questions []string `json:"questions"`
			Spec      string   `json:"spec"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil {
			return resp.Questions, resp.Spec
		}
	}
	// 解析失败：把整段当 spec
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		return nil, trimmed
	}
	return nil, ""
}
