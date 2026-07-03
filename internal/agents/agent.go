// Package agents 定义 zzauto 各 agent 角色的统一接口与文档协议。
//
// 该包仅声明接口与文档 schema 约定，不包含具体 agent 的业务实现。
// 各具体 agent（Listener/Asker/Planner/Designer/Evaluator/Manager/
// Executor/Generator/Gittor）在后续任务中分别实现 Agent 接口。
//
// 编排器（internal/orchestrator）依赖此处的 Agent 接口调度各阶段，
// 通过 AIClient 调用 AI、通过 GittorClient 调用 git、通过事件总线
// 向外广播状态。
package agents

import (
	"context"

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
}

// GittorClient 是 agent 调用 git 操作的统一抽象。
//
// 所有 git 操作（commit/push）必须经此接口，禁止 agent 直接调 git CLI，
// 以保证上下文隔离（git 操作仅由 Gittor 在隔离环境执行）。
type GittorClient interface {
	// CommitAndPush 将指定路径暂存并提交，随后推送到远端。
	CommitAndPush(ctx context.Context, paths []string, message string) error
}

// Agent 是所有 agent 角色的统一接口。
//
// Name 返回 agent 标识（与 workspace 阶段常量对应，如 "listener"）。
// Run 执行该 agent 的一次完整工作：读取上游文档、调用 AI、产出下游文档、
// 通过 bus 发布状态事件。失败时返回非 nil error，编排器据此终止流程。
type Agent interface {
	Name() string
	Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error
}
