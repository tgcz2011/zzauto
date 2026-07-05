// Package agents 中 Reviewer Agent 的实现。
//
// Reviewer（旧 Evaluator 代码评估模式拆出）：只评代码，不评协议。
// 读取 deal.md + spec.md + task.md + reports/coder.md + code/ 目录，
// 调用 AI 检查代码是否满足所有 Requirement 与验收标准，输出 JSON。
// 通过则把 spec.md 的 ### [ ] Requirement 改为 ### [x]；不通过返回
// ErrEvaluationFailed（orchestrator 会回到 Coder 重试）。
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

// reviewerSystemPrompt 是 Reviewer 调用 AI 时使用的系统提示词。
const reviewerSystemPrompt = `你是代码审查者。检查代码是否满足 spec 的所有 Requirement 和 deal 的验收标准。输出 JSON: {"passed": true/false, "issues": ["问题1", ...], "suggestions": ["建议1", ...]}。

判定规则：
1. 只有当所有 deal.md 验收点都被代码满足、coder report 自评属实、无重大缺陷时，passed 才为 true。
2. passed 为 false 时，issues 必须逐条列出具体问题（每条一个字符串）。
3. passed 为 true 时，issues 为空数组；suggestions 可列出改进建议（可为空）。
4. 严格输出 JSON，不要包裹代码块、不要输出任何额外说明文字。`

// reviewerResult 是 Reviewer 解析 AI JSON 输出的结构。
type reviewerResult struct {
	Passed      bool     `json:"passed"`
	Issues      []string `json:"issues"`
	Suggestions []string `json:"suggestions"`
}

// Reviewer 代码审查者：评估 Coder 产出的代码是否合格。
type Reviewer struct{}

// NewReviewer 构造一个 Reviewer 实例。
func NewReviewer() *Reviewer { return &Reviewer{} }

// Name 返回 agent 标识，与 workspace.StageReviewer 对应。
func (r *Reviewer) Name() string { return workspace.StageReviewer }

// Run 执行 Reviewer 的一次完整工作流：
//  1. 发布 agent_start
//  2. 读取 deal.md + spec.md + task.md + reports/coder.md + code/ 目录文件
//  3. 调用 AI 评估，输出 JSON
//  4. 解析 JSON
//  5. 写 reports/reviewer.md
//  6. passed=false → 返回 ErrEvaluationFailed（orchestrator 回到 Coder 重试）
//  7. passed=true → 把 spec.md 中 ### [ ] Requirement 改为 ### [x] Requirement
//  8. 发布 doc_update 与 agent_done；失败发布 agent_failed
func (r *Reviewer) Run(ctx context.Context, ws *workspace.Workspace, ai AIClient, git GittorClient, bus *eventbus.Bus, resolver ModelResolver) error {
	// 1. 发布 agent_start
	publishEvent(bus, eventbus.EventAgentStart, r.Name(), map[string]any{
		DataKeyStage: workspace.StageReviewer,
		DataKeyAgent: r.Name(),
	})

	// 2. 读取上游文档
	dealContent, err := r.readDoc(ws, bus, workspace.DocDeal)
	if err != nil {
		return err
	}
	specContent, err := r.readDoc(ws, bus, workspace.DocSpec)
	if err != nil {
		return err
	}
	taskContent, err := r.readDoc(ws, bus, workspace.DocTask)
	if err != nil {
		return err
	}
	coderReport, err := ws.ReadDoc(workspace.DocCoderReport)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return r.fail(bus, fmt.Errorf("reports/coder.md 不存在，Coder 应先产出代码"))
		}
		return r.fail(bus, fmt.Errorf("读取 reports/coder.md 失败: %w", err))
	}

	// 扫描 code/ 目录读代码文件
	codeSummary, err := readCodeFiles(ws)
	if err != nil {
		return r.fail(bus, err)
	}

	// 3. 拼装上下文，调用 AI 评估
	combined := fmt.Sprintf(
		"# deal.md（验收标准）\n%s\n\n# spec.md（需求）\n%s\n\n# task.md（任务）\n%s\n\n# Coder Report\n%s\n\n# 代码文件\n%s",
		dealContent, specContent, taskContent, coderReport, codeSummary,
	)
	model := ResolveModel(resolver, workspace.StageReviewer)
	resp, _, err := RunWithTracking(ctx, ws, bus, ai, r.Name(), model, reviewerSystemPrompt, combined)
	if err != nil {
		return r.fail(bus, fmt.Errorf("调用 AI 评估代码失败: %w", err))
	}

	// 4. 解析 JSON
	var result reviewerResult
	if err := parseJSONResponse(resp, &result); err != nil {
		return r.fail(bus, fmt.Errorf("解析 Reviewer 评估结果失败: %w", err))
	}

	// 5. 写 reports/reviewer.md
	reviewBody := buildReviewerReportBody(result)
	rendered := workspace.RenderDoc(workspace.DocMeta{
		Stage:     workspace.StageReviewer,
		Status:    workspace.StatusDone,
		UpdatedAt: time.Now(),
	}, reviewBody)
	if err := ws.WriteDoc(workspace.DocReviewReport, rendered); err != nil {
		return r.fail(bus, fmt.Errorf("写入 reports/reviewer.md 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, r.Name(), map[string]any{
		DataKeyStage: workspace.StageReviewer,
		DataKeyAgent: r.Name(),
		"doc":        workspace.DocReviewReport,
	})

	// 6. 不通过：返回 ErrEvaluationFailed
	if !result.Passed {
		publishEvent(bus, eventbus.EventAgentFailed, r.Name(), map[string]any{
			DataKeyStage:  workspace.StageReviewer,
			DataKeyAgent:  r.Name(),
			DataKeyReason: ErrEvaluationFailed.Error(),
		})
		return ErrEvaluationFailed
	}

	// 7. 通过：把 spec.md 中 ### [ ] Requirement 改为 ### [x] Requirement
	updatedSpec := strings.ReplaceAll(specContent, SpecRequirementPrefix, SpecRequirementDonePrefix)
	if err := ws.WriteDoc(workspace.DocSpec, updatedSpec); err != nil {
		return r.fail(bus, fmt.Errorf("写入 spec.md 失败: %w", err))
	}
	publishEvent(bus, eventbus.EventDocUpdate, r.Name(), map[string]any{
		DataKeyStage: workspace.StageReviewer,
		DataKeyAgent: r.Name(),
		"doc":        workspace.DocSpec,
	})

	// 8. 发布 agent_done
	publishEvent(bus, eventbus.EventAgentDone, r.Name(), map[string]any{
		DataKeyStage: workspace.StageReviewer,
		DataKeyAgent: r.Name(),
	})
	return nil
}

