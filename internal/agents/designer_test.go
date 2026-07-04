package agents

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// fakeDealDraftBody 是测试用的 deal.md 草案正文，结构符合 schema.go 约定。
const fakeDealDraftBody = `# 完工协议
交付待办增删改查后端接口，范围限 auth 模块；不含前端 UI。

## 验收标准
- [ ] D1: 能添加待办并返回 id
- [ ] D2: 能标记待办为已完成
`

// fakeDealRevisedBody 是测试用的修订后 deal.md 正文（吸收批评后新增验收点）。
const fakeDealRevisedBody = `# 完工协议
交付待办增删改查后端接口，范围限 auth 模块；不含前端 UI。修订：补充边界验收。

## 验收标准
- [ ] D1: 能添加待办并返回 id
- [ ] D2: 能标记待办为已完成
- [ ] D3: 空标题提交返回 400 错误
`

// fakeReviewBody 是测试用的 deal_review.md 批评正文。
const fakeReviewBody = `# 完工协议评审
- D1/D2 缺少边界情况验收，建议补充空输入校验。
- 协议概述未说明是否含前端，需明确。`

// newDesignerTestWorkspace 创建临时 workspace。
//
// writeSpec 控制是否写入 spec.md（用于测试缺失场景）；
// writePrevDeal 控制是否写入上一轮 deal.md 草案；
// writeReview 控制是否写入 Evaluator 批评。
func newDesignerTestWorkspace(t *testing.T, writeSpec, writePrevDeal, writeReview bool) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "designer-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if writeSpec {
		if err := w.WriteDoc(workspace.DocSpec, fakeSpecBody); err != nil {
			t.Fatalf("写入 spec.md 失败: %v", err)
		}
	}
	if writePrevDeal {
		if err := w.WriteDoc(workspace.DocDeal, fakeDealDraftBody); err != nil {
			t.Fatalf("写入 deal.md 失败: %v", err)
		}
	}
	if writeReview {
		if err := w.WriteDoc(dealReviewDoc, fakeReviewBody); err != nil {
			t.Fatalf("写入 deal_review.md 失败: %v", err)
		}
	}
	return w
}

// TestDesigner_Name 验证 Name 返回 "designer"。
func TestDesigner_Name(t *testing.T) {
	d := NewDesigner("")
	if got := d.Name(); got != workspace.StageDesigner {
		t.Errorf("Name() = %q, want %q", got, workspace.StageDesigner)
	}
}

