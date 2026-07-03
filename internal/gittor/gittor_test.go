package gittor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setGitIdentity 为测试设置 git 提交身份，避免无 user.email 报错。
// 通过环境变量注入，子进程（git）会继承。
func setGitIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "zzauto-test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@zzauto.local")
	t.Setenv("GIT_COMMITTER_NAME", "zzauto-test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@zzauto.local")
}

// initBareRepo 在 dir 下创建一个本地 bare 仓库，返回其路径。
// 用作 push 目标，避免测试触网。
func initBareRepo(t *testing.T, dir string) string {
	t.Helper()
	bareDir := filepath.Join(dir, "bare.git")
	if err := os.MkdirAll(bareDir, 0o755); err != nil {
		t.Fatalf("创建 bare 目录失败: %v", err)
	}
	cmd := exec.Command("git", "init", "--bare", bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare 失败: %v\n%s", err, out)
	}
	return bareDir
}

// bareLog 返回 bare 仓库某分支的提交日志（--oneline）。
func bareLog(t *testing.T, bareDir, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", bareDir, "log", "--oneline", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("查询 bare 仓库 log 失败: %v\n%s", err, out)
	}
	return string(out)
}

func TestCommitAndPush(t *testing.T) {
	setGitIdentity(t)
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("创建工作目录失败: %v", err)
	}
	bareDir := initBareRepo(t, base)

	g := New(workDir, bareDir, "main", "")
	ctx := context.Background()

	// EnsureRepo 应初始化仓库、配置 origin、切到 main
	if err := g.EnsureRepo(ctx); err != nil {
		t.Fatalf("EnsureRepo 失败: %v", err)
	}

	// 验证 origin 已正确指向 bare 仓库
	cmd := exec.Command("git", "-C", workDir, "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("获取 origin 失败: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != bareDir {
		t.Errorf("origin 指向不正确: got=%q want=%q", strings.TrimSpace(string(out)), bareDir)
	}

	// 写入一个文件并提交
	if err := os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	if err := g.CommitAndPush(ctx, nil, "feat: 添加 hello 文件"); err != nil {
		t.Fatalf("CommitAndPush 失败: %v", err)
	}

	// 验证 commit 已推送到 bare 仓库
	log := bareLog(t, bareDir, "main")
	if !strings.Contains(log, "feat: 添加 hello 文件") {
		t.Errorf("bare 仓库未包含期望的 commit:\n%s", log)
	}

	// 提交后工作区应干净
	st, err := g.Status(ctx)
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if st != "" {
		t.Errorf("期望干净工作区，实际 status=%q", st)
	}
}

func TestCommitAndPushWithPaths(t *testing.T) {
	setGitIdentity(t)
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("创建工作目录失败: %v", err)
	}
	bareDir := initBareRepo(t, base)

	g := New(workDir, bareDir, "main", "")
	ctx := context.Background()
	if err := g.EnsureRepo(ctx); err != nil {
		t.Fatalf("EnsureRepo 失败: %v", err)
	}

	// 写两个文件，只提交 a.txt
	if err := os.WriteFile(filepath.Join(workDir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("写入 a.txt 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("写入 b.txt 失败: %v", err)
	}
	if err := g.CommitAndPush(ctx, []string{"a.txt"}, "feat: 仅添加 a"); err != nil {
		t.Fatalf("CommitAndPush 失败: %v", err)
	}

	// b.txt 应仍未提交（出现在 status 中）
	st, err := g.Status(ctx)
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if !strings.Contains(st, "b.txt") {
		t.Errorf("期望 b.txt 仍未提交，status=%q", st)
	}
	// a.txt 不应出现在 status（已提交）
	if strings.Contains(st, "a.txt") {
		t.Errorf("a.txt 不应出现在 status（已提交）: %q", st)
	}

	// bare 仓库只应有 a.txt
	log := bareLog(t, bareDir, "main")
	if !strings.Contains(log, "feat: 仅添加 a") {
		t.Errorf("bare 仓库未包含期望的 commit:\n%s", log)
	}
}

func TestEnsureRepoInitNonGitDir(t *testing.T) {
	setGitIdentity(t)
	dir := t.TempDir()
	// 确认初始非 git 仓库
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		t.Fatal("测试目录不应已含 .git")
	}

	g := New(dir, "", "main", "")
	ctx := context.Background()
	if err := g.EnsureRepo(ctx); err != nil {
		t.Fatalf("EnsureRepo 失败: %v", err)
	}

	// 现在应是 git 仓库
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("EnsureRepo 后应存在 .git: %v", err)
	}

	// 应已切到 main 分支（首次提交后验证）
	cmd := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("获取当前分支失败: %v\n%s", err, out)
	}
	// 新仓库 HEAD 指向 main（unborn 也算）
	if got := strings.TrimSpace(string(out)); got != "main" {
		t.Errorf("期望当前分支 main，实际 %q", got)
	}
}

