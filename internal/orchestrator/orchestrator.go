// Package orchestrator 实现 zzauto 的多 agent 编排器（v0.6.0 架构）。
//
// 编排器按以下流程调度 5 个 LLM 角色 + 1 个 Mixor：
//
//	Analyst → Architect → Planner → (Coder ↔ Reviewer 评估循环)
//	→ commitAndPush →（如有新需求）Mixor → 从 Planner 或 Analyst 重跑
//
// 文档在 agent 间通过 workspace 文件系统传递：
//
//	input.md → spec.md → deal.md → task.md → code/ + reports/coder.md
//	→ reports/reviewer.md →（新需求）requirements_queue.md → reports/progress.md
//
// 关键循环：
//   - Coder↔Reviewer：Coder 生成代码后 Reviewer 评估，不通过则回到 Coder 重试
//     （最大 maxEval 次，默认 3）。
//   - 主循环：每次 pipeline 跑完后检查 requirements_queue.md，若有新需求则
//     调用 Mixor：返回 nil 则从 Planner 继续；返回 ErrNeedRerun 则从 Analyst 重跑。
//
// 控制信号：通过 Pause/Stop/Resume 方法与 Run 协作，Pause 在阶段边界生效，
// Stop 立即终止（下一个 checkControl 返回 ErrStopped）。
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// Coder→Reviewer 评估循环的默认上限。
const defaultMaxEval = 3

// controlSignal 控制信号枚举：sigNone/sigPause/sigStop。
type controlSignal int

const (
	sigNone controlSignal = iota
	sigPause
	sigStop
)

// Orchestrator v0.6.0 多 agent 编排器。
//
// 各字段在 New 后不再变更（agents map 通过 Register 填充）。
// Run 不可重入：单实例同一时刻只应有一个 Run 在执行。
type Orchestrator struct {
	cfg     *config.Config
	ws      *workspace.Workspace
	ai      agents.AIClient
	git     agents.GittorClient
	bus     *eventbus.Bus
	agents  map[string]agents.Agent
	maxEval int // Coder→Reviewer 最大重试次数（默认 3）

	// resolver 按 stage 解析模型（可为 nil，使用 AI 默认模型）。
	resolver agents.ModelResolver

	// 控制通道（暂停/停止/恢复）
	controlMu  sync.Mutex
	controlSig controlSignal
	pausedCond *sync.Cond
	paused     bool

	// 异步需求注入：Mixor 决定的重跑起点
	resumeFrom string
}

// New 创建编排器。各依赖由调用方注入，便于测试与后续接入。
// resolver 可为 nil（agent 使用 AI 默认模型）。
func New(cfg *config.Config, ws *workspace.Workspace, ai agents.AIClient, git agents.GittorClient, bus *eventbus.Bus, resolver agents.ModelResolver) *Orchestrator {
	o := &Orchestrator{
		cfg:      cfg,
		ws:       ws,
		ai:       ai,
		git:      git,
		bus:      bus,
		agents:   make(map[string]agents.Agent),
		maxEval:  defaultMaxEval,
		resolver: resolver,
	}
	o.pausedCond = sync.NewCond(&o.controlMu)
	return o
}

// Register 注册某阶段的 agent。stage 应为 workspace 阶段常量
// （如 workspace.StageAnalyst）。重复注册将覆盖。
func (o *Orchestrator) Register(stage string, a agents.Agent) {
	o.agents[stage] = a
}

// RegisterDefaultNoop 为所有阶段注册占位 noop agent，便于编译与冒烟。
// 真实 agent 实现后，可用 Register 逐个覆盖。noop agent 仅打日志并返回 nil
// （对 Reviewer 即代表「评估通过」，循环立即结束）。
func (o *Orchestrator) RegisterDefaultNoop() {
	for _, stage := range workspace.AllStages() {
		o.agents[stage] = &noopAgent{name: stage}
	}
}

// SetMaxEvalRetries 覆盖 Coder→Reviewer 最大重试次数（测试用）。
func (o *Orchestrator) SetMaxEvalRetries(n int) {
	if n > 0 {
		o.maxEval = n
	}
}

