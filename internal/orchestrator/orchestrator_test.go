package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mockAI 模拟 AIClient，返回空字符串。
type mockAI struct{}

func (m *mockAI) Ask(ctx context.Context, system, user string) (string, error) {
	return "", nil
}

// AskWithModel 与 Ask 相同（mock 不区分模型）。
func (m *mockAI) AskWithModel(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}

// RunStream 立即回调一个空的 text 事件并返回空 runID。
func (m *mockAI) RunStream(_ context.Context, _, _, _ string, onEvent func(aicli.RunEvent) error) (string, error) {
	if onEvent != nil {
		_ = onEvent(aicli.RunEvent{Type: "system", RunID: "mock"})
		_ = onEvent(aicli.RunEvent{Type: "text", Content: "", RunID: "mock"})
		_ = onEvent(aicli.RunEvent{Type: "result", RunID: "mock"})
	}
	return "mock", nil
}

// GetRun 返回空 RunDetail。
func (m *mockAI) GetRun(_ context.Context, _ string) (*aicli.RunDetail, error) {
	return &aicli.RunDetail{}, nil
}

// mockGit 模拟 GittorClient，不做任何事。
type mockGit struct {
	calls int
	last  string
}

func (m *mockGit) CommitAndPush(ctx context.Context, paths []string, message string) error {
	m.calls++
	m.last = message
	return nil
}

// newTestOrchestrator 构造一个使用 noop agent 的编排器，并返回事件订阅 channel。
// 预写入 spec.md / task.md（满足 buildCoderInstruction 与 commitAndPush 的前置条件）。
func newTestOrchestrator(t *testing.T) (*Orchestrator, <-chan eventbus.Event) {
	t.Helper()
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	// 预写入 spec.md（含已打勾 Requirement，让 commitAndPush 通过）与 task.md（供 buildCoderInstruction 读取）
	if err := ws.WriteDoc(workspace.DocSpec, "# Spec\n## Requirements\n### [x] Requirement: Foo\n完成\n"); err != nil {
		t.Fatalf("写入 spec.md 失败: %v", err)
	}
	if err := ws.WriteDoc(workspace.DocTask, "# Tasks\n- [x] T1: 完成 Foo\n"); err != nil {
		t.Fatalf("写入 task.md 失败: %v", err)
	}
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	cfg := config.Default()
	o := New(cfg, ws, &mockAI{}, &mockGit{}, bus, nil)
	o.RegisterDefaultNoop()
	return o, bus.Subscribe()
}

// drainEvents 非阻塞地抽干 channel 中已缓冲的事件并返回。
func drainEvents(ch <-chan eventbus.Event) []eventbus.Event {
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

// TestRunNoopAllStages 验证注册 noop agent 后 Run 能走完所有阶段。
func TestRunNoopAllStages(t *testing.T) {
	o, ch := newTestOrchestrator(t)

	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	evts := drainEvents(ch)
	// 期望 5 个 pipeline 阶段（analyst..reviewer）+ coder_instruction + gittor
	// 各有一条 agent_start 与 agent_done。
	// Mixor 不在断言内：它仅在 requirements_queue.md 有内容时才运行，
	// 而 noop 冒烟测试不注入新需求。
	wantStages := []string{
		workspace.StageAnalyst,
		workspace.StageArchitect,
		workspace.StagePlanner,
		workspace.StageCoder,
		workspace.StageReviewer,
	}
	startSeen := map[string]bool{}
	doneSeen := map[string]bool{}
	for _, e := range evts {
		switch e.Type {
		case eventbus.EventAgentStart:
			startSeen[e.Agent] = true
		case eventbus.EventAgentDone:
			doneSeen[e.Agent] = true
		}
	}
	for _, s := range wantStages {
		if !startSeen[s] {
			t.Errorf("缺少 %s 的 agent_start 事件", s)
		}
		if !doneSeen[s] {
			t.Errorf("缺少 %s 的 agent_done 事件", s)
		}
	}
	if !startSeen["coder_instruction"] {
		t.Errorf("缺少 coder_instruction 的 agent_start 事件")
	}
	if !startSeen["gittor"] {
		t.Errorf("缺少 gittor 的 agent_start 事件")
	}
}

// TestAgentNotRegistered 验证未注册阶段返回明确错误。
func TestAgentNotRegistered(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	cfg := config.Default()
	o := New(cfg, ws, &mockAI{}, &mockGit{}, bus, nil)
	// 不注册任何 agent。
	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("期望未注册错误，得到 nil")
	}
	want := "agent " + workspace.StageAnalyst + " not registered"
	if err.Error() != want {
		t.Fatalf("错误信息不符, got %q want %q", err.Error(), want)
	}
}

