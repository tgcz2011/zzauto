// Package agents 中 Mixor Agent 的实现。
//
// Mixor 融合者：管理需求队列 requirements_queue.md + 融合文档/代码 +
// 产出进度报告。当用户通过队列追加新需求时，Mixor 判断新需求与现有产出
// 是否冲突：可合并则更新 spec.md/task.md 并返回 nil（从 Planner 继续）；
// 冲突则把新需求追加到 input.md 并返回 ErrNeedRerun（从 Analyst 重跑）。
package agents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mixorSystemPrompt 是 Mixor 调用 AI 时使用的系统提示词。
const mixorSystemPrompt = `你是融合者。分析新需求与现有产出是否冲突。输出 JSON: {"conflict": true/false, "action": "merge"/"rerun", "merged_spec": "合并后的 spec 内容（仅 merge 时）", "reason": "判断理由"}。

判定规则：
1. 若新需求可融入现有 spec/deal/task 而不推翻既有设计，action 为 "merge"，
   并在 merged_spec 字段填入合并后的完整 spec.md 正文（保留原有 ### [x]/### [ ] 标记）。
2. 若新需求与现有产出根本性冲突（架构变更、推翻核心设计等），action 为 "rerun"，
   merged_spec 留空字符串。
3. conflict 字段：action 为 "rerun" 时为 true，"merge" 时为 false。
4. reason 必须给出判断理由（一句话）。
5. 严格输出 JSON，不要包裹代码块、不要输出任何额外说明文字。`

// mixorResult 是 Mixor 解析 AI JSON 输出的结构。
type mixorResult struct {
	Conflict    bool   `json:"conflict"`
	Action      string `json:"action"`
	MergedSpec  string `json:"merged_spec"`
	Reason      string `json:"reason"`
}

// Mixor 融合者：管理需求队列 + 融合文档/代码 + 进度报告。
type Mixor struct{}

// NewMixor 构造一个 Mixor 实例。
func NewMixor() *Mixor { return &Mixor{} }

// Name 返回 agent 标识，与 workspace.StageMixor 对应。
func (m *Mixor) Name() string { return workspace.StageMixor }

// Run 执行 Mixor 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读 requirements_queue.md；缺失或空则视为无需融合，直接发布 done 返回 nil
//  3. 读现有 spec.md / deal.md / task.md + code/ 文件列表
//  4. 调用 AI 判断 merge / rerun
//  5. 解析 JSON
//  6. action=="merge"：把 merged_spec 写入 spec.md，task.md 追加新任务标记，
//     清空 requirements_queue.md，写 reports/progress.md，返回 nil
//  7. action=="rerun"：把新需求追加到 input.md，清空 requirements_queue.md，
//     写 reports/progress.md，返回 ErrNeedRerun
//  8. 失败发布 agent_failed
func (m *Mixor) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, m.Name(), map[string]any{
		DataKeyStage: workspace.StageMixor,
		DataKeyAgent: m.Name(),
	})

	// 2. 读 requirements_queue.md；缺失或空则视为无需融合
	queueContent, err := ws.ReadDoc(workspace.DocReqQueue)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// 队列不存在：无新需求，直接完成
			publishEvent(bus, eventbus.EventAgentDone, m.Name(), map[string]any{
				DataKeyStage: workspace.StageMixor,
				DataKeyAgent: m.Name(),
			})
			return nil
		}
		return m.fail(bus, fmt.Errorf("读取 requirements_queue.md 失败: %w", err))
	}
	if strings.TrimSpace(queueContent) == "" {
		// 队列为空：无新需求，直接完成
		publishEvent(bus, eventbus.EventAgentDone, m.Name(), map[string]any{
			DataKeyStage: workspace.StageMixor,
			DataKeyAgent: m.Name(),
		})
		return nil
	}

	// 3. 读现有 spec/deal/task（缺失则视为空）+ code/ 文件列表
	specContent := readOptionalDoc(ws, workspace.DocSpec)
	dealContent := readOptionalDoc(ws, workspace.DocDeal)
	taskContent := readOptionalDoc(ws, workspace.DocTask)
	codeFiles := listCodeFiles(ws)

	// 4. 拼装上下文，调用 AI 判断
	combined := fmt.Sprintf(
		"# 新需求（requirements_queue.md）\n%s\n\n# 现有 spec.md\n%s\n\n# 现有 deal.md\n%s\n\n# 现有 task.md\n%s\n\n# code/ 文件列表\n%s",
		queueContent, specContent, dealContent, taskContent, codeFiles,
	)
	model := ResolveModel(resolver, workspace.StageMixor)
	resp, _, err := RunWithTracking(ctx, ws, bus, ai, m.Name(), model, mixorSystemPrompt, combined)
	if err != nil {
		return m.fail(bus, fmt.Errorf("调用 AI 融合判断失败: %w", err))
	}

	// 5. 解析 JSON
	var result mixorResult
	if err := parseJSONResponse(resp, &result); err != nil {
		return m.fail(bus, fmt.Errorf("解析 Mixor 结果失败: %w", err))
	}

	// 6/7. 按 action 分流
	switch result.Action {
	case "merge":
		return m.handleMerge(ws, bus, result, queueContent)
	case "rerun":
		return m.handleRerun(ws, bus, result, queueContent)
	default:
		return m.fail(bus, fmt.Errorf("未知的 mixor action: %q", result.Action))
	}
}