// readDoc 读取一份上游文档。文件不存在或读取失败时发布 agent_failed 并返回错误。
func (r *Reviewer) readDoc(ws *workspace.Workspace, bus *eventbus.Bus, name string) (string, error) {
	content, err := ws.ReadDoc(name)
	if err != nil {
		var failErr error
		if errors.Is(err, fs.ErrNotExist) {
			failErr = fmt.Errorf("%s 不存在", name)
		} else {
			failErr = fmt.Errorf("读取 %s 失败: %w", name, err)
		}
		return "", r.fail(bus, failErr)
	}
	return content, nil
}

// fail 发布 agent_failed 事件并返回错误，统一错误出口。
func (r *Reviewer) fail(bus *eventbus.Bus, err error) error {
	publishEvent(bus, eventbus.EventAgentFailed, r.Name(), map[string]any{
		DataKeyStage:  workspace.StageReviewer,
		DataKeyAgent:  r.Name(),
		DataKeyReason: err.Error(),
	})
	return err
}

// readCodeFiles 遍历 workspace 下 code/ 目录，读取所有代码文件并拼装为摘要。
// code/ 目录不存在时返回空摘要（代码可能尚未生成），不视为错误。
func readCodeFiles(ws *workspace.Workspace) (string, error) {
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

// buildReviewerReportBody 根据评估结果构造 reports/reviewer.md 正文。
func buildReviewerReportBody(result reviewerResult) string {
	var b strings.Builder
	b.WriteString("# Reviewer Report\n")
	if result.Passed {
		b.WriteString("评估结果：**通过**\n\n")
	} else {
		b.WriteString("评估结果：**不通过**\n\n")
	}
	b.WriteString("## 问题\n")
	if len(result.Issues) == 0 {
		b.WriteString("（无）\n")
	} else {
		for _, issue := range result.Issues {
			b.WriteString(fmt.Sprintf("- %s\n", issue))
		}
	}
	b.WriteString("\n## 建议\n")
	if len(result.Suggestions) == 0 {
		b.WriteString("（无）\n")
	} else {
		for _, s := range result.Suggestions {
			b.WriteString(fmt.Sprintf("- %s\n", s))
		}
	}
	return b.String()
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