func TestCommitAndPushRejectsInvalidMessage(t *testing.T) {
	setGitIdentity(t)
	dir := t.TempDir()
	g := New(dir, "", "main", "")
	ctx := context.Background()
	if err := g.EnsureRepo(ctx); err != nil {
		t.Fatalf("EnsureRepo 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	err := g.CommitAndPush(ctx, nil, "随便写的消息")
	if err == nil {
		t.Fatal("期望非 conventional commit 被拒绝，实际成功")
	}
	if !strings.Contains(err.Error(), "conventional commits") {
		t.Errorf("错误信息应提及 conventional commits，实际: %v", err)
	}

	// 校验失败时不应执行 add/commit：x.txt 应仍处于未暂存状态
	st, err := g.Status(ctx)
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if !strings.Contains(st, "x.txt") {
		t.Errorf("校验失败不应提交，x.txt 应仍在 status 中: %q", st)
	}
}

func TestValidateCommitMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want bool // true=期望通过
	}{
		{"feat: 添加新功能", true},
		{"fix(scope): 修复 bug", true},
		{"feat!: breaking change", true},
		{"docs(scope)!: 文档变更", true},
		{"refactor: 重构", true},
		{"随便写的消息", false},
		{"feat没有冒号空格", false},
		{"unknown: 未知类型", false},
		{"", false},
		{"feat: 添加功能\n正文第二行", true},
		{"feat: ", false}, // 首行描述为空，不合规
	}
	for _, c := range cases {
		err := validateCommitMessage(c.msg)
		if c.want && err != nil {
			t.Errorf("validateCommitMessage(%q) 期望通过，实际失败: %v", c.msg, err)
		}
		if !c.want && err == nil {
			t.Errorf("validateCommitMessage(%q) 期望失败，实际通过", c.msg)
		}
	}
}

func TestWithTokenURL(t *testing.T) {
	g := New("", "https://github.com/owner/repo.git", "main", "ghp_secret")
	got := g.withTokenURL()
	want := "https://x-access-token:ghp_secret@github.com/owner/repo.git"
	if got != want {
		t.Errorf("withTokenURL=%q, want=%q", got, want)
	}

	// 已含凭据时应剥离
	g2 := New("", "https://user:pass@github.com/owner/repo.git", "main", "ghp_new")
	got2 := g2.withTokenURL()
	want2 := "https://x-access-token:ghp_new@github.com/owner/repo.git"
	if got2 != want2 {
		t.Errorf("withTokenURL(含凭据)=%q, want=%q", got2, want2)
	}
}

func TestRedact(t *testing.T) {
	g := New("", "", "", "ghp_secret")
	got := g.redact("push to https://x-access-token:ghp_secret@github.com/x")
	if strings.Contains(got, "ghp_secret") {
		t.Errorf("redact 未脱敏 token: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("redact 应包含 ***: %q", got)
	}

	// 无 token 时原样返回
	g2 := New("", "", "", "")
	in := "nothing to redact"
	if g2.redact(in) != in {
		t.Errorf("无 token 时应原样返回")
	}
}
