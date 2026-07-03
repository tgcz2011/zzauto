// Package orchestrator 实现 zzauto 的多 agent 编排器。
//
// 编排器按固定流程顺序调度 9 个 agent：
//
//	Listener → Asker → Planner → (Designer ↔ Evaluator 讨论循环)
//	→ Manager → Executor → Generator → Evaluator → Gittor
//
// 文档在 agent 间通过 workspace 文件系统传递：
//
//	desire.md → need.md → spec.md → deal.md → task.md → report
//
// 关键循环：
//   - Designer↔Evaluator：多轮讨论直到 Evaluator 返回 nil（达成共识）
//     或达到最大轮数（默认 5）。
//   - Evaluator→Generator：Generator 完成后 Evaluator 评估，不通过则回到
//     Generator 重试（最大 3 次）。
//
// 每个 agent 在执行前后通过事件总线发布 agent_start / agent_done /
// agent_failed 事件，供 UI 与日志订阅。某阶段 agent 未注册时返回明确错误。
package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// 讨论循环与评估重试的默认上限。
const (
	defaultMaxDiscussRounds = 5
	defaultMaxEvalRetries   = 3
)

// Orchestrator 多 agent 编排器。
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
	maxDisc int // Designer↔Evaluator 最大讨论轮数
	maxEval int // Evaluator→Generator 最大重试次数
}

// New 创建编排器。各依赖由调用方注入，便于测试与后续接入。
func New(cfg *config.Config, ws *workspace.Workspace, ai agents.AIClient, git agents.GittorClient, bus *eventbus.Bus) *Orchestrator {
	return &Orchestrator{
		cfg:     cfg,
		ws:      ws,
		ai:      ai,
		git:     git,
		bus:     bus,
		agents:  make(map[string]agents.Agent),
		maxDisc: defaultMaxDiscussRounds,
		maxEval: defaultMaxEvalRetries,
	}
}

// Register 注册某阶段的 agent。stage 应为 workspace 阶段常量
// （如 workspace.StageListener）。重复注册将覆盖。
func (o *Orchestrator) Register(stage string, a agents.Agent) {
	o.agents[stage] = a
}

// RegisterDefaultNoop 为所有阶段注册占位 noop agent，便于编译与冒烟。
// 真实 agent 实现后，可用 Register 逐个覆盖。noop agent 仅打日志并返回 nil
// （对 Evaluator 即代表「达成共识 / 评估通过」，循环立即结束）。
func (o *Orchestrator) RegisterDefaultNoop() {
	for _, stage := range []string{
		workspace.StageListener,
		workspace.StageAsker,
		workspace.StagePlanner,
		workspace.StageDesigner,
		workspace.StageEvaluator,
		workspace.StageManager,
		workspace.StageExecutor,
		workspace.StageGenerator,
		workspace.StageGittor,
	} {
		o.agents[stage] = &noopAgent{name: stage}
	}
}

// SetMaxDiscussRounds 覆盖 Designer↔Evaluator 最大讨论轮数（测试用）。
func (o *Orchestrator) SetMaxDiscussRounds(n int) {
	if n > 0 {
		o.maxDisc = n
	}
}

// SetMaxEvalRetries 覆盖 Evaluator→Generator 最大重试次数（测试用）。
func (o *Orchestrator) SetMaxEvalRetries(n int) {
	if n > 0 {
		o.maxEval = n
	}
}

// Run 启动编排状态机，按顺序执行各阶段。
//
// 流程：
//  1. Listener
//  2. Asker
//  3. Planner
//  4. Designer↔Evaluator 讨论循环（最多 maxDisc 轮）
//  5. Manager
//  6. Executor
//  7. Generator → Evaluator 评估循环（最多 maxEval 次重试）
//  8. Gittor
//
// 任一阶段失败（返回非哨兵 error）即发布 agent_failed 并返回错误。
// 讨论循环中 Evaluator 返回 ErrNoConsensus 视为继续，达到上限则视为失败。
func (o *Orchestrator) Run(ctx context.Context) error {
	// 顺序执行的简单阶段。
	sequential := []string{
		workspace.StageListener,
		workspace.StageAsker,
		workspace.StagePlanner,
	}
	for _, stage := range sequential {
		if err := o.runStage(ctx, stage); err != nil {
			return err
		}
	}

	// Designer↔Evaluator 讨论循环。
	if err := o.runDiscussLoop(ctx); err != nil {
		return err
	}

	// Manager、Executor。
	if err := o.runStage(ctx, workspace.StageManager); err != nil {
		return err
	}
	if err := o.runStage(ctx, workspace.StageExecutor); err != nil {
		return err
	}

	// Generator → Evaluator 评估循环。
	if err := o.runEvalLoop(ctx); err != nil {
		return err
	}

	// Gittor 收尾。
	if err := o.runStage(ctx, workspace.StageGittor); err != nil {
		return err
	}
	return nil
}

