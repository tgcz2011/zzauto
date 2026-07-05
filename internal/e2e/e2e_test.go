// Package e2e 提供 zzauto 端到端集成测试。
//
// 通过 mock AIClient 按调用顺序返回预设响应，配合本地 bare git 仓库，
// 验证 Analyst → Architect → Planner → Coder ↔ Reviewer → commitAndPush
// 全流程能正确产出各阶段文档、代码文件，并将代码提交到 git 仓库。
package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tgcz2011/zzauto/internal/agents"
	"github.com/tgcz2011/zzauto/internal/aicli"
	"github.com/tgcz2011/zzauto/internal/config"
	"github.com/tgcz2011/zzauto/internal/eventbus"
	"github.com/tgcz2011/zzauto/internal/gittor"
	"github.com/tgcz2011/zzauto/internal/registry"
	"github.com/tgcz2011/zzauto/internal/workspace"
)

// mockAI 按调用顺序依次返回预设响应的 AIClient 实现。
//
// 编排器各 agent 串行调用 Ask，mockAI 用递增索引取出对应预设响应，
// 响应耗尽时返回错误以便测试尽早暴露「调用次数不符预期」的问题。
type mockAI struct {
	mu        sync.Mutex
	responses []string
	idx       int
}

// Ask 实现 agents.AIClient 接口，返回预设响应中的下一条。
func (m *mockAI) Ask(_ context.Context, _, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.responses) {
		return "", fmt.Errorf("mockAI 响应已耗尽（第 %d 次调用，超出预设 %d 条）", m.idx+1, len(m.responses))
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

// AskWithModel 与 Ask 相同，model 参数仅作签名兼容（mock 不区分模型）。
func (m *mockAI) AskWithModel(_ context.Context, _, _, _ string) (string, error) {
	return m.Ask(context.Background(), "", "")
}

// RunStream 模拟 SSE 流：构造 system（含 run_id）+ text（content=下一条预设响应）
// + result 三个事件回调 onEvent，返回 runID。
// 与 Ask 共享同一递增索引，保证调用顺序与编排器实际 AI 调用次数一致。
func (m *mockAI) RunStream(_ context.Context, _, _, _ string, onEvent func(aicli.RunEvent) error) (string, error) {
	m.mu.Lock()
	if m.idx >= len(m.responses) {
		m.mu.Unlock()
		return "", fmt.Errorf("mockAI 响应已耗尽（第 %d 次调用，超出预设 %d 条）", m.idx+1, len(m.responses))
	}
	resp := m.responses[m.idx]
	m.idx++
	m.mu.Unlock()

	runID := mockRunID()
	if onEvent != nil {
		// system 事件：声明 runID
		if err := onEvent(aicli.RunEvent{Type: "system", RunID: runID}); err != nil {
			return runID, err
		}
		// text 事件：携带模型回答
		if err := onEvent(aicli.RunEvent{Type: "text", Content: resp, RunID: runID}); err != nil {
			return runID, err
		}
		// result 事件：结束标记
		if err := onEvent(aicli.RunEvent{Type: "result", RunID: runID}); err != nil {
			return runID, err
		}
	}
	return runID, nil
}

// GetRun 返回空 RunDetail，e2e 测试不依赖此方法。
func (m *mockAI) GetRun(_ context.Context, _ string) (*aicli.RunDetail, error) {
	return &aicli.RunDetail{}, nil
}

// mockRunID 生成 mock 用的随机 run id，避免不同调用间冲突。
func mockRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "mock-" + hex.EncodeToString(b)
}

// hasGit 检查系统是否安装了 git 命令。
func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// setupBareRepo 在指定路径创建一个 bare git 仓库，作为推送目标。
func setupBareRepo(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("创建 bare 仓库失败: %v\n%s", err, out)
	}
}

// setLocalGitConfig 为指定仓库设置本地 user.name 与 user.email，
// 避免 git commit 因缺少身份信息而失败。
func setLocalGitConfig(t *testing.T, repoDir string) {
	t.Helper()
	for _, kv := range [][2]string{
		{"user.name", "zzauto-e2e"},
		{"user.email", "e2e@zzauto.test"},
	} {
		if out, err := exec.Command("git", "-C", repoDir, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config %s 失败: %v\n%s", kv[0], err, out)
		}
	}
}