// TestDesigner_Run_FirstRound 验证第一轮起草：无 deal.md 与批评时，
// 依据 spec.md 起草 deal.md，上下文只含 spec，frontmatter Status=running，
// 事件序列正确。
func TestDesigner_Run_FirstRound(t *testing.T) {
	w := newDesignerTestWorkspace(t, true, false, false)
	ai := &mockAI{resp: fakeDealDraftBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	d := NewDesigner("")
	if err := d.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	events := filterLifecycleEvents(drainEvents(ch))

	// 断言 AI 被调用，system prompt 正确
	if !ai.called {
		t.Fatal("期望 AI 被调用")
	}
	if ai.lastSystem != designerSystemPrompt {
		t.Errorf("system prompt 不匹配：got 长度 %d, want 长度 %d", len(ai.lastSystem), len(designerSystemPrompt))
	}

	// 第一轮：上下文应只含 spec.md，不含上一轮 deal 与批评
	if !strings.Contains(ai.lastUser, "# spec.md") {
		t.Errorf("上下文应包含 spec.md 段：\n%s", ai.lastUser)
	}
	if strings.Contains(ai.lastUser, "上一轮 deal.md 草案") {
		t.Errorf("第一轮上下文不应包含上一轮 deal.md 草案：\n%s", ai.lastUser)
	}
	if strings.Contains(ai.lastUser, "Evaluator 批评") {
		t.Errorf("第一轮上下文不应包含 Evaluator 批评：\n%s", ai.lastUser)
	}
	if !strings.Contains(ai.lastUser, "短信验证码") {
		t.Errorf("上下文应包含 spec.md 全文内容：\n%s", ai.lastUser)
	}

	// 断言 deal.md 被写入，frontmatter 正确（Status=running 表示草案）
	raw, err := w.ReadDoc(workspace.DocDeal)
	if err != nil {
		t.Fatalf("读取 deal.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageDesigner {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageDesigner)
	}
	if meta.Status != workspace.StatusRunning {
		t.Errorf("frontmatter Status = %q, want %q（草案应为 running）", meta.Status, workspace.StatusRunning)
	}
	if body != fakeDealDraftBody {
		t.Errorf("deal 正文不匹配：\ngot=%q\nwant=%q", body, fakeDealDraftBody)
	}

	// 断言正文包含 deal 各关键片段
	for _, want := range []string{
		"# 完工协议",
		"## 验收标准",
		"- [ ] D1:",
		"- [ ] D2:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("deal 正文缺少关键片段 %q", want)
		}
	}

	// 断言事件序列：agent_start -> doc_update -> agent_done
	wantSeq := []string{
		eventbus.EventAgentStart,
		eventbus.EventDocUpdate,
		eventbus.EventAgentDone,
	}
	if len(events) < len(wantSeq) {
		t.Fatalf("事件数量不足：got %d, want 至少 %d", len(events), len(wantSeq))
	}
	for i, want := range wantSeq {
		if events[i].Type != want {
			t.Errorf("第 %d 个事件类型 = %q, want %q", i, events[i].Type, want)
		}
		if events[i].Agent != workspace.StageDesigner {
			t.Errorf("第 %d 个事件 Agent = %q, want %q", i, events[i].Agent, workspace.StageDesigner)
		}
	}
	// doc_update 应携带 doc=deal.md
	docEvt := events[1]
	data, ok := docEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("doc_update 事件 Data 应为 map[string]any, got %T", docEvt.Data)
	}
	if data["doc"] != workspace.DocDeal {
		t.Errorf("doc_update 事件 doc = %v, want %q", data["doc"], workspace.DocDeal)
	}
}

