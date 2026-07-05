package agents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// ── 共享 mock 与辅助（供本包所有 _test.go 复用）────────────────

// mockAI 实现 AIClient，按调用次序返回预设响应。
type mockAI struct {
	responses []string // 按调用次序返回；超出长度则返回最后一个
	errs      []error  // 与 responses 对齐的错误（非 nil 时优先返回）
	calls     int
	systems   []string
	users     []string
}

func (m *mockAI) Ask(ctx context.Context, system, user string) (string, error) {
	idx := m.calls
	m.calls++
	m.systems = append(m.systems, system)
	m.users = append(m.users, user)
	if idx < len(m.errs) && m.errs[idx] != nil {
		return "", m.errs[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	if len(m.responses) > 0 {
		return m.responses[len(m.responses)-1], nil
	}
	return "", fmt.Errorf("mockAI: 无预设响应（第 %d 次调用）", idx+1)
}

// AskWithModel 与 Ask 行为一致，model 参数仅作签名兼容。
func (m *mockAI) AskWithModel(ctx context.Context, _ string, system, user string) (string, error) {
	return m.Ask(ctx, system, user)
}

// RunStream 复用 Ask 的记录与响应逻辑，并通过 text 事件回传。
func (m *mockAI) RunStream(ctx context.Context, _ string, system, user string, onEvent func(aicli.RunEvent) error) (string, error) {
	resp, err := m.Ask(ctx, system, user)
	if err != nil {
		return "", err
	}
	if onEvent != nil {
		_ = onEvent(aicli.RunEvent{Type: "system", RunID: "mock"})
		_ = onEvent(aicli.RunEvent{Type: "text", Content: resp, RunID: "mock"})
		_ = onEvent(aicli.RunEvent{Type: "result", RunID: "mock"})
	}
	return "mock", nil
}

// GetRun 返回空 RunDetail。
func (m *mockAI) GetRun(_ context.Context, _ string) (*aicli.RunDetail, error) {
	return &aicli.RunDetail{}, nil
}

// mockGittor 实现 GittorClient，记录调用、不执行真实 git 操作。
type mockGittor struct {
	calls    int
	lastMsg  string
	lastPath []string
}

func (m *mockGittor) CommitAndPush(ctx context.Context, paths []string, message string) error {
	m.calls++
	m.lastPath = paths
	m.lastMsg = message
	return nil
}

// mockResolver 实现 ModelResolver，固定返回空串（默认模型）。
type mockResolver struct {
	calls  int
	stages []string
}

func (m *mockResolver) ModelFor(stage string) string {
	m.calls++
	m.stages = append(m.stages, stage)
	return ""
}

// drainEvents 同步排空订阅 channel，返回已收集的事件切片。
func drainEvents(ch <-chan eventbus.Event) []eventbus.Event {
	var events []eventbus.Event
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, e)
		default:
			return events
		}
	}
}

// filterLifecycleEvents 过滤掉 agent_run_event，仅保留 agent 生命周期事件。
func filterLifecycleEvents(events []eventbus.Event) []eventbus.Event {
	out := make([]eventbus.Event, 0, len(events))
	for _, e := range events {
		if e.Type == eventbus.EventAgentRunEvent {
			continue
		}
		out = append(out, e)
	}
	return out
}

// eventTypes 提取事件类型列表，便于失败时打印。
func eventTypes(events []eventbus.Event) []string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// ── Analyst 测试 ────────────────────────────────────────────────

// fakeAnalystSpec 是测试用的 spec.md 正文。
const fakeAnalystSpec = `# 测试 Spec

## Why
验证 Analyst 流程。

## What Changes
- 新增功能 A

## Impact
影响主模块。

## Requirements
### [ ] Requirement: 功能A
系统 SHALL 提供功能 A。
`

// newAnalystTestWorkspace 创建临时 workspace 并写入 input.md。
func newAnalystTestWorkspace(t *testing.T, input string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "analyst-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if input != "" {
		if err := w.WriteDoc(workspace.DocInput, input); err != nil {
			t.Fatalf("写入 input.md 失败: %v", err)
		}
	}
	return w
}

