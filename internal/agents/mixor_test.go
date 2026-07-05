package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mixorQueue 是测试用的 requirements_queue.md 内容。
const mixorQueue = "- 新增：导出待办为 CSV\n"

// mixorSpec 是测试用的 spec.md 正文（含一个已打勾、一个未打勾的 Requirement）。
const mixorSpec = "# 待办 Spec\n## Requirements\n### [x] Requirement: 增删改查\n已完成\n\n### [ ] Requirement: 导出\n待实现\n"

// mixorDeal 是测试用的 deal.md 正文。
const mixorDeal = "# 完工协议\n## 验收标准\n- [x] D1: 增删改查\n- [ ] D2: 导出 CSV\n"

// mixorTask 是测试用的 task.md 正文。
const mixorTask = "# Tasks\n- [x] T1: 实现增删改查\n"

// newMixorTestWorkspace 创建临时 workspace 并写入 requirements_queue.md + 现有产出。
// 当 withExisting 为 true 时写入 spec/deal/task + code/main.go。
func newMixorTestWorkspace(t *testing.T, queue string, withExisting bool) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "mixor-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if queue != "" {
		if err := w.WriteDoc(workspace.DocReqQueue, queue); err != nil {
			t.Fatalf("写入 requirements_queue.md 失败: %v", err)
		}
	}
	if withExisting {
		writeTestDoc(t, w, workspace.DocSpec, workspace.StageAnalyst, mixorSpec)
		writeTestDoc(t, w, workspace.DocDeal, workspace.StageArchitect, mixorDeal)
		writeTestDoc(t, w, workspace.DocTask, workspace.StagePlanner, mixorTask)
		// code/main.go
		codeDir := filepath.Join(w.Path(), "code")
		if err := os.MkdirAll(codeDir, 0o755); err != nil {
			t.Fatalf("创建 code 目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(codeDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("写入 code/main.go 失败: %v", err)
		}
		// input.md（供 rerun 追加）
		if err := w.WriteDoc(workspace.DocInput, "做一个待办应用。\n"); err != nil {
			t.Fatalf("写入 input.md 失败: %v", err)
		}
	}
	return w
}

