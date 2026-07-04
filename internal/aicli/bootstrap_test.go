package aicli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- 可注入测试点辅助函数 ---

// setInstallFunc 临时替换 installFunc，测试结束后自动恢复。
func setInstallFunc(t *testing.T, f func(context.Context) error) {
	t.Helper()
	old := installFunc
	installFunc = f
	t.Cleanup(func() { installFunc = old })
}

// setHealthTimeout 临时替换 healthCheckTimeout，测试结束后自动恢复。
func setHealthTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := healthCheckTimeout
	healthCheckTimeout = d
	t.Cleanup(func() { healthCheckTimeout = old })
}

// setLookPath 临时替换 lookPath，测试结束后自动恢复。
func setLookPath(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	old := lookPath
	lookPath = f
	t.Cleanup(func() { lookPath = old })
}

// setStartDaemonFunc 临时替换 startDaemonFunc，测试结束后自动恢复。
func setStartDaemonFunc(t *testing.T, f func(context.Context) error) {
	t.Helper()
	old := startDaemonFunc
	startDaemonFunc = f
	t.Cleanup(func() { startDaemonFunc = old })
}

// withPath 临时将目录追加到 PATH 前面，测试结束后自动恢复。
func withPath(t *testing.T, dirs ...string) {
	t.Helper()
	old := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })
	joined := strings.Join(dirs, string(os.PathListSeparator))
	_ = os.Setenv("PATH", joined+string(os.PathListSeparator)+old)
}

// --- EnsureInstalled 测试 ---

// TestEnsureInstalled_AlreadyHealthy 验证 aiclibridge 已可达时立即返回 nil，
// 不调用 lookPath / install / startDaemon。
func TestEnsureInstalled_AlreadyHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	installCalled := false
	setInstallFunc(t, func(ctx context.Context) error {
		installCalled = true
		return nil
	})
	lookPathCalled := false
	setLookPath(t, func(string) (string, error) {
		lookPathCalled = true
		return "", errors.New("not found")
	})
	startDaemonCalled := false
	setStartDaemonFunc(t, func(ctx context.Context) error {
		startDaemonCalled = true
		return nil
	})

	if err := EnsureInstalled(context.Background(), srv.URL, "k"); err != nil {
		t.Fatalf("EnsureInstalled 期望 nil, got=%v", err)
	}
	if installCalled {
		t.Fatal("已健康时不应调用安装函数")
	}
	if lookPathCalled {
		t.Fatal("已健康时不应调用 lookPath")
	}
	if startDaemonCalled {
		t.Fatal("已健康时不应调用 startDaemon")
	}
}

// TestEnsureInstalled_InstalledButNotRunning 验证 Health 失败、lookPath 命中、
// startDaemon OK、后续 Health 通过 → return nil（不调 installFunc）。
func TestEnsureInstalled_InstalledButNotRunning(t *testing.T) {
	// 首次请求返回 503（失败），后续请求返回 200（成功）
	var healthCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&healthCalls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	installCalled := false
	setInstallFunc(t, func(ctx context.Context) error {
		installCalled = true
		return nil
	})
	setLookPath(t, func(string) (string, error) {
		return "/usr/local/bin/aiclibridge", nil
	})
	startDaemonCalled := false
	setStartDaemonFunc(t, func(ctx context.Context) error {
		startDaemonCalled = true
		return nil
	})

	if err := EnsureInstalled(context.Background(), srv.URL, "k"); err != nil {
		t.Fatalf("EnsureInstalled 期望 nil, got=%v", err)
	}
	if installCalled {
		t.Fatal("已安装时不应调用 installFunc")
	}
	if !startDaemonCalled {
		t.Fatal("期望调用 startDaemonFunc")
	}
}

// TestEnsureInstalled_NotInstalled 验证 Health 失败、lookPath 失败、
// installFunc OK、startDaemon OK、后续 Health 通过 → return nil。
func TestEnsureInstalled_NotInstalled(t *testing.T) {
	// 首次请求返回 503（失败），后续请求返回 200（成功）
	var healthCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&healthCalls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	installCalled := false
	setInstallFunc(t, func(ctx context.Context) error {
		installCalled = true
		return nil
	})
	setLookPath(t, func(string) (string, error) {
		return "", errors.New("not found")
	})
	startDaemonCalled := false
	setStartDaemonFunc(t, func(ctx context.Context) error {
		startDaemonCalled = true
		return nil
	})

	if err := EnsureInstalled(context.Background(), srv.URL, "k"); err != nil {
		t.Fatalf("EnsureInstalled 期望 nil, got=%v", err)
	}
	if !installCalled {
		t.Fatal("期望调用 installFunc")
	}
	if !startDaemonCalled {
		t.Fatal("期望调用 startDaemonFunc")
	}
}

