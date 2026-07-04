// Package agents 中 Gittor Agent 的实现。
//
// Gittor 是流程收尾阶段的 agent：在 Evaluator 代码评估通过后，确认 spec.md
// 中所有 Requirement 均已打勾，随后将 code/ 目录的代码提交并推送到远端仓库。
//
// commit message 遵循 conventional commits（如 "feat: 实现 <projectID> 项目代码"）。
// git 操作通过 Run 参数注入的 GittorClient 完成，保证上下文隔离。
package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// GittorAgent 是提交阶段的 agent：评估通过后将 code/ 目录提交并推送。
//
// 该 agent 不持有状态，git 客户端由编排器通过 Run 参数注入（与其他 agent
// 保持一致）。Run 时读取 spec.md 确认所有 Requirement 已打勾，再调用
// git.CommitAndPush 提交 code/ 目录。
type GittorAgent struct {
	model string // 保留字段以与其他 agent 构造签名一致；Gittor 不调用 AI
}

// NewGittorAgent 构造一个 GittorAgent 实例。model 仅为签名一致性保留，本 agent 不调用 AI。
func NewGittorAgent(model string) *GittorAgent {
	return &GittorAgent{model: model}
}

// Name 返回 agent 标识，与 workspace 阶段常量 StageGittor 对应。
func (g *GittorAgent) Name() string {
	return workspace.StageGittor
}

// Run 执行 Gittor 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 spec.md，确认所有 Requirement 已打勾（### [x] Requirement:）
//  3. 调用 git.CommitAndPush 提交 code/ 目录并推送
//  4. 发布 agent_done；任一步骤失败发布 agent_failed
func (g *GittorAgent) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, g.Name(), map[string]any{
		DataKeyStage: workspace.StageGittor,
		DataKeyAgent: g.Name(),
	})

	// 2. 读取 spec.md 确认所有 Requirement 已打勾
	specContent, err := ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		return g.fail(bus, fmt.Errorf("读取 spec.md 失败: %w", err))
	}

	// 检查是否存在未打勾的 Requirement（### Requirement: 但非 ### [x] Requirement:）
	// SpecRequirementPrefix 为 "### Requirement: "，不会匹配 "### [x] Requirement: "
	if strings.Contains(specContent, SpecRequirementPrefix) {
		return g.fail(bus, fmt.Errorf("尚有未完成的 Requirement"))
	}

	// 3. 提交并推送 code/ 目录
	commitMessage := fmt.Sprintf("feat: 实现 %s 项目代码", ws.ProjectID())
	if err := git.CommitAndPush(ctx, []string{"code/"}, commitMessage); err != nil {
		return g.fail(bus, fmt.Errorf("提交代码失败: %w", err))
	}

	// 4. 发布 agent_done
	publishEvent(bus, eventbus.EventAgentDone, g.Name(), map[string]any{
		DataKeyStage: workspace.StageGittor,
		DataKeyAgent: g.Name(),
	})
	return nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (g *GittorAgent) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, g.Name(), map[string]any{
		DataKeyStage:  workspace.StageGittor,
		DataKeyAgent:  g.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}
