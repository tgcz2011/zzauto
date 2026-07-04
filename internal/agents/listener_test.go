package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mockListenerAI 模拟 AIClient，返回预设的 desire.md 正文。
type mockListenerAI struct {
	resp string
	err  error
}

func (m *mockListenerAI) Ask(ctx context.Context, system, user string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.resp, nil
}

// AskWithModel 与 Ask 行为一致。
func (m *mockListenerAI) AskWithModel(ctx context.Context, _, system, user string) (string, error) {
	return m.Ask(ctx, system, user)
}

// RunStream 复用 Ask 的响应并通过 text 事件回传。
func (m *mockListenerAI) RunStream(ctx context.Context, _, system, user string, onEvent func(aicli.RunEvent) error) (string, error) {
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
func (m *mockListenerAI) GetRun(_ context.Context, _ string) (*aicli.RunDetail, error) {
	return &aicli.RunDetail{}, nil
}

// fakeDesireBody 模拟 AI 产出的 desire.md 正文，符合 schema 约定。
const fakeDesireBody = `# 用户需求
做一个待办事项应用，支持增删改查。

# 改进点
- 输入校验与 XSS 转义
- 空列表与超长标题的边界处理
- 本地持久化与离线可用
`

// drain 非阻塞地抽干 channel 中已缓冲的事件并返回。
func drain(ch <-chan eventbus.Event) []eventbus.Event {
	var evts []eventbus.Event
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return evts
			}
			evts = append(evts, e)
		default:
			return evts
		}
	}
}

func TestListenerName(t *testing.T) {
	l := NewListener("")
	if got, want := l.Name(), "listener"; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

func TestListenerRunSuccess(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "proj-listener")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	// 用户通过 UI 提交的原始需求
	if err := ws.WriteDoc("input.md", "做一个待办事项应用，支持增删改查。"); err != nil {
		t.Fatalf("写入 input.md 失败: %v", err)
	}

	bus := eventbus.New()
	t.Cleanup(bus.Close)
	ch := bus.Subscribe()

	ai := &mockListenerAI{resp: fakeDesireBody}
	l := NewListener("")
	if err := l.Run(context.Background(), ws, ai, nil, bus); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// 验证 desire.md 已写入且 frontmatter 正确
	raw, err := ws.ReadDoc(workspace.DocDesire)
	if err != nil {
		t.Fatalf("读取 desire.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageListener {
		t.Errorf("frontmatter stage=%q want=%q", meta.Stage, workspace.StageListener)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter status=%q want=%q", meta.Status, workspace.StatusDone)
	}
	if meta.UpdatedAt.IsZero() {
		t.Errorf("frontmatter updated_at 不应为零值")
	}
	if body != fakeDesireBody {
		t.Errorf("desire 正文不匹配:\n got=%q\nwant=%q", body, fakeDesireBody)
	}

	// 验证事件序列：agent_start → doc_update → agent_done
	// 注：agent_run_event 已被 filterLifecycleEvents 过滤。
	var gotTypes []string
	for _, e := range filterLifecycleEvents(drain(ch)) {
		gotTypes = append(gotTypes, e.Type)
	}
	wantTypes := []string{
		eventbus.EventAgentStart,
		eventbus.EventDocUpdate,
		eventbus.EventAgentDone,
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("事件数量不匹配: got=%v want=%v", gotTypes, wantTypes)
	}
	for i := range wantTypes {
		if gotTypes[i] != wantTypes[i] {
			t.Errorf("事件[%d]=%q want=%q (全量 got=%v)", i, gotTypes[i], wantTypes[i], gotTypes)
			break
		}
	}
}

func TestListenerRunNoInput(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "proj-listener-noinput")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	// 故意不写 input.md

	bus := eventbus.New()
	t.Cleanup(bus.Close)
	ch := bus.Subscribe()

	ai := &mockListenerAI{resp: fakeDesireBody}
	l := NewListener("")
	err := l.Run(context.Background(), ws, ai, nil, bus)
	if err == nil {
		t.Fatal("期望返回错误，实际 nil")
	}
	if !strings.Contains(err.Error(), "input.md") {
		t.Errorf("错误信息应包含 input.md，实际: %v", err)
	}

	// 验证发布了 ask_user 事件
	var hasAskUser bool
	for _, e := range drain(ch) {
		if e.Type == eventbus.EventAskUser {
			hasAskUser = true
		}
	}
	if !hasAskUser {
		t.Errorf("期望发布 ask_user 事件，实际未发布")
	}
}

func TestListenerRunAIFailed(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "proj-listener-aierr")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if err := ws.WriteDoc("input.md", "做一个待办事项应用"); err != nil {
		t.Fatalf("写入 input.md 失败: %v", err)
	}

	bus := eventbus.New()
	t.Cleanup(bus.Close)
	ch := bus.Subscribe()

	ai := &mockListenerAI{err: context.DeadlineExceeded}
	l := NewListener("")
	err := l.Run(context.Background(), ws, ai, nil, bus)
	if err == nil {
		t.Fatal("期望返回错误，实际 nil")
	}

	// 验证发布了 agent_failed 事件
	var hasFailed bool
	for _, e := range drain(ch) {
		if e.Type == eventbus.EventAgentFailed {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Errorf("期望发布 agent_failed 事件，实际未发布")
	}
}