// TestEnsureInstalled_StartDaemonFails 验证 Health 失败、lookPath OK、
// startDaemon 返回 error → 返回 error。
func TestEnsureInstalled_StartDaemonFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	setInstallFunc(t, func(ctx context.Context) error {
		t.Fatal("已安装时不应调用 installFunc")
		return nil
	})
	setLookPath(t, func(string) (string, error) {
		return "/usr/local/bin/aiclibridge", nil
	})
	startDaemonErr := errors.New("start daemon boom")
	setStartDaemonFunc(t, func(ctx context.Context) error {
		return startDaemonErr
	})

	err := EnsureInstalled(context.Background(), srv.URL, "k")
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if !errors.Is(err, startDaemonErr) {
		t.Errorf("期望包装 startDaemon 错误, got=%v", err)
	}
}

// TestEnsureInstalled_HealthTimeout 验证启动 daemon 后健康检查始终失败时超时返回错误。
func TestEnsureInstalled_HealthTimeout(t *testing.T) {
	// 健康检查始终返回 503
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	setInstallFunc(t, func(ctx context.Context) error { return nil })
	setLookPath(t, func(string) (string, error) {
		return "/usr/local/bin/aiclibridge", nil
	})
	setStartDaemonFunc(t, func(ctx context.Context) error { return nil })
	// 缩短超时使测试快速完成
	setHealthTimeout(t, 80*time.Millisecond)

	err := EnsureInstalled(context.Background(), srv.URL, "k")
	if err == nil {
		t.Fatal("期望超时错误")
	}
	if !strings.Contains(err.Error(), "超时") {
		t.Errorf("期望超时错误, got=%v", err)
	}
}

// --- StartDaemon 测试 ---

// TestStartDaemon 验证 StartDaemon 调用 aiclibridge start 子命令。
// 通过将 aicliBinaryName 指向 PATH 中的临时脚本，验证命令被正确调用。
func TestStartDaemon(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "aiclibridge")
	markerPath := filepath.Join(dir, "start-called")
	// 假 aiclibridge 脚本：写入标记文件并退出 0
	script := "#!/bin/sh\necho start-ok > " + markerPath + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("写入假脚本失败: %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("chmod 失败: %v", err)
	}
	withPath(t, dir)

	if err := StartDaemon(context.Background()); err != nil {
		t.Fatalf("StartDaemon 期望 nil, got=%v", err)
	}
	// 验证脚本被调用
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("读取标记文件失败: %v", err)
	}
	if strings.TrimSpace(string(data)) != "start-ok" {
		t.Errorf("标记文件内容不匹配: got=%q want=start-ok", string(data))
	}
}

// --- UpgradeAiclibridge 测试 ---

// TestUpgradeAiclibridge_NotInstalled 验证 aiclibridge 不在 PATH 时返回「未安装」错误。
func TestUpgradeAiclibridge_NotInstalled(t *testing.T) {
	// 注入 lookPath 使其始终找不到
	setLookPath(t, func(file string) (string, error) {
		return "", errors.New("executable not found in $PATH")
	})

	err := UpgradeAiclibridge()
	if err == nil {
		t.Fatal("期望未安装错误")
	}
	if !strings.Contains(err.Error(), "未安装") {
		t.Errorf("期望「未安装」错误, got=%v", err)
	}
}

// TestUpgradeAiclibridge_Success 验证 aiclibridge 在 PATH 中且 upgrade 成功时返回 nil。
func TestUpgradeAiclibridge_Success(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "aiclibridge")
	// 假 aiclibridge 脚本：输出成功标记并退出 0
	script := "#!/bin/sh\necho upgrade-ok\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("写入假脚本失败: %v", err)
	}
	// 确保可执行权限（不受 umask 影响）
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("chmod 失败: %v", err)
	}
	withPath(t, dir)

	if err := UpgradeAiclibridge(); err != nil {
		t.Fatalf("UpgradeAiclibridge 期望 nil, got=%v", err)
	}
}

// TestUpgradeAiclibridge_CommandFail 验证 aiclibridge upgrade 退出非 0 时返回含输出的错误。
func TestUpgradeAiclibridge_CommandFail(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "aiclibridge")
	// 假 aiclibridge 脚本：输出失败标记并退出 1
	script := "#!/bin/sh\necho upgrade-fail\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("写入假脚本失败: %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("chmod 失败: %v", err)
	}
	withPath(t, dir)

	err := UpgradeAiclibridge()
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if !strings.Contains(err.Error(), "upgrade-fail") {
		t.Errorf("期望错误含命令输出, got=%v", err)
	}
}
