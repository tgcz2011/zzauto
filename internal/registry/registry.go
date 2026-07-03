// Package registry 负责 zzauto 全部组件的装配：创建 aicli 客户端、gittor、
// 各阶段 agent，并将它们注册到 orchestrator。
//
// 主要导出函数：
//   - BuildOrchestrator：生产用入口，内部创建真实 aicli 客户端与 gittor
//   - BuildOrchestratorWithDeps：测试用入口，接受注入的 AI 与 git 客户端
//   - RegisterAgents：将全部 9 个 agent 注册到已有编排器
package registry

import (
	"context"
	"fmt"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/gittor"
	"github.com/tgcz2011/zzauto/internal/orchestrator"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// BuildOrchestrator 创建并装配编排器（生产用）。
//
// 内部创建 aicli 客户端与 gittor，初始化 git 仓库，注册全部 9 个 agent。
// askFunc 为 Asker agent 的提问回调，为 nil 时 Asker 使用默认 askViaBus
// （通过事件总线与 ask_reply.md 文件交互）。
func BuildOrchestrator(cfg *config.Config, ws *workspace.Workspace, bus *eventbus.Bus, askFunc agents.AskFunc) (*orchestrator.Orchestrator, error) {
	// 创建 aicli 客户端
	aiClient := aicli.New(cfg.AicliAddr, cfg.AicliKey)

	// 创建 gittor 并初始化 git 仓库
	gitClient := gittor.New(ws.Path(), cfg.Github.Remote, cfg.Github.Branch, cfg.Github.Token)
	if err := gitClient.EnsureRepo(context.Background()); err != nil {
		return nil, fmt.Errorf("初始化 git 仓库失败: %w", err)
	}

	// 创建编排器并注册 agent
	orch := orchestrator.New(cfg, ws, aiClient, gitClient, bus)
	RegisterAgents(orch, askFunc)
	return orch, nil
}

// BuildOrchestratorWithDeps 用给定的 AI 客户端与 git 客户端装配编排器（测试用）。
//
// 与 BuildOrchestrator 的区别：AI 与 git 客户端由调用方注入，便于测试使用
// mock AI 与本地 bare 仓库。调用方需自行确保 git 仓库已初始化（如调用
// gittor.EnsureRepo）。
func BuildOrchestratorWithDeps(cfg *config.Config, ws *workspace.Workspace, bus *eventbus.Bus, ai agents.AIClient, git agents.GittorClient, askFunc agents.AskFunc) *orchestrator.Orchestrator {
	orch := orchestrator.New(cfg, ws, ai, git, bus)
	RegisterAgents(orch, askFunc)
	return orch
}

// RegisterAgents 将全部 9 个 agent 注册到已有编排器。
//
// 阶段顺序：
//
//	Listener → Asker → Planner → Designer ↔ Evaluator（讨论循环）
//	→ Manager → Executor → Generator → Evaluator（评估循环）→ Gittor
//
// askFunc 为 nil 时 Asker 使用默认 askViaBus 实现。
func RegisterAgents(orch *orchestrator.Orchestrator, askFunc agents.AskFunc) {
	orch.Register(workspace.StageListener, agents.NewListener())
	orch.Register(workspace.StageAsker, agents.NewAsker(askFunc))
	orch.Register(workspace.StagePlanner, agents.NewPlanner())
	orch.Register(workspace.StageDesigner, agents.NewDesigner())
	orch.Register(workspace.StageEvaluator, agents.NewEvaluator())
	orch.Register(workspace.StageManager, agents.NewManager())
	orch.Register(workspace.StageExecutor, agents.NewExecutor())
	orch.Register(workspace.StageGenerator, agents.NewGenerator())
	orch.Register(workspace.StageGittor, agents.NewGittorAgent())
}
