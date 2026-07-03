package agents

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// evaluatorSpecBody 是测试用的 spec.md 正文，含两个未打勾的 Requirement。
const evaluatorSpecBody = "# 评估测试 Spec\n## ADDED Requirements\n### Requirement: 功能A\n描述A\n#### Scenario\n- WHEN 触发 THEN 结果\n\n### Requirement: 功能B\n描述B\n#### Scenario\n- WHEN 触发 THEN 结果\n"

// evaluatorDealBody 是测试用的 deal.md 正文，含两个验收点。
const evaluatorDealBody = "# 完工协议\n交付功能A与功能B\n\n## 验收标准\n- [ ] D1: 功能A 可用\n- [ ] D2: 功能B 可用\n"

// evaluatorTaskBody 是测试用的 task.md 正文。
const evaluatorTaskBody = "# Tasks\n- [ ] T1: 实现功能A（验收点：功能A 可用）\n- [ ] T2: 实现功能B（验收点：功能B 可用）\n"

// evaluatorReportBody 是测试用的 Generator report 正文。
const evaluatorReportBody = "# Generator A Report\n## 完成内容\n- 实现功能A\n- 实现功能B\n\n## 自评\n已按 deal.md 验收点完成\n"

// writeEvalDoc 写入一份带 frontmatter 的文档，失败即 t.Fatalf。
func writeEvalDoc(t *testing.T, w *workspace.Workspace, name, stage, status, body string) {
	t.Helper()
	doc := workspace.RenderDoc(workspace.DocMeta{
		Stage:     stage,
		Status:    status,
		UpdatedAt: time.Now(),
	}, body)
	if err := w.WriteDoc(name, doc); err != nil {
		t.Fatalf("写入 %s 失败: %v", name, err)
	}
}

// newEvaluatorDiscussionWorkspace 创建讨论模式的临时 workspace：
// 写入 spec.md 与（可选）deal.md，不写 reports/generator.md。
func newEvaluatorDiscussionWorkspace(t *testing.T, withDeal bool) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "eval-discuss")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	writeEvalDoc(t, w, workspace.DocSpec, workspace.StagePlanner, workspace.StatusDone, evaluatorSpecBody)
	if withDeal {
		writeEvalDoc(t, w, workspace.DocDeal, workspace.StageDesigner, workspace.StatusRunning, evaluatorDealBody)
	}
	return w
}

// newEvaluatorCodeWorkspace 创建代码评估模式的临时 workspace：
// 写入 deal.md、spec.md、task.md、reports/generator.md、code/main.go。
// 不创建 reports/evaluated/generator.md。
func newEvaluatorCodeWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "eval-code")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	writeEvalDoc(t, w, workspace.DocSpec, workspace.StagePlanner, workspace.StatusDone, evaluatorSpecBody)
	writeEvalDoc(t, w, workspace.DocDeal, workspace.StageDesigner, workspace.StatusDone, evaluatorDealBody)
	writeEvalDoc(t, w, workspace.DocTask, workspace.StageManager, workspace.StatusDone, evaluatorTaskBody)
	// reports/generator.md
	if err := os.WriteFile(filepath.Join(w.ReportsDir(), "generator.md"), []byte(evaluatorReportBody), 0o644); err != nil {
		t.Fatalf("写入 report 失败: %v", err)
	}
	// code/main.go
	codeDir := filepath.Join(w.Path(), "code")
	if err := os.MkdirAll(codeDir, 0o755); err != nil {
		t.Fatalf("创建 code 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("写入 code/main.go 失败: %v", err)
	}
	return w
}

// TestEvaluator_Name 验证 Name 返回 "evaluator"。
func TestEvaluator_Name(t *testing.T) {
	e := NewEvaluator()
	if got := e.Name(); got != "evaluator" {
		t.Errorf("Name() = %q, want %q", got, "evaluator")
	}
}