// Run 启动编排状态机：
//
//  1. 运行一次 pipeline（Analyst→Architect→Planner→evalLoop→commitAndPush）。
//  2. 检查 requirements_queue.md，无新需求则完成。
//  3. 有新需求则运行 Mixor：
//     - Mixor 返回 nil（合并）：从 Planner 继续（resumeFrom=Planner）。
//     - Mixor 返回 ErrNeedRerun（冲突）：从 Analyst 重跑（resumeFrom=Analyst）。
//  4. 回到步骤 1，直到无新需求或被 Stop/ctx 取消。
func (o *Orchestrator) Run(ctx context.Context) error {
	for {
		if err := o.runPipeline(ctx); err != nil {
			return err
		}
		if !o.hasQueuedRequirements() {
			return nil
		}
		// 有新需求，运行 Mixor
		err := o.runStage(ctx, workspace.StageMixor)
		if err == nil {
			o.resumeFrom = workspace.StagePlanner
			continue
		}
		if errors.Is(err, agents.ErrNeedRerun) {
			o.resumeFrom = workspace.StageAnalyst
			continue
		}
		return err
	}
}

// runPipeline 执行一次完整 pipeline（顺序阶段 + 评估循环 + commitAndPush）。
// 若 resumeFrom 非空，则从该阶段开始（消费后清空）。
func (o *Orchestrator) runPipeline(ctx context.Context) error {
	stages := []string{workspace.StageAnalyst, workspace.StageArchitect, workspace.StagePlanner}
	startIdx := 0
	if o.resumeFrom != "" {
		for i, s := range stages {
			if s == o.resumeFrom {
				startIdx = i
				break
			}
		}
		o.resumeFrom = "" // 消费掉
	}

	for i := startIdx; i < len(stages); i++ {
		if err := o.runStage(ctx, stages[i]); err != nil {
			return err
		}
		if err := o.checkControl(ctx); err != nil {
			return err
		}
	}

	if err := o.runEvalLoop(ctx); err != nil {
		return err
	}
	if err := o.checkControl(ctx); err != nil {
		return err
	}

	if err := o.commitAndPush(); err != nil {
		return err
	}
	return nil
}

// runEvalLoop 执行 Coder → Reviewer 评估循环。
//
// 每次先构建 Coder 指令、调 Coder 生成代码，再调 Reviewer 评估。
// Reviewer 返回 nil 视为通过；返回 ErrEvaluationFailed 重试 Coder；
// 其他错误终止流程。达到最大重试次数仍未通过则返回错误。
func (o *Orchestrator) runEvalLoop(ctx context.Context) error {
	for attempt := 1; attempt <= o.maxEval; attempt++ {
		if err := o.buildCoderInstruction(); err != nil {
			return fmt.Errorf("构建 Coder 指令失败: %w", err)
		}
		if err := o.runStage(ctx, workspace.StageCoder); err != nil {
			return fmt.Errorf("评估第 %d 次 Coder 失败: %w", attempt, err)
		}
		err := o.runStage(ctx, workspace.StageReviewer)
		if err == nil {
			return nil
		}
		if !errors.Is(err, agents.ErrEvaluationFailed) {
			return fmt.Errorf("评估第 %d 次 Reviewer 失败: %w", attempt, err)
		}
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventLog,
			Agent: workspace.StageReviewer,
			Data: map[string]any{
				agents.DataKeyRound:    attempt,
				agents.DataKeyMaxRound: o.maxEval,
				agents.DataKeyReason:   "evaluation failed, retry coder",
			},
		})
	}
	return fmt.Errorf("评估达到最大重试次数 %d 仍未通过", o.maxEval)
}

