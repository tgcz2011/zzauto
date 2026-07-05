// Package agents 定义 zzauto 各 agent 角色的统一接口与文档协议。
//
// v0.6.0 架构精简后包含 5 个 LLM 角色 + 1 个 Mixor：
//   - Analyst：分析 input.md，按需向用户提问，产出 spec.md
//   - Architect：批判性分析 spec.md，设计 deal.md
//   - Planner：基于 spec + deal 拆解为 task.md
//   - Coder：根据指令文件生成代码与自评报告
//   - Reviewer：审查代码，决定是否打勾 spec 的 Requirement
//   - Mixor：管理需求队列、融合文档/代码、产出进度报告
//
// 编排器（internal/orchestrator）依赖此处的 Agent 接口调度各阶段，
// 通过 AIClient 调用 AI、通过 GittorClient 调用 git、通过事件总线
// 向外广播状态。模型按 stage 解析（ModelResolver）。
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// AIClient 是 agent 调用 AI 推理的统一抽象。
//
// 具体实现（如走 aiclibridge HTTP 的客户端）在 internal/aicli 提供。
// agent 只依赖此接口，不直接耦合任何 AI SDK / CLI。
type AIClient interface {
	// Ask 以 system 提示 + user 输入发起一次 AI 对话，返回模型回答。
	Ask(ctx context.Context, system, user string) (string, error)
	// AskWithModel 与 Ask 相同，但用传入 model 覆盖本次请求使用的模型。
	// model 为空时由实现自行选择默认模型。
	AskWithModel(ctx context.Context, model, system, user string) (string, error)
	// RunStream 以 SSE 形式发起一次 run，按事件回调 onEvent。
	// 返回最终的 runID。model 为空时由实现选择默认模型。
	RunStream(ctx context.Context, model, system, user string, onEvent func(aicli.RunEvent) error) (string, error)
	// GetRun 拉取指定 run 的详情（含完整事件时间线）。
	GetRun(ctx context.Context, runID string) (*aicli.RunDetail, error)
}

// GittorClient 是 agent 调用 git 操作的统一抽象。
//
// 所有 git 操作（commit/push）必须经此接口，禁止 agent 直接调 git CLI，
// 以保证上下文隔离（git 操作仅由 Gittor 在隔离环境执行）。
type GittorClient interface {
	// CommitAndPush 将指定路径暂存并提交，随后推送到远端。
	CommitAndPush(ctx context.Context, paths []string, message string) error
}

// ModelResolver 按 agent stage 返回配置的模型名（若该 stage 未配置返回空字符串，调用方用默认）。
type ModelResolver interface {
	ModelFor(stage string) string
}

// ResolveModel 从 ModelResolver 取 stage 对应模型，未配置或 resolver 为 nil 返回空串。
func ResolveModel(r ModelResolver, stage string) string {
	if r == nil {
		return ""
	}
	return r.ModelFor(stage)
}

// Agent 是所有 agent 角色的统一接口。
//
// Name 返回 agent 标识（与 workspace 阶段常量对应，如 "analyst"）。
// Run 执行该 agent 的一次完整工作：读取上游文档、调用 AI、产出下游文档、
// 通过 bus 发布状态事件。resolver 用于按 stage 解析配置的模型。
// 失败时返回非 nil error，编排器据此终止流程或执行重试。
type Agent interface {
	Name() string
	Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error
}

// RunWithTracking 用 RunStream 调用 AI，捕获所有事件并持久化到
// <projectDir>/runs/<agent>/<runID>.json，同时通过 bus 发布 agent_run_event 事件。
// model 为空时 AI 用默认模型。
// 返回 (response_text, runID, error)：response_text 为所有 text 事件 content 拼接。
func RunWithTracking(ctx context.Context, ws *workspace.Workspace, bus *eventbus.Bus, ai AIClient, agentName, model, system, user string) (string, string, error) {
	var (
		events []aicli.RunEvent
		sb     strings.Builder
	)

	runID, err := ai.RunStream(ctx, model, system, user, func(evt aicli.RunEvent) error {
		events = append(events, evt)
		if evt.Type == "text" {
			sb.WriteString(evt.Content)
		}
		// 通过 bus 发布 agent_run_event 事件（含 run_id / event_type / content / tool 信息）
		if bus != nil {
			bus.Publish(eventbus.Event{
				Type:  eventbus.EventAgentRunEvent,
				Agent: agentName,
				Data: map[string]any{
					"run_id":     evt.RunID,
					"event_type": evt.Type,
					"content":    evt.Content,
					"tool_name":  evt.ToolName,
					"tool_input": evt.ToolInput,
				},
			})
		}
		return nil
	})
	if err != nil {
		return "", runID, err
	}

	// 持久化事件到 <ws.Path()>/runs/<agentName>/<runID>.json
	if ws != nil && runID != "" {
		dir := filepath.Join(ws.Path(), "runs", agentName)
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			data, jsonErr := json.MarshalIndent(events, "", "  ")
			if jsonErr == nil {
				_ = os.WriteFile(filepath.Join(dir, runID+".json"), data, 0o644)
			}
		}
	}

	return sb.String(), runID, nil
}

// AskFunc 是向用户提问的回调类型：传入问题，返回用户回答。
//
// 具体实现可由 UI 层提供（实时交互）；为 nil 时 Analyst 在 Run 中使用默认
// 实现 askViaBus：发布 ask_user 事件后轮询 workspace 的 ask_reply.md，
// 无 UI 时人工写入该文件即可完成回答。
type AskFunc func(ctx context.Context, question string) (string, error)

// askReplyDoc 是默认提问回调轮询的用户回复文件名。
const askReplyDoc = "ask_reply.md"

// askReplyTimeout 是默认提问回调等待用户回复的超时时间。
const askReplyTimeout = 10 * time.Minute

// askPollInterval 是默认提问回调轮询 ask_reply.md 的间隔。
// 设为包级变量便于测试在不等待真实时间的前提下覆盖轮询逻辑。
var askPollInterval = time.Second

// askViaBus 是 AskFunc 的默认实现：发布 ask_user 事件后轮询 workspace 的
// ask_reply.md，直到其内容相对提问前发生变化或文件出现，超时 askReplyTimeout。
//
// 无 UI 时人工写入 ask_reply.md 即可完成回答；文件内容相对提问前快照变化
// （或从不存在变为存在）即视为用户已回复，返回该文件内容。
// agentName 仅用于事件标注，可为空。
func askViaBus(ctx context.Context, ws *workspace.Workspace, bus *eventbus.Bus, agentName, question string) (string, error) {
	// 记录提问前 ask_reply.md 的内容与是否存在
	prev, prevErr := ws.ReadDoc(askReplyDoc)
	prevExists := prevErr == nil

	// 发布 ask_user 事件，等待 UI 或人工写入 ask_reply.md
	publishEvent(bus, eventbus.EventAskUser, agentName, map[string]any{"question": question})

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
