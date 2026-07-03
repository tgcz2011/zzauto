package agents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// Executor 是任务准备阶段的 agent：读取 task.md 与 spec.md，为 Generator
// 构造隔离工作目录与指令文件。
//
// 指令文件仅含任务描述、spec 验收要点与输出路径，不含 desire/need/deal
// 等用户原始需求文档，以保证 Generator 上下文隔离——Generator 只能看见
// Executor 投喂的内容，无法触达用户的原始欲望与讨论过程。
//
// Executor 不调用 AI，一次性准备完指令即交付，符合 MVP 单 Generator 模式。
type Executor struct{}

// NewExecutor 构造一个 Executor 实例。
func NewExecutor() *Executor { return &Executor{} }

// Name 返回 agent 标识，与 workspace 阶段常量 StageExecutor 对应。
func (e *Executor) Name() string { return workspace.StageExecutor }

// instructionDocName 是 Executor 写入的指令文件相对路径（相对 projectDir）。
const instructionDocName = "agents/generator/instruction.md"

// Run 执行 Executor 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 task.md 与 spec.md；任一缺失返回错误
//  3. 创建隔离目录 agents/generator 与代码输出目录 code/
//  4. 拼装指令（任务指令 + Spec 要点 + 输出路径），不含 desire/need/deal
//  5. 写入 agents/generator/instruction.md
//  6. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (e *Executor) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, e.Name(), map[string]any{
		DataKeyStage: workspace.StageExecutor,
		DataKeyAgent: e.Name(),
	})

	// 2. 读取 task.md 与 spec.md；缺失返回错误并发布 agent_failed
	taskContent, err := e.readDoc(ws, bus, workspace.DocTask)
	if err != nil {
		return err
	}
	specContent, err := e.readDoc(ws, bus, workspace.DocSpec)
	if err != nil {
		return err
	}

	// 3. 创建隔离目录 agents/generator
	genDir := filepath.Join(ws.AgentsDir(), "generator")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return e.fail(bus, fmt.Errorf("创建隔离目录 %s 失败: %w", genDir, err))
	}

	// 创建代码输出目录 code/
	codeDir := filepath.Join(ws.Path(), "code")
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		return e.fail(bus, fmt.Errorf("创建代码输出目录 %s 失败: %w", codeDir, err))
	}

	// 4. 拼装指令内容（隔离：仅任务 + spec 要点 + 输出路径）
	instruction := buildInstruction(taskContent, specContent)

	// 5. 写入指令文件（WriteDoc 只 MkdirAll projectDir，需先确保 agents 子目录存在）
	if err := os.MkdirAll(ws.AgentsDir(), 0o755); err != nil {
		return e.fail(bus, fmt.Errorf("创建 agents 目录失败: %w", err))
	}
	if err := ws.WriteDoc(instructionDocName, instruction); err != nil {
		return e.fail(bus, fmt.Errorf("写入指令文件失败: %w", err))
	}

	// 6. 发布 doc_update 与 agent_done
	publishEvent(bus, eventbus.EventDocUpdate, e.Name(), map[string]any{
		DataKeyStage: workspace.StageExecutor,
		DataKeyAgent: e.Name(),
		"doc":        instructionDocName,
	})
	publishEvent(bus, eventbus.EventAgentDone, e.Name(), map[string]any{
		DataKeyStage: workspace.StageExecutor,
		DataKeyAgent: e.Name(),
	})
	return nil
}

// readDoc 读取一份文档。文件不存在或读取失败时发布 agent_failed 并返回
// 包装后的错误，避免在 Run 中重复编写相同错误处理逻辑。
func (e *Executor) readDoc(ws *workspace.Workspace, bus *eventbus.Bus, name string) (string, error) {
	content, err := ws.ReadDoc(name)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("%s 不存在", name)
		} else {
			failErr = fmt.Errorf("读取 %s 失败: %w", name, err)
		}
		return "", e.fail(bus, failErr)
	}
	return content, nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (e *Executor) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, e.Name(), map[string]any{
		DataKeyStage:  workspace.StageExecutor,
		DataKeyAgent:  e.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// buildInstruction 根据任务正文与 spec 正文拼装 Generator 指令文件内容。
//
// 指令分三段：
//   - # 任务指令：直接复制 task.md 正文（所有任务项）
//   - # Spec 要点：直接复制 spec.md 全文（Generator 需知道验收标准）
//   - # 输出路径：代码输出到 code/，report 输出到 reports/generator.md
//
// 注意：不含 desire.md / need.md / deal.md 内容，保证 Generator 与用户
// 原始需求隔离。
func buildInstruction(taskContent, specContent string) string {
	var b strings.Builder
	b.WriteString("# 任务指令\n")
	b.WriteString(strings.TrimRight(taskContent, "\n"))
	b.WriteString("\n\n# Spec 要点\n")
	b.WriteString(strings.TrimRight(specContent, "\n"))
	b.WriteString("\n\n# 输出路径\n")
	b.WriteString("- 代码输出到 `code/` 目录（相对项目根）\n")
	b.WriteString("- report 输出到 `reports/generator.md`\n")
	return b.String()
}