// runStage 执行单一阶段 agent。
//
// agent 未注册时返回 "agent <stage> not registered"。
// 开始时发布 agent_start，成功发布 agent_done，失败发布 agent_failed。
// Reviewer 返回的哨兵错误（ErrEvaluationFailed）会原样上抛，由调用方处理。
func (o *Orchestrator) runStage(ctx context.Context, stage string) error {
	a, ok := o.agents[stage]
	if !ok {
		err := fmt.Errorf("agent %s not registered", stage)
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: stage,
			Data:  map[string]any{agents.DataKeyStage: stage, agents.DataKeyReason: err.Error()},
		})
		return err
	}

	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentStart,
		Agent: stage,
		Data:  map[string]any{agents.DataKeyStage: stage},
	})

	err := a.Run(ctx, o.ws, o.ai, o.git, o.bus, o.resolver)
	if err != nil {
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: stage,
			Data:  map[string]any{agents.DataKeyStage: stage, agents.DataKeyReason: err.Error()},
		})
		return err
	}

	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentDone,
		Agent: stage,
		Data:  map[string]any{agents.DataKeyStage: stage},
	})
	return nil
}

// checkControl 在阶段边界检查暂停/停止信号与 ctx 取消。
//
// - sigStop：返回 ErrStopped。
// - sigPause：进入阻塞等待 Resume 唤醒，唤醒后清除信号。
// - 任何状态下若 ctx 已取消，返回 ctx.Err()。
func (o *Orchestrator) checkControl(ctx context.Context) error {
	o.controlMu.Lock()
	defer o.controlMu.Unlock()

	if o.controlSig == sigStop {
		return agents.ErrStopped
	}
	if o.controlSig == sigPause {
		o.paused = true
		o.bus.Publish(eventbus.Event{
			Type:  "orch_paused",
			Data:  map[string]any{"stage": o.ws.Path()},
		})
		o.pausedCond.Wait()
		o.paused = false
		// 唤醒后重新检查信号：Stop 可能在 pause 期间到达，
		// 此时应优先返回 ErrStopped，而不是无条件清除信号后继续。
		if o.controlSig == sigStop {
			o.bus.Publish(eventbus.Event{Type: "orch_stopped", Data: map[string]any{}})
			return agents.ErrStopped
		}
		o.controlSig = sigNone
		o.bus.Publish(eventbus.Event{Type: "orch_resumed", Data: map[string]any{}})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Pause 请求编排器在下一个阶段边界暂停。
func (o *Orchestrator) Pause() {
	o.controlMu.Lock()
	o.controlSig = sigPause
	o.controlMu.Unlock()
}

// Stop 请求编排器在下一个阶段边界停止。若当前处于暂停，会唤醒以检查 stop。
func (o *Orchestrator) Stop() {
	o.controlMu.Lock()
	o.controlSig = sigStop
	o.controlMu.Unlock()
	o.pausedCond.Broadcast()
}

// Resume 唤醒已暂停的编排器继续执行。
func (o *Orchestrator) Resume() {
	o.controlMu.Lock()
	if o.paused {
		o.pausedCond.Broadcast()
	}
	o.controlMu.Unlock()
}

// IsPaused 返回当前是否处于暂停状态。
func (o *Orchestrator) IsPaused() bool {
	o.controlMu.Lock()
	defer o.controlMu.Unlock()
	return o.paused
}

// buildCoderInstruction 为 Coder 构造隔离指令文件（不调 LLM）。
//
// 读 task.md + spec.md，拼接为 agents/coder/instruction.md。
// 发布 agent_start/agent_done 事件（stage="coder_instruction"，保持 UI 兼容）。
func (o *Orchestrator) buildCoderInstruction() error {
	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentStart,
		Agent: "coder_instruction",
		Data:  map[string]any{agents.DataKeyStage: "coder_instruction"},
	})

	taskContent, err := o.ws.ReadDoc(workspace.DocTask)
	if err != nil {
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: "coder_instruction",
			Data:  map[string]any{agents.DataKeyStage: "coder_instruction", agents.DataKeyReason: err.Error()},
		})
		return fmt.Errorf("读取 task.md 失败: %w", err)
	}
	_, taskBody := workspace.ParseDoc(taskContent)

	specContent, err := o.ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: "coder_instruction",
			Data:  map[string]any{agents.DataKeyStage: "coder_instruction", agents.DataKeyReason: err.Error()},
		})
		return fmt.Errorf("读取 spec.md 失败: %w", err)
	}
	_, specBody := workspace.ParseDoc(specContent)

	var sb strings.Builder
	sb.WriteString("# 任务指令\n")
	sb.WriteString(strings.TrimRight(taskBody, "\n"))
	sb.WriteString("\n\n# Spec 要点\n")
	sb.WriteString(strings.TrimRight(specBody, "\n"))
	sb.WriteString("\n\n# 输出路径\n- 代码输出到 code/\n")

	instruction := sb.String()
	instrPath := filepath.Join("agents", "coder", "instruction.md")
	if err := o.ws.WriteDoc(instrPath, instruction); err != nil {
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: "coder_instruction",
			Data:  map[string]any{agents.DataKeyStage: "coder_instruction", agents.DataKeyReason: err.Error()},
		})
		return fmt.Errorf("写入 instruction.md 失败: %w", err)
	}

	// 确保 code/ 目录存在
	codeDir := filepath.Join(o.ws.Path(), "code")
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventAgentFailed,
			Agent: "coder_instruction",
			Data:  map[string]any{agents.DataKeyStage: "coder_instruction", agents.DataKeyReason: err.Error()},
		})
		return fmt.Errorf("创建 code 目录失败: %w", err)
	}

	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentDone,
		Agent: "coder_instruction",
		Data:  map[string]any{agents.DataKeyStage: "coder_instruction"},
	})
	return nil
}

