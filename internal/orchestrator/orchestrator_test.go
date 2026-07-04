package orchestrator

import (
	"context"
	"errors"
	"testing"

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
type mockGit struct{}

func (m *mockGit) CommitAndPush(ctx context.Context, paths []string, message string) error {
	return nil
}

// newTestOrchestrator 构造一个使用 noop agent 的编排器，并返回事件订阅 channel。
func newTestOrchestrator(t *testing.T) (*Orchestrator, <-chan eventbus.Event) {
	t.Helper()
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	cfg := config.Default()
	o := New(cfg, ws, &mockAI{}, &mockGit{}, bus)
	o.RegisterDefaultNoop()
	return o, bus.Subscribe()
}

// drainEvents 非阻塞地抽干 channel 中已缓冲的事件并返回。
//
// 由于事件总线缓冲为 256，而单个编排流程事件数远小于此，
// 在 Run 返回后调用本函数即可拿到全部事件，无需后台 goroutine。
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
	// 期望所有阶段至少各有一条 agent_start 与 agent_done。
	wantStages := []string{
		workspace.StageListener,
		workspace.StageAsker,
		workspace.StagePlanner,
		workspace.StageDesigner,
		workspace.StageEvaluator,
		workspace.StageManager,
		workspace.StageExecutor,
		workspace.StageGenerator,
		workspace.StageGittor,
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
	o := New(cfg, ws, &mockAI{}, &mockGit{}, bus)
	// 不注册任何 agent。
	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("期望未注册错误，得到 nil")
	}
	want := "agent " + workspace.StageListener + " not registered"
	if err.Error() != want {
		t.Fatalf("错误信息不符, got %q want %q", err.Error(), want)
	}
}

// TestDiscussLoopConsensus 验证讨论循环在 Evaluator 返回 nil 时立即结束。
func TestDiscussLoopConsensus(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus)
	o.Register(workspace.StageDesigner, &stubAgent{name: workspace.StageDesigner})
	o.Register(workspace.StageEvaluator, &stubAgent{name: workspace.StageEvaluator, ret: nil})
	o.SetMaxDiscussRounds(5)

	if err := o.RunDiscussLoop(context.Background()); err != nil {
		t.Fatalf("期望讨论循环成功, got %v", err)
	}
}

// TestDiscussLoopMaxRounds 验证 Evaluator 持续返回 ErrNoConsensus 时达到上限报错。
func TestDiscussLoopMaxRounds(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus)
	o.Register(workspace.StageDesigner, &stubAgent{name: workspace.StageDesigner})
	o.Register(workspace.StageEvaluator, &stubAgent{name: workspace.StageEvaluator, ret: agents.ErrNoConsensus})
	o.SetMaxDiscussRounds(3)

	err := o.RunDiscussLoop(context.Background())
	if err == nil {
		t.Fatal("期望达到上限错误, got nil")
	}
	// 轮次上限应保持为 maxDisc=3。
	if o.maxDisc != 3 {
		t.Fatalf("maxDisc 被意外修改: %d", o.maxDisc)
	}
}

// TestEvalLoopPass 验证评估循环在 Evaluator 返回 nil 时通过。
func TestEvalLoopPass(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus)
	o.Register(workspace.StageGenerator, &stubAgent{name: workspace.StageGenerator})
	o.Register(workspace.StageEvaluator, &stubAgent{name: workspace.StageEvaluator, ret: nil})
	o.SetMaxEvalRetries(3)

	if err := o.RunEvalLoop(context.Background()); err != nil {
		t.Fatalf("期望评估循环通过, got %v", err)
	}
}

// TestEvalLoopMaxRetries 验证 Evaluator 持续返回 ErrEvaluationFailed 时重试到上限。
func TestEvalLoopMaxRetries(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus)
	o.Register(workspace.StageGenerator, &stubAgent{name: workspace.StageGenerator})
	o.Register(workspace.StageEvaluator, &stubAgent{name: workspace.StageEvaluator, ret: agents.ErrEvaluationFailed})
	o.SetMaxEvalRetries(3)

	err := o.RunEvalLoop(context.Background())
	if err == nil {
		t.Fatal("期望达到上限错误, got nil")
	}
}

// TestDiscussLoopEvaluatorFatal 验证 Evaluator 返回非哨兵错误时立即终止。
func TestDiscussLoopEvaluatorFatal(t *testing.T) {
	dir := t.TempDir()
	ws := workspace.New(dir, "test-project")
	_ = ws.EnsureDirs()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fatal := errors.New("boom")
	o := New(config.Default(), ws, &mockAI{}, &mockGit{}, bus)
	o.Register(workspace.StageDesigner, &stubAgent{name: workspace.StageDesigner})
	o.Register(workspace.StageEvaluator, &stubAgent{name: workspace.StageEvaluator, ret: fatal})
	o.SetMaxDiscussRounds(5)

	err := o.RunDiscussLoop(context.Background())
	if !errors.Is(err, fatal) {
		t.Fatalf("期望返回 fatal 错误, got %v", err)
	}
}

// stubAgent 可配置返回值的测试 agent。
type stubAgent struct {
	name string
	ret  error
}

func (s *stubAgent) Name() string { return s.name }

func (s *stubAgent) Run(ctx context.Context, ws *workspace.Workspace, ai agents.AIClient, git agents.GittorClient, bus *eventbus.Bus) error {
	return s.ret
}
