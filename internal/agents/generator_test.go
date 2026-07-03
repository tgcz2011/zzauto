package agents

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mockGenAI 实现 AIClient，可按调用次序返回不同响应，便于测试 Generator
// 的两次 AI 调用（生成代码 + 自评）。命名独特，避免与同包 mockAI 冲突。
type mockGenAI struct {
	responses []string // 按调用次序返回；超出长度则重复最后一个
	err       error    // 非 nil 时所有调用都返回该错误
	calls     int
	systems   []string
	users     []string
}

func (m *mockGenAI) Ask(ctx context.Context, system, user string) (string, error) {
	m.calls++
	m.systems = append(m.systems, system)
	m.users = append(m.users, user)
	if m.err != nil {
		return "", m.err
	}
	idx := m.calls - 1
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	return m.responses[idx], nil
}

// genInstruction 是测试用的指令文件内容。
const genInstruction = "# 任务指令\n- [ ] T1: 实现 hello 程序\n\n# Spec 要点\n程序 SHALL 运行后打印 hello\n\n# 输出路径\n- 代码输出到 code/\n- report 输出到 reports/generator.md\n"

// genCodeOutput 是模拟 AI 生成代码的输出（含一个代码块）。
// 使用双引号字符串以容纳反引号围栏。
const genCodeOutput = "```go:code/main.go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n"

// genSelfReview 是模拟 AI 自评的回答。
const genSelfReview = "合格。已实现 main 函数并打印 hello，符合验收点。"

// newGeneratorTestWorkspace 创建临时 workspace。当 writeInstr 为 true 时
// 预先写入指令文件 agents/generator/instruction.md。
func newGeneratorTestWorkspace(t *testing.T, writeInstr bool) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "generator-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if writeInstr {
		// WriteDoc 只 MkdirAll projectDir，需先创建 agents/generator 子目录
		genDir := filepath.Join(w.AgentsDir(), "generator")
		if err := os.MkdirAll(genDir, 0o755); err != nil {
			t.Fatalf("创建隔离目录失败: %v", err)
		}
		if err := w.WriteDoc(instructionReadName, genInstruction); err != nil {
			t.Fatalf("写入指令文件失败: %v", err)
		}
	}
	return w
}

// TestGenerator_Name 验证 Name 返回 "generator"。
func TestGenerator_Name(t *testing.T) {
	g := NewGenerator()
	if got := g.Name(); got != "generator" {
		t.Errorf("Name() = %q, want %q", got, "generator")
	}
}

// TestGenerator_Run_Success 验证 Generator 正常流程：读指令、调用 AI 生成代码
// 写入 code/、调用 AI 自评、写 report 到 reports/generator.md，事件序列正确。
func TestGenerator_Run_Success(t *testing.T) {
	w := newGeneratorTestWorkspace(t, true)
	ai := &mockGenAI{responses: []string{genCodeOutput, genSelfReview}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	g := NewGenerator()
	if err := g.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	// AI 应被调用两次：生成代码 + 自评
	if ai.calls != 2 {
		t.Fatalf("AI 应被调用 2 次，got %d", ai.calls)
	}
	// 第一次：system=生成代码提示，user=指令文件全文
	if ai.systems[0] != generatorSystemPrompt {
		t.Errorf("第 1 次 system prompt 不匹配：got 长度 %d, want 长度 %d", len(ai.systems[0]), len(generatorSystemPrompt))
	}
	if ai.users[0] != genInstruction {
		t.Errorf("第 1 次 user 应为指令文件全文：\ngot=%q\nwant=%q", ai.users[0], genInstruction)
	}
	// 第二次：system=自评提示且含 SelfReviewQuestion，user=代码摘要（含文件清单）
	if ai.systems[1] != selfReviewPrompt {
		t.Errorf("第 2 次 system prompt 不匹配：got 长度 %d, want 长度 %d", len(ai.systems[1]), len(selfReviewPrompt))
	}
	if !strings.Contains(ai.systems[1], SelfReviewQuestion) {
		t.Errorf("自评 system prompt 应含 SelfReviewQuestion %q", SelfReviewQuestion)
	}
	if !strings.Contains(ai.users[1], "code/main.go") {
		t.Errorf("自评 user 应含文件清单 code/main.go，got: %s", ai.users[1])
	}

	// code/main.go 应被写入，内容正确
	data, err := os.ReadFile(filepath.Join(w.Path(), "code", "main.go"))
	if err != nil {
		t.Fatalf("读取 code/main.go 失败: %v", err)
	}
	codeStr := string(data)
	if !strings.Contains(codeStr, "package main") {
		t.Errorf("code/main.go 应含 package main:\n%s", codeStr)
	}
	if !strings.Contains(codeStr, `fmt.Println("hello")`) {
		t.Errorf("code/main.go 应含 fmt.Println(\"hello\"):\n%s", codeStr)
	}

	// reports/generator.md 应被写入，frontmatter 与正文正确
	raw, err := w.ReadDoc(reportDocName)
	if err != nil {
		t.Fatalf("读取 report 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageGenerator {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageGenerator)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	for _, want := range []string{
		"# Generator A Report",
		"## 完成内容",
		"- code/main.go",
		"## 自评",
		genSelfReview,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("report 正文应含 %q:\n%s", want, body)
		}
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
		if events[i].Agent != "generator" {
			t.Errorf("第 %d 个事件 Agent = %q, want %q", i, events[i].Agent, "generator")
		}
	}
	docEvt := events[1]
	d, ok := docEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("doc_update 事件 Data 应为 map[string]any, got %T", docEvt.Data)
	}
	if d["doc"] != reportDocName {
		t.Errorf("doc_update 事件 doc = %v, want %q", d["doc"], reportDocName)
	}
}