func TestMixor_Name(t *testing.T) {
	m := NewMixor()
	if got, want := m.Name(), workspace.StageMixor; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

// TestMixor_Run_EmptyQueue 验证队列为空时直接发布 done 返回 nil。
func TestMixor_Run_EmptyQueue(t *testing.T) {
	w := newMixorTestWorkspace(t, "", false)
	ai := &mockAI{responses: []string{"x"}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	m := NewMixor()
	if err := m.Run(context.Background(), w, ai, git, bus, &mockResolver{}); err != nil {
		t.Fatalf("空队列 Run 应返回 nil，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("空队列时不应调用 AI")
	}
	// 应发布 agent_start + agent_done
	var hasDone bool
	for _, e := range drainEvents(ch) {
		if e.Type == eventbus.EventAgentDone {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("期望发布 agent_done")
	}
}

// TestMixor_Run_QueueMissing 验证 requirements_queue.md 不存在时直接完成。
func TestMixor_Run_QueueMissing(t *testing.T) {
	w := newMixorTestWorkspace(t, "", false) // 不写 queue
	ai := &mockAI{responses: []string{"x"}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	m := NewMixor()
	if err := m.Run(context.Background(), w, ai, git, bus, &mockResolver{}); err != nil {
		t.Fatalf("队列缺失 Run 应返回 nil，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("队列缺失时不应调用 AI")
	}
}

// TestMixor_Run_Merge 验证 merge 路径：清空 queue、写 spec.md、写 progress、返回 nil。
func TestMixor_Run_Merge(t *testing.T) {
	w := newMixorTestWorkspace(t, mixorQueue, true)
	mergedSpec := "# 待办 Spec\n## Requirements\n### [x] Requirement: 增删改查\n### [ ] Requirement: 导出 CSV\n待实现\n"
	ai := &mockAI{responses: []string{
		`{"conflict": false, "action": "merge", "merged_spec": "# 待办 Spec\n## Requirements\n### [x] Requirement: 增删改查\n### [ ] Requirement: 导出 CSV\n待实现\n", "reason": "可融入现有设计"}`,
	}}
	// 用 quoted 形式更可靠
	ai.responses = []string{
		mergeJSONResponse(mergedSpec),
	}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()
	resolver := &mockResolver{}

	m := NewMixor()
	if err := m.Run(context.Background(), w, ai, git, bus, resolver); err != nil {
		t.Fatalf("merge 路径应返回 nil，got %v", err)
	}

	if ai.calls != 1 {
		t.Fatalf("AI 调用次数 = %d, want 1", ai.calls)
	}
	if ai.systems[0] != mixorSystemPrompt {
		t.Errorf("system 应为 mixorSystemPrompt")
	}
	// user 应含新需求 + 现有 spec/deal/task + code 文件列表
	for _, want := range []string{"# 新需求", "# 现有 spec.md", "# 现有 deal.md", "# 现有 task.md", "# code/ 文件列表", "main.go"} {
		if !strings.Contains(ai.users[0], want) {
			t.Errorf("user 应含 %q", want)
		}
	}
	if resolver.calls != 1 || resolver.stages[0] != workspace.StageMixor {
		t.Errorf("resolver 调用不符: %v", resolver.stages)
	}

	// spec.md 应被覆盖为 merged_spec
	raw, err := w.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	_, body := workspace.ParseDoc(raw)
	if !strings.Contains(body, "导出 CSV") {
		t.Errorf("spec.md 应含合并后的导出 CSV Requirement:\n%s", body)
	}
	// 应保留已打勾标记
	if !strings.Contains(body, "### [x] Requirement: 增删改查") {
		t.Errorf("spec.md 应保留已打勾的增删改查:\n%s", body)
	}

	// requirements_queue.md 应被清空
	q, err := w.ReadDoc(workspace.DocReqQueue)
	if err != nil {
		t.Fatalf("读取 requirements_queue.md 失败: %v", err)
	}
	if strings.TrimSpace(q) != "" {
		t.Errorf("requirements_queue.md 应被清空，got %q", q)
	}

	// reports/progress.md 应被写入
	p, err := w.ReadDoc(workspace.DocProgress)
	if err != nil {
		t.Fatalf("读取 reports/progress.md 失败: %v", err)
	}
	if !strings.Contains(p, "merge") || !strings.Contains(p, "可融入现有设计") {
		t.Errorf("progress 应含 merge 与理由:\n%s", p)
	}

	// 事件序列：agent_start → doc_update(spec) → doc_update(task) → doc_update(progress 由 doc_update 体现) → agent_done
	events := filterLifecycleEvents(drainEvents(ch))
	var hasDone bool
	for _, e := range events {
		if e.Type == eventbus.EventAgentDone {
			hasDone = true
		}
	}
	if !hasDone {
		t.Errorf("merge 路径应发布 agent_done, events=%v", eventTypes(events))
	}
}

// TestMixor_Run_Rerun 验证 rerun 路径：追加新需求到 input.md、清空 queue、
// 写 progress、返回 ErrNeedRerun。
func TestMixor_Run_Rerun(t *testing.T) {
	w := newMixorTestWorkspace(t, mixorQueue, true)
	ai := &mockAI{responses: []string{
		`{"conflict": true, "action": "rerun", "merged_spec": "", "reason": "架构根本性冲突"}`,
	}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	m := NewMixor()
	err := m.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if !errors.Is(err, ErrNeedRerun) {
		t.Fatalf("期望 ErrNeedRerun，got %v", err)
	}

	// input.md 应被追加新需求
	input, err := w.ReadDoc(workspace.DocInput)
	if err != nil {
		t.Fatalf("读取 input.md 失败: %v", err)
	}
	if !strings.Contains(input, "追加需求") || !strings.Contains(input, "导出待办为 CSV") {
		t.Errorf("input.md 应含追加的新需求:\n%s", input)
	}

	// requirements_queue.md 应被清空
	q, err := w.ReadDoc(workspace.DocReqQueue)
	if err != nil {
		t.Fatalf("读取 requirements_queue.md 失败: %v", err)
	}
	if strings.TrimSpace(q) != "" {
		t.Errorf("requirements_queue.md 应被清空，got %q", q)
	}

	// reports/progress.md 应被写入，含 rerun
	p, err := w.ReadDoc(workspace.DocProgress)
	if err != nil {
		t.Fatalf("读取 reports/progress.md 失败: %v", err)
	}
	if !strings.Contains(p, "rerun") || !strings.Contains(p, "架构根本性冲突") {
		t.Errorf("progress 应含 rerun 与理由:\n%s", p)
	}

	// 应发布 agent_failed（哨兵），不应发布 agent_done
	var hasFailed, hasDone bool
	for _, e := range drainEvents(ch) {
		switch e.Type {
		case eventbus.EventAgentFailed:
			hasFailed = true
		case eventbus.EventAgentDone:
			hasDone = true
		}
	}
	if !hasFailed {
		t.Error("期望发布 agent_failed")
	}
	if hasDone {
		t.Error("rerun 路径不应发布 agent_done")
	}
}

// TestMixor_Run_AIError 验证 AI 失败时返回错误并发布 agent_failed。
func TestMixor_Run_AIError(t *testing.T) {
	w := newMixorTestWorkspace(t, mixorQueue, true)
	ai := &mockAI{errs: []error{errors.New("AI 不可用")}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	m := NewMixor()
	err := m.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "AI 不可用") {
		t.Fatalf("期望含 AI 错误，got %v", err)
	}
	// queue 不应被清空
	q, _ := w.ReadDoc(workspace.DocReqQueue)
	if strings.TrimSpace(q) == "" {
		t.Error("AI 失败时不应清空 requirements_queue.md")
	}
}

// mergeJSONResponse 构造一个 merge 动作的 JSON 响应，确保 spec 中的换行被正确转义。
func mergeJSONResponse(mergedSpec string) string {
	// 用 Go 字符串转义后嵌入 JSON
	escaped := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\t", "\\t",
	).Replace(mergedSpec)
	return `{"conflict": false, "action": "merge", "merged_spec": "` + escaped + `", "reason": "可融入现有设计"}`
}

// 确保 time 包被引用（writeTestDoc 使用），避免 import 报错。
var _ = time.Now