// commitAndPush 提交并推送代码（不调 LLM）。
//
// 检查 spec.md 所有 Requirement 已打勾，调 git.CommitAndPush 提交 code/ 目录。
// 发布 agent_start/agent_done 事件（stage="gittor"，保持 UI 兼容）。
func (o *Orchestrator) commitAndPush() error {
	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentStart,
		Agent: "gittor",
		Data:  map[string]any{agents.DataKeyStage: "gittor"},
	})

	specContent, err := o.ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		return o.failCommit(fmt.Errorf("读取 spec.md 失败: %w", err))
	}
	if strings.Contains(specContent, agents.SpecRequirementPrefix) {
		return o.failCommit(fmt.Errorf("spec.md 仍有未完成的 Requirement，无法提交"))
	}

	projectID := o.ws.ProjectID()
	commitMsg := fmt.Sprintf("feat: 实现 %s 项目代码", projectID)
	codePath := filepath.Join(o.ws.Path(), "code")
	if err := o.git.CommitAndPush(context.Background(), []string{codePath}, commitMsg); err != nil {
		return o.failCommit(err)
	}

	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentDone,
		Agent: "gittor",
		Data:  map[string]any{agents.DataKeyStage: "gittor"},
	})
	return nil
}

// failCommit 发布 gittor 的 agent_failed 事件并返回原错误。
func (o *Orchestrator) failCommit(err error) error {
	o.bus.Publish(eventbus.Event{
		Type:  eventbus.EventAgentFailed,
		Agent: "gittor",
		Data:  map[string]any{agents.DataKeyStage: "gittor", agents.DataKeyReason: err.Error()},
	})
	return err
}

// hasQueuedRequirements 检查 requirements_queue.md 是否有内容。
func (o *Orchestrator) hasQueuedRequirements() bool {
	raw, err := o.ws.ReadDoc(workspace.DocReqQueue)
	if err != nil {
		return false
	}
	return strings.TrimSpace(raw) != ""
}

// noopAgent 占位 agent，仅打日志，不做任何实际工作。
//
// 用于 RegisterDefaultNoop，保证编排器在具体 agent 未实现时也能编译
// 并跑通冒烟流程。其 Run 永远返回 nil（对 Reviewer 即代表评估通过）。
type noopAgent struct {
	name string
}

func (n *noopAgent) Name() string { return n.name }

func (n *noopAgent) Run(ctx context.Context, ws *workspace.Workspace, ai agents.AIClient, git agents.GittorClient, bus *eventbus.Bus, resolver agents.ModelResolver) error {
	log.Printf("[noop] agent %s run (skipped)", n.name)
	bus.Publish(eventbus.Event{
		Type:  eventbus.EventLog,
		Agent: n.name,
		Data:  map[string]any{agents.DataKeyAgent: n.name, agents.DataKeyReason: "noop agent, skipped"},
	})
	return nil
}
