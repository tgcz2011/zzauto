package ghcli

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// --- 可注入测试点辅助函数 ---

// setLookPath 临时替换 lookPath，测试结束后自动恢复。
func setLookPath(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	old := lookPath
	lookPath = f
	t.Cleanup(func() { lookPath = old })
}

// setRunCommand 临时替换 runCommand，测试结束后自动恢复。
func setRunCommand(t *testing.T, f func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	old := runCommand
	runCommand = f
	t.Cleanup(func() { runCommand = old })
}

// --- EnsureInstalled 测试 ---

// TestEnsureInstalled_OK 验证 lookPath 找到 gh 时 EnsureInstalled 返回 nil。
func TestEnsureInstalled_OK(t *testing.T) {
	setLookPath(t, func(file string) (string, error) {
		if file == ghBinaryName {
			return "/usr/bin/gh", nil
		}
		return "", errors.New("not found")
	})

	if err := EnsureInstalled(); err != nil {
		t.Fatalf("EnsureInstalled 期望 nil, got=%v", err)
	}
}

// TestEnsureInstalled_Missing 验证 lookPath 找不到 gh 时 EnsureInstalled 返回含平台关键词的错误。
func TestEnsureInstalled_Missing(t *testing.T) {
	setLookPath(t, func(file string) (string, error) {
		return "", errors.New("executable not found in $PATH")
	})

	err := EnsureInstalled()
	if err == nil {
		t.Fatal("期望返回错误")
	}
	// 按当前平台检查关键词
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = "macOS"
	case "linux":
		want = "Linux"
	case "windows":
		want = "Windows"
	default:
		want = "github.com/cli/cli"
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("期望错误含 %q, got=%v", want, err)
	}
}

// --- InstallHint 测试 ---

// TestInstallHint 验证 InstallHint 返回非空且含 "gh"。
func TestInstallHint(t *testing.T) {
	hint := InstallHint()
	if hint == "" {
		t.Fatal("期望非空安装提示")
	}
	if !strings.Contains(hint, "gh") {
		t.Errorf("期望提示含 \"gh\", got=%q", hint)
	}
}

// --- AuthStatus 测试 ---

// TestAuthStatus_LoggedIn 验证 runCommand 返回 ("", nil) 时 AuthStatus 返回 (true, nil)。
func TestAuthStatus_LoggedIn(t *testing.T) {
	setRunCommand(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	})

	ok, err := AuthStatus(context.Background())
	if err != nil {
		t.Fatalf("AuthStatus 期望 nil error, got=%v", err)
	}
	if !ok {
		t.Fatal("期望已登录返回 true")
	}
}

// TestAuthStatus_NotLoggedIn 验证 runCommand 返回 ("not logged in", exit 1) 时 AuthStatus 返回 (false, nil)。
func TestAuthStatus_NotLoggedIn(t *testing.T) {
	setRunCommand(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// 模拟 gh auth status 未登录：输出含 "not logged" 且退出码 1
		cmd := exec.CommandContext(ctx, "sh", "-c", "echo 'You are not logged into any GitHub hosts.'; exit 1")
		return cmd.CombinedOutput()
	})

	ok, err := AuthStatus(context.Background())
	if err != nil {
		t.Fatalf("AuthStatus 期望 nil error, got=%v", err)
	}
	if ok {
		t.Fatal("期望未登录返回 false")
	}
}

// --- Repos 测试 ---

// TestRepos_OK 验证 runCommand 返回合法 JSON 时 Repos 正确解析。
func TestRepos_OK(t *testing.T) {
	jsonOut := `[{"nameWithOwner":"tgcz2011/zzauto","isPrivate":false,"url":"https://github.com/tgcz2011/zzauto","description":"test repo"}]`
	setRunCommand(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(jsonOut), nil
	})

	repos, err := Repos(context.Background())
	if err != nil {
		t.Fatalf("Repos 期望 nil error, got=%v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("期望 1 个仓库, got=%d", len(repos))
	}
	r := repos[0]
	if r.NameWithOwner != "tgcz2011/zzauto" {
		t.Errorf("期望 NameWithOwner=tgcz2011/zzauto, got=%s", r.NameWithOwner)
	}
	if r.IsPrivate {
		t.Errorf("期望 IsPrivate=false, got=true")
	}
	if r.URL != "https://github.com/tgcz2011/zzauto" {
		t.Errorf("期望 URL=https://github.com/tgcz2011/zzauto, got=%s", r.URL)
	}
	if r.Description != "test repo" {
		t.Errorf("期望 Description=test repo, got=%s", r.Description)
	}
}

// TestRepos_NotAuthenticated 验证 runCommand 返回 auth 错误时 Repos 返回 ErrNotAuthenticated。
func TestRepos_NotAuthenticated(t *testing.T) {
	setRunCommand(t, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("You are not logged into any GitHub hosts. Run: gh auth login"), errors.New("exit status 1")
	})

	repos, err := Repos(context.Background())
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("期望 ErrNotAuthenticated, got=%v", err)
	}
	if repos != nil {
		t.Errorf("期望 nil repos, got=%v", repos)
	}
}