func TestAnalyst_Name(t *testing.T) {
	a := NewAnalyst(nil)
	if got, want := a.Name(), workspace.StageAnalyst; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

func TestAnalyst_Run_Success(t *testing.T) {
	w := newAnalystTestWorkspace(t, "做一个待办事项应用。")
	// AI 首轮即 satisfied，无问题，直接产出 spec
	ai := &mockAI{responses: []string{
		fmt.Sprintf(`{"questions":[], "spec": %q}`, fakeAnalystSpec),
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()
	resolver := &mockResolver{}

	a := NewAnalyst(nil)
	if err := a.Run(context.Background(), w, ai, git, bus, resolver); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// AI 应被调用 1 次
	if ai.calls != 1 {
		t.Fatalf("AI 调用次数 = %d, want 1", ai.calls)
	}
	if ai.systems[0] != analystSystemPrompt {
		t.Errorf("第 1 次 system 应为 analystSystemPrompt")
	}
	if !strings.Contains(ai.users[0], "做一个待办事项应用") {
		t.Errorf("第 1 次 user 应含 input.md 全文")
	}
	// resolver 应被调用一次取 analyst 模型
	if resolver.calls != 1 || len(resolver.stages) == 0 || resolver.stages[0] != workspace.StageAnalyst {
		t.Errorf("resolver 调用不符: calls=%d stages=%v", resolver.calls, resolver.stages)
	}

	// spec.md 应被写入，frontmatter 正确
	raw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageAnalyst {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageAnalyst)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if body != fakeAnalystSpec {
		t.Errorf("spec 正文不匹配:\ngot=%q\nwant=%q", body, fakeAnalystSpec)
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

func TestAnalyst_Run_InputMissing(t *testing.T) {
	w := newAnalystTestWorkspace(t, "") // 不写 input.md
	ai := &mockAI{responses: []string{`{"questions":[], "spec": "x"}`}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	a := NewAnalyst(nil)
	err := a.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil {
		t.Fatal("期望返回错误，got nil")
	}
	if !strings.Contains(err.Error(), "input.md") {
		t.Errorf("错误应提示 input.md: %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("input.md 缺失时不应调用 AI, got calls=%d", ai.calls)
	}
	// 应发布 agent_failed
	var hasFailed bool
	for _, e := range drainEvents(ch) {
		if e.Type == eventbus.EventAgentFailed {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Error("期望发布 agent_failed 事件")
	}
}

// TestAnalyst_Run_WithQuestions 验证提问循环：首轮有问题 → 调 ask → 次轮产出 spec。
func TestAnalyst_Run_WithQuestions(t *testing.T) {
	w := newAnalystTestWorkspace(t, "做一个待办应用。")
	ai := &mockAI{responses: []string{
		`{"questions":["目标用户是谁？"], "spec": ""}`,
		fmt.Sprintf(`{"questions":[], "spec": %q}`, fakeAnalystSpec),
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	var asked []string
	a := NewAnalyst(AskFunc(func(ctx context.Context, q string) (string, error) {
		asked = append(asked, q)
		return "个人用户", nil
	}))
	if err := a.Run(context.Background(), w, ai, git, bus, &mockResolver{}); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if len(asked) != 1 || asked[0] != "目标用户是谁？" {
		t.Errorf("应提问 1 次，got %v", asked)
	}
	if ai.calls != 2 {
		t.Errorf("AI 调用次数 = %d, want 2", ai.calls)
	}
	// 第 2 次上下文应含首轮问答
	if !strings.Contains(ai.users[1], "目标用户是谁？") || !strings.Contains(ai.users[1], "个人用户") {
		t.Errorf("第 2 次上下文应含首轮问答")
	}
	// spec.md 应已写入
	raw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	_, body := workspace.ParseDoc(raw)
	if body != fakeAnalystSpec {
		t.Errorf("spec 正文不匹配")
	}
}

// TestAnalyst_Run_AIError 验证 AI 失败时返回错误并发布 agent_failed。
func TestAnalyst_Run_AIError(t *testing.T) {
	w := newAnalystTestWorkspace(t, "做一个应用。")
	ai := &mockAI{errs: []error{errors.New("AI 服务不可用")}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	a := NewAnalyst(nil)
	err := a.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Fatalf("期望含 AI 错误，got %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocSpec); err == nil {
		t.Error("AI 失败时不应写入 spec.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 spec.md 不存在, got %v", err)
	}
}

func TestParseAnalystResponse(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantQ     []string
		wantSpec  string
	}{
		{
			name:     "有问题无 spec",
			raw:      `{"questions":["Q1"], "spec": ""}`,
			wantQ:    []string{"Q1"},
			wantSpec: "",
		},
		{
			name:     "无问题有 spec",
			raw:      `{"questions":[], "spec": "# Spec\n"}`,
			wantQ:    []string{},
			wantSpec: "# Spec\n",
		},
		{
			name:     "含多余文本与代码块",
			raw:      "分析：\n```json\n{\"questions\":[], \"spec\": \"# S\"}\n```\n",
			wantQ:    []string{},
			wantSpec: "# S",
		},
		{
			name:     "非 JSON 整段当 spec",
			raw:      "# 直接给出的 spec 正文",
			wantQ:    nil,
			wantSpec: "# 直接给出的 spec 正文",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotQ, gotSpec := parseAnalystResponse(c.raw)
			if len(gotQ) != len(c.wantQ) {
				t.Fatalf("questions 长度 = %d, want %d (got=%v)", len(gotQ), len(c.wantQ), gotQ)
			}
			if gotSpec != c.wantSpec {
				t.Errorf("spec = %q, want %q", gotSpec, c.wantSpec)
			}
		})
	}
}

// 确保 askPollInterval 在测试中不影响（避免 askViaBus 路径意外触发）
var _ = time.Second
