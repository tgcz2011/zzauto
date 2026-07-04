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

// withPath 临时将目录追加到 PATH 前面，测试结束后自动恢复。
func withPath(t *testing.T, dirs ...string) {
	t.Helper()
	old := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })
	joined := strings.Join(dirs, string(os.PathListSeparator))
	_ = os.Setenv("PATH", joined+string(os.PathListSeparator)+old)
}

// --- EnsureInstalled 测试 ---

// TestEnsureInstalled_AlreadyHealthy 验证 aiclibridge 已可达时立即返回 nil，不调用安装。
func TestEnsureInstalled_AlreadyHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 注入 fake 安装函数，若被调用则标记失败
	installCalled := false
	setInstallFunc(t, func(ctx context.Context) error {
		installCalled = true
		return nil
	})

	if err := EnsureInstalled(context.Background(), srv.URL, "k"); err != nil {
		t.Fatalf("EnsureInstalled 期望 nil, got=%v", err)
	}
	if installCalled {
		t.Fatal("已健康时不应调用安装函数")
	}
}

// TestEnsureInstalled_InstallThenHealthy 验证首次健康检查失败、安装成功后第二次健康检查通过。
func TestEnsureInstalled_InstallThenHealthy(t *testing.T) {
	// 首次请求返回 503，后续请求返回 200
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

	if err := EnsureInstalled(context.Background(), srv.URL, "k"); err != nil {
		t.Fatalf("EnsureInstalled 期望 nil, got=%v", err)
	}
	if !installCalled {
		t.Fatal("期望调用安装函数")
	}
}

// TestEnsureInstalled_InstallFail 验证安装命令失败时返回包装错误。
func TestEnsureInstalled_InstallFail(t *testing.T) {
	// 服务不可达（已关闭）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	installErr := errors.New("install boom")
	setInstallFunc(t, func(ctx context.Context) error {
		return installErr
	})

	err := EnsureInstalled(context.Background(), srv.URL, "k")
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if !errors.Is(err, installErr) {
		t.Errorf("期望包装安装错误, got=%v", err)
	}
}

// TestEnsureInstalled_HealthTimeout 验证安装成功但健康检查始终失败时超时返回错误。
func TestEnsureInstalled_HealthTimeout(t *testing.T) {
	// 健康检查始终返回 503
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	setInstallFunc(t, func(ctx context.Context) error { return nil })
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
