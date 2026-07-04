package agents

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mockAI 实现 AIClient，按预设逻辑返回回答，便于断言。
type mockAI struct {
	// resp 为 Ask 返回的固定回答；若 err 非 nil 则直接返回该错误。
	resp string
	err  error
	// 记录最近一次调用参数，供断言。
	lastSystem string
	lastUser   string
	called     bool
}

func (m *mockAI) Ask(ctx context.Context, system, user string) (string, error) {
	m.called = true
	m.lastSystem = system
	m.lastUser = user
	if m.err != nil {
		return "", m.err
	}
	return m.resp, nil
}

// AskWithModel 与 Ask 行为一致，model 参数仅作签名兼容。
func (m *mockAI) AskWithModel(ctx context.Context, _, system, user string) (string, error) {
	return m.Ask(ctx, system, user)
}

// RunStream 模拟 SSE 流：记录调用参数后通过 text 事件返回预设响应。
// 行为与 Ask 等价，便于 RunWithTracking 走流式路径时复用同一断言。
func (m *mockAI) RunStream(ctx context.Context, _, system, user string, onEvent func(aicli.RunEvent) error) (string, error) {
	// 复用 Ask 的记录与错误处理逻辑
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

// GetRun 返回空 RunDetail，本组测试不依赖该方法。
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

// fakeSpecBody 是测试用的 spec.md 正文，结构符合 schema.go 约定。
const fakeSpecBody = `# 登录重构 Spec

## Why
当前登录流程存在安全隐患，需引入多因素认证以提升账户安全。

## What Changes
- 新增短信验证码二次校验
- 重构登录接口返回结构

## Impact
影响 auth 模块、用户表 schema 及移动端登录页；旧客户端需灰度兼容。

## ADDED Requirements
### Requirement: 短信验证码
该需求 SHALL 在用户输入正确密码后发送 6 位验证码至绑定手机。
#### Scenario
- WHEN 密码校验通过 THEN 发送验证码并返回 pending 状态
- WHEN 验证码错误 THEN 返回 401 且 60 秒内不可重试

### Requirement: 接口返回结构
该需求 SHALL 登录接口统一返回 token 与过期时间字段。
#### Scenario
- WHEN 登录成功 THEN 返回 {token, expires_in}
`

// newTestWorkspace 创建临时 workspace 并写入 need.md。
func newTestWorkspace(t *testing.T, needContent string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "planner-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if needContent != "" {
		if err := w.WriteDoc(workspace.DocNeed, needContent); err != nil {
			t.Fatalf("写入 need.md 失败: %v", err)
		}
	}
	return w
}

// drainEvents 同步排空订阅 channel，返回已收集的事件切片。
//
// 由于 eventbus.Bus 的订阅 channel 缓冲为 256 且 Publish 非阻塞投递，
// 在 Run 返回后所有已发布事件均已落入 channel，可直接同步读取。
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

// filterLifecycleEvents 过滤掉 agent_run_event，仅保留 agent 生命周期事件
// （agent_start / doc_update / agent_done / agent_failed）。
// 各 agent 改造为 RunWithTracking 后会发布 agent_run_event 事件，
// 但多数测试只断言生命周期事件序列，故在此统一过滤。
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

func TestPlanner_Name(t *testing.T) {
	p := NewPlanner("")
	if got := p.Name(); got != "planner" {
		t.Errorf("Name() = %q, want %q", got, "planner")
	}
}

