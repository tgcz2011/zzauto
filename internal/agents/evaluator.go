package agents

import (
	"context"
	"encoding/json"
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

// Evaluator 是评估阶段的 agent。编排器在讨论循环与评估循环都调用
// StageEvaluator 这一个 agent，故 Evaluator 需根据 workspace 状态智能
// 判定当前模式：
//   - 讨论模式（Designer↔Evaluator 循环）：读 spec.md + deal.md，批判性
//     评估完工协议。共识则 deal.md status=done 返回 nil；未共识写
//     deal_review.md（批评）返回 ErrNoConsensus。
//   - 代码评估模式（Generator→Evaluator 循环）：读 deal.md + spec.md +
//     task.md + Generator 的 report + code/，评估代码是否合格。合格则
//     spec.md 所有 Requirement 打勾 + report 移到 evaluated/ 返回 nil；
//     不合格返回 ErrEvaluationFailed + 把问题写入 report 的评审段。
//
// 模式判定：reports/generator.md 存在且 reports/evaluated/generator.md
// 不存在 → 代码评估模式；否则 → 讨论模式。
type Evaluator struct {
	model string // 该角色配置的模型，空则用 aicli 默认
}

// NewEvaluator 构造一个 Evaluator 实例。model 为该角色配置的模型名，空串表示用默认。
func NewEvaluator(model string) *Evaluator { return &Evaluator{model: model} }

// Name 返回 agent 标识，与 workspace 阶段常量 StageEvaluator 对应。
func (e *Evaluator) Name() string { return "evaluator" }

// discussEvalPrompt 是讨论模式下 Evaluator 调用 AI 时使用的系统提示。
//
// 要求 AI 扮演挑剔的 Evaluator，批判性评估 deal.md 是否覆盖 spec 所有
// 需求、验收点是否可客观判定、是否有遗漏边界，输出 JSON。
const discussEvalPrompt = `你是一名极其挑剔的软件评估专家（Evaluator），正与 Designer 进行完工协议讨论。

你的职责：阅读 spec.md（项目规格）与 deal.md（完工协议），带着批判性思维
评估 deal.md 是否充分覆盖 spec 的所有 Requirement、每个验收点是否可被客观
判定、是否存在遗漏的边界情况或风险。

输出必须严格为 JSON（不要包裹代码块、不要输出任何额外说明文字），格式：
{"consensus": true/false, "critique": "批评点清单，多条用换行分隔"}

判定规则：
1. 只有当 deal.md 覆盖 spec 所有 Requirement、每个验收点都可客观判定、
   无明显遗漏或歧义时，consensus 才为 true。
2. consensus 为 false 时，critique 必须逐条列出具体问题（覆盖不足、验收点
   不可客观判定、遗漏的边界情况等）。
3. consensus 为 true 时，critique 为空字符串。`

// codeEvalPrompt 是代码评估模式下 Evaluator 调用 AI 时使用的系统提示。
//
// 要求 AI 扮演 Evaluator，严格对照 deal.md 验收点评估 Generator 的代码
// 与 report 是否合格，输出 JSON。
const codeEvalPrompt = `你是一名严格的代码评估专家（Evaluator），正在评估 Generator 的产出。

你的职责：阅读 deal.md（验收标准）、spec.md（需求）、task.md（任务）、
Generator 的 report 与代码文件，严格对照 deal.md 的验收点评估代码是否合格。

输出必须严格为 JSON（不要包裹代码块、不要输出任何额外说明文字），格式：
{"pass": true/false, "issues": ["问题1", "问题2"]}

判定规则：
1. 只有当所有 deal.md 验收点都被代码满足、report 自评属实、无重大缺陷时，
   pass 才为 true。
2. pass 为 false 时，issues 必须逐条列出具体问题（每条一个字符串）。
3. pass 为 true 时，issues 为空数组。`

// Run 执行 Evaluator 的一次完整工作流：
//  1. 发布 agent_start
//  2. 模式判定（依据 reports/generator.md 与 reports/evaluated/generator.md 是否存在）
//  3. 讨论模式或代码评估模式分别处理
//  4. 出错（非哨兵）发布 agent_failed 并返回错误
func (e *Evaluator) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, e.Name(), map[string]any{
		DataKeyStage: workspace.StageEvaluator,
		DataKeyAgent: e.Name(),
	})

	// 2. 模式判定
	genReportPath := filepath.Join(ws.ReportsDir(), "generator.md")
	evaluatedPath := filepath.Join(ws.ReportsDir(), "evaluated", "generator.md")
	_, genStatErr := os.Stat(genReportPath)
	_, evalStatErr := os.Stat(evaluatedPath)
	// generator.md 存在且 evaluated/generator.md 不存在 → 代码评估模式
	codeEvalMode := genStatErr == nil && errors.Is(evalStatErr, fs.ErrNotExist)

	if codeEvalMode {
		return e.runCodeEvaluation(ctx, ws, ai, bus, genReportPath)
	}
	return e.runDiscussion(ctx, ws, ai, bus)
}

