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

// coderInstruction 是测试用的指令文件内容。
const coderInstruction = "# 任务指令\n- [ ] T1: 实现 hello 程序\n\n# Spec 要点\n程序 SHALL 打印 hello\n\n# 输出路径\n- 代码输出到 code/\n"

// coderAIOutput 是模拟 AI 生成代码的输出（v0.6.0 围栏格式 + 自评文本）。
// 围栏起始行 ```go，块内首行 path: code/main.go，其余为内容；围栏外为自评。
const coderAIOutput = "```go\npath: code/main.go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\n自评：已实现 main 函数并打印 hello，符合验收点。"

// newCoderTestWorkspace 创建临时 workspace。当 writeInstr 为 true 时
// 预先写入指令文件 agents/coder/instruction.md。
func newCoderTestWorkspace(t *testing.T, writeInstr bool) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	w := workspace.New(dir, "coder-test")
	if err := w.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}
	if writeInstr {
		// WriteDoc 只 MkdirAll projectDir，需先创建 agents/coder 子目录
		coderDir := filepath.Join(w.AgentsDir(), "coder")
		if err := os.MkdirAll(coderDir, 0o755); err != nil {
			t.Fatalf("创建指令目录失败: %v", err)
		}
		if err := w.WriteDoc(coderInstructionReadName, coderInstruction); err != nil {
			t.Fatalf("写入指令文件失败: %v", err)
		}
	}
	return w
}

func TestCoder_Name(t *testing.T) {
	c := NewCoder()
	if got, want := c.Name(), workspace.StageCoder; got != want {
		t.Errorf("Name()=%q want=%q", got, want)
	}
}

func TestCoder_Run_Success(t *testing.T) {
	w := newCoderTestWorkspace(t, true)
	ai := &mockAI{responses: []string{coderAIOutput}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()
	resolver := &mockResolver{}

	c := NewCoder()
	if err := c.Run(context.Background(), w, ai, git, bus, resolver); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	// AI 应被调用 1 次（v0.6.0 单次调用生成代码 + 自评文本）
	if ai.calls != 1 {
		t.Fatalf("AI 调用次数 = %d, want 1", ai.calls)
	}
	if ai.systems[0] != coderSystemPrompt {
		t.Errorf("system 应为 coderSystemPrompt")
	}
	if ai.users[0] != coderInstruction {
		t.Errorf("user 应为指令文件全文:\ngot=%q\nwant=%q", ai.users[0], coderInstruction)
	}
	if resolver.calls != 1 || resolver.stages[0] != workspace.StageCoder {
		t.Errorf("resolver 调用不符: %v", resolver.stages)
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
	// 内容不应包含 path: 行
	if strings.Contains(codeStr, "path:") {
		t.Errorf("code/main.go 不应含 path: 行:\n%s", codeStr)
	}

	// reports/coder.md 应被写入，frontmatter 与正文正确
	raw, err := w.ReadDoc(workspace.DocCoderReport)
	if err != nil {
		t.Fatalf("读取 reports/coder.md 失败: %v", err)
	}
	meta, body := workspace.ParseDoc(raw)
	if meta.Stage != workspace.StageCoder {
		t.Errorf("frontmatter Stage = %q, want %q", meta.Stage, workspace.StageCoder)
	}
	if meta.Status != workspace.StatusDone {
		t.Errorf("frontmatter Status = %q, want %q", meta.Status, workspace.StatusDone)
	}
	for _, want := range []string{
		"# Coder Report",
		"## 完成内容",
		"code/main.go",
		"## 自评",
		"自评：已实现 main",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("report 正文应含 %q:\n%s", want, body)
		}
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

func TestCoder_Run_InstructionMissing(t *testing.T) {
	w := newCoderTestWorkspace(t, false)
	ai := &mockAI{responses: []string{coderAIOutput}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()
	ch := bus.Subscribe()

	c := NewCoder()
	err := c.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "instruction.md") {
		t.Fatalf("期望指令缺失错误，got %v", err)
	}
	if ai.calls != 0 {
		t.Errorf("指令缺失时不应调用 AI")
	}
	if _, err := os.ReadFile(filepath.Join(w.Path(), "code", "main.go")); err == nil {
		t.Error("不应写代码文件")
	}
	if _, err := w.ReadDoc(workspace.DocCoderReport); err == nil {
		t.Error("不应写 report")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 report 不存在, got %v", err)
	}
	// 应发布 agent_failed
	var hasFailed bool
	for _, e := range drainEvents(ch) {
		if e.Type == eventbus.EventAgentFailed {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Error("期望发布 agent_failed")
	}
}

func TestCoder_Run_AIError(t *testing.T) {
	w := newCoderTestWorkspace(t, true)
	ai := &mockAI{errs: []error{errors.New("AI 不可用")}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	c := NewCoder()
	err := c.Run(context.Background(), w, ai, git, bus, &mockResolver{})
	if err == nil || !strings.Contains(err.Error(), "AI 不可用") {
		t.Fatalf("期望含 AI 错误，got %v", err)
	}
	if _, err := w.ReadDoc(workspace.DocCoderReport); err == nil {
		t.Error("AI 失败时不应写 report")
	}
}

// TestCoder_Run_NoCodeBlocks 验证 AI 未按围栏格式输出时整段写入 code/README.md。
func TestCoder_Run_NoCodeBlocks(t *testing.T) {
	w := newCoderTestWorkspace(t, true)
	plainOut := "这是一段没有代码块的纯文本输出。"
	ai := &mockAI{responses: []string{plainOut}}
	git := &mockGittor{}
	bus := eventbus.New()
	defer bus.Close()

	c := NewCoder()
	if err := c.Run(context.Background(), w, ai, git, bus, &mockResolver{}); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(w.Path(), "code", "README.md"))
	if err != nil {
		t.Fatalf("读取 code/README.md 失败: %v", err)
	}
	if !strings.Contains(string(data), "没有代码块") {
		t.Errorf("code/README.md 应含原始输出:\n%s", string(data))
	}
	// report 应记录 code/README.md
	raw, err := w.ReadDoc(workspace.DocCoderReport)
	if err != nil {
		t.Fatalf("读取 report 失败: %v", err)
	}
	if !strings.Contains(raw, "code/README.md") {
		t.Errorf("report 应记录 code/README.md:\n%s", raw)
	}
}

// TestParseCodeBlocks 验证 v0.6.0 围栏格式解析：```lang\npath: ...\n内容\n```
func TestParseCodeBlocks(t *testing.T) {
	output := "```go\npath: code/main.go\npackage main\n```\n```go\npath: code/util.go\npackage main\n\nfunc Foo() {}\n```\n"
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

// TestParseCodeBlocks_NoPath 验证无 path 行的围栏被跳过。
func TestParseCodeBlocks_NoPath(t *testing.T) {
	output := "```go\npackage main\n```\n这是说明文字\n"
	files := parseCodeBlocks(output)
	if len(files) != 0 {
		t.Fatalf("无 path 行围栏应被跳过，got %d 个文件", len(files))
	}
}
