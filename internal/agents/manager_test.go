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

// fakeTaskBody 是测试用的 task.md 正文，结构符合 schema.go 约定。
const fakeTaskBody = `# Tasks
- [ ] T1: 实现待办模型（验收点：存在待办结构体与持久化）
- [ ] T2: 实现添加待办接口（验收点：能添加待办并返回 id）
- [ ] T3: 实现标记完成接口（验收点：能标记待办为已完成）
`

// newManagerTestWorkspace 创建临时 workspace 并写入四份上游文档。
// skip 指定要跳过（不写入）的文档名，用于测试缺失场景；传空字符串表示全部写入。
func newManagerTestWorkspace(t *testing.T, skip string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "manager-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	docs := []struct {
		name    string
		content string
	}{
		{workspace.DocDesire, "# 用户需求\n做一个待办应用，支持添加与完成\n\n# 改进点\n- 错误处理\n"},
		{workspace.DocNeed, "# 需求清单\n- N1: 用户可添加待办\n- N2: 用户可标记完成\n"},
		{workspace.DocSpec, "# Todo Spec\n## Why\n管理日常事务\n\n## What Changes\n- 新增待办模型\n- 新增完成接口\n\n## Impact\n仅后端\n\n## ADDED Requirements\n### Requirement: 添加待办\n用户可添加一条待办\n#### Scenario\n- WHEN 用户提交标题 THEN 创建待办\n\n### Requirement: 标记完成\n用户可标记待办为完成\n#### Scenario\n- WHEN 用户点击完成 THEN 待办状态变为已完成\n"},
		{workspace.DocDeal, "# 完工协议\n交付待办增删改查\n\n## 验收标准\n- [ ] D1: 能添加待办\n- [ ] D2: 能标记完成\n"},
	}
	for _, d := range docs {
		if d.name == skip {
			continue
		}
		if err := w.WriteDoc(d.name, d.content); err != nil {
			t.Fatalf("写入 %s 失败: %v", d.name, err)
		}
	}
	return w
}

// TestManager_Name 验证 Name 返回 "manager"。
func TestManager_Name(t *testing.T) {
	m := NewManager()
	if got := m.Name(); got != "manager" {
		t.Errorf("Name() = %q, want %q", got, "manager")
	}
}

// TestManager_Run_Success 验证 Manager 正常流程：读取四份上游文档、调用 AI、
// 生成 task.md 含 "- [ ] T1" 格式且 frontmatter 正确，事件序列正确。
func TestManager_Run_Success(t *testing.T) {
	w := newManagerTestWorkspace(t, "")
	ai := &mockAI{resp: fakeTaskBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	m := NewManager()
	if err := m.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	events := drainEvents(ch)

	// 断言 AI 被调用，system prompt 正确，user 上下文包含四份文档
	if !ai.called {
		t.Fatal("期望 AI 被调用")
	}
	if ai.lastSystem != managerSystemPrompt {
		t.Errorf("system prompt 不匹配:\n got 长度 %d\nwant 长度 %d", len(ai.lastSystem), len(managerSystemPrompt))
	}
	for _, want := range []string{"# desire.md", "# need.md", "# spec.md", "# deal.md", "添加待办", "标记完成"} {
		if !strings.Contains(ai.lastUser, want) {
			t.Errorf("拼装上下文应包含 %q", want)
		}
	}

	// 断言 task.md 被写入，frontmatter 正确，正文含 - [ ] T1 格式
	raw, err := w.ReadDoc(workspace.DocTask)
	if err != nil {
		t.Fatalf("读取 task.md 失败: %v", err)
	}
	if !strings.Contains(raw, "- [ ] T1") {
		t.Errorf("task.md 应包含 '- [ ] T1' 格式:\n%s", raw)
	}
	if !strings.Contains(raw, "验收点") {
		t.Errorf("task.md 应包含验收点:\n%s", raw)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageManager {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageManager)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if body != fakeTaskBody {
		t.Errorf("task 正文不匹配:\ngot=%q\nwant=%q", body, fakeTaskBody)
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
		if events[i].Agent != "manager" {
			t.Errorf("第 %d 个事件 Agent = %q, want %q", i, events[i].Agent, "manager")
		}
	}
	// doc_update 应携带 doc=task.md
	docEvt := events[1]
	data, ok := docEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("doc_update 事件 Data 应为 map[string]any, got %T", docEvt.Data)
	}
	if data["doc"] != workspace.DocTask {
		t.Errorf("doc_update 事件 doc = %v, want %q", data["doc"], workspace.DocTask)
	}
}

// TestManager_Run_DocMissing 验证任一上游文档缺失时返回错误且不调用 AI。
func TestManager_Run_DocMissing(t *testing.T) {
	// 跳过 spec.md，制造缺失
	w := newManagerTestWorkspace(t, workspace.DocSpec)
	ai := &mockAI{resp: fakeTaskBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	m := NewManager()
	err := m.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望缺失文档返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "spec.md") {
		t.Errorf("错误应提示 spec.md 缺失: %v", err)
	}

	// AI 不应被调用
	if ai.called {
		t.Error("文档缺失时不应调用 AI")
	}
	// task.md 不应被写入
	if _, err := w.ReadDoc(workspace.DocTask); err == nil {
		t.Error("文档缺失时不应写入 task.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 task.md 不存在错误，got %v", err)
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

// TestManager_Run_AIError 验证 AI 调用失败时返回错误并发布 agent_failed。
func TestManager_Run_AIError(t *testing.T) {
	w := newManagerTestWorkspace(t, "")
	aiErr := errors.New("AI 服务不可用")
	ai := &mockAI{err: aiErr}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	m := NewManager()
	err := m.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望 AI 失败返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Errorf("错误应包含原始 AI 错误: %v", err)
	}

	// task.md 不应被写入
	if _, err := w.ReadDoc(workspace.DocTask); err == nil {
		t.Error("AI 失败时不应写入 task.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 task.md 不存在错误，got %v", err)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足：got %d, want 至少 2", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestManager_Run_NilBus 验证 bus 为 nil 时不 panic 且正常写入 task.md。
func TestManager_Run_NilBus(t *testing.T) {
	w := newManagerTestWorkspace(t, "")
	ai := &mockAI{resp: fakeTaskBody}
	git := &mockGittor{}

	m := NewManager()
	if err := m.Run(context.Background(), w, ai, git, nil); err != nil {
		t.Fatalf("bus=nil 时 Run 返回错误: %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocTask); err != nil {
		t.Errorf("bus=nil 时 task.md 仍应被写入: %v", err)
	}
}