// TestEvalLoopPass 验证评估循环在 Reviewer 返回 nil 时通过。
func TestEvalLoopPass(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	_ = ws.WriteDoc(workspace.DocSpec, "# Spec\n## Requirements\n### [x] Requirement: Foo\n")
	_ = ws.WriteDoc(workspace.DocTask, "# Tasks\n- [x] T1: 完成 Foo\n")
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	o.Register(workspace.StageCoder, &stubAgent{name: workspace.StageCoder})
	o.Register(workspace.StageReviewer, &stubAgent{name: workspace.StageReviewer, ret: nil})
	o.SetMaxEvalRetries(3)

	if err := o.runEvalLoop(context.Background()); err != nil {
		t.Fatalf("期望评估循环通过, got %v", err)
	}
}

// TestEvalLoopMaxRetries 验证 Reviewer 持续返回 ErrEvaluationFailed 时重试到上限。
func TestEvalLoopMaxRetries(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	_ = ws.WriteDoc(workspace.DocSpec, "# Spec\n## Requirements\n### [x] Requirement: Foo\n")
	_ = ws.WriteDoc(workspace.DocTask, "# Tasks\n- [x] T1: 完成 Foo\n")
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	o.Register(workspace.StageCoder, &stubAgent{name: workspace.StageCoder})
	o.Register(workspace.StageReviewer, &stubAgent{name: workspace.StageReviewer, ret: agents.ErrEvaluationFailed})
	o.SetMaxEvalRetries(3)

	err := o.runEvalLoop(context.Background())
	if err == nil {
		t.Fatal("期望达到上限错误, got nil")
	}
}

// TestEvalLoopReviewerFatal 验证 Reviewer 返回非哨兵错误时立即终止。
func TestEvalLoopReviewerFatal(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	_ = ws.WriteDoc(workspace.DocSpec, "# Spec\n## Requirements\n### [x] Requirement: Foo\n")
	_ = ws.WriteDoc(workspace.DocTask, "# Tasks\n- [x] T1: 完成 Foo\n")
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fatal := errors.New("boom")
	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	o.Register(workspace.StageCoder, &stubAgent{name: workspace.StageCoder})
	o.Register(workspace.StageReviewer, &stubAgent{name: workspace.StageReviewer, ret: fatal})
	o.SetMaxEvalRetries(3)

	err := o.runEvalLoop(context.Background())
	if !errors.Is(err, fatal) {
		t.Fatalf("期望返回 fatal 错误, got %v", err)
	}
}

// TestCheckControlStop 验证 Stop 信号在 checkControl 时返回 ErrStopped。
func TestCheckControlStop(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	o.Stop()
	err := o.checkControl(context.Background())
	if !errors.Is(err, agents.ErrStopped) {
		t.Fatalf("期望 ErrStopped, got %v", err)
	}
}

// TestCheckControlCtxCancel 验证 ctx 取消时返回 ctx.Err()。
func TestCheckControlCtxCancel(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := o.checkControl(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled, got %v", err)
	}
}

// TestPauseResume 验证 Pause→阻塞→Resume 的协作流程。
func TestPauseResume(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	o.Pause()

	done := make(chan error, 1)
	go func() {
		done <- o.checkControl(context.Background())
	}()

	// 等待编排器进入 paused 阻塞
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if o.IsPaused() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !o.IsPaused() {
		t.Fatal("期望编排器进入 paused 状态")
	}

	o.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("期望 checkControl 返回 nil, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("checkControl 未在 Resume 后返回")
	}
	if o.IsPaused() {
		t.Errorf("Resume 后应不再是 paused 状态")
	}
}

// TestStopWakesPaused 验证 Stop 能唤醒 paused 状态并返回 ErrStopped。
func TestStopWakesPaused(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	o.Pause()

	done := make(chan error, 1)
	go func() {
		done <- o.checkControl(context.Background())
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if o.IsPaused() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !o.IsPaused() {
		t.Fatal("期望编排器进入 paused 状态")
	}

	o.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, agents.ErrStopped) {
			t.Fatalf("期望 ErrStopped, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("checkControl 未在 Stop 后返回")
	}
}

// TestHasQueuedRequirements 验证队列为空 / 有内容 / 文件缺失三种情况。
func TestHasQueuedRequirements(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus, nil)
	if o.hasQueuedRequirements() {
		t.Errorf("文件缺失时应返回 false")
	}
	_ = ws.WriteDoc(workspace.DocReqQueue, "   \n\n")
	if o.hasQueuedRequirements() {
		t.Errorf("纯空白内容应返回 false")
	}
	_ = ws.WriteDoc(workspace.DocReqQueue, "- 新增：导出 CSV\n")
	if !o.hasQueuedRequirements() {
		t.Errorf("有内容时应返回 true")
	}
}

// stubAgent 可配置返回值的测试 agent。
type stubAgent struct {
	name string
	ret  error
}

func (s *stubAgent) Name() string { return s.name }

func (s *stubAgent) Run(ctx context.Context, ws *workspace.Workspace, ai agents.AIClient, git agents.GittorClient, bus *eventbus.Bus, resolver agents.ModelResolver) error {
	return s.ret
}

// 确保 sync 包被引用（避免在某些场景下被自动移除）。
var _ = sync.Mutex{}