// gitLogOneline 返回 bare 仓库的 git log --oneline 输出。
func gitLogOneline(t *testing.T, gitDir string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", gitDir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log 失败: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// assertDocExists 断言 workspace 中的文档已生成且非空。
func assertDocExists(t *testing.T, ws *workspace.Workspace, name string) {
	t.Helper()
	content, err := ws.ReadDoc(name)
	if err != nil {
		t.Errorf("文档 %s 应存在但读取失败: %v", name, err)
		return
	}
	if strings.TrimSpace(content) == "" {
		t.Errorf("文档 %s 不应为空", name)
	}
}

// TestE2EFullFlow 验证完整编排流程：6 个 agent 依次执行，产出全部文档、
// 代码文件，并将代码提交到本地 bare git 仓库。
//
// mock AI 按调用顺序返回预设响应：
//  1. Analyst → JSON（questions=[]，spec 含 ### [ ] Requirement:）
//  2. Architect → deal.md 正文
//  3. Planner → task.md 正文
//  4. Coder → 代码块（```go\npath: code/main.go\n...）
//  5. Reviewer → JSON（passed=true，评估通过）
//
// 断言：input/spec/deal/task.md 均生成、code/ 下有代码文件、
// reports/coder.md 与 reports/reviewer.md 存在、spec.md 所有 Requirement 已打勾、
// bare git 仓库有 feat 提交记录。
func TestE2EFullFlow(t *testing.T) {
	if !hasGit() {
		t.Skip("系统未安装 git，跳过 e2e 测试")
	}

	// 准备临时目录：workspace 根目录 + bare git 仓库
	tmpDir := t.TempDir()
	workspaceRoot := filepath.Join(tmpDir, "workspace")
	bareDir := filepath.Join(tmpDir, "bare.git")
	setupBareRepo(t, bareDir)

	// 创建工作区并确保目录就绪
	ws := workspace.NewProject(workspaceRoot)
	if err := ws.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs 失败: %v", err)
	}

	// 写入用户原始需求（Listener 读取 input.md）
	if err := ws.WriteDoc("input.md", "实现一个简单的计算器，支持加减乘除。"); err != nil {
		t.Fatalf("写入 input.md 失败: %v", err)
	}

	// 初始化 git 仓库（repoDir 为项目目录，remote 指向本地 bare 仓库）
	gitClient := gittor.New(ws.Path(), bareDir, "main", "")
	ctx := context.Background()
	if err := gitClient.EnsureRepo(ctx); err != nil {
		t.Fatalf("EnsureRepo 失败: %v", err)
	}
	setLocalGitConfig(t, ws.Path())

	// 按调用顺序预设 mock AI 响应
	specJSON := `{"questions":[], "spec": "# 计算器 Spec\n## Why\n用户需要一个计算器\n\n## What Changes\n- 新增四则运算\n\n## Impact\n仅影响计算器模块\n\n## Requirements\n### [ ] Requirement: 四则运算\n该需求 SHALL 支持加减乘除。\n"}`
	dealBody := "# 完工协议\n交付一个支持四则运算的计算器\n\n" +
		"## 批判性分析\nspec 未明确数据持久化方式\n\n" +
		"## 验收标准\n- [ ] D1: 支持 1+1=2\n\n" +
		"## 风险点与缓解\n- 输入校验：使用统一解析\n"
	taskBody := "# Tasks\n- [ ] T1: 实现计算器（验收点：支持 1+1=2）\n"
	codeResponse := "```go\npath: code/main.go\npackage main\n\n" +
		"import \"fmt\"\n\n" +
		"func main() {\n" +
		"\tfmt.Println(\"1+1=\", 2)\n" +
		"}\n```"
	reviewerJSON := `{"passed": true, "issues": [], "suggestions": []}`

	ai := &mockAI{
		responses: []string{
			specJSON,     // 1. Analyst 分析需求产出 spec.md
			dealBody,     // 2. Architect 设计 deal.md
			taskBody,     // 3. Planner 拆解 task.md
			codeResponse, // 4. Coder 生成代码
			reviewerJSON, // 5. Reviewer 评估通过
		},
	}

	// 装配编排器（注入 mock AI 与真实 gittor，指向本地 bare 仓库）
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	cfg := config.Default()
	orch := registry.BuildOrchestratorWithDeps(cfg, ws, bus, ai, gitClient, nil, nil)

	// 执行完整编排流程
	if err := orch.Run(ctx); err != nil {
		t.Fatalf("orchestrator.Run 失败: %v", err)
	}

	// 断言：核心文档均已生成
	assertDocExists(t, ws, workspace.DocInput)
	assertDocExists(t, ws, workspace.DocSpec)
	assertDocExists(t, ws, workspace.DocDeal)
	assertDocExists(t, ws, workspace.DocTask)

	// 断言：code/ 目录下有代码文件
	codeDir := filepath.Join(ws.Path(), "code")
	entries, err := os.ReadDir(codeDir)
	if err != nil {
		t.Fatalf("读取 code/ 目录失败: %v", err)
	}
	if len(entries) == 0 {
		t.Error("code/ 目录应包含代码文件")
	}

	// 断言：reports/coder.md 与 reports/reviewer.md 已生成
	assertDocExists(t, ws, workspace.DocCoderReport)
	assertDocExists(t, ws, workspace.DocReviewReport)

	// 断言：spec.md 中所有 Requirement 已打勾（### [x] Requirement:）
	specContent, err := ws.ReadDoc(workspace.DocSpec)
	if err != nil {
		t.Fatalf("读取 spec.md 失败: %v", err)
	}
	if !strings.Contains(specContent, "### [x] Requirement:") {
		t.Error("spec.md 应包含 ### [x] Requirement:（已打勾的 Requirement）")
	}
	if strings.Contains(specContent, agents.SpecRequirementPrefix) {
		t.Error("spec.md 不应再包含未打勾的 ### [ ] Requirement: ")
	}

	// 断言：bare git 仓库中有 feat 提交记录
	logOut := gitLogOneline(t, bareDir)
	if !strings.Contains(logOut, "feat:") {
		t.Errorf("bare 仓库 git log 应包含 feat 提交，实际: %s", logOut)
	}
}
