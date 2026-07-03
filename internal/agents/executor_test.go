package agents

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// executorTaskBody 是测试用的 task.md 正文，结构符合 schema.go 约定。
const executorTaskBody = `# Tasks
- [ ] T1: 实现入口程序（验收点：main 函数可运行）
- [ ] T2: 输出 hello（验收点：运行后打印 hello）
`

// executorSpecBody 是测试用的 spec.md 正文，结构符合 schema.go 约定。
const executorSpecBody = `# Hello Spec
## Why
演示 Executor→Generator 隔离流程

## What Changes
- 新增 main.go

## Impact
仅 code/ 目录

## ADDED Requirements
### Requirement: 输出 hello
程序 SHALL 运行后打印 hello。
#### Scenario
- WHEN 运行程序 THEN 标准输出包含 hello
`

// newExecutorTestWorkspace 创建临时 workspace 并写入 task.md 与 spec.md。
// 同时写入 desire/need/deal（带独特标记 SECRET），用于验证指令文件不含
// 这些上游文档内容（隔离性）。skip 指定要跳过的文档名以测试缺失场景。
func newExecutorTestWorkspace(t *testing.T, skip string) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "executor-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	docs := []struct {
		name    string
		content string
	}{
		{workspace.DocTask, executorTaskBody},
		{workspace.DocSpec, executorSpecBody},
		// 以下三份文档带独特标记，用于验证指令文件不含它们（隔离）
		{workspace.DocDesire, "# 用户需求\nDESIRE_SECRET 内容\n"},
		{workspace.DocNeed, "# 需求清单\n- N1: NEED_SECRET 内容\n"},
		{workspace.DocDeal, "# 完工协议\nDEAL_SECRET 内容\n"},
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

// TestExecutor_Name 验证 Name 返回 "executor"。
func TestExecutor_Name(t *testing.T) {
	e := NewExecutor()
	if got := e.Name(); got != "executor" {
		t.Errorf("Name() = %q, want %q", got, "executor")
	}
}

// TestExecutor_Run_Success 验证 Executor 正常流程：读 task/spec、写指令文件，
// 指令含三段（任务指令/Spec 要点/输出路径）且不含 desire/need/deal 内容，
// 事件序列正确。
func TestExecutor_Run_Success(t *testing.T) {
	w := newExecutorTestWorkspace(t, "")
	ai := &mockAI{} // Executor 不调用 AI
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewExecutor()
	if err := e.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	// Executor 不应调用 AI
	if ai.called {
		t.Error("Executor 不应调用 AI")
	}

	// 指令文件应被写入
	raw, err := w.ReadDoc(instructionDocName)
	if err != nil {
		t.Fatalf("读取指令文件失败: %v", err)
	}

	// 含三段标题
	for _, want := range []string{"# 任务指令", "# Spec 要点", "# 输出路径"} {
		if !strings.Contains(raw, want) {
			t.Errorf("指令文件应包含 %q:\n%s", want, raw)
		}
	}
	// 含 task 与 spec 内容
	for _, want := range []string{"T1", "实现入口程序", "ADDED Requirements", "输出 hello"} {
		if !strings.Contains(raw, want) {
			t.Errorf("指令文件应包含 %q", want)
		}
	}
	// 含输出路径说明
	if !strings.Contains(raw, "code/") {
		t.Errorf("指令文件应说明代码输出到 code/ 目录")
	}
	if !strings.Contains(raw, "reports/generator.md") {
		t.Errorf("指令文件应说明 report 输出到 reports/generator.md")
	}

	// 隔离性：不含 desire/need/deal 标记内容
	for _, bad := range []string{"DESIRE_SECRET", "NEED_SECRET", "DEAL_SECRET"} {
		if strings.Contains(raw, bad) {
			t.Errorf("指令文件不应包含上游文档内容 %q（隔离失败）", bad)
		}
	}
	// 不含 desire/need 字样（英文文档名也不应出现）
	for _, bad := range []string{"desire", "need", "deal"} {
		if strings.Contains(strings.ToLower(raw), bad) {
			t.Errorf("指令文件不应出现 %q 字样（隔离失败）", bad)
		}
	}

	// 隔离目录与代码输出目录应被创建
	genDir := w.Path() + "/agents/generator"
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("隔离目录 agents/generator 未创建: %v", err)
	}
	codeDir := w.Path() + "/code"
	if _, err := os.Stat(codeDir); err != nil {
		t.Errorf("代码输出目录 code/ 未创建: %v", err)
	}

	// 事件序列：agent_start -> doc_update -> agent_done
	events := drainEvents(ch)
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
		if events[i].Agent != "executor" {
			t.Errorf("第 %d 个事件 Agent = %q, want %q", i, events[i].Agent, "executor")
		}
	}
	// doc_update 应携带 doc=指令文件路径
	docEvt := events[1]
	data, ok := docEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("doc_update 事件 Data 应为 map[string]any, got %T", docEvt.Data)
	}
	if data["doc"] != instructionDocName {
		t.Errorf("doc_update 事件 doc = %v, want %q", data["doc"], instructionDocName)
	}
}

// TestExecutor_Run_TaskMissing 验证 task.md 缺失时返回错误、不写指令、发 agent_failed。
func TestExecutor_Run_TaskMissing(t *testing.T) {
	w := newExecutorTestWorkspace(t, workspace.DocTask)
	ai := &mockAI{}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewExecutor()
	err := e.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望缺失 task.md 返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "task.md") {
		t.Errorf("错误应提示 task.md 缺失: %v", err)
	}

	// AI 不应被调用
	if ai.called {
		t.Error("task.md 缺失时不应调用 AI")
	}
	// 指令文件不应被写入
	if _, err := w.ReadDoc(instructionDocName); err == nil {
		t.Error("task.md 缺失时不应写入指令文件")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望指令文件不存在错误，got %v", err)
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

// TestExecutor_Run_SpecMissing 验证 spec.md 缺失时返回错误并发布 agent_failed。
func TestExecutor_Run_SpecMissing(t *testing.T) {
	w := newExecutorTestWorkspace(t, workspace.DocSpec)
	ai := &mockAI{}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	e := NewExecutor()
	err := e.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望缺失 spec.md 返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "spec.md") {
		t.Errorf("错误应提示 spec.md 缺失: %v", err)
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足：got %d, want 至少 2", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestExecutor_Run_NilBus 验证 bus 为 nil 时不 panic 且正常写入指令文件。
func TestExecutor_Run_NilBus(t *testing.T) {
	w := newExecutorTestWorkspace(t, "")
	ai := &mockAI{}
	git := &mockGittor{}

	e := NewExecutor()
	if err := e.Run(context.Background(), w, ai, git, nil); err != nil {
		t.Fatalf("bus=nil 时 Run 返回错误: %v", err)
	}
	if _, err := w.ReadDoc(instructionDocName); err != nil {
		t.Errorf("bus=nil 时指令文件仍应被写入: %v", err)
	}
}
