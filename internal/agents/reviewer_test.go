package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// reviewerSpecBody 是测试用的 spec.md 正文，含两个未打勾的 Requirement。
const reviewerSpecBody = "# 评估测试 Spec\n## Requirements\n### [ ] Requirement: 功能A\n描述A\n\n### [ ] Requirement: 功能B\n描述B\n"

// reviewerDealBody 是测试用的 deal.md 正文。
const reviewerDealBody = "# 完工协议\n## 验收标准\n- [ ] D1: 功能A 可用\n- [ ] D2: 功能B 可用\n"

// reviewerTaskBody 是测试用的 task.md 正文。
const reviewerTaskBody = "# Tasks\n- [ ] T1: 实现功能A\n- [ ] T2: 实现功能B\n"

// reviewerCoderReport 是测试用的 reports/coder.md 正文。
const reviewerCoderReport = "# Coder Report\n## 完成内容\n- code/main.go\n\n## 自评\n已实现功能A与功能B\n"

// newReviewerTestWorkspace 创建代码评估模式的临时 workspace：
// 写入 deal.md、spec.md、task.md、reports/coder.md、code/main.go。
func newReviewerTestWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "reviewer-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	writeTestDoc(t, w, workspace.DocSpec, workspace.StageAnalyst, reviewerSpecBody)
	writeTestDoc(t, w, workspace.DocDeal, workspace.StageArchitect, reviewerDealBody)
	writeTestDoc(t, w, workspace.DocTask, workspace.StagePlanner, reviewerTaskBody)
	// reports/coder.md
	if err := os.WriteFile(filepath.Join(w.ReportsDir(), "coder.md"), []byte(reviewerCoderReport), 0o644); err != nil {
		t.Fatalf("写入 coder report 失败: %v", err)
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

// writeTestDoc 写入一份带 frontmatter 的文档，失败即 t.Fatalf。
func writeTestDoc(t *testing.T, w *workspace.Workspace, name, stage, body string) {
	t.Helper()
	doc := workspace.RenderDoc(workspace.DocMeta{
		Stage: stage, Status: workspace.StatusDone, UpdatedAt: time.Now(),
	}, body)
	if err := w.WriteDoc(name, doc); err != nil {
		t.Fatalf("写入 %s 失败: %v", name, err)
	}
}

func TestReviewer_Name(t *testing.T) {
	r := NewReviewer()
	if got, want := r.Name(), workspace.StageReviewer; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

// TestReviewer_Run_Passed 验证 passed=true 时：写 reports/reviewer.md、
// 把 spec.md 中 ### [ ] Requirement 改为 ### [x] Requirement、返回 nil。
func TestReviewer_Run_Passed(t *testing.T) {
	w := newReviewerTestWorkspace(t)
	ai := &mockAI{responses: []string{`{"passed": true, "issues": [], "suggestions": ["可加注释"]}`}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()
	resolver := &mockResolver{}

	r := NewReviewer()
	if err := r.Run(context.Background(), w, ai, git, bus, resolver); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	if ai.calls != 1 {
		t.Fatalf("AI 调用次数 = %d, want 1", ai.calls)
	}
	if ai.systems[0] != reviewerSystemPrompt {
		t.Errorf("system 应为 reviewerSystemPrompt")
	}
	// user 应含 deal/spec/task/coder report + 代码摘要
	for _, want := range []string{"# deal.md", "# spec.md", "# task.md", "# Coder Report", "# 代码文件", "main.go"} {
		if !strings.Contains(ai.users[0], want) {
			t.Errorf("user 应含 %q", want)
		}
	}
	if resolver.calls != 1 || resolver.stages[0] != workspace.StageReviewer {
		t.Errorf("resolver 调用不符: %v", resolver.stages)
	}

	// reports/reviewer.md 应被写入
	raw, err := w.ReadDoc(workspace.DocReviewReport)
	if err != nil {
		t.Fatalf("读取 reports/reviewer.md 失败: %v", err)
	}
	_, body := workspace.ParseDoc(raw)
	if !strings.Contains(body, "通过") {
		t.Errorf("report 应标注通过:\n%s", body)
	}

	// spec.md 中所有 ### [ ] Requirement 应改为 ### [x] Requirement
	specRaw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	if strings.Contains(specRaw, SpecRequirementPrefix) {
		t.Errorf("spec.md 仍含未打勾的 Requirement:\n%s", specRaw)
	}
	if !strings.Contains(specRaw, SpecRequirementDonePrefix+"功能A") {
		t.Errorf("spec.md 应含打勾的功能A:\n%s", specRaw)
	}
	if !strings.Contains(specRaw, SpecRequirementDonePrefix+"功能B") {
		t.Errorf("spec.md 应含打勾的功能B:\n%s", specRaw)
	}

	// 事件序列：agent_start → doc_update(reviewer.md) → doc_update(spec.md) → agent_done
	events := filterLifecycleEvents(drainEvents(ch))
	wantSeq := []string{eventbus.EventAgentStart, eventbus.EventDocUpdate, eventbus.EventDocUpdate, eventbus.EventAgentDone}
	if len(events) != len(wantSeq) {
		t.Fatalf("事件数量 = %d, want %d (got=%v)", len(events), len(wantSeq), eventTypes(events))
	}
	for i, want := range wantSeq {
		if events[i].Type != want {
			t.Errorf("事件[%d] = %q, want %q", i, events[i].Type, want)
		}
	}
}

// TestReviewer_Run_Failed 验证 passed=false 时返回 ErrEvaluationFailed、
// 不打勾 spec、写 reports/reviewer.md 含问题清单。
func TestReviewer_Run_Failed(t *testing.T) {
	w := newReviewerTestWorkspace(t)
	ai := &mockAI{responses: []string{`{"passed": false, "issues": ["功能A 未实现", "缺少测试"], "suggestions": []}`}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	r := NewReviewer()
	err := r.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if !errors.Is(err, ErrEvaluationFailed) {
		t.Fatalf("期望 ErrEvaluationFailed，got %v", err)
	}

	// reports/reviewer.md 应被写入，含问题清单
	raw, err := w.ReadDoc(workspace.DocReviewReport)
	if err != nil {
		t.Fatalf("读取 reports/reviewer.md 失败: %v", err)
	}
	_, body := workspace.ParseDoc(raw)
	if !strings.Contains(body, "不通过") {
		t.Errorf("report 应标注不通过:\n%s", body)
	}
	if !strings.Contains(body, "功能A 未实现") || !strings.Contains(body, "缺少测试") {
		t.Errorf("report 应含问题清单:\n%s", body)
	}

	// spec.md 不应被打勾
	specRaw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	if !strings.Contains(specRaw, SpecRequirementPrefix+"功能A") {
		t.Errorf("不通过时 spec.md 应保留未打勾的 Requirement:\n%s", specRaw)
	}
	if strings.Contains(specRaw, SpecRequirementDonePrefix) {
		t.Errorf("不通过时 spec.md 不应含打勾的 Requirement:\n%s", specRaw)
	}

	// 应发布 agent_failed（哨兵），不应发布 agent_done
	var hasFailed, hasDone bool
	for _, e := range drainEvents(ch) {
		switch e.Type {
		case eventbus.EventAgentFailed:
			hasFailed = true
		case eventbus.EventAgentDone:
			hasDone = true
		}
	}
	if !hasFailed {
		t.Error("期望发布 agent_failed")
	}
	if hasDone {
		t.Error("不通过时不应发布 agent_done")
	}
}

func TestReviewer_Run_CoderReportMissing(t *testing.T) {
	w := newReviewerTestWorkspace(t)
	// 删除 reports/coder.md
	if err := os.Remove(filepath.Join(w.ReportsDir(), "coder.md")); err != nil {
		t.Fatalf("删除 coder.md 失败: %v", err)
	}
	ai := &mockAI{responses: []string{`{"passed": true, "issues": [], "suggestions": []}`}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	r := NewReviewer()
	err := r.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "coder.md") {
		t.Fatalf("期望 coder.md 缺失错误，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("coder.md 缺失时不应调用 AI")
	}
}