// runDiscussion 执行讨论模式：评估 Designer 产出的 deal.md 完工协议。
func (e *Evaluator) runDiscussion(ctx context.Context, ws *workspace.Workspace, ai AIClient, bus *eventbus.Bus) error {
	// 读 spec.md + deal.md。deal.md 缺失返回错误（Designer 应先起草）
	specContent, err := ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("读取 spec.md 失败: %w", err))
	}
	dealContent, err := ws.ReadDoc(workspace.DocDeal)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return e.failWith(bus, fmt.Errorf("deal.md 不存在，Designer 应先起草完工协议"))
		}
		return e.failWith(bus, fmt.Errorf("读取 deal.md 失败: %w", err))
	}

	// 拼装上下文：spec.md + deal.md
	combined := fmt.Sprintf("# spec.md（项目规格）\n%s\n\n# deal.md（完工协议）\n%s", specContent, dealContent)

	// 调用 AI 批判性评估
	resp, _, err := RunWithTracking(ctx, ws, bus, ai, e.Name(), e.model, discussEvalPrompt, combined)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("调用 AI 评估 deal 失败: %w", err))
	}

	// 解析 JSON（容错：提取 {...}）
	var result struct {
		Consensus bool   `json:"consensus"`
		Critique  string `json:"critique"`
	}
	if err := parseJSONResponse(resp, &result); err != nil {
		return e.failWith(bus, fmt.Errorf("解析 Evaluator 讨论结果失败: %w", err))
	}

	if result.Consensus {
		// 共识：把 deal.md 的 frontmatter status 改为 done
		meta, body := workspace.ParseDoc(dealContent)
		meta.Stage = workspace.StageEvaluator
		meta.Status = workspace.StatusDone
		meta.UpdatedAt = time.Now()
		rendered := workspace.RenderDoc(meta, body)
		if err := ws.WriteDoc(workspace.DocDeal, rendered); err != nil {
			return e.failWith(bus, fmt.Errorf("写入 deal.md 失败: %w", err))
		}
		publishEvent(bus, eventbus.EventDocUpdate, e.Name(), map[string]any{
			DataKeyStage: workspace.StageEvaluator,
			DataKeyAgent: e.Name(),
			"doc":        workspace.DocDeal,
		})
		return nil
	}

	// 未共识：把 critique 写入 deal_review.md
	reviewMeta := workspace.DocMeta{
		Stage:     workspace.StageEvaluator,
		Status:    workspace.StatusRunning,
		UpdatedAt: time.Now(),
	}
	reviewDoc := workspace.RenderDoc(reviewMeta, result.Critique)
	if err := ws.WriteDoc("deal_review.md", reviewDoc); err != nil {
		return e.failWith(bus, fmt.Errorf("写入 deal_review.md 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, e.Name(), map[string]any{
		DataKeyStage: workspace.StageEvaluator,
		DataKeyAgent: e.Name(),
		"doc":        "deal_review.md",
	})
	return ErrNoConsensus
}

