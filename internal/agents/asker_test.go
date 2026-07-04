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

// mockAskerAI 实现 AIClient，按调用顺序返回预设响应，便于覆盖 Asker
// 多轮 AI 调用（生成问题 → satisfied → 整理 need.md）的场景。
type mockAskerAI struct {
	responses []string // 按调用顺序返回的响应
	errs      []error  // 与 responses 对齐的错误（非 nil 时优先返回）
	calls     int
	systems   []string
	users     []string
}

func (m *mockAskerAI) Ask(ctx context.Context, system, user string) (string, error) {
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
	return "", fmt.Errorf("mockAskerAI: 无更多预设响应（第 %d 次调用）", idx+1)
}

// AskWithModel 与 Ask 行为一致，model 参数仅作签名兼容。
func (m *mockAskerAI) AskWithModel(ctx context.Context, _, system, user string) (string, error) {
	return m.Ask(ctx, system, user)
}

// RunStream 复用 Ask 的记录与响应逻辑，并通过 text 事件回传。
// 因 agent 现走 RunWithTracking→RunStream，systems/users/calls 在此被填充。
func (m *mockAskerAI) RunStream(ctx context.Context, _, system, user string, onEvent func(aicli.RunEvent) error) (string, error) {
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
func (m *mockAskerAI) GetRun(_ context.Context, _ string) (*aicli.RunDetail, error) {
	return &aicli.RunDetail{}, nil
}

// fakeAskerDesire 是测试用的 desire.md 正文。
const fakeAskerDesire = "# 用户需求\n做一个待办事项应用，支持增删改查。\n\n# 改进点\n- 错误处理\n- 边界情况\n"

// fakeNeedBody 是测试用的 need.md 正文，结构符合 schema.go 约定。
const fakeNeedBody = "# 需求清单\n- N1: 面向个人用户的待办应用\n- N2: 支持离线可用与本地持久化\n"

// newAskerTestWorkspace 创建临时 workspace 并写入 desire.md。
// desire 为空字符串时不写入（用于测试缺失场景）。
func newAskerTestWorkspace(t *testing.T, desire string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "asker-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if desire != "" {
		if err := w.WriteDoc(workspace.DocDesire, desire); err != nil {
			t.Fatalf("写入 desire.md 失败: %v", err)
		}
	}
	return w
}

func TestAsker_Name(t *testing.T) {
	a := NewAsker(nil, "")
	if got := a.Name(); got != "asker" {
		t.Errorf("Name() = %q, want %q", got, "asker")
	}
}