// handleMerge 处理合并路径：把 merged_spec 写入 spec.md，task.md 追加新任务标记，
// 清空 requirements_queue.md，写 reports/progress.md，返回 nil。
func (m *Mixor) handleMerge(ws *workspace.Workspace, bus *eventbus.Bus, result mixorResult, queueContent string) error {
	if strings.TrimSpace(result.MergedSpec) == "" {
		return m.fail(bus, fmt.Errorf("merge 动作但 merged_spec 为空"))
	}
	// 把 merged_spec 写入 spec.md（保留原 frontmatter 若有，否则新建）
	prevSpec, _ := ws.ReadDoc(workspace.DocSpec)
	meta, _ := workspace.ParseDoc(prevSpec)
	if meta.Stage == "" {
		meta.Stage = workspace.StageMixor
	}
	meta.Status = workspace.StatusDone
	meta.UpdatedAt = time.Now()
	rendered := workspace.RenderDoc(meta, result.MergedSpec)
	if err := ws.WriteDoc(workspace.DocSpec, rendered); err != nil {
		return m.fail(bus, fmt.Errorf("写入 spec.md 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, m.Name(), map[string]any{
		DataKeyStage: workspace.StageMixor,
		DataKeyAgent: m.Name(),
		"doc":        workspace.DocSpec,
	})

	// task.md 追加新任务标记（提示 Planner 后续重新拆解）
	prevTask, _ := ws.ReadDoc(workspace.DocTask)
	_, taskBody := workspace.ParseDoc(prevTask)
	updatedTask := strings.TrimRight(taskBody, "\n") + "\n\n<!-- mixor: 新需求已合并入 spec，请 Planner 重新拆解本文件 -->\n"
	taskMeta := workspace.DocMeta{
		Stage:     workspace.StageMixor,
		Status:    workspace.StatusRunning,
		UpdatedAt: time.Now(),
	}
	if err := ws.WriteDoc(workspace.DocTask, workspace.RenderDoc(taskMeta, updatedTask)); err != nil {
		return m.fail(bus, fmt.Errorf("写入 task.md 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, m.Name(), map[string]any{
		DataKeyStage: workspace.StageMixor,
		DataKeyAgent: m.Name(),
		"doc":        workspace.DocTask,
	})

	// 清空 requirements_queue.md
	if err := ws.WriteDoc(workspace.DocReqQueue, ""); err != nil {
		return m.fail(bus, fmt.Errorf("清空 requirements_queue.md 失败: %w", err))
	}

	// 写 reports/progress.md
	if err := m.writeProgress(ws, "merge", result.Reason, queueContent); err != nil {
		return m.fail(bus, err)
	}

	publishEvent(bus, eventbus.EventAgentDone, m.Name(), map[string]any{
		DataKeyStage: workspace.StageMixor,
		DataKeyAgent: m.Name(),
	})
	return nil
}

// handleRerun 处理重跑路径：把新需求追加到 input.md，清空 requirements_queue.md，
// 写 reports/progress.md，返回 ErrNeedRerun。
func (m *Mixor) handleRerun(ws *workspace.Workspace, bus *eventbus.Bus, result mixorResult, queueContent string) error {
	// 把新需求追加到 input.md
	prevInput, _ := ws.ReadDoc(workspace.DocInput)
	appended := strings.TrimRight(prevInput, "\n") + "\n\n# 追加需求（由 Mixor 转入重跑）\n" + queueContent + "\n"
	if err := ws.WriteDoc(workspace.DocInput, appended); err != nil {
		return m.fail(bus, fmt.Errorf("追加 input.md 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, m.Name(), map[string]any{
		DataKeyStage: workspace.StageMixor,
		DataKeyAgent: m.Name(),
		"doc":        workspace.DocInput,
	})

	// 清空 requirements_queue.md
	if err := ws.WriteDoc(workspace.DocReqQueue, ""); err != nil {
		return m.fail(bus, fmt.Errorf("清空 requirements_queue.md 失败: %w", err))
	}

	// 写 reports/progress.md
	if err := m.writeProgress(ws, "rerun", result.Reason, queueContent); err != nil {
		return m.fail(bus, err)
	}

	// 发布 agent_failed（携带原因）后返回哨兵 ErrNeedRerun
	publishEvent(bus, eventbus.EventAgentFailed, m.Name(), map[string]any{
		DataKeyStage:  workspace.StageMixor,
		DataKeyAgent:  m.Name(),
		DataKeyReason: ErrNeedRerun.Error(),
	})
	return ErrNeedRerun
}

// writeProgress 写 reports/progress.md 进度报告。
func (m *Mixor) writeProgress(ws *workspace.Workspace, action, reason, queueContent string) error {
	if err := os.MkdirAll(ws.ReportsDir(), 0o755); err != nil {
		return fmt.Errorf("创建 reports 目录失败: %w", err)
	}
	var b strings.Builder
	b.WriteString("# Progress Report\n\n")
	b.WriteString(fmt.Sprintf("- 时间：%s\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- 动作：%s\n", action))
	b.WriteString(fmt.Sprintf("- 理由：%s\n", reason))
	b.WriteString("\n## 新需求\n")
	b.WriteString(strings.TrimRight(queueContent, "\n"))
	b.WriteString("\n")
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageMixor,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, b.String())
	if err := ws.WriteDoc(workspace.DocProgress, rendered); err != nil {
		return fmt.Errorf("写入 reports/progress.md 失败: %w", err)
	}
	return nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (m *Mixor) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, m.Name(), map[string]any{
		DataKeyStage:  workspace.StageMixor,
		DataKeyAgent:  m.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// readOptionalDoc 读取一份可选文档。文件不存在或读取失败均返回空字符串，
// 不视为错误（Mixor 容忍部分文档缺失）。
func readOptionalDoc(ws *workspace.Workspace, name string) string {
	content, err := ws.ReadDoc(name)
	if err != nil {
		return "（无）"
	}
	return content
}

// listCodeFiles 列出 code/ 目录下的文件名清单（每行一个）。
// 目录不存在时返回提示字符串。
func listCodeFiles(ws *workspace.Workspace) string {
	codeDir := filepath.Join(ws.Path(), "code")
	entries, err := os.ReadDir(codeDir)
	if err != nil {
		return "（无代码文件）"
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "（无代码文件）"
	}
	return strings.Join(names, "\n")
}