// TestEvaluator_Discussion_Consensus 验证讨论模式达成共识：
// deal.md status 置 done、发布 doc_update(deal.md)、不创建 deal_review.md、返回 nil。
func TestEvaluator_Discussion_Consensus(t *testing.T) {
	w := newEvaluatorDiscussionWorkspace(t, true)
	ai := &mockAI{resp: `{"consensus": true, "critique": ""}`}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	if err := e.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("共识时应返回 nil, got %v", err)
	}

	// AI 被调用，system prompt 正确，user 上下文含 spec + deal
	if !ai.called {
		t.Fatal("期望 AI 被调用")
	}
	if ai.lastSystem != discussEvalPrompt {
		t.Errorf("讨论模式 system prompt 不匹配")
	}
	if !strings.Contains(ai.lastUser, "# spec.md") || !strings.Contains(ai.lastUser, "# deal.md") {
		t.Errorf("讨论上下文应包含 spec.md 与 deal.md:\n%s", ai.lastUser)
	}

	// deal.md status 应为 done，stage 应为 evaluator，body 保留
	raw, err := w.ReadDoc(workspace.DocDeal)
	if err != nil {
		t.Fatalf("读取 deal.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Status != workspace.StatusDone {
		t.Errorf("deal.md status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if meta.Stage != workspace.StageEvaluator {
		t.Errorf("deal.md stage = %q, want %q", meta.Stage, workspace.StageEvaluator)
	}
	if !strings.Contains(body, "# 完工协议") {
		t.Errorf("deal.md body 应被保留: %s", body)
	}

	// deal_review.md 不应被创建
	if _, err := w.ReadDoc("deal_review.md"); err == nil {
		t.Error("共识时不应创建 deal_review.md")
	}

	// 事件：应发布 agent_start + doc_update(deal.md)，不应有 agent_failed
	events := drainEvents(ch)
	foundDealUpdate := false
	for _, evt := range events {
		if evt.Type == eventbus.EventAgentFailed {
			t.Error("共识时不应发布 agent_failed")
		}
		if evt.Type == eventbus.EventDocUpdate {
			data, _ := evt.Data.(map[string]any)
			if data["doc"] == workspace.DocDeal {
				foundDealUpdate = true
			}
		}
	}
	if !foundDealUpdate {
		t.Error("应发布 deal.md 的 doc_update")
	}
}

// TestEvaluator_Discussion_NoConsensus 验证讨论模式未共识：
// 写 deal_review.md（含 critique）、返回 ErrNoConsensus、deal.md 不置 done。
func TestEvaluator_Discussion_NoConsensus(t *testing.T) {
	w := newEvaluatorDiscussionWorkspace(t, true)
	ai := &mockAI{resp: `{"consensus": false, "critique": "验收点 D1 不可客观判定\n缺少边界情况处理"}`}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	err := e.Run(context.Background(), w, ai, git, bus)
	if !errors.Is(err, ErrNoConsensus) {
		t.Fatalf("未共识应返回 ErrNoConsensus, got %v", err)
	}

	// deal_review.md 应被创建，含 critique 与正确 frontmatter
	raw, err := w.ReadDoc("deal_review.md")
	if err != nil {
		t.Fatalf("读取 deal_review.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageEvaluator {
		t.Errorf("deal_review.md stage = %q, want %q", meta.Stage, workspace.StageEvaluator)
	}
	if meta.Status != workspace.StatusRunning {
		t.Errorf("deal_review.md status = %q, want %q", meta.Status, workspace.StatusRunning)
	}
	if !strings.Contains(body, "验收点 D1 不可客观判定") {
		t.Errorf("deal_review.md 应包含 critique: %s", body)
	}
	if !strings.Contains(body, "缺少边界情况处理") {
		t.Errorf("deal_review.md 应包含第二条 critique: %s", body)
	}

	// deal.md status 不应被改为 done
	dealRaw, _ := w.ReadDoc(workspace.DocDeal)
	dealMeta, _ := workspace.ParseDoc(dealRaw)
	if dealMeta.Status == workspace.StatusDone {
		t.Error("未共识时 deal.md status 不应为 done")
	}

	// 事件：应发布 doc_update(deal_review.md)，不应有 agent_failed
	events := drainEvents(ch)
	foundReviewUpdate := false
	for _, evt := range events {
		if evt.Type == eventbus.EventAgentFailed {
			t.Error("未共识时不应发布 agent_failed")
		}
		if evt.Type == eventbus.EventDocUpdate {
			data, _ := evt.Data.(map[string]any)
			if data["doc"] == "deal_review.md" {
				foundReviewUpdate = true
			}
		}
	}
	if !foundReviewUpdate {
		t.Error("应发布 deal_review.md 的 doc_update")
	}
}

// TestEvaluator_Discussion_DealMissing 验证 deal.md 缺失时返回错误、
// 不调用 AI、发布 agent_failed。
func TestEvaluator_Discussion_DealMissing(t *testing.T) {
	w := newEvaluatorDiscussionWorkspace(t, false) // 不写 deal.md
	ai := &mockAI{resp: `{"consensus": true, "critique": ""}`}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	err := e.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("deal.md 缺失应返回错误")
	}
	if !strings.Contains(err.Error(), "deal.md") {
		t.Errorf("错误应提及 deal.md: %v", err)
	}
	if ai.called {
		t.Error("deal.md 缺失时不应调用 AI")
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件不足: %d", len(events))
	}
	if events[0].Type != eventbus.EventAgentStart {
		t.Errorf("第 0 个事件应为 agent_start, got %q", events[0].Type)
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestEvaluator_CodeEval_Pass 验证代码评估合格：
// spec.md 所有 Requirement 打勾、report 移到 evaluated/、发布 agent_done、返回 nil。
func TestEvaluator_CodeEval_Pass(t *testing.T) {
	w := newEvaluatorCodeWorkspace(t)
	ai := &mockAI{resp: `{"pass": true, "issues": []}`}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	if err := e.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("合格应返回 nil, got %v", err)
	}

	// AI 被调用，system prompt 正确，user 上下文含 deal/spec/task/report/code
	if !ai.called {
		t.Fatal("期望 AI 被调用")
	}
	if ai.lastSystem != codeEvalPrompt {
		t.Errorf("代码评估模式 system prompt 不匹配")
	}
	for _, want := range []string{"# deal.md", "# spec.md", "# task.md", "# Generator Report", "# 代码文件", "main.go"} {
		if !strings.Contains(ai.lastUser, want) {
			t.Errorf("代码评估上下文应包含 %q", want)
		}
	}

	// spec.md 所有 Requirement 应被打勾
	raw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	if strings.Contains(raw, SpecRequirementPrefix) {
		t.Errorf("spec.md 仍含未打勾的 Requirement:\n%s", raw)
	}
	doneCount := strings.Count(raw, SpecRequirementDonePrefix)
	if doneCount != 2 {
		t.Errorf("打勾 Requirement 数 = %d, want 2", doneCount)
	}

	// 原 report 应被移走
	if _, err := os.Stat(filepath.Join(w.ReportsDir(), "generator.md")); err == nil {
		t.Error("原 report 应已被移走")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望原 report 不存在, got %v", err)
	}
	// evaluated/generator.md 应存在且内容不变
	evalData, err := os.ReadFile(filepath.Join(w.ReportsDir(), "evaluated", "generator.md"))
	if err != nil {
		t.Fatalf("evaluated/generator.md 应存在: %v", err)
	}
	if string(evalData) != evaluatorReportBody {
		t.Errorf("移动后的 report 内容应不变:\ngot=%q\nwant=%q", string(evalData), evaluatorReportBody)
	}

	// 事件：应发布 doc_update(spec.md) + agent_done，不应有 agent_failed
	events := drainEvents(ch)
	foundSpecUpdate := false
	foundDone := false
	for _, evt := range events {
		if evt.Type == eventbus.EventAgentFailed {
			t.Error("合格时不应发布 agent_failed")
		}
		if evt.Type == eventbus.EventDocUpdate {
			data, _ := evt.Data.(map[string]any)
			if data["doc"] == workspace.DocSpec {
				foundSpecUpdate = true
			}
		}
		if evt.Type == eventbus.EventAgentDone {
			foundDone = true
		}
	}
	if !foundSpecUpdate {
		t.Error("应发布 spec.md 的 doc_update")
	}
	if !foundDone {
		t.Error("合格应发布 agent_done")
	}
}

// TestEvaluator_CodeEval_Fail 验证代码评估不合格：
// 把 issues 追加到 report、spec.md 不打勾、report 不移动、返回 ErrEvaluationFailed。
func TestEvaluator_CodeEval_Fail(t *testing.T) {
	w := newEvaluatorCodeWorkspace(t)
	ai := &mockAI{resp: `{"pass": false, "issues": ["功能A 未实现", "缺少单元测试"]}`}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	err := e.Run(context.Background(), w, ai, git, bus)
	if !errors.Is(err, ErrEvaluationFailed) {
		t.Fatalf("不合格应返回 ErrEvaluationFailed, got %v", err)
	}

	// report 应被追加评审问题段，保留原内容
	data, err := os.ReadFile(filepath.Join(w.ReportsDir(), "generator.md"))
	if err != nil {
		t.Fatalf("读取 report 失败: %v", err)
	}
	report := string(data)
	if !strings.Contains(report, evaluatorReportBody) {
		t.Errorf("report 应保留原内容: %s", report)
	}
	if !strings.Contains(report, "## 评审问题") {
		t.Errorf("report 应包含评审问题段: %s", report)
	}
	if !strings.Contains(report, "- 功能A 未实现") {
		t.Errorf("report 应包含问题1: %s", report)
	}
	if !strings.Contains(report, "- 缺少单元测试") {
		t.Errorf("report 应包含问题2: %s", report)
	}

	// spec.md 不应被打勾
	specRaw, _ := w.ReadDoc(workspace.DocSpec)
	if strings.Contains(specRaw, SpecRequirementDonePrefix) {
		t.Error("不合格时 spec.md 不应被打勾")
	}
	// report 不应被移走，evaluated/ 不应有 generator.md
	if _, err := os.Stat(filepath.Join(w.ReportsDir(), "evaluated", "generator.md")); err == nil {
		t.Error("不合格时 report 不应被移到 evaluated/")
	}

	// 事件：应发布 doc_update，不应有 agent_failed / agent_done
	events := drainEvents(ch)
	foundUpdate := false
	for _, evt := range events {
		if evt.Type == eventbus.EventAgentFailed {
			t.Error("不合格时不应发布 agent_failed")
		}
		if evt.Type == eventbus.EventAgentDone {
			t.Error("不合格时不应发布 agent_done")
		}
		if evt.Type == eventbus.EventDocUpdate {
			foundUpdate = true
		}
	}
	if !foundUpdate {
		t.Error("应发布 doc_update")
	}
}

// TestEvaluator_ModeDetermination 验证三种模式判定场景下选用的 system prompt。
func TestEvaluator_ModeDetermination(t *testing.T) {
	// 场景1：无 reports/generator.md → 讨论模式
	w1 := newEvaluatorDiscussionWorkspace(t, true)
	ai1 := &mockAI{resp: `{"consensus": true, "critique": ""}`}
	e := NewEvaluator()
	if err := e.Run(context.Background(), w1, ai1, &mockGittor{}, nil); err != nil {
		t.Fatalf("场景1 运行失败: %v", err)
	}
	if ai1.lastSystem != discussEvalPrompt {
		t.Errorf("无 report 时应使用讨论 prompt, got 长度 %d", len(ai1.lastSystem))
	}

	// 场景2：有 generator.md 且无 evaluated/generator.md → 代码评估模式
	w2 := newEvaluatorCodeWorkspace(t)
	ai2 := &mockAI{resp: `{"pass": true, "issues": []}`}
	e2 := NewEvaluator()
	if err := e2.Run(context.Background(), w2, ai2, &mockGittor{}, nil); err != nil {
		t.Fatalf("场景2 运行失败: %v", err)
	}
	if ai2.lastSystem != codeEvalPrompt {
		t.Errorf("有 report 未评估时应使用代码评估 prompt, got 长度 %d", len(ai2.lastSystem))
	}

	// 场景3：有 generator.md 且有 evaluated/generator.md → 回退讨论模式
	w3 := newEvaluatorCodeWorkspace(t)
	evaluatedDir := filepath.Join(w3.ReportsDir(), "evaluated")
	if err := os.MkdirAll(evaluatedDir, 0o755); err != nil {
		t.Fatalf("创建 evaluated 目录失败: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(w3.ReportsDir(), "generator.md"))
	if err != nil {
		t.Fatalf("读取 report 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evaluatedDir, "generator.md"), src, 0o644); err != nil {
		t.Fatalf("写入 evaluated/generator.md 失败: %v", err)
	}
	ai3 := &mockAI{resp: `{"consensus": true, "critique": ""}`}
	e3 := NewEvaluator()
	if err := e3.Run(context.Background(), w3, ai3, &mockGittor{}, nil); err != nil {
		t.Fatalf("场景3 运行失败: %v", err)
	}
	if ai3.lastSystem != discussEvalPrompt {
		t.Errorf("已评估时应回退讨论模式, got 长度 %d", len(ai3.lastSystem))
	}
}

// TestEvaluator_JSONFaultTolerance 验证 AI 返回被代码块包裹的 JSON 仍能正确解析。
func TestEvaluator_JSONFaultTolerance(t *testing.T) {
	w := newEvaluatorDiscussionWorkspace(t, true)
	// AI 返回被 ```json 代码块包裹的 JSON
	ai := &mockAI{resp: "好的，评估如下：\n```json\n{\"consensus\": true, \"critique\": \"\"}\n```\n"}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	e := NewEvaluator()
	if err := e.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("包裹的 JSON 应能解析, got %v", err)
	}
	// deal.md 应被标记 done，证明 consensus 被正确解析为 true
	raw, _ := w.ReadDoc(workspace.DocDeal)
	meta, _ := workspace.ParseDoc(raw)
	if meta.Status != workspace.StatusDone {
		t.Errorf("包裹 JSON 解析后 deal.md status = %q, want done", meta.Status)
	}
}

// TestEvaluator_Discussion_AIError 验证讨论模式 AI 调用失败时返回错误并发布 agent_failed。
func TestEvaluator_Discussion_AIError(t *testing.T) {
	w := newEvaluatorDiscussionWorkspace(t, true)
	ai := &mockAI{err: errors.New("AI 服务不可用")}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	err := e.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望 AI 失败返回错误")
	}
	if !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Errorf("错误应包含原始 AI 错误: %v", err)
	}
	// deal.md 不应被改写为 done
	dealRaw, _ := w.ReadDoc(workspace.DocDeal)
	dealMeta, _ := workspace.ParseDoc(dealRaw)
	if dealMeta.Status == workspace.StatusDone {
		t.Error("AI 失败时 deal.md 不应被置 done")
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件不足: %d", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestEvaluator_CodeEval_BadJSON 验证代码评估模式 AI 返回非法 JSON 时返回错误。
func TestEvaluator_CodeEval_BadJSON(t *testing.T) {
	w := newEvaluatorCodeWorkspace(t)
	ai := &mockAI{resp: "这不是 JSON"}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewEvaluator()
	err := e.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
	// 非法 JSON 应是普通错误，不应是哨兵（哨兵仅由合格/不合格语义触发）
	if errors.Is(err, ErrEvaluationFailed) {
		t.Errorf("非法 JSON 不应返回 ErrEvaluationFailed（应是普通错误）: %v", err)
	}
	if errors.Is(err, ErrNoConsensus) {
		t.Errorf("非法 JSON 不应返回 ErrNoConsensus（应是普通错误）: %v", err)
	}
	// spec.md 不应被打勾
	specRaw, _ := w.ReadDoc(workspace.DocSpec)
	if strings.Contains(specRaw, SpecRequirementDonePrefix) {
		t.Error("非法 JSON 时 spec.md 不应被打勾")
	}
	// report 应留在原位
	if _, statErr := os.Stat(filepath.Join(w.ReportsDir(), "generator.md")); statErr != nil {
		t.Errorf("非法 JSON 时 report 应留在原位: %v", statErr)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件不足: %d", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestEvaluator_NilBus 验证 bus 为 nil 时不 panic。
func TestEvaluator_NilBus(t *testing.T) {
	w := newEvaluatorDiscussionWorkspace(t, true)
	ai := &mockAI{resp: `{"consensus": true, "critique": ""}`}
	git := &mockGittor{}

	e := NewEvaluator()
	if err := e.Run(context.Background(), w, ai, git, nil); err != nil {
		t.Fatalf("bus=nil 时 Run 返回错误: %v", err)
	}
	// deal.md 仍应被标记 done
	raw, _ := w.ReadDoc(workspace.DocDeal)
	meta, _ := workspace.ParseDoc(raw)
	if meta.Status != workspace.StatusDone {
		t.Errorf("bus=nil 时 deal.md 仍应被置 done, got %q", meta.Status)
	}
}

// TestExtractJSON 验证 extractJSON 对各类输入的容错截取。
func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"纯JSON", `{"a":1}`, `{"a":1}`},
		{"代码块包裹", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"带前后说明", "结果如下：\n{\"a\":1}\n以上。", `{"a":1}`},
		{"无JSON", "没有花括号", ""},
		{"只有左括号", "{不完整", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractJSON(c.in)
			if got != c.want {
				t.Errorf("extractJSON(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