func TestPlanner_Run_Success(t *testing.T) {
	need := "# 需求清单\n- N1: 用户希望登录时支持短信验证码\n- N2: 登录接口返回 token 与过期时间\n"
	w := newTestWorkspace(t, need)
	ai := &mockAI{resp: fakeSpecBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	p := NewPlanner("")
	if err := p.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	events := filterLifecycleEvents(drainEvents(ch))

	// 断言 AI 被调用，且参数正确
	if !ai.called {
		t.Fatal("期望 AI 被调用")
	}
	if ai.lastSystem != plannerSystemPrompt {
		t.Errorf("system prompt 不匹配：got 长度 %d, want 长度 %d", len(ai.lastSystem), len(plannerSystemPrompt))
	}
	if ai.lastUser != need {
		t.Errorf("user 参数应为 need.md 全文：\ngot=%q\nwant=%q", ai.lastUser, need)
	}

	// 断言 spec.md 被写入，且 frontmatter 正确
	raw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StagePlanner {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StagePlanner)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if body != fakeSpecBody {
		t.Errorf("spec 正文不匹配：\ngot=%q\nwant=%q", body, fakeSpecBody)
	}

	// 断言正文包含 spec 各关键段落
	for _, want := range []string{
		"# ", " Spec",
		"## Why",
		"## What Changes",
		"## Impact",
		"## ADDED Requirements",
		"### Requirement: ",
		"#### Scenario",
		"WHEN", "THEN",
		"SHALL",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("spec 正文缺少关键片段 %q", want)
		}
	}

	// 断言事件序列：agent_start -> doc_update -> agent_done
	// 注：agent_run_event 已被 filterLifecycleEvents 过滤（见上）。
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
		if events[i].Agent != "planner" {
			t.Errorf("第 %d 个事件 Agent = %q, want %q", i, events[i].Agent, "planner")
		}
	}
	// doc_update 应携带 doc=spec.md
	docEvt := events[1]
	data, ok := docEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("doc_update 事件 Data 应为 map[string]any, got %T", docEvt.Data)
	}
	if data["doc"] != workspace.DocSpec {
		t.Errorf("doc_update 事件 doc = %v, want %q", data["doc"], workspace.DocSpec)
	}
}

func TestPlanner_Run_NeedMissing(t *testing.T) {
	// 不写 need.md
	w := newTestWorkspace(t, "")
	ai := &mockAI{resp: fakeSpecBody}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	p := NewPlanner("")
	err := p.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望返回错误，got nil")
	}
	if !strings.Contains(err.Error(), "need.md 不存在") {
		t.Errorf("错误信息应包含 'need.md 不存在'，got %q", err.Error())
	}

	// AI 不应被调用
	if ai.called {
		t.Error("need.md 缺失时不应调用 AI")
	}
	// spec.md 不应被写入
	if _, err := w.ReadDoc(workspace.DocSpec); err == nil {
		t.Error("need.md 缺失时不应写入 spec.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 spec.md 不存在错误，got %v", err)
	}

	events := drainEvents(ch)
	// 应发布 agent_start 与 agent_failed
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

func TestPlanner_Run_AIError(t *testing.T) {
	need := "# 需求清单\n- N1: 测试需求\n"
	w := newTestWorkspace(t, need)
	aiErr := errors.New("AI 服务不可用")
	ai := &mockAI{err: aiErr}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	p := NewPlanner("")
	err := p.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望返回错误，got nil")
	}
	if !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Errorf("错误应包含原始 AI 错误，got %q", err.Error())
	}

	// spec.md 不应被写入
	if _, err := w.ReadDoc(workspace.DocSpec); err == nil {
		t.Error("AI 失败时不应写入 spec.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 spec.md 不存在错误，got %v", err)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足：got %d, want 至少 2", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

func TestPlanner_Run_NilBus(t *testing.T) {
	// bus 为 nil 时不应 panic
	need := "# 需求清单\n- N1: 测试\n"
	w := newTestWorkspace(t, need)
	ai := &mockAI{resp: fakeSpecBody}
	git := &mockGittor{}

	p := NewPlanner("")
	if err := p.Run(context.Background(), w, ai, git, nil); err != nil {
		t.Fatalf("bus=nil 时 Run 返回错误: %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocSpec); err != nil {
		t.Errorf("bus=nil 时 spec.md 仍应被写入: %v", err)
	}
}