// runStage 执行单一阶段 agent。
//
// agent 未注册时返回 "agent <stage> not registered"。
// 开始时发布 agent_start，成功发布 agent_done，失败发布 agent_failed。
// Evaluator 返回的哨兵错误（ErrNoConsensus/ErrEvaluationFailed）会原样
// 上抛，由调用方（循环逻辑）处理。
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

	if err := a.Run(ctx, o.ws, o.ai, o.git, o.bus); err != nil {
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

// runDiscussLoop 执行 Designer↔Evaluator 讨论循环。
//
// 每轮先调 Designer（起草/修订协议），再调 Evaluator（批判评估）。
// Evaluator 返回 nil 视为达成共识，结束循环；返回 ErrNoConsensus 进入
// 下一轮；其他错误终止流程。达到最大轮数仍未共识则返回错误。
func (o *Orchestrator) RunDiscussLoop(ctx context.Context) error {
	return o.runDiscussLoop(ctx)
}

func (o *Orchestrator) runDiscussLoop(ctx context.Context) error {
	for round := 1; round <= o.maxDisc; round++ {
		// Designer 起草/修订。
		if err := o.runStage(ctx, workspace.StageDesigner); err != nil {
			return fmt.Errorf("讨论第 %d 轮 Designer 失败: %w", round, err)
		}
		// Evaluator 评估。
		err := o.runStage(ctx, workspace.StageEvaluator)
		if err == nil {
			// 达成共识。
			o.bus.Publish(eventbus.Event{
				Type:  eventbus.EventLog,
				Agent: workspace.StageEvaluator,
				Data: map[string]any{
					agents.DataKeyRound:    round,
					agents.DataKeyMaxRound: o.maxDisc,
					agents.DataKeyReason:   "consensus reached",
				},
			})
			return nil
		}
		if err != agents.ErrNoConsensus {
			// 非「未共识」的真实失败。
			return fmt.Errorf("讨论第 %d 轮 Evaluator 失败: %w", round, err)
		}
		// 未共识，继续下一轮。
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventLog,
			Agent: workspace.StageEvaluator,
			Data: map[string]any{
				agents.DataKeyRound:    round,
				agents.DataKeyMaxRound: o.maxDisc,
				agents.DataKeyReason:   "no consensus, continue",
			},
		})
	}
	return fmt.Errorf("讨论达到最大轮数 %d 仍未达成共识", o.maxDisc)
}

// runEvalLoop 执行 Generator → Evaluator 评估循环。
//
// 每次先调 Generator 生成/修复，再调 Evaluator 评估。
// Evaluator 返回 nil 视为评估通过，结束循环；返回 ErrEvaluationFailed 进入
// 下一轮重试；其他错误终止流程。达到最大重试次数仍未通过则返回错误。
func (o *Orchestrator) RunEvalLoop(ctx context.Context) error {
	return o.runEvalLoop(ctx)
}

func (o *Orchestrator) runEvalLoop(ctx context.Context) error {
	for attempt := 1; attempt <= o.maxEval; attempt++ {
		// Generator 生成/修复。
		if err := o.runStage(ctx, workspace.StageGenerator); err != nil {
			return fmt.Errorf("评估第 %d 次 Generator 失败: %w", attempt, err)
		}
		// Evaluator 评估。
		err := o.runStage(ctx, workspace.StageEvaluator)
		if err == nil {
			// 评估通过。
			o.bus.Publish(eventbus.Event{
				Type:  eventbus.EventLog,
				Agent: workspace.StageEvaluator,
				Data: map[string]any{
					agents.DataKeyRound:    attempt,
					agents.DataKeyMaxRound: o.maxEval,
					agents.DataKeyReason:   "evaluation passed",
				},
			})
			return nil
		}
		if err != agents.ErrEvaluationFailed {
			return fmt.Errorf("评估第 %d 次 Evaluator 失败: %w", attempt, err)
		}
		// 不通过，回到 Generator 重试。
		o.bus.Publish(eventbus.Event{
			Type:  eventbus.EventLog,
			Agent: workspace.StageEvaluator,
			Data: map[string]any{
				agents.DataKeyRound:    attempt,
				agents.DataKeyMaxRound: o.maxEval,
				agents.DataKeyReason:   "evaluation failed, retry generator",
			},
		})
	}
	return fmt.Errorf("评估达到最大重试次数 %d 仍未通过", o.maxEval)
}

// noopAgent 占位 agent，仅打日志，不做任何实际工作。
//
// 用于 RegisterDefaultNoop，保证编排器在具体 agent 未实现时也能编译
// 并跑通冒烟流程。其 Run 永远返回 nil（对 Evaluator 即代表共识/通过）。
type noopAgent struct {
	name string
}

func (n *noopAgent) Name() string { return n.name }

func (n *noopAgent) Run(ctx context.Context, ws *workspace.Workspace, ai agents.AIClient, git agents.GittorClient, bus *eventbus.Bus) error {
	log.Printf("[noop] agent %s run (skipped)", n.name)
	bus.Publish(eventbus.Event{
		Type:  eventbus.EventLog,
		Agent: n.name,
		Data:  map[string]any{agents.DataKeyAgent: n.name, agents.DataKeyReason: "noop agent, skipped"},
	})
	return nil
}
