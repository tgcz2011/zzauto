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

// fakeArchitectDeal 是测试用的 deal.md 正文，含批判性分析、验收标准、风险点。
const fakeArchitectDeal = `# 完工协议
交付待办事项应用的增删改查能力。

## 批判性分析
spec 未明确数据持久化方式，建议本地存储。

## 验收标准
- [ ] D1: 可创建待办项
- [ ] D2: 可删除待办项

## 风险点与缓解
- 并发写入：使用单例存储
`

// newArchitectTestWorkspace 创建临时 workspace 并写入 spec.md。
func newArchitectTestWorkspace(t *testing.T, spec string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "architect-test")
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
	return w
}

func TestArchitect_Name(t *testing.T) {
	a := NewArchitect()
	if got, want := a.Name(), workspace.StageArchitect; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

func TestArchitect_Run_Success(t *testing.T) {
	w := newArchitectTestWorkspace(t, "# 待办 Spec\n## Requirements\n### [ ] Requirement: 增删改查\n")
	ai := &mockAI{responses: []string{fakeArchitectDeal}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()
	resolver := &mockResolver{}

	a := NewArchitect()
	if err := a.Run(context.Background(), w, ai, git, bus, resolver); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	if ai.calls != 1 {
		t.Fatalf("AI 调用次数 = %d, want 1", ai.calls)
	}
	if ai.systems[0] != architectSystemPrompt {
		t.Errorf("system 应为 architectSystemPrompt")
	}
	if !strings.Contains(ai.users[0], "待办 Spec") {
		t.Errorf("user 应为 spec.md 全文")
	}
	if resolver.calls != 1 || resolver.stages[0] != workspace.StageArchitect {
		t.Errorf("resolver 调用不符: %v", resolver.stages)
	}

	// deal.md 应被写入，frontmatter 正确
	raw, err := w.ReadDoc(workspace.DocDeal)
	if err != nil {
		t.Fatalf("读取 deal.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageArchitect {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageArchitect)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if body != fakeArchitectDeal {
		t.Errorf("deal 正文不匹配:\ngot=%q\nwant=%q", body, fakeArchitectDeal)
	}
	// 正文应含批判性分析、验收标准、风险点
	for _, want := range []string{"## 批判性分析", "## 验收标准", "- [ ] D1", "## 风险点与缓解"} {
		if !strings.Contains(body, want) {
			t.Errorf("deal 正文缺少 %q", want)
		}
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

func TestArchitect_Run_SpecMissing(t *testing.T) {
	w := newArchitectTestWorkspace(t, "") // 不写 spec.md
	ai := &mockAI{responses: []string{fakeArchitectDeal}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	a := NewArchitect()
	err := a.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "spec.md") {
		t.Fatalf("期望 spec.md 错误，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("spec.md 缺失时不应调用 AI")
	}
	if _, err := w.ReadDoc(workspace.DocDeal); err == nil {
		t.Error("不应写入 deal.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 deal.md 不存在, got %v", err)
	}
	// 应发布 agent_failed
	var hasFailed bool
	for _, e := range drainEvents(ch) {
		if e.Type == eventbus.EventAgentFailed {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Error("期望发布 agent_failed")
	}
}

func TestArchitect_Run_AIError(t *testing.T) {
	w := newArchitectTestWorkspace(t, "# Spec\n")
	ai := &mockAI{errs: []error{errors.New("AI 不可用")}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	a := NewArchitect()
	err := a.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "AI 不可用") {
		t.Fatalf("期望含 AI 错误，got %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocDeal); err == nil {
		t.Error("AI 失败时不应写入 deal.md")
	}
}