// TestAsker_Run_Success 验证完整流程：读取 desire.md、两轮提问（首轮问 2 个问题、
// 次轮 satisfied）、整理 need.md 写入，事件序列与上下文拼装正确。
func TestAsker_Run_Success(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	ai := &mockAskerAI{responses: []string{
		`{"questions":["目标用户是谁？","是否需要离线可用？"],"satisfied":false}`,
		`{"questions":[],"satisfied":true}`,
		fakeNeedBody,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	var asked []string
	askFn := AskFunc(func(ctx context.Context, q string) (string, error) {
		asked = append(asked, q)
		return "用户回答：" + q, nil
	})

	a := NewAsker(askFn, "")
	if err := a.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	// 断言 AskFunc 被调用 2 次，且问题与 AI 输出一致
	wantQuestions := []string{"目标用户是谁？", "是否需要离线可用？"}
	if len(asked) != len(wantQuestions) {
		t.Fatalf("AskFunc 调用次数 = %d, want %d", len(asked), len(wantQuestions))
	}
	for i, want := range wantQuestions {
		if asked[i] != want {
			t.Errorf("asked[%d] = %q, want %q", i, asked[i], want)
		}
	}

	// 断言 AI 被调用 3 次：问题 → satisfied → 整理 need.md
	if ai.calls != 3 {
		t.Errorf("AI 调用次数 = %d, want 3", ai.calls)
	}
	if ai.systems[0] != askerSystemPrompt {
		t.Errorf("第 1 次 system 应为 askerSystemPrompt")
	}
	if ai.systems[2] != askerSummaryPrompt {
		t.Errorf("第 3 次 system 应为 askerSummaryPrompt")
	}
	// 第 1 次上下文：含 desire 且历史为空（暂无）
	if !strings.Contains(ai.users[0], fakeAskerDesire) {
		t.Errorf("第 1 次上下文应含 desire.md 全文")
	}
	if !strings.Contains(ai.users[0], "暂无") {
		t.Errorf("第 1 次上下文应标注历史为空")
	}
	// 第 2 次上下文：含首轮问答
	if !strings.Contains(ai.users[1], "目标用户是谁？") || !strings.Contains(ai.users[1], "用户回答：目标用户是谁？") {
		t.Errorf("第 2 次上下文应含首轮问答历史")
	}

	// 断言 need.md 已写入，frontmatter 正确，正文匹配
	raw, err := w.ReadDoc(workspace.DocNeed)
	if err != nil {
		t.Fatalf("读取 need.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageAsker {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageAsker)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if meta.UpdatedAt.IsZero() {
		t.Errorf("frontmatter UpdatedAt 不应为零值")
	}
	if body != fakeNeedBody {
		t.Errorf("need 正文不匹配:\ngot=%q\nwant=%q", body, fakeNeedBody)
	}

	// 断言事件序列：agent_start → doc_update → agent_done
	// 注：使用自定义 AskFunc 时不发布 ask_user 事件（该事件由默认 askViaBus 路径
	// 发布，见 TestAsker_Run_AskViaBus）；提问循环正确性已通过上方 asked 断言覆盖。
	// 注：agent_run_event 已被 filterLifecycleEvents 过滤。
	events := filterLifecycleEvents(drainEvents(ch))
	wantSeq := []string{
		eventbus.EventAgentStart,
		eventbus.EventDocUpdate,
		eventbus.EventAgentDone,
	}
	if len(events) != len(wantSeq) {
		t.Fatalf("事件数量 = %d, want %d (got=%v)", len(events), len(wantSeq), eventTypes(events))
	}
	for i, want := range wantSeq {
		if events[i].Type != want {
			t.Errorf("事件[%d] = %q, want %q (全量=%v)", i, events[i].Type, want, eventTypes(events))
			break
		}
		if events[i].Agent != "asker" {
			t.Errorf("事件[%d] Agent = %q, want %q", i, events[i].Agent, "asker")
		}
	}
	// doc_update 应携带 doc=need.md
	docEvt := events[1]
	data, ok := docEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("doc_update 事件 Data 应为 map[string]any, got %T", docEvt.Data)
	}
	if data["doc"] != workspace.DocNeed {
		t.Errorf("doc_update 事件 doc = %v, want %q", data["doc"], workspace.DocNeed)
	}
}

// TestAsker_Run_DesireMissing 验证 desire.md 缺失时返回错误、不调用 AI、发布 agent_failed。
func TestAsker_Run_DesireMissing(t *testing.T) {
	w := newAskerTestWorkspace(t, "") // 不写 desire.md
	ai := &mockAskerAI{responses: []string{fakeNeedBody}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	a := NewAsker(nil, "")
	err := a.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望缺失 desire.md 返回错误，got nil")
	}
	if !strings.Contains(err.Error(), "desire.md") {
		t.Errorf("错误应提示 desire.md: %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("desire.md 缺失时不应调用 AI, got calls=%d", ai.calls)
	}
	if _, err := w.ReadDoc(workspace.DocNeed); err == nil {
		t.Error("desire.md 缺失时不应写入 need.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 need.md 不存在错误, got %v", err)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足: got %d, want 至少 2", len(events))
	}
	if events[0].Type != eventbus.EventAgentStart {
		t.Errorf("第 0 个事件应为 agent_start, got %q", events[0].Type)
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestAsker_Run_SatisfiedImmediate 验证首轮即 satisfied 且无问题时直接进入整理，
// 不调用 AskFunc。
func TestAsker_Run_SatisfiedImmediate(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	ai := &mockAskerAI{responses: []string{
		`{"questions":[],"satisfied":true}`,
		fakeNeedBody,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	var askCalled int
	a := NewAsker(AskFunc(func(ctx context.Context, q string) (string, error) {
		askCalled++
		return "x", nil
	}), "")
	if err := a.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if askCalled != 0 {
		t.Errorf("首轮 satisfied 时不应调用 AskFunc, got %d", askCalled)
	}
	if ai.calls != 2 {
		t.Errorf("AI 调用次数 = %d, want 2", ai.calls)
	}
	// need.md 应已写入
	if _, err := w.ReadDoc(workspace.DocNeed); err != nil {
		t.Errorf("need.md 应已写入: %v", err)
	}
}

// TestAsker_Run_JSONFallback 验证 AI 输出非 JSON 时把整段当一个问题问用户。
func TestAsker_Run_JSONFallback(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	// 第 1 次：非 JSON 纯文本 → 当作单个问题
	// 第 2 次：satisfied
	// 第 3 次：整理 need.md
	ai := &mockAskerAI{responses: []string{
		"请说明目标用户是谁？",
		`{"questions":[],"satisfied":true}`,
		fakeNeedBody,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	var asked []string
	a := NewAsker(AskFunc(func(ctx context.Context, q string) (string, error) {
		asked = append(asked, q)
		return "个人用户", nil
	}), "")
	if err := a.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if len(asked) != 1 || asked[0] != "请说明目标用户是谁？" {
		t.Errorf("应把整段当作单个问题, got %v", asked)
	}
	if _, err := w.ReadDoc(workspace.DocNeed); err != nil {
		t.Errorf("need.md 应已写入: %v", err)
	}
}

// TestAsker_Run_JSONWithExtraText 验证 AI 输出含多余文本时仍能提取 JSON。
func TestAsker_Run_JSONWithExtraText(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	ai := &mockAskerAI{responses: []string{
		"好的，我分析如下：\n```json\n{\"questions\":[\"性能要求？\"],\"satisfied\":false}\n```\n以上。",
		`{"questions":[],"satisfied":true}`,
		fakeNeedBody,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	var asked []string
	a := NewAsker(AskFunc(func(ctx context.Context, q string) (string, error) {
		asked = append(asked, q)
		return "P99 < 200ms", nil
	}), "")
	if err := a.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if len(asked) != 1 || asked[0] != "性能要求？" {
		t.Errorf("应从多余文本中提取 JSON 问题, got %v", asked)
	}
}

// TestAsker_Run_MaxRounds 验证 AI 永不 satisfied 时循环在 maxAskerRounds 后停止，
// 仍能产出 need.md，不会死循环。
func TestAsker_Run_MaxRounds(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	// 前 maxAskerRounds 次返回未满足的单问题，最后一次整理 need.md
	respQ := `{"questions":["还需要明确什么？"],"satisfied":false}`
	responses := make([]string, 0, maxAskerRounds+1)
	for i := 0; i < maxAskerRounds; i++ {
		responses = append(responses, respQ)
	}
	responses = append(responses, fakeNeedBody)
	ai := &mockAskerAI{responses: responses}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	var askCalled int
	a := NewAsker(AskFunc(func(ctx context.Context, q string) (string, error) {
		askCalled++
		return "回答", nil
	}), "")
	if err := a.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	// AI 调用：maxAskerRounds 次生成问题 + 1 次整理 = maxAskerRounds+1
	if ai.calls != maxAskerRounds+1 {
		t.Errorf("AI 调用次数 = %d, want %d", ai.calls, maxAskerRounds+1)
	}
	// AskFunc 应被调用 maxAskerRounds 次
	if askCalled != maxAskerRounds {
		t.Errorf("AskFunc 调用次数 = %d, want %d", askCalled, maxAskerRounds)
	}
	// need.md 仍应写入
	raw, err := w.ReadDoc(workspace.DocNeed)
	if err != nil {
		t.Fatalf("need.md 应已写入: %v", err)
	}
	if !strings.Contains(raw, fakeNeedBody) {
		t.Errorf("need.md 正文应匹配")
	}
}

// TestAsker_Run_AskFuncError 验证提问回调返回错误时发布 agent_failed 并返回错误。
func TestAsker_Run_AskFuncError(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	ai := &mockAskerAI{responses: []string{
		`{"questions":["问题1"],"satisfied":false}`,
		fakeNeedBody,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	askErr := errors.New("用户取消")
	a := NewAsker(AskFunc(func(ctx context.Context, q string) (string, error) {
		return "", askErr
	}), "")
	err := a.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望返回错误, got nil")
	}
	if !strings.Contains(err.Error(), "用户取消") {
		t.Errorf("错误应包含原始错误: %v", err)
	}
	// need.md 不应写入
	if _, err := w.ReadDoc(workspace.DocNeed); err == nil {
		t.Error("AskFunc 失败时不应写入 need.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 need.md 不存在错误, got %v", err)
	}
	events := drainEvents(ch)
	var hasFailed bool
	for _, e := range events {
		if e.Type == eventbus.EventAgentFailed {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Error("期望发布 agent_failed 事件")
	}
}

// TestAsker_Run_AIError 验证 AI 调用失败时发布 agent_failed 并返回错误。
func TestAsker_Run_AIError(t *testing.T) {
	w := newAskerTestWorkspace(t, fakeAskerDesire)
	ai := &mockAskerAI{
		errs: []error{errors.New("AI 服务不可用")},
	}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	a := NewAsker(AskFunc(func(ctx context.Context, q string) (string, error) {
		return "回答", nil
	}), "")
	err := a.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望返回错误, got nil")
	}
	if !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Errorf("错误应包含原始 AI 错误: %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocNeed); err == nil {
		t.Error("AI 失败时不应写入 need.md")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 need.md 不存在错误, got %v", err)
	}
	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足: got %d, want 至少 2", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestAsker_Run_AskViaBus 验证 ask 为 nil 时的默认路径：发布 ask_user 事件后
// 轮询 ask_reply.md，由模拟“人工写入回复”的协程完成回答。
func TestAsker_Run_AskViaBus(t *testing.T) {
	// 缩短轮询间隔，避免测试等待真实时间
	oldInterval := askPollInterval
	askPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { askPollInterval = oldInterval })

	w := newAskerTestWorkspace(t, fakeAskerDesire)
	ai := &mockAskerAI{responses: []string{
		`{"questions":["目标用户是谁？","是否需要离线可用？"],"satisfied":false}`,
		`{"questions":[],"satisfied":true}`,
		fakeNeedBody,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	// 一个订阅供“人工回复”协程消费 ask_user 事件
	replyCh := bus.Subscribe()
	// 另一个订阅供断言事件序列
	assertCh := bus.Subscribe()

	// 协程：收到 ask_user 即把回复写入 ask_reply.md（用问题文本保证两次回复内容不同）
	go func() {
		for evt := range replyCh {
			if evt.Type != eventbus.EventAskUser {
				continue
			}
			data, ok := evt.Data.(map[string]any)
			if !ok {
				continue
			}
			q, _ := data["question"].(string)
			_ = w.WriteDoc(askReplyDoc, "回复："+q)
		}
	}()

	a := NewAsker(nil, "") // 使用默认 askViaBus
	if err := a.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	// 验证 need.md 已写入且 frontmatter 正确
	raw, err := w.ReadDoc(workspace.DocNeed)
	if err != nil {
		t.Fatalf("读取 need.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageAsker {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageAsker)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	if body != fakeNeedBody {
		t.Errorf("need 正文不匹配:\ngot=%q\nwant=%q", body, fakeNeedBody)
	}

	// 验证事件序列包含 ask_user ×2、doc_update、agent_done
	events := drainEvents(assertCh)
	var askCount int
	var hasDocUpdate, hasDone bool
	for _, e := range events {
		switch e.Type {
		case eventbus.EventAskUser:
			askCount++
		case eventbus.EventDocUpdate:
			hasDocUpdate = true
		case eventbus.EventAgentDone:
			hasDone = true
		}
	}
	if askCount != 2 {
		t.Errorf("ask_user 事件数 = %d, want 2", askCount)
	}
	if !hasDocUpdate {
		t.Error("期望发布 doc_update 事件")
	}
	if !hasDone {
		t.Error("期望发布 agent_done 事件")
	}
}

// TestParseAskerResponse 单元测试 JSON 容错解析。
func TestParseAskerResponse(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantQ       []string
		wantSatisfied bool
	}{
		{
			name:        "正常 JSON",
			raw:         `{"questions":["Q1","Q2"],"satisfied":false}`,
			wantQ:       []string{"Q1", "Q2"},
			wantSatisfied: false,
		},
		{
			name:        "satisfied 无问题",
			raw:         `{"questions":[],"satisfied":true}`,
			wantQ:       []string{},
			wantSatisfied: true,
		},
		{
			name:        "JSON 含多余文本与代码块",
			raw:         "分析：\n```json\n{\"questions\":[\"X\"],\"satisfied\":true}\n```\n",
			wantQ:       []string{"X"},
			wantSatisfied: true,
		},
		{
			name:        "非 JSON 整段当一个问题",
			raw:         "请说明性能要求？",
			wantQ:       []string{"请说明性能要求？"},
			wantSatisfied: false,
		},
		{
			name:        "空字符串",
			raw:         "   ",
			wantQ:       nil,
			wantSatisfied: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotQ, gotS := parseAskerResponse(c.raw)
			if gotS != c.wantSatisfied {
				t.Errorf("satisfied = %v, want %v", gotS, c.wantSatisfied)
			}
			if len(gotQ) != len(c.wantQ) {
				t.Fatalf("questions 长度 = %d, want %d (got=%v)", len(gotQ), len(c.wantQ), gotQ)
			}
			for i, want := range c.wantQ {
				if gotQ[i] != want {
					t.Errorf("questions[%d] = %q, want %q", i, gotQ[i], want)
				}
			}
		})
	}
}

// eventTypes 提取事件类型列表，便于失败时打印。
func eventTypes(events []eventbus.Event) []string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}
