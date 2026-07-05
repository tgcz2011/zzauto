// Package registry 负责 zzauto 全部组件的装配：创建 aicli 客户端、gittor、
// 各阶段 agent，并将它们注册到 orchestrator。
//
// 主要导出函数：
//   - BuildOrchestrator：生产用入口，内部创建真实 aicli 客户端与 gittor
//   - BuildOrchestratorWithDeps：测试用入口，接受注入的 AI 与 git 客户端
//   - RegisterAgents：将全部 6 个 agent 注册到已有编排器
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
// 内部创建 aicli 客户端与 gittor，初始化 git 仓库，注册全部 6 个 agent。
// askFunc 为 Analyst agent 的提问回调，为 nil 时 Analyst 使用默认 askViaBus
// （通过事件总线与 ask_reply.md 文件交互）。
// resolver 按 stage 解析模型（可为 nil，使用 AI 默认模型）。
func BuildOrchestrator(cfg *config.Config, ws *workspace.Workspace, bus *eventbus.Bus, askFunc agents.AskFunc, resolver agents.ModelResolver) (*orchestrator.Orchestrator, error) {
	// 创建 aicli 客户端
	aiClient := aicli.New(cfg.AicliAddr, cfg.AicliKey)

	// 创建 gittor 并初始化 git 仓库
	gitClient := gittor.New(ws.Path(), cfg.Github.Remote, cfg.Github.Branch, cfg.Github.Token)
	if err := gitClient.EnsureRepo(context.Background()); err != nil {
		return nil, fmt.Errorf("初始化 git 仓库失败: %w", err)
	}

	// 创建编排器并注册 agent
	orch := orchestrator.New(cfg, ws, aiClient, gitClient, bus, resolver)
	RegisterAgents(orch, askFunc)
	return orch, nil
}

// BuildOrchestratorWithDeps 用给定的 AI 客户端与 git 客户端装配编排器（测试用）。
//
// 与 BuildOrchestrator 的区别：AI 与 git 客户端由调用方注入，便于测试使用
// mock AI 与本地 bare 仓库。调用方需自行确保 git 仓库已初始化（如调用
// gittor.EnsureRepo）。resolver 为各 stage 配置的模型解析器，可为 nil。
func BuildOrchestratorWithDeps(cfg *config.Config, ws *workspace.Workspace, bus *eventbus.Bus, ai agents.AIClient, git agents.GittorClient, askFunc agents.AskFunc, resolver agents.ModelResolver) *orchestrator.Orchestrator {
	orch := orchestrator.New(cfg, ws, ai, git, bus, resolver)
	RegisterAgents(orch, askFunc)
	return orch
}

// RegisterAgents 将全部 6 个 agent 注册到已有编排器。
//
// 阶段顺序：
//
//	Analyst → Architect → Planner → (Coder ↔ Reviewer 评估循环)
//	→ commitAndPush → Mixor（处理异步需求）
//
// askFunc 为 nil 时 Analyst 使用默认 askViaBus 实现。
// 模型在运行时由 resolver 解析（不再启动时传入 roleModels）。
func RegisterAgents(orch *orchestrator.Orchestrator, askFunc agents.AskFunc) {
	orch.Register(workspace.StageAnalyst, agents.NewAnalyst(askFunc))
	orch.Register(workspace.StageArchitect, agents.NewArchitect())
	orch.Register(workspace.StagePlanner, agents.NewPlanner())
	orch.Register(workspace.StageCoder, agents.NewCoder())
	orch.Register(workspace.StageReviewer, agents.NewReviewer())
	orch.Register(workspace.StageMixor, agents.NewMixor())
}