// TestDesigner_Run_RevisionWithReview 验证修订轮：存在上一轮 deal.md 与
// Evaluator 批评时，上下文含三段内容，AI 据此修订 deal.md。
func TestDesigner_Run_RevisionWithReview(t *testing.T) {
	w := newDesignerTestWorkspace(t, true, true, true)
	ai := &mockAI{resp: fakeDealRevisedBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	d := NewDesigner("")
	if err := d.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	drainEvents(ch) // 排空，避免事件遗留

	// 上下文应同时含 spec.md、上一轮 deal.md 草案、Evaluator 批评
	if !ai.called {
		t.Fatal("期望 AI 被调用")
	}
	for _, want := range []string{
		"# spec.md",
		"上一轮 deal.md 草案",
		"Evaluator 批评",
		"短信验证码",   // spec 内容
		"能添加待办",   // 上一轮 deal 内容
		"边界情况验收", // 批评内容
	} {
		if !strings.Contains(ai.lastUser, want) {
			t.Errorf("修订轮上下文应包含 %q：\n%s", want, ai.lastUser)
		}
	}

	// deal.md 应被改写为修订版，且新增 D3 验收点
	raw, err := w.ReadDoc(workspace.DocDeal)
	if err != nil {
		t.Fatalf("读取 deal.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Status != workspace.StatusRunning {
		t.Errorf("修订后草案 Status 应仍为 running, got %q", meta.Status)
	}
	if body != fakeDealRevisedBody {
		t.Errorf("deal 修订正文不匹配：\ngot=%q\nwant=%q", body, fakeDealRevisedBody)
	}
	if !strings.Contains(body, "- [ ] D3:") {
		t.Errorf("修订版应包含吸收批评后的 D3 验收点：\n%s", body)
	}
}

// TestDesigner_Run_SpecMissing 验证 spec.md 缺失时返回错误且不调用 AI。
func TestDesigner_Run_SpecMissing(t *testing.T) {
	// 不写 spec.md
	w := newDesignerTestWorkspace(t, false, false, false)
	ai := &mockAI{resp: fakeDealDraftBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	d := NewDesigner("")
	err := d.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望 spec.md 缺失返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "spec.md 不存在") {
		t.Errorf("错误信息应包含 'spec.md 不存在'，got %q", err.Error())
	}

	// AI 不应被调用
	if ai.called {
		t.Error("spec.md 缺失时不应调用 AI")
	}
	// deal.md 不应被写入
	if _, err := w.ReadDoc(workspace.DocDeal); err == nil {
		t.Error("spec.md 缺失时不应写入 deal.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 deal.md 不存在错误，got %v", err)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足：got %d, want 至少 2", len(events))
	}
	if events[0].Type != eventbus.EventAgentStart {
		t.Errorf("第 0 个事件应为 agent_start, got %q", events[0].Type)
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestDesigner_Run_AIError 验证 AI 调用失败时返回错误并发布 agent_failed。
func TestDesigner_Run_AIError(t *testing.T) {
	w := newDesignerTestWorkspace(t, true, false, false)
	aiErr := errors.New("AI 服务不可用")
	ai := &mockAI{err: aiErr}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	d := NewDesigner("")
	err := d.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望 AI 失败返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Errorf("错误应包含原始 AI 错误，got %q", err.Error())
	}

	// deal.md 不应被写入
	if _, err := w.ReadDoc(workspace.DocDeal); err == nil {
		t.Error("AI 失败时不应写入 deal.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 deal.md 不存在错误，got %v", err)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足：got %d, want 至少 2", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestDesigner_Run_NilBus 验证 bus 为 nil 时不 panic 且正常写入 deal.md。
func TestDesigner_Run_NilBus(t *testing.T) {
	w := newDesignerTestWorkspace(t, true, false, false)
	ai := &mockAI{resp: fakeDealDraftBody}
	git := &mockGittor{}

	d := NewDesigner("")
	if err := d.Run(context.Background(), w, ai, git, nil); err != nil {
		t.Fatalf("bus=nil 时 Run 返回错误: %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocDeal); err != nil {
		t.Errorf("bus=nil 时 deal.md 仍应被写入: %v", err)
	}
}

// TestDesigner_BuildContext 验证上下文拼装逻辑覆盖各文档组合。
func TestDesigner_BuildContext(t *testing.T) {
	d := NewDesigner("")

	// 仅 spec
	got := d.buildContext("SPEC", "", false, "", false)
	if !strings.Contains(got, "# spec.md") || !strings.Contains(got, "SPEC") {
		t.Errorf("仅 spec 模式拼装错误：%q", got)
	}
	if strings.Contains(got, "上一轮 deal.md 草案") || strings.Contains(got, "Evaluator 批评") {
		t.Errorf("仅 spec 模式不应含其他段：%q", got)
	}

	// spec + 上一轮 deal + 批评
	got = d.buildContext("SPEC", "DEAL", true, "REVIEW", true)
	for _, want := range []string{"# spec.md", "SPEC", "上一轮 deal.md 草案", "DEAL", "Evaluator 批评", "REVIEW"} {
		if !strings.Contains(got, want) {
			t.Errorf("完整模式应包含 %q：%q", want, got)
		}
	}

	// spec + 仅批评（异常但应容错）
	got = d.buildContext("SPEC", "", false, "REVIEW", true)
	if !strings.Contains(got, "Evaluator 批评") || strings.Contains(got, "上一轮 deal.md 草案") {
		t.Errorf("spec+review 模式拼装错误：%q", got)
	}
}
