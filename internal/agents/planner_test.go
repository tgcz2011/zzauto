package agents

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// fakePlannerTask 是测试用的 task.md 正文。
const fakePlannerTask = `# Tasks
- [ ] T1: 实现待办项数据结构（引用 D1）
- [ ] T2: 实现创建接口（引用 D1）
- [ ] T3: 实现删除接口（引用 D2）
`

// newPlannerTestWorkspace 创建临时 workspace 并写入 spec.md + deal.md。
func newPlannerTestWorkspace(t *testing.T, spec, deal string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "planner-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if spec != "" {
		doc := workspace.RenderDoc(workspace.DocMeta{
			Stage: workspace.StageAnalyst, Status: workspace.StatusDone, UpdatedAt: time.Now(),
		}, spec)
		if err := w.WriteDoc(workspace.DocSpec, doc); err != nil {
			t.Fatalf("写入 spec.md 失败: %v", err)
		}
	}
	if deal != "" {
		doc := workspace.RenderDoc(workspace.DocMeta{
			Stage: workspace.StageArchitect, Status: workspace.StatusDone, UpdatedAt: time.Now(),
		}, deal)
		if err := w.WriteDoc(workspace.DocDeal, doc); err != nil {
			t.Fatalf("写入 deal.md 失败: %v", err)
		}
	}
	return w
}

func TestPlanner_Name(t *testing.T) {
	p := NewPlanner()
	if got, want := p.Name(), workspace.StagePlanner; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

func TestPlanner_Run_Success(t *testing.T) {
	w := newPlannerTestWorkspace(t, "# Spec\n## Requirements\n### [ ] Requirement: 增删改查\n", "# 完工协议\n## 验收标准\n- [ ] D1: 创建\n- [ ] D2: 删除\n")
	ai := &mockAI{responses: []string{fakePlannerTask}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()
	resolver := &mockResolver{}

	p := NewPlanner()
	if err := p.Run(context.Background(), w, ai, git, bus, resolver); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	if ai.calls != 1 {
		t.Fatalf("AI 调用次数 = %d, want 1", ai.calls)
	}
	if ai.systems[0] != plannerSystemPrompt {
		t.Errorf("system 应为 plannerSystemPrompt")
	}
	// user 应同时含 spec 与 deal 全文
	if !strings.Contains(ai.users[0], "# Spec") || !strings.Contains(ai.users[0], "# 完工协议") {
		t.Errorf("user 应拼装 spec + deal 全文")
	}
	if !strings.Contains(ai.users[0], "D1") {
		t.Errorf("user 应含 deal 验收点")
	}
	if resolver.calls != 1 || resolver.stages[0] != workspace.StagePlanner {
		t.Errorf("resolver 调用不符: %v", resolver.stages)
	}

	// task.md 应被写入，frontmatter 正确
	raw, err := w.ReadDoc(workspace.DocTask)
	if err != nil {
		t.Fatalf("读取 task.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StagePlanner {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StagePlanner)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if body != fakePlannerTask {
		t.Errorf("task 正文不匹配:\ngot=%q\nwant=%q", body, fakePlannerTask)
	}

	// 事件序列：agent_start → doc_update → agent_done
	events := filterLifecycleEvents(drainEvents(ch))
	wantSeq := []string{eventbus.EventAgentStart, eventbus.EventDocUpdate, eventbus.EventAgentDone}
	if len(events) != len(wantSeq) {
		t.Fatalf("事件数量 = %d, want %d (got=%v)", len(events), len(wantSeq), eventTypes(events))
	}
	for i, want := range wantSeq {
		if events[i].Type != want {
			t.Errorf("事件[%d] = %q, want %q", i, events[i].Type, want)
		}
	}
}

func TestPlanner_Run_SpecMissing(t *testing.T) {
	// 不写 spec.md
	w := newPlannerTestWorkspace(t, "", "# deal\n## 验收标准\n- [ ] D1: x\n")
	ai := &mockAI{responses: []string{fakePlannerTask}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	p := NewPlanner()
	err := p.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "spec.md") {
		t.Fatalf("期望 spec.md 错误，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("spec.md 缺失时不应调用 AI")
	}
}

func TestPlanner_Run_DealMissing(t *testing.T) {
	// 写 spec 不写 deal
	w := newPlannerTestWorkspace(t, "# Spec\n", "")
	ai := &mockAI{responses: []string{fakePlannerTask}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	p := NewPlanner()
	err := p.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "deal.md") {
		t.Fatalf("期望 deal.md 错误，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("deal.md 缺失时不应调用 AI")
	}
	if _, err := w.ReadDoc(workspace.DocTask); err == nil {
		t.Error("不应写入 task.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 task.md 不存在, got %v", err)
	}
}

func TestPlanner_Run_AIError(t *testing.T) {
	w := newPlannerTestWorkspace(t, "# Spec\n", "# Deal\n")
	ai := &mockAI{errs: []error{errors.New("AI 不可用")}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	p := NewPlanner()
	err := p.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "AI 不可用") {
		t.Fatalf("期望含 AI 错误，got %v", err)
	}
}