// runCodeEvaluation 执行代码评估模式：评估 Generator 产出的代码与 report。
func (e *Evaluator) runCodeEvaluation(ctx context.Context, ws *workspace.Workspace, ai AIClient, bus *eventbus.Bus, reportPath string) error {
	// 读 deal.md + spec.md + task.md + report + 代码文件
	dealContent, err := ws.ReadDoc(workspace.DocDeal)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("读取 deal.md 失败: %w", err))
	}
	specContent, err := ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("读取 spec.md 失败: %w", err))
	}
	taskContent, err := ws.ReadDoc(workspace.DocTask)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("读取 task.md 失败: %w", err))
	}
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("读取 generator report 失败: %w", err))
	}
	reportContent := string(reportBytes)

	// 遍历 code/ 目录读代码文件
	codeSummary, err := e.readCodeFiles(ws)
	if err != nil {
		return e.failWith(bus, err)
	}

	// 拼装上下文：deal 验收点 + spec requirements + task 任务 + report + 代码摘要
	combined := fmt.Sprintf(
		"# deal.md（验收标准）\n%s\n\n# spec.md（需求）\n%s\n\n# task.md（任务）\n%s\n\n# Generator Report\n%s\n\n# 代码文件\n%s",
		dealContent, specContent, taskContent, reportContent, codeSummary,
	)

	// 调用 AI 评估
	resp, _, err := RunWithTracking(ctx, ws, bus, ai, e.Name(), e.model, codeEvalPrompt, combined)
	if err != nil {
		return e.failWith(bus, fmt.Errorf("调用 AI 评估代码失败: %w", err))
	}

	// 解析 JSON（容错：提取 {...}）
	var result struct {
		Pass   bool     `json:"pass"`
		Issues []string `json:"issues"`
	}
	if err := parseJSONResponse(resp, &result); err != nil {
		return e.failWith(bus, fmt.Errorf("解析 Evaluator 代码评估结果失败: %w", err))
	}

	if result.Pass {
		// 合格：在 spec.md 把所有 ### Requirement: 改为 ### [x] Requirement:
		updatedSpec := strings.Replace(specContent, SpecRequirementPrefix, SpecRequirementDonePrefix, -1)
		if err := ws.WriteDoc(workspace.DocSpec, updatedSpec); err != nil {
			return e.failWith(bus, fmt.Errorf("写入 spec.md 失败: %w", err))
		}
		// 把 report 移到 reports/evaluated/
		evaluatedDir := filepath.Join(ws.ReportsDir(), "evaluated")
		if err := os.MkdirAll(evaluatedDir, 0o755); err != nil {
			return e.failWith(bus, fmt.Errorf("创建 evaluated 目录失败: %w", err))
		}
		if err := os.Rename(reportPath, filepath.Join(evaluatedDir, "generator.md")); err != nil {
			return e.failWith(bus, fmt.Errorf("移动 report 到 evaluated 失败: %w", err))
		}
		publishEvent(bus, eventbus.EventDocUpdate, e.Name(), map[string]any{
			DataKeyStage: workspace.StageEvaluator,
			DataKeyAgent: e.Name(),
			"doc":        workspace.DocSpec,
		})
		publishEvent(bus, eventbus.EventAgentDone, e.Name(), map[string]any{
			DataKeyStage: workspace.StageEvaluator,
			DataKeyAgent: e.Name(),
		})
		return nil
	}

	// 不合格：把 issues 追加到 report（## 评审问题 段 + issues 列表）
	var sb strings.Builder
	sb.Write(reportBytes)
	sb.WriteString("\n\n## 评审问题\n")
	for _, issue := range result.Issues {
		sb.WriteString(fmt.Sprintf("- %s\n", issue))
	}
	if err := os.WriteFile(reportPath, []byte(sb.String()), 0o644); err != nil {
		return e.failWith(bus, fmt.Errorf("写回 report 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, e.Name(), map[string]any{
		DataKeyStage: workspace.StageEvaluator,
		DataKeyAgent: e.Name(),
		"doc":        "reports/generator.md",
	})
	return ErrEvaluationFailed
}

// readCodeFiles 遍历 workspace 下 code/ 目录，读取所有代码文件并拼装为摘要。
// code/ 目录不存在时返回空摘要（代码可能尚未生成），不视为错误。
func (e *Evaluator) readCodeFiles(ws *workspace.Workspace) (string, error) {
	codeDir := filepath.Join(ws.Path(), "code")
	entries, err := os.ReadDir(codeDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "（无代码文件）", nil
		}
		return "", fmt.Errorf("读取 code 目录失败: %w", err)
	}
	var sb strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(codeDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("读取代码文件 %s 失败: %w", entry.Name(), err)
		}
		sb.WriteString(fmt.Sprintf("## 文件: %s\n```\n%s\n```\n\n", entry.Name(), string(data)))
	}
	if sb.Len() == 0 {
		return "（无代码文件）", nil
	}
	return sb.String(), nil
}

// failWith 发布 agent_failed 事件并返回原错误，供非哨兵错误统一处理。
func (e *Evaluator) failWith(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, e.Name(), map[string]any{
		DataKeyStage:  workspace.StageEvaluator,
		DataKeyAgent:  e.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// parseJSONResponse 从 AI 回答中提取首个 JSON 对象并解析到 v。
// 容错处理：AI 可能将 JSON 包裹在代码块或附加说明文字中，此处截取第一个
// '{' 到最后一个 '}' 之间的子串再解析。
func parseJSONResponse(resp string, v any) error {
	s := extractJSON(resp)
	if s == "" {
		return fmt.Errorf("响应中未找到 JSON 对象: %s", resp)
	}
	return json.Unmarshal([]byte(s), v)
}

// extractJSON 截取字符串中第一个 '{' 到最后一个 '}' 之间的子串。
// 找不到时返回空字符串。
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return ""
	}
	end := strings.LastIndex(s, "}")
	if end == -1 || end <= start {
		return ""
	}
	return s[start : end+1]
}