// TestGenerator_Run_InstructionMissing 验证指令文件缺失时返回错误、不调用 AI、发 agent_failed。
func TestGenerator_Run_InstructionMissing(t *testing.T) {
	w := newGeneratorTestWorkspace(t, false)
	ai := &mockGenAI{responses: []string{"x"}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	g := NewGenerator()
	err := g.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望指令缺失返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "instruction.md") {
		t.Errorf("错误应提示指令文件缺失: %v", err)
	}
	// AI 不应被调用
	if ai.calls != 0 {
		t.Errorf("指令缺失时不应调用 AI，got calls=%d", ai.calls)
	}
	// code/ 与 report 不应被写入
	if _, err := os.ReadFile(filepath.Join(w.Path(), "code", "main.go")); err == nil {
		t.Error("指令缺失时不应写代码文件")
	}
	if _, err := w.ReadDoc(reportDocName); err == nil {
		t.Error("指令缺失时不应写 report")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 report 不存在错误，got %v", err)
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

// TestGenerator_Run_CodeAIError 验证生成代码阶段 AI 失败时返回错误并发布 agent_failed。
func TestGenerator_Run_CodeAIError(t *testing.T) {
	w := newGeneratorTestWorkspace(t, true)
	ai := &mockGenAI{err: errors.New("AI 服务不可用")}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	g := NewGenerator()
	err := g.Run(context.Background(), w, ai, git, bus)
	if err == nil {
		t.Fatal("期望 AI 失败返回错误，但返回 nil")
	}
	if !strings.Contains(err.Error(), "AI 服务不可用") {
		t.Errorf("错误应含原始 AI 错误: %v", err)
	}
	// 失败时不应写 report
	if _, err := w.ReadDoc(reportDocName); err == nil {
		t.Error("AI 失败时不应写 report")
	}

	events := drainEvents(ch)
	if len(events) < 2 {
		t.Fatalf("事件数量不足：got %d, want 至少 2", len(events))
	}
	if events[1].Type != eventbus.EventAgentFailed {
		t.Errorf("第 1 个事件应为 agent_failed, got %q", events[1].Type)
	}
}

// TestGenerator_Run_NoCodeBlocks 验证 AI 未按围栏格式输出时整段写入 code/README.md。
func TestGenerator_Run_NoCodeBlocks(t *testing.T) {
	w := newGeneratorTestWorkspace(t, true)
	plainOut := "这是一段没有代码块的纯文本输出。"
	ai := &mockGenAI{responses: []string{plainOut, "不合格，未生成代码。"}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	g := NewGenerator()
	if err := g.Run(context.Background(), w, ai, git, bus); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}

	// code/README.md 应被写入，内容为整段输出
	data, err := os.ReadFile(filepath.Join(w.Path(), "code", "README.md"))
	if err != nil {
		t.Fatalf("读取 code/README.md 失败: %v", err)
	}
	if !strings.Contains(string(data), "没有代码块") {
		t.Errorf("code/README.md 应含原始输出:\n%s", string(data))
	}

	// report 应记录 code/README.md
	raw, err := w.ReadDoc(reportDocName)
	if err != nil {
		t.Fatalf("读取 report 失败: %v", err)
	}
	if !strings.Contains(raw, "code/README.md") {
		t.Errorf("report 应记录 code/README.md:\n%s", raw)
	}
}

// TestGenerator_Run_NilBus 验证 bus 为 nil 时不 panic 且正常写代码与 report。
func TestGenerator_Run_NilBus(t *testing.T) {
	w := newGeneratorTestWorkspace(t, true)
	ai := &mockGenAI{responses: []string{genCodeOutput, genSelfReview}}
	git := &mockGittor{}

	g := NewGenerator()
	if err := g.Run(context.Background(), w, ai, git, nil); err != nil {
		t.Fatalf("bus=nil 时 Run 返回错误: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(w.Path(), "code", "main.go")); err != nil {
		t.Errorf("bus=nil 时 code/main.go 仍应被写入: %v", err)
	}
	if _, err := w.ReadDoc(reportDocName); err != nil {
		t.Errorf("bus=nil 时 report 仍应被写入: %v", err)
	}
}

// TestParseCodeBlocks 验证代码块解析：识别 ```lang:path 围栏、多文件、内容正确。
func TestParseCodeBlocks(t *testing.T) {
	output := "```go:code/main.go\npackage main\n```\n```go:code/util.go\npackage main\n\nfunc Foo() {}\n```\n"
	files := parseCodeBlocks(output)
	if len(files) != 2 {
		t.Fatalf("应解析出 2 个文件，got %d", len(files))
	}
	if files[0].Path != "code/main.go" {
		t.Errorf("files[0].Path = %q, want %q", files[0].Path, "code/main.go")
	}
	if files[0].Content != "package main" {
		t.Errorf("files[0].Content = %q, want %q", files[0].Content, "package main")
	}
	if files[1].Path != "code/util.go" {
		t.Errorf("files[1].Path = %q, want %q", files[1].Path, "code/util.go")
	}
	if !strings.Contains(files[1].Content, "func Foo() {}") {
		t.Errorf("files[1].Content 应含 func Foo(): %q", files[1].Content)
	}
}

// TestParseCodeBlocks_NoPath 验证无路径信息的围栏被跳过。
func TestParseCodeBlocks_NoPath(t *testing.T) {
	output := "```go\npackage main\n```\n这是说明文字\n"
	files := parseCodeBlocks(output)
	if len(files) != 0 {
		t.Fatalf("无路径围栏应被跳过，got %d 个文件", len(files))
	}
}
